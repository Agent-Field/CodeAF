package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// linkLab is a surface with two nodes on it and one settled answer from the
// model, laid out wide enough that the paragraph is one row.
func linkLab(t *testing.T, said string) *app {
	t.Helper()
	a, _, _ := roomApp(t)
	a.width, a.height = 200, 24
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Write the auth tests",
		session.TaskRunning, session.TaskNotice{})})
	a.entries = append(a.entries, entry{kind: entryAssistant, text: said, settled: true})
	a.touch()
	return a
}

// linkedRow is the first row on the FRAME that carries a task link, and the
// screen row it landed on. It resolves the y exactly as [app.rowAt] does, so a
// click built from it is the click a person makes.
func linkedRow(a *app) (row, int, bool) {
	body, _ := a.window(a.bodyWidth(), a.viewHeight())
	for i, r := range body {
		if len(r.links) > 0 {
			return r, a.bodyTop() + i, true
		}
	}
	return row{}, 0, false
}

// THE GRAMMAR, POSITIVE AND NEGATIVE, ON ONE TABLE. What is recognized names a
// task out loud; what is not is prose that happens to have a number in it, and
// a paragraph pockmarked with underlined numbers is worse than no links at all.
func TestTheTaskLinkGrammarRecognizesOnlyWhatNamesATask(t *testing.T) {
	// Two nodes exist: 7 and 8. Everything else is an unknown id.
	look := func(id uint64) (string, bool) {
		switch id {
		case 7:
			return "Fix the nil-map", true
		case 8:
			return "Write the auth", true
		}
		return "", false
	}
	pal := newPalette(tokens.ANSI256, false)

	for _, tc := range []struct {
		name, text string
		want       []string // the phrases that must become links, in order
	}{
		// ── the four shapes ──
		{"bare", "I split this into task 7 and task 8.", []string{"task 7", "task 8"}},
		{"hash", "See task #7 for the guard.", []string{"task #7"}},
		{"id", "Steer it with tasks id 7 say \"...\".", []string{"tasks id 7"}},
		{"id hash", "That is task id #8 now.", []string{"task id #8"}},
		{"opening the row", "task 7 is the one that failed.", []string{"task 7"}},
		{"after punctuation", "(task 7) is the one.", []string{"task 7"}},
		{"capitalized", "Task 7 landed and TASKS ID 8 is still going.",
			[]string{"Task 7", "TASKS ID 8"}},

		// ── the negatives ──
		{"a bare number", "It took 7 minutes.", nil},
		{"a bare hash", "Issue #7 is unrelated.", nil},
		{"a hash with no task before it", "The PR #7 fixes it.", nil},
		{"a longer word", "The taskbar 7 is a Windows thing.", nil},
		{"a word ending in task", "The subtask 7 is not a task.", nil},
		{"a word beginning with task", "It tasked 7 people with it.", nil},
		{"no number at all", "The task ran and the task finished.", nil},
		{"a version", "task 7.1 is a version number.", nil},
		{"a date", "task 7-8 is a range.", nil},
		{"a number glued to a word", "task 7a is not an id.", nil},
		{"an unknown id", "I also opened task 9 and task 40.", nil},
		{"a task nobody minted", "task 0 is nothing.", nil},
		{"inside a URL", "See https://example.com/task 7 for it.", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, links := linkifyTasks(tc.text, pal, look)
			if len(links) != len(tc.want) {
				t.Fatalf("%q produced %d links, want %d: %q", tc.text, len(links), len(tc.want), plain(out))
			}
			if len(tc.want) == 0 {
				if out != tc.text {
					t.Fatalf("%q was repainted for nothing: %q", tc.text, out)
				}
				return
			}
			// The plain text is untouched: a link is ink and columns, never a
			// rewritten sentence.
			if plain(out) != tc.text {
				t.Fatalf("the link pass changed what the row SAYS:\n%q\n%q", tc.text, plain(out))
			}
			for i, want := range tc.want {
				at := strings.Index(tc.text, want)
				span := hudSpan{
					from: ansi.StringWidth(tc.text[:at]),
					to:   ansi.StringWidth(tc.text[:at+len(want)]),
				}
				if links[i].span != span {
					t.Fatalf("link %d over %q is at %+v, want %+v", i, want, links[i].span, span)
				}
			}
		})
	}
}

