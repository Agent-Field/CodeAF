package appwire

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// messageEngine is a send engine that is also a MessageSender.
type messageEngine struct {
	sendEngine
	sentAgent string
	message   AgentMessage
}

func (m *messageEngine) SendMessage(agent string, message AgentMessage) (uint64, error) {
	m.sentAgent, m.message = agent, message
	return 11, nil
}

// A SEND THAT CARRIES SAID OR READS REACHES THE ENGINE'S DOOR FOR THEM WHOLE,
// or is refused: the words, the person's own words (an explicit empty one
// included), the staged ids, the client id and every count of every read. A
// send with neither goes by the road it always went by, and an engine with no
// door for them refuses rather than running the words without them.
func TestASendWithSaidOrReadsReachesTheMessageDoorWhole(t *testing.T) {
	send := methods["agents.send"]
	eng := &messageEngine{}
	params := `{"id":"a1","text":"compare\n\n- docs/a.md","said":"compare","client_id":"c9","attachments":["at1"],"reads":[` +
		`{"source":{"kind":"collection","id":"c1","name":"Specs"},"count":200,"missing":1,"unavailable":0,"unread":0,"more":false,"complete":true,` +
		`"warnings":["one is gone"],"items":[{"kind":"file","id":"r1","name":"a.md","owner":"fs","path":"docs/a.md","url":"https://x/a"}]}]}`
	r, err := send.call(eng, json.RawMessage(params))
	if err != nil || r.(*TurnResult).Turn != 11 {
		t.Fatalf("the send answered %v %v", r, err)
	}
	said := "compare"
	want := AgentMessage{Text: "compare\n\n- docs/a.md", Said: &said, ClientID: "c9", Attachments: []string{"at1"}, Reads: []MessageRead{{
		Source: MessageReference{Kind: "collection", ID: "c1", Name: "Specs"}, Count: 200, Missing: 1, Complete: true,
		Warnings: []string{"one is gone"},
		Items:    []MessageReference{{Kind: "file", ID: "r1", Name: "a.md", Owner: "fs", Path: "docs/a.md", URL: "https://x/a"}},
	}}}
	if eng.sentAgent != "a1" || !reflect.DeepEqual(eng.message, want) {
		t.Fatalf("the engine heard %q %+v", eng.sentAgent, eng.message)
	}
	if _, err := send.call(eng, json.RawMessage(`{"id":"a1","text":"x","said":""}`)); err != nil || eng.message.Said == nil || *eng.message.Said != "" {
		t.Fatalf("an explicit empty said answered %v, the engine heard %+v", err, eng.message)
	}
	eng.message = AgentMessage{}
	if _, err := send.call(eng, json.RawMessage(`{"id":"a1","text":"plain"}`)); err != nil || eng.message.Text != "" {
		t.Fatalf("a plain send went by the message door: %v %+v", err, eng.message)
	}
	if _, err := send.call(eng, json.RawMessage(`{"id":"a1","text":"x","said":"x","attachments":["at1","at1"]}`)); codeOf(err) != CodeBadParams {
		t.Fatalf("an id named twice answered %v", err)
	}
	if _, err := send.call(&sendEngine{}, json.RawMessage(`{"id":"a1","text":"x","reads":[{"source":{"kind":"collection","id":"c1"},"count":1}]}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatalf("an engine without the door answered %v", err)
	}

	message := methods["crews.message"]
	if _, err := message.call(eng, json.RawMessage(`{"crew":"c1","text":"x","said":"","reads":[{"source":{"kind":"collection","id":"c1"},"count":1}]}`)); err != nil ||
		eng.crew.Said == nil || *eng.crew.Said != "" || len(eng.crew.Reads) != 1 || eng.crew.Reads[0].Count != 1 {
		t.Fatalf("a crew message answered %v, the engine heard %+v", err, eng.crew)
	}
}

// A READ'S COUNTS AND FLAGS ARE ALWAYS SAID ON THE WIRE, zero and false
// included, and an entry with no reads says nothing of them.
func TestAReadSaysEveryCountAndAnEntryWithoutReadsSaysNone(t *testing.T) {
	out, err := json.Marshal(Entry{ID: "u0", Kind: EntryUser, Reads: []MessageRead{{Source: MessageReference{Kind: "collection", ID: "c1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"count":0`, `"missing":0`, `"unavailable":0`, `"unread":0`, `"more":false`, `"complete":false`, `"source":{"kind":"collection","id":"c1"}`} {
		if !strings.Contains(string(out), key) {
			t.Fatalf("%s is not in %s", key, out)
		}
	}
	plain, _ := json.Marshal(Entry{ID: "u0", Kind: EntryUser})
	if strings.Contains(string(plain), "reads") {
		t.Fatalf("an entry without reads says %s", plain)
	}
}

// HELLO SAYS THE ENGINE KEEPS READS exactly when it is a MessageSender.
func TestHelloNamesMessageReadsOnlyForAnEngineThatKeepsThem(t *testing.T) {
	has := func(hello Hello) bool {
		for _, f := range hello.Features {
			if f == FeatureMessageReads {
				return true
			}
		}
		return false
	}
	if !has(withMessageReadsFeature(Hello{}, &messageEngine{})) || has(withMessageReadsFeature(Hello{}, &sendEngine{})) {
		t.Fatal("hello names messages.reads for the wrong engine")
	}
	twice := withMessageReadsFeature(Hello{Features: []string{FeatureMessageReads}}, &messageEngine{})
	if len(twice.Features) != 1 {
		t.Fatalf("hello names it %d times", len(twice.Features))
	}
}
