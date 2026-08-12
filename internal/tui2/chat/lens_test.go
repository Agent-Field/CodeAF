package chat

import (
	"image"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/keychip"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// What an empty lens says, and where a room begins — 13.13's three filed
// defects, each asserted on the rows a reader actually gets.
//
// All three are frame-shaped rather than span-shaped, for the reason 13.13's
// own method note gives: a tier is correct in isolation and wrong beside its
// neighbours, and a left edge is not a property of any one surface at all.

// -- the taught empty state (5.22 rule 6) ------------------------------------

// THE DEFECT: the home transcript on a cold open was an entirely blank lens.
// 5.22 rule 6 asks an empty state to TEACH.
func TestAColdOpenTeachesRatherThanDrawingNothing(t *testing.T) {
	app := newTestApp(&fakeBackend{}, nil, nil)
	poll(t, app)
	frame := ansi.Strip(app.Frame(80, 14))

	if !strings.Contains(frame, "say what you want done") {
		t.Fatalf("a cold open said nothing about what to do here:\n%s", frame)
	}
	// The three rows are the registry's own words — verb, accelerator and the
	// one-line description — so a reader who clicks one and a reader who types
	// its chord learn the same thing about it.
	for _, id := range teachIDs {
		entry, ok := registry.ByID(id)
		if !ok {
			t.Fatalf("the empty state teaches %q, which the catalog does not have", id)
		}
		for _, want := range []string{teachKey(entry), entry.Verb, entry.Description} {
			if !strings.Contains(frame, want) {
				t.Fatalf("the empty state left out %q:\n%s", want, frame)
			}
		}
	}
}

// A TAUGHT ROW IS A VERB·KEY CHIP AND LEADS WITH THE VERB (§16, keychip).
//
// It used to lead with the KEY, in an aligned column of its own, which is the
// `esc close` bug wearing a layout: the first word on every row was the thing to
// press rather than the thing it does, so a reader parsed each row by already
// knowing the answer. The column went with it — nobody scans three rows for a
// keystroke they are being taught, and what the column actually did was hold the
// verbs at a left edge that moved with the longest accelerator in the set.
func TestATaughtExampleLeadsWithItsVerb(t *testing.T) {
	app := newTestApp(&fakeBackend{}, nil, nil)
	poll(t, app)
	rows := app.pane.teachRows(80)

	taught := 0
	for _, row := range rows {
		if row.id == "" {
			continue
		}
		taught++
		entry, ok := registry.ByID(row.id)
		if !ok {
			t.Fatalf("the empty state taught %q, which the catalog does not have", row.id)
		}
		var line strings.Builder
		for _, span := range row.spans {
			line.WriteString(span.text)
		}
		words := strings.TrimLeft(line.String(), " ")
		if !strings.HasPrefix(words, entry.Verb) {
			t.Errorf("the row for %q reads %q — it does not lead with its verb %q",
				row.id, words, entry.Verb)
		}
		chip := keychip.Text(registry.ChipFor(entry.Verb, teachKey(entry)))
		if !strings.HasPrefix(words, chip) {
			t.Errorf("the row for %q reads %q, want it to open on the chip %q",
				row.id, words, chip)
		}
	}
	if taught == 0 {
		t.Fatal("the empty state drew no example rows, so this test proves nothing")
	}
}

// A taught door has to open. Every example is a row [App.runEntry] performs
// unconditionally — 5.20 rule 3 lets a row refuse with a reason, but a first
// frame that taught a reader a dead end would be teaching the wrong lesson.
func TestEveryTaughtExampleIsALiveDoor(t *testing.T) {
	app := newTestApp(&fakeBackend{}, nil, nil)
	poll(t, app)
	for _, id := range teachIDs {
		if reason := app.entryReason(id); reason != "" {
			t.Fatalf("the empty state teaches %q, which this room refuses: %q", id, reason)
		}
	}
}

// 5.22 rule 6 says CLICKABLE, and 13.14's chain says a click runs the registry
// row the word was drawn from — the same executor the footer's own words reach.
func TestClickingATaughtExampleRunsItsRegistryRow(t *testing.T) {
	app := newTestApp(&fakeBackend{}, nil, nil)
	poll(t, app)
	_ = app.Frame(80, 14)

	rows := app.pane.teachRows(80)
	target, y := "", -1
	for i, row := range rows {
		if row.id != "" {
			target, y = row.id, i
			break
		}
	}
	if y < 0 {
		t.Fatal("the empty state drew no example rows")
	}
	if target != helpEntryID {
		t.Fatalf("the first example is %q; this test wants the help sheet", target)
	}
	app.pane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y})
	if app.overlay != overlayCapability {
		t.Fatalf("clicking the taught help row raised %v, not the capability sheet", app.overlay)
	}

	// And the prose above the rows is not a door: it is a statement about the
	// room, and a surface where half the words did something on click would be
	// worse than one where none of them did (footer/hit.go's own line).
	if _, ok := app.pane.teachAt(0); ok {
		t.Fatal("the lead sentence answered as a target")
	}
}

