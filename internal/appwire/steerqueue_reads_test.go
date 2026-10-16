package appwire

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

// talkEngine is a Talker whose plain steer, queue and edit are recorded.
type talkEngine struct {
	sendEngine
	plain []string
}

func (t *talkEngine) Steer(agent, text, clientID string) error {
	t.plain = append(t.plain, "steer:"+text)
	return nil
}

func (t *talkEngine) Queue(agent, text, clientID string) ([]Queued, error) {
	t.plain = append(t.plain, "queue:"+text)
	return []Queued{{ID: "q1", Text: text}}, nil
}

func (t *talkEngine) EditQueued(agent, id, text string) ([]Queued, error) {
	t.plain = append(t.plain, "edit:"+id+":"+text)
	return []Queued{{ID: id, Text: text}}, nil
}

// markedTalkEngine has the doors for a correction's and a queued message's
// said and reads.
type markedTalkEngine struct {
	talkEngine
	steered, queued []AgentMessage
	edited          map[string]AgentMessage
}

func (m *markedTalkEngine) SteerMessage(agent string, message AgentMessage) error {
	m.steered = append(m.steered, message)
	return nil
}

func (m *markedTalkEngine) QueueMessage(agent string, message AgentMessage) ([]Queued, error) {
	m.queued = append(m.queued, message)
	return []Queued{{ID: "q1", Text: *message.Said, Reads: message.Reads}}, nil
}

func (m *markedTalkEngine) EditQueuedMessage(agent, id string, message AgentMessage) ([]Queued, error) {
	if m.edited == nil {
		m.edited = map[string]AgentMessage{}
	}
	m.edited[id] = message
	return []Queued{{ID: id, Text: "edited", Reads: message.Reads}}, nil
}

const oneRead = `[{"source":{"kind":"collection","id":"c1","name":"Specs"},"count":3,"missing":1,"unavailable":0,"unread":0,"more":false,"complete":false,` +
	`"items":[{"kind":"file","path":"docs/a.md"}]}]`