// CODE IS NOT PROSE AND GETS NO LINKS — both ways code reaches a row: the fenced
// block behind its gutter, and the inline span on the raised plane.
func TestTaskLinksSkipCode(t *testing.T) {
	look := func(uint64) (string, bool) { return "Fix the nil-map", true }
	pal := newPalette(tokens.ANSI256, false)

	// A FENCED LINE. prose draws every row of a block behind one hairline, which
	// is the mark this pass reads (prose/code.go).
	fenced := "  " + tokens.GlyphCodeGutter + " run(\"task 7\")"
	if out, links := linkifyTasks(fenced, pal, look); len(links) != 0 || out != fenced {
		t.Fatalf("a fenced line grew %d links: %q", len(links), plain(out))
	}

	// AN INLINE SPAN, through the real renderer: a code span is drawn on the
	// surface's one raised plane, and this pass reads the plane off the row.
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	if !tokens.TrueColor.SheetGround() {
		t.Skip("this profile has no raised plane to test")
	}
	rows := renderMarkdownWith(st, "Run `task 7` and then task 7 again.", 100)
	if len(rows) == 0 {
		t.Fatal("prose rendered nothing")
	}
	out, links := linkifyTasks(rows[0], pal, look)
	if len(links) != 1 {
		t.Fatalf("the code span and the prose were not told apart: %d links in %q", len(links), plain(out))
	}
	// The one link is the SECOND "task 7" — the prose one, past the code span.
	flat := plain(rows[0])
	if want := ansi.StringWidth(flat[:strings.LastIndex(flat, "task 7")]); links[0].span.from != want {
		t.Fatalf("the link landed at column %d, want %d: %q", links[0].span.from, want, flat)
	}

	// AND WHERE THERE IS NO PLANE THE BACKTICKS COME BACK (prose/inline.go), so
	// the same reference is masked by the backticks instead.
	if out, links := linkifyTasks("Run `task 7` please.", pal, look); len(links) != 0 {
		t.Fatalf("a backticked reference was linked: %q", plain(out))
	}
}

// THE PAINT AROUND A LINK SURVIVES IT. This surface's ink closes with SGR 39
// rather than a full reset, so a link that did not put the row's own foreground
// back would take the rest of the paragraph's colour with it.
func TestATaskLinkRestoresTheInkAroundIt(t *testing.T) {
	look := func(uint64) (string, bool) { return "Fix the nil-map", true }
	pal := newPalette(tokens.ANSI256, false)
	painted := pal.ink("before task 7 after")

	out, links := linkifyTasks(painted, pal, look)
	if len(links) != 1 {
		t.Fatalf("the painted row produced %d links: %q", len(links), plain(out))
	}
	if plain(out) != "before task 7 after" {
		t.Fatalf("the link pass changed the row's text: %q", plain(out))
	}
	// The link is accent and underlined, and the row's own ink is re-opened after
	// it closes.
	if !strings.Contains(out, "\x1b[4m") || !strings.Contains(out, sgr256(hueAccent)) {
		t.Fatalf("the link is not inked as a link: %q", out)
	}
	tail := out[strings.LastIndex(out, "\x1b[24m"):]
	if !strings.Contains(tail, sgr256(hueInk)) {
		t.Fatalf("the row's own ink was not restored after the link: %q", tail)
	}

	// AND IT IS IDEMPOTENT: the pass over its own output is the same output,
	// which is what makes a re-render at the same width a no-op.
	again, links := linkifyTasks(out, pal, look)
	if again != out || len(links) != 1 {
		t.Fatalf("the pass is not idempotent:\n%q\n%q", out, again)
	}

	// A TERMINAL THAT DRAWS NO SGR KEEPS THE ROW AND KEEPS THE DOOR: no ink to
	// spend, and the columns are recorded all the same.
	bare := newPalette(tokens.NoColor, false)
	out, links = linkifyTasks("before task 7 after", bare, look)
	if out != "before task 7 after" || len(links) != 1 {
		t.Fatalf("a colourless row lost its link: %q (%d links)", out, len(links))
	}
}

