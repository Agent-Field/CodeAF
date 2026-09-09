package tui3

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// A FILE ON SCREEN, in the three states it is seen in: while it is arriving,
// once it has landed, and when it was too big to carry whole.

// goSource is a plausible file body, long enough to be capped and shaped enough
// to be worth colouring.
func goSource(lines int) string {
	var b strings.Builder
	b.WriteString("package main\n\nimport \"fmt\"\n\n")
	for i := 0; i < lines; i++ {
		b.WriteString("// a line of prose about what the next line does\n")
		b.WriteString("func step() { fmt.Println(\"the quick brown fox\") }\n")
	}
	return b.String()
}

func writeArgs(t *testing.T, path, content string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"path": path, "content": content})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// ── 1. the dash ─────────────────────────────────────────────────────────────

// THE REGRESSION PIN. A write past session's display cap used to arrive here as
// JSON cut mid-string; argsOf answered nil, the content read as empty, and the
// expansion drew the dim em dash it draws for a call with nothing in it — so the
// biggest writes were the ones that showed a dash. The cap is now spent inside
// the string, so the payload still parses and the content is still there, ending
// in the marker that says how much of it was left behind.
func TestALargeWriteExpansionShowsItsContent(t *testing.T) {
	// The payload as session now emits it: whole JSON, one shortened field,
	// [capBytes]'s marker on the end of it.
	content := goSource(400)
	kept := content[:6000]
	args := writeArgs(t, "internal/session/loop.go",
		kept+"… ("+itoa(len(content)-6000)+" more bytes)")

	a := toolApp(t, tokens.NoColor,
		call("write", args, "Successfully wrote 41000 bytes to internal/session/loop.go."))
	rows := openFirst(t, a)
	body := strings.Join(rows, "\n")

	if strings.Contains(body, "—") {
		t.Fatalf("the expansion drew the empty dash for a write that has content:\n%s", body)
	}
	if !strings.Contains(body, "package main") {
		t.Fatalf("the expansion is missing the file's first line:\n%s", body)
	}
	// The cut says so, in session's own words, once the window is lifted — which
	// is what a click on the "… N more lines" foot does.
	for i := range a.entries {
		a.entries[i].full = true
	}
	a.touch()
	if full := strings.Join(plainRows(a), "\n"); !strings.Contains(full, "more bytes)") {
		t.Fatalf("a shortened write does not say it was shortened:\n%s", full)
	}
	if line := toolLineOf(t, a); !strings.Contains(line, "+ lines") {
		t.Fatalf("a shortened write's line count is stated as exact: %q", line)
	}
}

// A write that fits is unmarked: no "+", no marker, and the whole content.
func TestAnOrdinaryWriteKeepsAnExactCount(t *testing.T) {
	content := "package main\n\nfunc main() {}\n"
	a := toolApp(t, tokens.NoColor,
		call("write", writeArgs(t, "main.go", content), "Successfully wrote 30 bytes to main.go."))
	if line := toolLineOf(t, a); !strings.Contains(line, "+3 lines") {
		t.Fatalf("write line = %q, want an exact +3 lines", line)
	}
}

// An edit whose replacement blocks were shortened says the same thing about its
// own two figures, because they too were computed from part of the change.
func TestAShortenedEditStatIsAFloor(t *testing.T) {
	marker := "… (9000 more bytes)"
	args := editArgs(t, "f.go", [2]string{"alpha\nbravo" + marker, "alpha\nBRAVO" + marker})
	adds, dels, floor := editStat(args)
	if adds != 1 || dels != 1 {
		t.Fatalf("editStat = +%d −%d, want +1 −1 — the marker was counted as a change", adds, dels)
	}
	if floor != "+" {
		t.Fatalf("a shortened edit reports its counts as exact")
	}
	whole := editArgs(t, "f.go", [2]string{"alpha\nbravo", "alpha\nBRAVO"})
	if _, _, floor := editStat(whole); floor != "" {
		t.Fatalf("an edit that fit was marked as shortened")
	}
}

// ── 2. the live file ────────────────────────────────────────────────────────

// A WRITE IS READABLE WHILE IT IS BEING WRITTEN. The row that says how many
// bytes have arrived now hangs the end of the file underneath it, and the block
// FOLLOWS: fragment by fragment it shows the last lines, not the first.
func TestAFormingWriteShowsTheFileArriving(t *testing.T) {
	a, _ := formingTurn(t)
	showLiveWork(t, a)

	head := `{"path":"notes.go","content":"package main\nfunc first() {}\n`
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", "write", "write notes.go", head)})
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "func first() {}") {
		t.Fatalf("the arriving file is not on screen:\n%s", body)
	}

	// Enough lines to overflow the block, and the last of them must be the one
	// showing. previewWindow rows is the ceiling; a tail is what follows growth.
	grown := head
	for i := 0; i < 3*previewWindow; i++ {
		grown += `func step` + itoa(i) + `() {}\n`
	}
	grown += `func last() {}\n`
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", "write", "write notes.go", grown)})
	body = strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "func last() {}") {
		t.Fatalf("the block did not follow the file down:\n%s", body)
	}
	if strings.Contains(body, "func first() {}") {
		t.Fatalf("the block is showing the head of a file that has outgrown it:\n%s", body)
	}
	if n := blockRows(a); n > previewWindow {
		t.Fatalf("the live block drew %d rows, past the %d ceiling", n, previewWindow)
	}
}