// The teaching is only ever on screen while it is true.
func TestTheEmptyStateLeavesTheMomentTheRoomIsSpokenIn(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, nil, nil)
	poll(t, app)
	if !strings.Contains(ansi.Strip(app.Frame(80, 14)), "say what you want done") {
		t.Fatal("the cold open did not teach")
	}

	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "hello there"})
	poll(t, app)
	frame := ansi.Strip(app.Frame(80, 14))
	if strings.Contains(frame, "say what you want done") {
		t.Fatalf("the teaching outlived the emptiness it described:\n%s", frame)
	}
	if _, ok := app.pane.teachAt(0); ok {
		t.Fatal("a spoken-in room still hit-tests as the empty state")
	}
}

// -- the empty-room note (tier and measure) ----------------------------------

// THE DEFECT: the entered room's empty note was model-facing prose at the
// PRIMARY tier — the tier 5.13 reserves for speech — running the full width of
// the lens with no measure. Nobody said it: it is chrome about an absence.
func TestTheEmptyRoomNoteIsChromeAtAReadableMeasure(t *testing.T) {
	style := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	block := &noteBlock{id: "empty-room", text: emptyRoomNote, style: style}

	// Wide enough that an unmeasured note would run the whole way.
	rows := block.Rows(200)
	if len(rows) < 2 {
		t.Fatalf("the note rendered %d rows: %q", len(rows), rows)
	}
	longest := 0
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > longest {
			longest = w
		}
	}
	if longest > tokens.ProseMeasure {
		t.Fatalf("the note ran %d cells at a 200-cell width; the measure is %d",
			longest, tokens.ProseMeasure)
	}
	if longest <= tokens.LensIndent {
		t.Fatalf("the note rendered nothing readable: %q", rows)
	}

	chrome := sgrOf(style, tokens.TextTertiary)
	speech := sgrOf(style, tokens.TextPrimary)
	body := strings.Join(rows, "\n")
	if !strings.Contains(body, chrome) {
		t.Fatalf("the note is not drawn at the chrome tier: %q", body)
	}
	if strings.Contains(body, speech) {
		t.Fatalf("the note is drawn at the speech tier: %q", body)
	}

	// A lens narrower than the measure wraps at the lens: the measure is a
	// ceiling, never a width.
	for _, row := range block.Rows(30) {
		if w := ansi.StringWidth(row); w > 30 {
			t.Fatalf("the note ran %d cells at a 30-cell width: %q", w, row)
		}
	}
}

// sgrOf is the escape one token paints with, recovered from the styler itself
// rather than written out as a hex code a palette change would silently rot.
func sgrOf(style *tokens.Styler, token tokens.Token) string {
	prefix, _, _ := strings.Cut(style.PaintToken("X", token), "X")
	return prefix
}

// -- the lens's left edge (5.13's spacing rhythm) ----------------------------

