package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── THE `@` LIST ON HOME, AND A PATH THE WAY A SHELL WOULD READ IT ───────────

// atHome is home open over a target folder holding a note and a picture.
func atHome(t *testing.T) (*app, string) {
	t.Helper()
	lab := newHomeLab(t)
	a := lab.door("")
	root := t.TempDir()
	for _, name := range []string{"notes.md", "shot.png"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a.target.where = root
	a.showPage(pageHome)
	return a, root
}

// Typing `@` and a letter into home's box opens the list over the target
// folder, enter puts the path into the sentence, and the list does not reopen
// over its own answer.
func TestAtOpensTheFileListOnHomeAndEnterPutsThePathIn(t *testing.T) {
	a, root := atHome(t)
	drive(t, a, key("@"), key("n"))
	h := &a.home
	if !h.comp.open {
		t.Fatal("typing @n on home did not open the list")
	}
	if h.walked != root {
		t.Fatalf("the list walked %q, want the target %q", h.walked, root)
	}
	if !h.comp.loaded {
		t.Fatal("the walk did not land")
	}
	text := homeText(a)
	if !strings.Contains(text, "notes.md") {
		t.Fatalf("home does not offer the note:\n%s", text)
	}
	if !strings.Contains(a.homeHint(), "enter put it in") {
		t.Fatalf("the foot does not name the list's keys: %q", a.homeHint())
	}
	if a.noticeHomeHint() != "" {
		t.Fatal("a tip drew under the @ list")
	}
	line, ok := h.focusedLine()
	if !ok || line.kind != homeCompletion {
		t.Fatalf("the cursor is not on a completion row: %+v", line)
	}
	drive(t, a, key("enter"))
	if got := h.box.String(); got != "@notes.md" {
		t.Fatalf("enter left the box as %q, want @notes.md", got)
	}
	if h.comp.open {
		t.Fatal("the list stayed open on top of its own answer")
	}
	if !a.at(pageHome) {
		t.Fatal("completing a path left home")
	}
}

// Choosing a picture takes the token out of the sentence and puts the picture
// on home's tray, which rides into the next conversation.
func TestAPictureChosenFromTheListGoesOnHomesTray(t *testing.T) {
	a, root := atHome(t)
	drive(t, a, key("@"), key("s"), key("h"))
	h := &a.home
	if !h.comp.open {
		t.Fatal("typing @sh on home did not open the list")
	}
	drive(t, a, key("enter"))
	if got := h.box.String(); got != "" {
		t.Fatalf("the token was left in the box: %q", got)
	}
	if len(a.chips) != 1 || a.chips[0].path != filepath.Join(root, "shot.png") {
		t.Fatalf("the picture did not reach the tray: %+v", a.chips)
	}
	if !h.carrying {
		t.Fatal("home is not carrying the tray")
	}
	if want := folderAttachedWord + "shot.png" + homeRidesWord; h.msg != want {
		t.Fatalf("home said %q, want %q", h.msg, want)
	}
}

// esc closes the list and leaves the word alone; the next letter of the token
// opens it again, which is the conversation list's own rule (app.go's
// [app.dismissLists] seals only the command list).
func TestEscClosesTheAtListOnHomeAndLeavesTheWord(t *testing.T) {
	a, _ := atHome(t)
	drive(t, a, key("@"), key("n"))
	h := &a.home
	drive(t, a, key("esc"))
	if h.comp.open {
		t.Fatal("esc did not close the list")
	}
	if got := h.box.String(); got != "@n" {
		t.Fatalf("esc changed the draft to %q", got)
	}
	drive(t, a, key("o"))
	if !h.comp.open {
		t.Fatal("the next letter of the token did not open the list again")
	}
}

// The command list wins over the file list, so `/` never draws both.
func TestTheCommandListWinsOverTheFileListOnHome(t *testing.T) {
	a, _ := atHome(t)
	drive(t, a, key("/"), key("m"))
	h := &a.home
	if !h.cmd.open || h.comp.open {
		t.Fatalf("over a slash word cmd=%v comp=%v, want the command list alone", h.cmd.open, h.comp.open)
	}
}

// A path in quotes, or with its spaces backslashed, is the one path it is —
// the shape Finder and a terminal drop hand you.
func TestAQuotedOrEscapedPathIsReadAsOnePath(t *testing.T) {
	a, _ := sheetApp(t)
	want := "/Users/me/Screenshot 2026-09-18 at 1.35.20 PM.png"
	for _, typed := range []string{
		"'" + want + "'",
		`"` + want + `"`,
		strings.ReplaceAll(want, " ", `\ `),
		"  '" + want + "'  ",
	} {
		if got := a.resolvePath(typed); got != want {
			t.Errorf("resolvePath(%q) = %q, want %q", typed, got, want)
		}
	}
	// A path with raw spaces and no quotes is left exactly as typed.
	if got := a.resolvePath(want); got != want {
		t.Errorf("a raw path was changed: %q", got)
	}
	// And a quoted picture reaches the tray as a picture.
	dir := t.TempDir()
	shot := filepath.Join(dir, "Screen Shot.png")
	if err := os.WriteFile(shot, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.attachFilePath("'" + shot + "'")
	if len(a.chips) != 1 || a.chips[0].path != shot || !isImagePath(a.chips[0].path) {
		t.Fatalf("a quoted picture did not reach the tray as a picture: %+v", a.chips)
	}
}

// /image is gone: the table does not offer it, and typing it is an unknown
// word that attaches nothing.
func TestThereIsNoImageCommand(t *testing.T) {
	for _, c := range commands {
		if c.name == "image" {
			t.Fatal("the command table still offers /image")
		}
	}
	a, _ := sheetApp(t)
	a.slash("/image shot.png")
	if len(a.chips) != 0 {
		t.Fatalf("/image still attached something: %+v", a.chips)
	}
	if body := strings.Join(plainRows(a), "\n"); !strings.Contains(body, "no command called /image") {
		t.Fatalf("/image was not answered as an unknown word:\n%s", body)
	}
}
