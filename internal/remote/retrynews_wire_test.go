package remote

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A RETRY THAT MOVED TO ANOTHER MODEL SAYS SO ON THE FAR SIDE TOO.
//
// [EventWire] embeds the event whole, so this passes for free — and "for free"
// is precisely the kind of claim that stops being true in a refactor nobody
// thought touched this. What is actually being pinned is the json tag: without
// one the payload would ride under a key an older peer chokes on, and with one a
// peer built before it existed simply does not see it.
func TestARetryThatMovedModelsCrossesTheWire(t *testing.T) {
	sent := session.Event{
		Kind: session.EventRetrying,
		Text: "the model kept turning the request away — finishing this one on z-ai/glm-5.3-flash",
		Retry: &session.RetryNews{
			Model:    "deepseek/deepseek-v4.1-flash",
			Attempt:  4,
			Attempts: 4,
			Reason:   "the model would not take the request",
			Next:     "z-ai/glm-5.3-flash",
		},
	}
	payload, err := json.Marshal(WireEvent(sent))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire EventWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := wire.Unwire()
	if got.Retry == nil {
		t.Fatalf("the retry's news did not survive the link; text was %q", got.Text)
	}
	if *got.Retry != *sent.Retry {
		t.Fatalf("the news arrived as %+v, want %+v", *got.Retry, *sent.Retry)
	}
	if got.Text != sent.Text {
		t.Fatalf("the sentence did not survive the link: %q", got.Text)
	}
}

// AND AN ORDINARY RETRY FROM A PEER BUILT BEFORE THE FIELD EXISTED ARRIVES WITH
// none of it — which is the same thing this build's surface already handles,
// because [session.Event.Text] is still the whole line and always has been.
func TestARetryFromAnOlderPeerArrivesWithNoNews(t *testing.T) {
	current, err := json.Marshal(WireEvent(session.Event{
		Kind:  session.EventRetrying,
		Text:  "the request failed — asking again",
		Retry: &session.RetryNews{Model: "test/model", Attempt: 1, Attempts: 4},
	}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(current, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, carried := fields["Retry"]; !carried {
		t.Fatal("the news rides under some other key than Retry")
	}
	delete(fields, "Retry")
	legacy, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire EventWire
	if err := json.Unmarshal(legacy, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := wire.Unwire()
	if got.Retry != nil {
		t.Fatalf("an older peer's retry arrived carrying %+v", *got.Retry)
	}
	if got.Text != "the request failed — asking again" {
		t.Fatalf("the sentence did not survive the link: %q", got.Text)
	}
}
