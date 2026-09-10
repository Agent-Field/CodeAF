package tui3

// THE SURFACE'S OWN DIM LINES: WHAT THEY SAY, AND HOW WIDE THEY ARE.
//
// A note is where this program talks about itself, and two of them were wrong
// in the same place. The line a resumed session opens on said where the journal
// file lives — four to six wrapped rows of absolute path, first thing on the
// page, where a person's first impression goes. And every note was wrapped two
// cells wider than the column it is drawn in, so the row overshot the frame and
// was clipped with an ellipsis: three characters gone out of the MIDDLE of a
// path, with the row under it carrying on from after the gap.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// journalUnderHome is the shape that broke both: a real journal path, absolute, under
// the person's home, with a project slug in the middle that is one long segment.
const journalUnderHome = "/home/dev/.aforge/v3/projects/" +
	"tmpclaude1001homedevafdev2418657d81734e64b3ec73decce3d3d5scratchpaddemohomeaforgev2/" +
	"ffff0000longmsg1aforgev3sessionfolderwithalongidentifieronitsownline/transcript.jsonl"

// noticeBlockRows is one note as the conversation draws it: every row of that block,
// with colour stripped and the indent law's gutter left on, because the gutter
// is part of what the row costs.
func noticeBlockRows(a *app, at, width int) []string {
	var out []string
	for _, r := range a.visible(width) {
		if r.entry == at && strings.TrimSpace(plain(r.text)) != "" {
			out = append(out, plain(r.text))
		}
	}
	return out
}

// A RESUMED SESSION OPENS BY SAYING WHICH CONVERSATION IT IS. The path is a
// machine's fact and it is asked for on /status; the name is what a person needs
// to know they are where they left off, and it fits on one row at every width.
func TestAResumedConversationOpensWithItsNameAndNotItsPath(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 60, 30
	a.tilde, a.file, a.title = "/home/dev", journalUnderHome, "long question"

	said := a.resumedNote()
	if want := resumedWord + " · long question"; said != want {
		t.Fatalf("a resumed session opens with %q, want %q", said, want)
	}
	if strings.Contains(said, "/projects/") {
		t.Fatalf("the opening line is still a transcript path: %q", said)
	}

	a.note(said)
	if got := noticeBlockRows(a, 0, a.bodyWidth()); len(got) != 1 {
		t.Fatalf("the opening line took %d rows at %d columns, want one:\n%s",
			len(got), a.bodyWidth(), strings.Join(got, "\n"))
	}

	// AND THE LADDER ENDS ON THE PATH, because a line that named nothing would be
	// worse than a long one — written against $HOME, which is the one shortening
	// that survives being pasted into a shell.
	a.title, a.entries = "", nil
	said = a.resumedNote()
	if !strings.HasPrefix(said, resumedWord+" ~/.aforge/") {
		t.Fatalf("an unnamed conversation opens with %q, want the path written against $HOME", said)
	}
	if strings.Contains(said, "/home/dev") {
		t.Fatalf("the fallback path still spells the person's home out: %q", said)
	}

	// AND FAILING A NAME OF ITS OWN IT USES THE OPENING OF WHAT WAS ASKED, which
	// is the resume picker's own second rung.
	a.entries = []entry{{kind: entryUser, text: "why does the loader crash on an empty map?"}}
	if want := resumedWord + " · why does the loader crash on an"; a.resumedNote() != want {
		t.Fatalf("an unnamed conversation opens with %q, want %q", a.resumedNote(), want)
	}
}

// A NOTE IS WRAPPED TO THE COLUMN IT IS DRAWN IN. It wears two leads — its own
// `· ` marker and the indent law's gutter — and it was wrapped allowing for only
// one, so every row was two cells too wide and the clip ate the characters where
// the rows join. An off-by-two in a wrapper is wrong for every notice, so this
// is asserted at four widths and against the text coming back whole.
func TestAWrappedNoteFitsTheColumnItIsDrawnIn(t *testing.T) {
	for _, width := range []int{60, 80, 120, 160} {
		a := newTestApp(&fakeAgent{model: "m"})
		a.width, a.height = width, 40
		a.railAway = true
		// The path alone, and no sentence around it: [wrap] eats the space it
		// breaks on, so a fixture with one in it could not tell a lost space
		// from a lost character.
		a.note(journalUnderHome)

		body := a.bodyWidth()
		rows := noticeBlockRows(a, 0, body)
		if len(rows) < 2 {
			t.Fatalf("at %d columns the fixture did not wrap: %v", width, rows)
		}
		var joined strings.Builder
		for i, r := range rows {
			// The row is measured AS DRAWN — the reading gutter is cells this
			// row really occupies — and only then stepped past, because what
			// the two trims below read is the note's own marker and the indent
			// law under it (pastGutter, gutter.go).
			if w := ansi.StringWidth(r); w > body {
				t.Fatalf("at %d columns a note row is %d cells wide in a %d-cell column:\n%q",
					width, w, body, r)
			}
			if strings.Contains(r, glyphMore) {
				t.Fatalf("at %d columns a note row was clipped: %q", width, r)
			}
			// The first row wears the marker, the rest the two spaces under it;
			// both sit inside the indent law's own gutter.
			inset := pastGutter(body, r)
			line := strings.TrimPrefix(unindented(inset), "· ")
			if i > 0 {
				line = strings.TrimPrefix(unindented(inset), "  ")
			}
			joined.WriteString(line)
		}
		// AND NOTHING FELL OUT BETWEEN THE ROWS. The path has no spaces in it, so
		// what the rows hold, joined, is what the note said — the fragment that
		// went missing was `mp-`, out of the middle of a path somebody was about
		// to copy.
		if got, want := joined.String(), journalUnderHome; got != want {
			t.Fatalf("at %d columns the note's rows join to\n%q\nwant\n%q", width, got, want)
		}
	}
}

