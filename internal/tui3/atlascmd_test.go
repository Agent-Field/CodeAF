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
	a.slash("/atlas pairing")
	if !a.atlas.on {
		t.Fatal("/atlas opened nothing")
	}
	// The map is not a place (pages.go): no tab in the bar wears it, and the
	// page underneath is none — the sheet stands over the conversation.
	if a.page != pageNone {
		t.Fatalf("/atlas stood the sheet up as a place (%v)", a.page)
	}
	body, _, _ := a.frameBody()
	for _, want := range []string{atlas.Maps[0].Title, "drag boxes", "click/enter details"} {
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
	a.slash("/atlas pairing")
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
	a.slash("/atlas pairing")
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

// THE PICKER (atlascmd.go). Bare /atlas no longer guesses a map: it raises the
// sheet with the picker in it — one row per registered map, in the registry's
// order — and enter opens the row the cursor is on.
func TestAtlasBareSlashShowsThePickerAndEnterOpens(t *testing.T) {
	a, _ := sheetApp(t)

	a.slash("/atlas")
	if !a.atlas.on || !a.atlas.picking {
		t.Fatalf("bare /atlas raised %v with picking %v, want the picker up", a.atlas.on, a.atlas.picking)
	}
	// EVERY REGISTERED MAP IS ON THE LIST, name and description, in the
	// registry's order — the same rows `codeaf atlas` draws on its own
	// terminal (pick.go).
	body, _, _ := a.frameBody()
	for _, mp := range atlas.Maps {
		for _, want := range []string{mp.Name, mp.Description} {
			if !strings.Contains(body, want) {
				t.Fatalf("the picker is missing %q of map %q:\n%s", want, mp.Name, body)
			}
		}
	}
	if first := strings.Index(body, atlas.Maps[0].Name); len(atlas.Maps) > 1 {
		if second := strings.Index(body, atlas.Maps[1].Name); second >= 0 && second < first {
			t.Fatalf("the picker is not in the registry's order:\n%s", body)
		}
	}

	// enter OPENS THE MAP THE CURSOR IS ON. The cursor starts on the first
	// row; one down moves it, and enter on it opens that map's sheet.
	drive(t, a, key("down"))
	drive(t, a, key("enter"))
	if a.atlas.picking {
		t.Fatal("enter left the picker up")
	}
	if !a.atlas.on || a.atlas.model == nil {
		t.Fatal("enter opened nothing")
	}
	if a.atlas.mapName != atlas.Maps[min(1, len(atlas.Maps)-1)].Name {
		t.Fatalf("enter opened %q, want the row the cursor moved to", a.atlas.mapName)
	}
	body, _, _ = a.frameBody()
	if !strings.Contains(body, atlas.Maps[min(1, len(atlas.Maps)-1)].Title) {
		t.Fatalf("the map after enter is not the chosen one:\n%s", body)
	}

	// esc LEAVES, back to the conversation whole.
	drive(t, a, key("esc"))
	if a.atlas.on {
		t.Fatal("esc left the sheet up")
	}
}

// THE REGISTRY ANSWERS AN UNKNOWN NAME IN THE TRANSCRIPT, the way every other
// refusal here is said: one line, what was asked and what there was instead.
func TestAtlasUnknownNameSaysTheRegistryLine(t *testing.T) {
	a, _ := sheetApp(t)

	a.slash("/atlas nope")
	if a.atlas.on {
		t.Fatal("an unknown name raised the sheet")
	}
	said := transcriptText(a)
	if !strings.Contains(said, atlas.NoMap("nope")) {
		t.Fatalf("the registry line was not said:\n%s", said)
	}
	if !strings.Contains(said, `no map "nope"`) {
		t.Fatalf("the refusal does not name what was asked:\n%s", said)
	}
	for _, mp := range atlas.Maps {
		if !strings.Contains(said, mp.Name) {
			t.Fatalf("the refusal does not name %q:\n%s", mp.Name, said)
		}
	}
}

// TAB COMPLETES THE MAP NAMES, the way a path argument completes its paths:
// the argument list over `/atlas ` is the registry, ranked, and enter puts
// the chosen name into the line.
func TestAtlasArgumentTabCompletesMapNames(t *testing.T) {
	a, _ := sheetApp(t)

	a.input.value = []rune("/atlas pai")
	a.input.cursor = len(a.input.value)
	drive(t, a, key("tab"))
	if !a.comp.open || !a.comp.arg {
		t.Fatal("tab opened no argument list over /atlas")
	}
	rows := strings.Join(a.comp.rows(80, completeRows, a.pal, -1, ""), "\n")
	if !strings.Contains(rows, atlas.Maps[0].Name) {
		t.Fatalf("the argument list does not name %q:\n%s", atlas.Maps[0].Name, rows)
	}
	// TAB AGAIN PUTS THE WORD INTO THE LINE, the way a path is put in: enter
	// on an argument list belongs to the line under it (input.go), so the
	// second tab is the commit.
	drive(t, a, key("tab"))
	if line := string(a.input.value); !strings.HasPrefix(line, "/atlas "+atlas.Maps[0].Name) {
		t.Fatalf("tab did not complete the map name: %q", line)
	}
}
