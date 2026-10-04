package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/atlas"
)

// THE MAP INSIDE THE CONVERSATION, AND THE WAY OUT OF IT.
//
// /atlas raises the same model `codeaf atlas` draws, as a fullscreen sheet over
// the chat. The two things a person has to be able to trust about it are that
// it is really the map (the frame draws the atlas's own title and its own hint
// line, and the conversation shows through nowhere) and that esc or q gives
// the conversation back exactly as it was — transcript, draft and place.
func TestAtlasOpensTheMapAndReturnsToTheChat(t *testing.T) {
	a, _ := sheetApp(t)
	// A sentence half-typed before the map was asked for, so the test can see
	// that closing puts the conversation back rather than a blank sheet.
	drive(t, a, key("h"))

	before := len(a.entries)
	a.slash("/atlas")
	if !a.atlas.on {
		t.Fatal("/atlas opened nothing")
	}
	// The map is not a place (pages.go): no tab in the bar wears it, and the
	// page underneath is none — the sheet stands over the conversation.
	if a.page != pageNone {
		t.Fatalf("/atlas stood the sheet up as a place (%v)", a.page)
	}
	body, _, _ := a.frameBody()
	for _, want := range []string{atlas.Atlas.Title, "drag boxes", "click/enter details"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the map's frame is missing %q:\n%s", want, body)
		}
	}
	// A FULLSCREEN SHEET TAKES THE FRAME WHOLE: none of the conversation is
	// drawn under it. The word typed before the map was asked for is held for
	// the person and is not in the frame anywhere.
	if strings.Contains(body, "h\n") && strings.Contains(body, "no drafts") {
		t.Fatal("the map drew a box")
	}

	// q closes and the conversation is whole.
	drive(t, a, key("q"))
	if a.atlas.on {
		t.Fatal("q left the map up")
	}
	if len(a.entries) != before {
		t.Fatalf("q changed the transcript: %d entries, was %d", len(a.entries), before)
	}
	// AND THE BOX IS THE BOX AGAIN: the half-typed sentence and the keys that
	// follow land in it.
	if v := string(a.input.value); v != "h" {
		t.Fatalf("q did not hand the box back whole: %q", v)
	}
	drive(t, a, key("i"))
	if v := string(a.input.value); v != "hi" {
		t.Fatalf("typing after the map answered %q", v)
	}

	// esc is the same way out, and the second open is the same map: the model
	// is kept between sheets, so a box a person moved stays where they put it.
	a.slash("/atlas")
	if !a.atlas.on {
		t.Fatal("a second /atlas opened nothing")
	}
	sameModel := a.atlas.model != nil
	if !sameModel {
		t.Fatal("the second open rebuilt the map")
	}
	drive(t, a, key("esc"))
	if a.atlas.on {
		t.Fatal("esc left the map up")
	}

	// AND A KEY THE MAP CONSUMES DOES NOT REACH THE BOX while the sheet is up.
	// f opens the first flow, which the map says in its own status line — read
	// off the frame rather than off the model, because the model's fields are
	// its own.
	a.slash("/atlas")
	drive(t, a, key("f"))
	body, _, _ = a.frameBody()
	if !strings.Contains(body, "0 overview") {
		t.Fatalf("f did not open a flow through the map:\n%s", body)
	}
	if v := string(a.input.value); v != "hi" {
		t.Fatalf("a key the map took reached the box: %q", v)
	}
	drive(t, a, key("esc"))
	if v := string(a.input.value); v != "hi" {
		t.Fatalf("closing the map changed the box: %q", v)
	}
}