// THE PHONE TIER IS TOLD ABOUT `/` LIKE EVERY OTHER TIER. Under seventy columns
// the hint slot went silent and the branch was dropped at the other end, so the
// legend was refused at both ends and fell back to a rule with nothing written
// on it — on the one width where a newcomer most needs the door named.
func TestTheNarrowLegendStillNamesTheCommandsDoor(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.title = "porting the parser"
	a.branch = "master"
	a.gitProbe = func(string) (string, bool, bool) { return "master", true, true }

	for _, width := range []int{hudTight - 1, 60, 44, 30} {
		line := plain(a.legend(width))
		if !strings.Contains(line, microcopy) {
			t.Fatalf("at %d columns the legend says nothing at all:\n%q", width, line)
		}
		if w := ansi.StringWidth(line); w > width {
			t.Fatalf("at %d columns the legend is %d cells wide:\n%q", width, w, line)
		}
	}
	// AND THE CELLS COME FROM THE BRANCH, which the shell prompt behind this one
	// still says — not from the door, which is written nowhere else on a frame
	// this narrow.
	if line := plain(a.legend(hudTight - 1)); strings.Contains(line, "master") {
		t.Fatalf("the tight legend kept the branch: %q", line)
	}
	if line := plain(a.legend(hudTight)); !strings.Contains(line, "master") {
		t.Fatalf("the legend lost the branch at a width that has room for it: %q", line)
	}
}

// A NOTE IS THIS SURFACE TALKING, AND IT DOES NOT JOIN THE MODEL'S LIST. Notes
// lead with `· ` at the conversation's own indent, which is the glyph and the
// column a markdown bullet lands on — so under an answer that ends in a list,
// `· 3 standing orders here — /standing` read as the model's fourth bullet. The
// one boundary a transcript must draw is who is talking.
func TestASurfaceNoteNeverReadsAsTheModelsNextBullet(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 80, 30
	a.railAway = true
	a.entries = append(a.entries,
		entry{kind: entryUser, text: "what should I do?"},
		entry{kind: entryAssistant, settled: true, text: "Try these:\n\n- one\n- two\n- three"},
	)
	a.note("3 standing orders here — /standing")
	a.note("esc interrupts · ctrl+c twice quits")
	a.touch()

	var drawn []string
	for _, r := range a.visible(a.bodyWidth()) {
		drawn = append(drawn, plain(r.text))
	}
	bullet, first, second := -1, -1, -1
	for i, line := range drawn {
		switch {
		case strings.HasSuffix(line, "· three"):
			bullet = i
		case strings.Contains(line, "3 standing orders here"):
			first = i
		case strings.Contains(line, "esc interrupts"):
			second = i
		}
	}
	if bullet < 0 || first < 0 || second < 0 {
		t.Fatalf("the fixture did not draw what it meant to:\n%s", strings.Join(drawn, "\n"))
	}
	if first != bullet+2 || strings.TrimSpace(drawn[bullet+1]) != "" {
		t.Fatalf("the surface's own line is wedged against the model's list:\n%s",
			strings.Join(drawn[bullet:first+1], "\n"))
	}
	// AND A RUN OF NOTES IS ONE BLOCK: the blank is bought where the voice
	// changes, not between two lines of the same voice, or the opening frame's
	// notes would arrive as three paragraphs.
	if second != first+1 {
		t.Fatalf("two notes in a row were split apart:\n%s", strings.Join(drawn[first:second+1], "\n"))
	}
}

// AND THE HINT SLOT NAMES THE ALWAYS KEY ONLY WHERE IT WOULD ACT. A stuck
// question borrows the consent lane to ask about a turn, and the engine drops a
// tool-session scope on it — the offer above leaves `[a]` off and the press does
// nothing. The slot promised it anyway, and nobody saw it because the slot was
// silent at the width this question is met at.
func TestTheHintSlotNamesTheAlwaysKeyOnlyWhereItWouldAct(t *testing.T) {
	ask := consentEvent(5, "bash", "bash make test", "the turn is repeating itself")
	ask.Memo = false
	_, a := wired([]session.Event{toolBegin("bash", "bash make test"), ask})
	typeLine(t, a, "go on")

	hint := a.legendRight(a.width)
	if strings.Contains(hint, "always") {
		t.Fatalf("the slot promised a key the block above it refused: %q", hint)
	}
	for _, want := range []string{"1 allow once", "3 deny"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("the slot lost %q with it: %q", want, hint)
		}
	}

	// And an ordinary question, which the always key does answer, keeps it.
	memo := consentEvent(6, "bash", "bash make vet", "")
	memo.Memo = true
	_, b := wired([]session.Event{toolBegin("bash", "bash make vet"), memo})
	typeLine(t, b, "go on")
	if hint := b.legendRight(b.width); !strings.Contains(hint, "2 always") {
		t.Fatalf("an ordinary question lost its always key: %q", hint)
	}
}
