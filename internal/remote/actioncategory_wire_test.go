package remote

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A CAPTION'S FAMILY CROSSES THE LINK WITH ITS SENTENCE.
//
// The mark the far surface draws beside a step comes off
// [session.Event.Category] (the engine's actioncategory.go). A wire that carried
// the words and dropped the family would leave `--host` drawing the generic
// bucket for every step while the same work on the same machine drew the right
// one — the same screen, two answers, and nothing to say which.
//
// [EventWire] embeds the event whole, so this is a claim about the tag rather
// than about a copy: it passes for free and it is asserted anyway, because "for
// free" is precisely the kind of claim that stops being true in a refactor
// nobody thought touched this.
func TestACaptionsFamilyCrossesTheWire(t *testing.T) {
	sent := session.Event{
		Kind:     session.EventCaption,
		Text:     "starting the local server",
		Category: session.ActionRun,
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
	if got.Category != session.ActionRun {
		t.Fatalf("the family did not survive the link: got %q, want %q", got.Category, session.ActionRun)
	}
	if got.Text != sent.Text {
		t.Fatalf("the sentence did not survive the link: %q", got.Text)
	}
}

// AND A PEER BUILT BEFORE THE FIELD EXISTED IS UNDERSTOOD EXACTLY AS IT ALWAYS
// WAS. Its frames carry a caption and no family; the field arrives empty, which
// is the surface's cue to derive the mark from the batch's own tool names rather
// than to draw nothing.
//
// The old frame is MADE by deleting the key from a current one rather than typed
// out as a literal, because a literal would have to spell EventCaption's ordinal
// — and the day a kind is inserted ahead of it, that literal would go on passing
// while testing a different event entirely.
func TestACaptionFromAnOlderPeerArrivesWithNoFamily(t *testing.T) {
	current, err := json.Marshal(WireEvent(session.Event{
		Kind:     session.EventCaption,
		Text:     "listing open github issues",
		Category: session.ActionSearch,
	}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(current, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, carried := fields["Category"]; !carried {
		t.Fatal("the current frame carries no family, so there is nothing to remove")
	}
	delete(fields, "Category")
	legacy, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("remarshal: %v", err)
	}

	var wire EventWire
	if err := json.Unmarshal(legacy, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := wire.Unwire()
	if got.Category != "" {
		t.Fatalf("an old frame invented the family %q", got.Category)
	}
	if got.Text != "listing open github issues" {
		t.Fatalf("an old frame's caption did not survive: %q", got.Text)
	}
}

// A CAPTION WITH NO FAMILY SPENDS NO BYTES ON ONE. The narrator names a family
// on most lines and not on all of them, and every stream frame is paid for on
// somebody's link — the `omitempty` is what keeps a field that is often empty
// from being a field that is always sent.
func TestAFamilylessCaptionWritesNoFamilyKey(t *testing.T) {
	payload, err := json.Marshal(WireEvent(session.Event{
		Kind: session.EventCaption,
		Text: "ranking bugs by end-result quality",
	}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(payload), "Category") {
		t.Fatalf("an empty family still rode the wire: %s", payload)
	}
}