// A CORRECTION, A QUEUED MESSAGE AND A QUEUED EDIT CARRY SAID AND READS TO THE
// ENGINE'S OWN DOORS WHOLE, and plain ones go by the roads they always went by:
// a plain edit included, which replaces the old mark rather than keeping it.
func TestASteerQueueAndEditCarrySaidAndReadsToTheirDoors(t *testing.T) {
	eng := &markedTalkEngine{}
	call := func(name, params string) (any, error) {
		return methods[name].call(eng, json.RawMessage(params))
	}
	reads := []MessageRead{{Source: MessageReference{Kind: "collection", ID: "c1", Name: "Specs"}, Count: 3, Missing: 1,
		Items: []MessageReference{{Kind: "file", Path: "docs/a.md"}}}}

	if _, err := call("agents.steer", `{"id":"a1","text":"use\n\n- docs/a.md","said":"use","client_id":"c1","reads":`+oneRead+`}`); err != nil {
		t.Fatal(err)
	}
	said := "use"
	if want := (AgentMessage{Text: "use\n\n- docs/a.md", Said: &said, ClientID: "c1", Reads: reads}); len(eng.steered) != 1 || !reflect.DeepEqual(eng.steered[0], want) {
		t.Fatalf("the steer door heard %+v", eng.steered)
	}
	r, err := call("agents.queue", `{"id":"a1","text":"later\n\n- docs/a.md","said":"later","client_id":"c2","reads":`+oneRead+`}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.(*queueResult).Queued; len(got) != 1 || got[0].Text != "later" || !reflect.DeepEqual(got[0].Reads, reads) {
		t.Fatalf("the queue answered %+v", got)
	}
	if len(eng.queued) != 1 || eng.queued[0].Text != "later\n\n- docs/a.md" || eng.queued[0].ClientID != "c2" {
		t.Fatalf("the queue door heard %+v", eng.queued)
	}
	if _, err := call("queue.edit", `{"agent":"a1","id":"q1","text":"body","said":"","reads":`+oneRead+`}`); err != nil {
		t.Fatal(err)
	}
	if got := eng.edited["q1"]; got.Text != "body" || got.Said == nil || *got.Said != "" || !reflect.DeepEqual(got.Reads, reads) {
		t.Fatalf("the edit door heard %+v", got)
	}

	for _, plain := range [][2]string{
		{"agents.steer", `{"id":"a1","text":"plain"}`},
		{"agents.queue", `{"id":"a1","text":"plain"}`},
		{"queue.edit", `{"agent":"a1","id":"q1","text":"plain"}`},
	} {
		if _, err := call(plain[0], plain[1]); err != nil {
			t.Fatalf("%s: %v", plain[0], err)
		}
	}
	if want := []string{"steer:plain", "queue:plain", "edit:q1:plain"}; !reflect.DeepEqual(eng.plain, want) {
		t.Fatalf("the plain roads heard %q", eng.plain)
	}
	if len(eng.steered) != 1 || len(eng.queued) != 1 || len(eng.edited) != 1 {
		t.Fatal("a plain call went by a marked door")
	}
}

// ATTACHMENTS ON A CORRECTION OR A QUEUED MESSAGE ARE REFUSED, never dropped,
// plain or marked; and said or reads to an engine without the door are
// refused as unknown before anything is steered, queued or edited.
func TestASteerOrQueueRefusesAttachmentsAndAnAbsentDoor(t *testing.T) {
	for _, eng := range []Engine{&talkEngine{}, &markedTalkEngine{}} {
		for _, c := range [][2]string{
			{"agents.steer", `{"id":"a1","text":"x","attachments":["at1"]}`},
			{"agents.queue", `{"id":"a1","text":"x","said":"x","attachments":["at1"]}`},
			{"queue.edit", `{"agent":"a1","id":"q1","text":"x","attachments":["at1"]}`},
		} {
			if _, err := methods[c[0]].call(eng, json.RawMessage(c[1])); codeOf(err) != CodeBadParams {
				t.Fatalf("%T %s with attachments answered %v", eng, c[0], err)
			}
		}
	}
	plain := &talkEngine{}
	for _, c := range [][2]string{
		{"agents.steer", `{"id":"a1","text":"x","said":"x"}`},
		{"agents.queue", `{"id":"a1","text":"x","reads":` + oneRead + `}`},
		{"queue.edit", `{"agent":"a1","id":"q1","text":"x","said":""}`},
	} {
		if _, err := methods[c[0]].call(plain, json.RawMessage(c[1])); codeOf(err) != CodeUnknownMethod {
			t.Fatalf("%s to an engine without the door answered %v", c[0], err)
		}
	}
	if len(plain.plain) != 0 {
		t.Fatalf("a refused call reached the engine: %q", plain.plain)
	}
}

// HELLO SAYS EACH DOOR ON ITS OWN: a send door says nothing of a queue's or a
// correction's, and a queued message's text and reads are said on the wire.
func TestHelloSaysEachMessageDoorOnItsOwn(t *testing.T) {
	sendOnly := withMessageReadsFeature(Hello{}, &messageEngine{}).Features
	if !slices.Contains(sendOnly, FeatureMessageReads) || slices.Contains(sendOnly, FeatureSteerReads) || slices.Contains(sendOnly, FeatureQueueReads) {
		t.Fatalf("a send-only engine said %q", sendOnly)
	}
	both := withMessageReadsFeature(Hello{}, &markedTalkEngine{}).Features
	if slices.Contains(both, FeatureMessageReads) || !slices.Contains(both, FeatureSteerReads) || !slices.Contains(both, FeatureQueueReads) {
		t.Fatalf("a steer and queue engine said %q", both)
	}
	out, _ := json.Marshal(Queued{ID: "q1", Text: "mine", Reads: []MessageRead{{Source: MessageReference{Kind: "collection", ID: "c1"}}}})
	if string(out) != `{"id":"q1","text":"mine","reads":[{"source":{"kind":"collection","id":"c1"},"count":0,"missing":0,"unavailable":0,"unread":0,"more":false,"complete":false}]}` {
		t.Fatalf("a queued message is %s", out)
	}
	if plain, _ := json.Marshal(Queued{ID: "q1", Text: "mine"}); string(plain) != `{"id":"q1","text":"mine"}` {
		t.Fatalf("a plain queued message is %s", plain)
	}
}