// The block is a WRITE's alone. An edit forming beside it hangs nothing: its
// block is a diff, and half a replacement is not a change.
func TestOnlyAFormingWriteHangsItsContent(t *testing.T) {
	a, _ := formingTurn(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", "edit", "edit f.go",
		`{"path":"f.go","edits":[{"oldText":"alpha\nbravo\ncharlie`)})
	if n := blockRows(a); n != 0 {
		t.Fatalf("a forming edit hung %d rows under itself", n)
	}
	if body := strings.Join(plainRows(a), "\n"); strings.Contains(body, "charlie") {
		t.Fatalf("a forming edit is showing half a replacement:\n%s", body)
	}
}

// The moment the call is whole the streamed tail is let go — the preview under
// the row is drawn from the arguments from then on, and holding a copy of every
// file the session wrote would be the transcript keeping something nothing reads.
func TestTheStreamedTailIsDroppedWhenTheCallLands(t *testing.T) {
	a, _ := formingTurn(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming("c1", "write", "write notes.go",
		`{"path":"notes.go","content":"package main\n`)})
	if a.entries[len(a.entries)-1].formed == "" {
		t.Fatal("the forming row kept nothing of the arriving file")
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolAnnounced, CallID: "c1", Tool: "write",
		Hint: "write notes.go", Args: writeArgs(t, "notes.go", "package main\n"),
	}})
	for i := range a.entries {
		if a.entries[i].formed != "" {
			t.Fatal("an announced row is still holding the streamed tail")
		}
	}
}

// ── 3. the colour ───────────────────────────────────────────────────────────

// Source is COLOURED and still fits. The paint goes on after the fit, so a row
// is measured as text and drawn as source — a width measured through escape
// sequences is a width measured wrong.
func TestSourceIsHighlightedAndStillTruncates(t *testing.T) {
	a := &app{pal: newPalette(tokens.TrueColor, false)}
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	long := "func step() { fmt.Println(\"" + strings.Repeat("x", 400) + "\") }"
	rows := a.codeRowsWith(st, "package main\n\n"+long+"\n", "main.go", 40)
	if len(rows) != 3 {
		t.Fatalf("codeRows drew %d rows for 3 lines — a long line wrapped", len(rows))
	}
	for i, r := range rows {
		if width := ansi.StringWidth(plain(r)); width > 40 {
			t.Fatalf("row %d is %d cells wide, past 40: %q", i, width, plain(r))
		}
	}
	if !strings.Contains(rows[0], "\x1b[") {
		t.Fatalf("nothing in `package main` was coloured: %q", rows[0])
	}
	// The text survives the paint whole.
	if plain(rows[0]) != "package main" {
		t.Fatalf("the first row reads %q, want `package main`", plain(rows[0]))
	}
}

// A file no lexer claims falls back to the flat dim block it always drew, rather
// than to chroma's fallback lexer painting a log as though it were code.
func TestUnknownFileTypesStayPlain(t *testing.T) {
	a := &app{pal: newPalette(tokens.TrueColor, false)}
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	text := "package main\nfunc step() {}\n"
	for _, path := range []string{"", "notes.zzzznotalanguage"} {
		got := strings.Join(a.codeRowsWith(st, text, path, 60), "\n")
		want := strings.Join(a.plainRows(text, 60), "\n")
		if got != want {
			t.Fatalf("path %q was highlighted anyway", path)
		}
	}
}

// THE LINEAR TIER STAYS PLAIN. Syntax colour is a claim carried by hue alone,
// and a surface being read aloud does not receive one.
func TestTheLinearTierDrawsSourceFlat(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	pal.linear = true
	a := &app{pal: pal}
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	text := "package main\nfunc step() {}\n"
	got := strings.Join(a.codeRowsWith(st, text, "main.go", 60), "\n")
	if want := strings.Join(a.plainRows(text, 60), "\n"); got != want {
		t.Fatalf("the linear tier highlighted source:\n%q", got)
	}
}

// blockRows is how many rows hang under the first tool row — the live block,
// the preview, whatever the row chose to draw beneath itself.
func blockRows(a *app) int {
	n, seen := 0, false
	for _, r := range plainRows(a) {
		r = strings.TrimLeft(r, " ")
		switch {
		case strings.HasPrefix(r, railMid), strings.HasPrefix(r, railLast),
			strings.HasPrefix(r, railASCII):
			seen = true
		case seen && (strings.HasPrefix(r, railCont) || strings.HasPrefix(r, railContASCII)):
			n++
		case seen:
			return n
		}
	}
	return n
}
