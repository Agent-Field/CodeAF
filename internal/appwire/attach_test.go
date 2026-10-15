package appwire

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// sendEngine is an Engine that is a Talker and a Crewer only as far as a send
// with attachments goes; every other verb of either is left unimplemented and
// would panic if a route reached for it.
type sendEngine struct {
	fakeEngine
	Talker
	Crewer
	agent, text string
	ids         []string
	crew        CrewMessage
}

func (s *sendEngine) SendWith(agent, text, clientID string, ids []string) (uint64, error) {
	s.agent, s.text, s.ids = agent, text, ids
	return 9, nil
}

func (s *sendEngine) CrewMessage(ask CrewMessage) (Entry, error) {
	s.crew = ask
	return Entry{Agent: "m1", Kind: EntryUser, Text: ask.Text}, nil
}

func codeOf(err error) string {
	var wire *Error
	if errors.As(err, &wire) {
		return wire.Code
	}
	return ""
}

// A CREATE'S ATTACHMENTS ARE CHECKED WHOLE: a known kind with its own field,
// at most ten, one paste within its bound and all of them within theirs.
func TestAttachSourcesAreCheckedWhole(t *testing.T) {
	big := strings.Repeat("é", MaxPasteChars) // characters, not bytes
	half := strings.Repeat("x", MaxAttachChars/2)
	eleven := make([]AttachSource, MaxAttachments+1)
	for i := range eleven {
		eleven[i] = AttachSource{Kind: AttachLink, URL: "https://example.com"}
	}
	for name, sources := range map[string][]AttachSource{
		"an unknown kind":      {{Kind: "image", Path: "a.png"}},
		"no kind":              {{Path: "a.go"}},
		"a file with no path":  {{Kind: AttachFile, Path: "  "}},
		"a link with no url":   {{Kind: AttachLink}},
		"a paste with no text": {{Kind: AttachPaste, Text: "\n"}},
		"eleven":               eleven,
		"one paste too long":   {{Kind: AttachPaste, Text: big + "x"}},
		"pastes too long":      {{Kind: AttachPaste, Text: half}, {Kind: AttachPaste, Text: half + "x"}},
	} {
		if err := CheckAttachSources(sources); codeOf(err) != CodeBadParams {
			t.Errorf("%s answered %v", name, err)
		}
	}
	ok := []AttachSource{{Kind: AttachPaste, Text: big}, {Kind: AttachFile, Path: "a.go"}, {Kind: AttachLink, URL: "https://example.com"}}
	if err := CheckAttachSources(append(ok, eleven[:MaxAttachments-len(ok)]...)); err != nil {
		t.Fatalf("ten attachments at the bound were refused: %v", err)
	}
	if err := CheckAttachSources(nil); err != nil {
		t.Fatalf("no attachments were refused: %v", err)
	}
}

// A SEND'S IDS ARE NAMED ONCE EACH, at most ten, none empty.
func TestAttachIDsAreNamedOnce(t *testing.T) {
	for name, ids := range map[string][]string{
		"twice":  {"at1", "at2", "at1"},
		"empty":  {"at1", " "},
		"eleven": strings.Fields("a b c d e f g h i j k"),
	} {
		if err := CheckAttachIDs(ids); codeOf(err) != CodeBadParams {
			t.Errorf("%s answered %v", name, err)
		}
	}
	if err := CheckAttachIDs(strings.Fields("a b c d e f g h i j")); err != nil {
		t.Fatal(err)
	}
}

// THE ROUTES: a create with a bad attachment never reaches the engine, a good
// one reaches it whole; attachments alone are a message on agents.send and
// crews.message; an id named twice is refused; and an engine that cannot
// carry attachments refuses rather than sending the words without them.
func TestAttachmentRoutes(t *testing.T) {
	create := methods["agents.create"]
	eng := &sendEngine{}
	if _, err := create.call(eng, json.RawMessage(`{"workspace":"/w","attachments":[{"kind":"image","path":"a.png"}]}`)); codeOf(err) != CodeBadParams {
		t.Fatalf("a bad kind answered %v", err)
	}
	if eng.fakeEngine.created.Workspace != "" {
		t.Fatalf("a refused create reached the engine: %+v", eng.fakeEngine.created)
	}
	if _, err := create.call(eng, json.RawMessage(`{"workspace":"/w","attachments":[{"kind":"paste","text":"log"},{"kind":"file","path":"a.go"}]}`)); err != nil {
		t.Fatal(err)
	}
	if got := eng.fakeEngine.created.Attachments; len(got) != 2 || got[0].Text != "log" || got[1].Path != "a.go" {
		t.Fatalf("the engine was asked %+v", eng.fakeEngine.created)
	}

	send := methods["agents.send"]
	if r, err := send.call(eng, json.RawMessage(`{"id":"a1","attachments":["at1","at2"]}`)); err != nil || r.(*TurnResult).Turn != 9 || eng.text != "" || len(eng.ids) != 2 {
		t.Fatalf("an attachments-only send answered %v %v, the engine heard %+v", r, err, eng)
	}
	if _, err := send.call(eng, json.RawMessage(`{"id":"a1","text":"hi","attachments":["at1","at1"]}`)); codeOf(err) != CodeBadParams {
		t.Fatalf("an id named twice answered %v", err)
	}
	if _, err := send.call(eng, json.RawMessage(`{"id":"a1"}`)); codeOf(err) != CodeBadParams {
		t.Fatalf("an empty send answered %v", err)
	}
	if _, err := send.call(&fakeEngine{}, json.RawMessage(`{"id":"a1","text":"hi","attachments":["at1"]}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatalf("an engine without attachments answered %v", err)
	}

	message := methods["crews.message"]
	if _, err := message.call(eng, json.RawMessage(`{"crew":"c1","attachments":["at3"]}`)); err != nil || len(eng.crew.Attachments) != 1 || eng.crew.Attachments[0] != "at3" {
		t.Fatalf("an attachments-only crew message answered %v, the engine heard %+v", err, eng.crew)
	}
	if _, err := message.call(eng, json.RawMessage(`{"crew":"c1"}`)); codeOf(err) != CodeBadParams {
		t.Fatalf("an empty crew message answered %v", err)
	}
	if _, err := message.call(eng, json.RawMessage(`{"crew":"c1","text":"x","attachments":["at3","at3"]}`)); codeOf(err) != CodeBadParams {
		t.Fatalf("a crew message naming an id twice answered %v", err)
	}
}