// §7 LEFT TWO SURFACES WHERE THERE WERE FOUR, and they no longer share one
// left edge — which is the change this test now pins rather than the rule it
// used to.
//
// The place line and the meta strip are gone as rows; the transcript and the
// bar row still open at [tokens.LensIndent], and the COMPOSER is an INSET
// SURFACE whose grid is measured from its own field rather than from the frame.
//
// Two cells stand outside that field's content box and neither is content: the
// hug's state edge at column 0 (chrome at the field's own edge, §19) and §19's
// one cell of inner padding inside it. What begins after them is §20's grid,
// unchanged — a two-cell gutter holding the prompt and its space, and the
// content edge at [tokens.LensIndent] measured from the field. So the composer
// keeps the room's rhythm; it keeps it one field-inset in, which is what an
// input surface drawn on its own ground is supposed to do.
func TestTheRoomsSurfacesShareOneLeftEdge(t *testing.T) {
	app := dressedApp(t)
	// A typed draft, so the composer's row is words rather than a placeholder
	// behind a caret cell — the edge under test is where the DRAFT begins.
	for _, key := range []string{"h", "i"} {
		press(app, key)
	}
	const width = 100
	rows := strings.Split(ansi.Strip(app.Frame(width, 24)), "\n")

	surfaces := map[string]string{}
	for _, row := range rows {
		switch {
		case strings.Contains(row, "hi"):
			surfaces["composer"] = row
		case strings.Contains(row, "chat  work  notebook"):
			surfaces["bar"] = row
		}
	}
	if len(surfaces) != 2 {
		t.Fatalf("only found %d of the two surfaces in:\n%s", len(surfaces), strings.Join(rows, "\n"))
	}
	// The bar keeps the room's edge.
	if got := contentEdge(surfaces["bar"]); got != tokens.LensIndent {
		t.Fatalf("the bar begins at column %d, the room begins at %d:\n%q",
			got, tokens.LensIndent, surfaces["bar"])
	}
	// The field's own two cells come first — the edge, then the pad — and the
	// prompt sits in the gutter after them. The column is counted in CELLS,
	// because the edge glyph is three bytes.
	if want := hugHead(tokens.GlyphPromptChat) + " "; !strings.HasPrefix(surfaces["composer"], want) {
		t.Fatalf("the composer's gutter is not the edge, the pad and the prompt: %q",
			surfaces["composer"])
	}
	// THE GRID ASSERTION (§20): measured from inside the field, the composer's
	// content edge is the room's own edge and not a number of its own.
	const fieldInset = 2 // the hug edge at column 0, and §19's pad inside it
	if got := composer.TextColumn(width) - fieldInset; got != tokens.LensIndent {
		t.Fatalf("the composer's content edge is %d cells inside its field, want %d",
			got, tokens.LensIndent)
	}
	// And the picture agrees: the typed words open exactly there.
	if got := ansi.StringWidth(surfaces["composer"][:strings.Index(surfaces["composer"], "hi")]); got != composer.TextColumn(width) {
		t.Fatalf("the composer drew its words at column %d, want the content edge %d:\n%q",
			got, composer.TextColumn(width), surfaces["composer"])
	}
}

// contentEdge is the column a row's words begin at, on an already-stripped row.
//
// A one-cell marker inside the gutter does not move the edge — it HANGS there,
// which is what the gutter is for (the composer's prompt glyph, a block
// header's). Anything else in those two columns is a surface starting early.
func contentEdge(row string) int {
	for i, r := range []rune(row) {
		if r == ' ' {
			continue
		}
		if i < tokens.LensIndent-1 {
			continue // a marker in the gutter, with its own space behind it
		}
		return i
	}
	return tokens.LensIndent
}

// The two components hold the same law on their own, at every width they can
// be drawn at: an indent that was not paid for out of the content width is an
// overflow one breakpoint later.
func TestTheEdgeIsPaidForOutOfTheContentWidth(t *testing.T) {
	app := dressedApp(t)
	for width := 20; width <= 140; width++ {
		for _, row := range strings.Split(app.Frame(width, 24), "\n") {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("at width %d a row ran %d cells: %q", width, got, row)
			}
		}
	}
}