// THE LINK IS A DOOR, and it is the same door the strip's chips are: one press,
// one room (room.go's [app.openRoomFor]).
func TestClickingATaskLinkOpensThatNodesRoom(t *testing.T) {
	a := linkLab(t, "I split this into task 7 and task 8, and task 40 is somebody else's.")
	r, y, ok := linkedRow(a)
	if !ok {
		t.Fatalf("the answer grew no links:\n%s", strings.Join(plainRows(a), "\n"))
	}
	if len(r.links) != 2 {
		t.Fatalf("the row carries %d links, want 2 (task 40 is not a node here)", len(r.links))
	}
	if r.links[0].id != 7 || r.links[1].id != 8 {
		t.Fatalf("the links point at %d and %d", r.links[0].id, r.links[1].id)
	}

	// The SECOND link opens the second node — the columns are what routes it, so
	// pressing the wrong ones would open the wrong room.
	drive(t, a, tea.MouseClickMsg{X: r.links[1].span.from + 1, Y: y, Button: tea.MouseLeft})
	if !a.roomOpen() || a.room.id != 8 {
		t.Fatalf("the second link did not open node 8: open=%v", a.roomOpen())
	}

	// A press in the PROSE beside a link is not a link press: it falls through to
	// the row's own answer, which for a paragraph on a page is the way out.
	drive(t, a, tea.MouseClickMsg{X: r.links[0].span.from - 2, Y: y, Button: tea.MouseLeft})
	if a.roomOpen() {
		t.Fatal("a click on the sentence between two links was swallowed by one of them")
	}
}

// AN UNKNOWN ID IS PLAIN TEXT, on the frame as well as in the pure pass: a link
// that opens nothing is the surface claiming a door it does not have.
func TestAnUnknownTaskIdIsNotALink(t *testing.T) {
	a := linkLab(t, "The work is in task 41 and task 42.")
	if r, _, ok := linkedRow(a); ok {
		t.Fatalf("an unknown id became a link: %+v", r.links)
	}
	said := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"task 41", "task 42"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the words went missing with the link: %q", said)
		}
	}
}

// THE PASS IS THE ROWS' AND NOT THE MARKDOWN'S, which is what keeps it right at
// every width: a reference the wrap split across two rows is simply not one, and
// the phone tier needs no special case.
func TestTaskLinksFollowTheWrapAtEveryTier(t *testing.T) {
	said := "The parser guard landed under task 7 and the suite is green."
	for _, width := range []int{200, 100, 80, 44} {
		a := linkLab(t, said)
		a.width = width
		a.touch()
		total := 0
		for _, r := range a.visible(a.bodyWidth()) {
			for _, link := range r.links {
				total++
				if link.span.to > a.bodyWidth() {
					t.Fatalf("at %d columns a link runs off the row: %+v", width, link.span)
				}
				// The columns name the phrase they were cut from.
				flat := plain(r.text)
				if !strings.Contains(flat, "task 7") {
					t.Fatalf("at %d columns a link landed on a row without one: %q", width, flat)
				}
			}
		}
		// The phrase is short and the measure is never tight enough to split it,
		// so every tier finds exactly the one link.
		if total != 1 {
			t.Fatalf("at %d columns the answer carries %d links, want 1:\n%s",
				width, total, strings.Join(plainRows(a), "\n"))
		}
	}
}

// THE PERSON'S OWN WORDS ARE NOT LINKED, and neither is anything else on the
// surface: this is a pass over what the MODEL wrote (render.go's [app.deckRows]).
func TestOnlyTheModelsProseGrowsTaskLinks(t *testing.T) {
	a := linkLab(t, "It is done.")
	a.entries = append(a.entries,
		entry{kind: entryUser, text: "look at task 7 please"},
		entry{kind: entryNote, text: "task 7 landed"},
	)
	a.touch()
	if r, _, ok := linkedRow(a); ok {
		t.Fatalf("a block that is not the model's answer grew a link: %+v", r.links)
	}
}
