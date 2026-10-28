package session

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	keyscan "github.com/Agent-Field/codeaf/internal/keys"
)

// queuedSeals is a seal watch holding sentences, as the seat's watch does.
type queuedSeals struct{ said []string }

func (q *queuedSeals) Failing() bool { return false }

func (q *queuedSeals) Take() string {
	if len(q.said) == 0 {
		return ""
	}
	first := q.said[0]
	q.said = q.said[1:]
	return first
}

func TestCompletedTurnCarriesWhatTheSealWatchHasToSay(t *testing.T) {
	seals := &queuedSeals{said: []string{
		"zero.txt cannot be read here, so it is left out of the saved history until its permissions allow it",
		"sealing works again, and the calls made meanwhile are sealed now",
	}}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}, func(config *Config) { config.Seals = seals })

	events := collect(t, mustSubmit(t, agent, "finish this"))

	var told []string
	for _, event := range events {
		if event.Kind == EventNotice {
			if !event.Told {
				t.Fatalf("a seal sentence is addressed to the person: %+v", event)
			}
			told = append(told, event.Text)
		}
	}
	if len(told) != 2 || told[0] != "zero.txt cannot be read here, so it is left out of the saved history until its permissions allow it" {
		t.Fatalf("the turn did not carry the watch's sentences, in order: %q", told)
	}
	if len(seals.said) != 0 {
		t.Fatalf("a sentence told is still held: %q", seals.said)
	}
}

func TestATurnWithNothingToSayCarriesNoSealNotice(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}, func(config *Config) { config.Seals = &queuedSeals{} })

	for _, event := range collect(t, mustSubmit(t, agent, "finish this")) {
		if event.Kind == EventNotice {
			t.Fatalf("a quiet watch produced a notice: %q", event.Text)
		}
	}
}

// filedSeals is a seal watch holding notices whole, as cellstore's SealWatch
// does: each sentence with, for a grouped one, its lead and files.
type filedSeals struct {
	queuedSeals
	leads []string
	files [][]keyscan.Finding
}

func (f *filedSeals) TakeFiled() (string, string, []keyscan.Finding) {
	said := f.Take()
	if said == "" {
		return "", "", nil
	}
	lead, files := f.leads[0], f.files[0]
	f.leads, f.files = f.leads[1:], f.files[1:]
	return said, lead, files
}

// A grouped notice reaches the turn's stream as its words AND as facts: typed
// kept_out, with the lead and every file. A one-file sentence stays a plain told
// line, exactly as before.
func TestAGroupedSealNoticeCarriesItsFilesAsFacts(t *testing.T) {
	grouped := "2 files look like they hold secrets, so they are kept out of the saved history; they travel through the key vault only to paired computers"
	seals := &filedSeals{
		queuedSeals: queuedSeals{said: []string{grouped + "\n· a_test.go (api-key-sk)\n· .env (dotenv-file)", "zero.txt cannot be read here, so it is left out of the saved history until its permissions allow it"}},
		leads:       []string{grouped, ""},
		files:       [][]keyscan.Finding{{{Path: "a_test.go", Rule: "api-key-sk"}, {Path: ".env", Rule: keyscan.RuleDotenv}}, nil},
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}, func(config *Config) { config.Seals = seals })

	var notices []Event
	for _, event := range collect(t, mustSubmit(t, agent, "finish this")) {
		if event.Kind == EventNotice {
			notices = append(notices, event)
		}
	}
	if len(notices) != 2 {
		t.Fatalf("notices %+v", notices)
	}
	first := notices[0]
	if !first.Told || first.NoticeKind != NoticeKindKeptOut || first.KeptOut == nil || first.KeptOut.Lead != grouped || !strings.HasPrefix(first.Text, grouped+"\n· a_test.go") {
		t.Fatalf("grouped notice %+v", first)
	}
	if want := []KeptOutFile{{Path: "a_test.go", Rule: "api-key-sk"}, {Path: ".env", Rule: keyscan.RuleDotenv}}; !reflect.DeepEqual(first.KeptOut.Files, want) {
		t.Fatalf("files %+v, want %+v", first.KeptOut.Files, want)
	}
	if second := notices[1]; second.NoticeKind != "" || second.KeptOut != nil || !second.Told {
		t.Fatalf("a one-file sentence gained facts: %+v", second)
	}
}

// The facts are additive on the wire: an event without them serialises with no
// trace of the field, and one with them comes back whole.
func TestKeptOutIsAdditiveOnTheWire(t *testing.T) {
	plain, err := json.Marshal(Event{Kind: EventNotice, Text: "x", Told: true})
	if err != nil || strings.Contains(string(plain), "KeptOut") {
		t.Fatalf("a plain notice carries the field: %s, %v", plain, err)
	}
	sent := Event{Kind: EventNotice, Text: "x", NoticeKind: NoticeKindKeptOut, KeptOut: &KeptOut{Lead: "x", Files: []KeptOutFile{{Path: "a", Rule: "r"}}}}
	raw, err := json.Marshal(sent)
	if err != nil {
		t.Fatal(err)
	}
	var back Event
	if err := json.Unmarshal(raw, &back); err != nil || !reflect.DeepEqual(back.KeptOut, sent.KeptOut) || back.NoticeKind != NoticeKindKeptOut {
		t.Fatalf("round trip %s gave %+v, %v", raw, back.KeptOut, err)
	}
}
