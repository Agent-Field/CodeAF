package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

func TestSealNoticesIdentifiesHostsThatCanWait(t *testing.T) {
	watch := &cellstore.SealWatch{}
	for _, tt := range []struct {
		name  string
		agent *Agent
		ok    bool
		news  bool
	}{
		{name: "nil", ok: true},
		{name: "no seals", agent: &Agent{}, ok: true},
		{name: "task", agent: &Agent{config: Config{InTask: true, Seals: watch}}, ok: true},
		{name: "errand", agent: &Agent{config: Config{Errand: true, Seals: watch}}, ok: true},
		{name: "legacy", agent: &Agent{config: Config{Seals: &queuedSeals{}}}},
		{name: "watch", agent: &Agent{config: Config{Seals: watch}}, ok: true, news: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			news, ok := tt.agent.SealNotices()
			if ok != tt.ok || (news != nil) != tt.news {
				t.Fatalf("SealNotices()=(%v, %v), want channel=%v, ok=%v", news, ok, tt.news, tt.ok)
			}
			if tt.news && news != watch.Changes() {
				t.Fatal("host did not receive the watch's channel")
			}
		})
	}
}

func TestSealNoticesOwedTracksFailuresAndHeldSentences(t *testing.T) {
	watch := &cellstore.SealWatch{}
	a := &Agent{config: Config{Seals: watch}}
	if a.SealNoticesOwed() {
		t.Fatal("healthy watch owes nothing")
	}
	watch.Report(errors.New("disk full"))
	a.TakeSealNotice()
	if !a.SealNoticesOwed() {
		t.Fatal("failure still needs recovery checks after its sentence was taken")
	}
	watch.Report(nil)
	a.TakeSealNotice()
	if a.SealNoticesOwed() {
		t.Fatal("recovered and drained watch owes nothing")
	}
	a.running = true
	watch.Note("file kept out")
	if a.TakeSealNotice() != "" || !a.SealNoticesOwed() {
		t.Fatal("running turn must hold its sentence and keep the host looking")
	}
	a.running = false
	if a.TakeSealNotice() != "file kept out" || a.SealNoticesOwed() {
		t.Fatal("ended turn must release its held sentence")
	}
	watch.Report(errors.New("disk full"))
	for _, refused := range []*Agent{nil, {}, {config: Config{InTask: true, Seals: watch}}, {config: Config{Errand: true, Seals: watch}}} {
		if refused.SealNoticesOwed() {
			t.Fatal("a host that cannot take notices must owe no seal work")
		}
	}
}

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
