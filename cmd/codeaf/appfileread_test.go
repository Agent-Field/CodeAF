package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

func readOK(t *testing.T, b *appBackend, agent, path string, offset int64, limit int) appwire.FileRead {
	t.Helper()
	got, err := b.ReadFile(agent, path, offset, limit)
	if err != nil {
		t.Fatalf("files.read %s: %v", path, err)
	}
	return got
}

// A FILE CHIP OPENS A BOUNDED, TRUTHFUL, READ-ONLY PREVIEW: a workspace file is
// text with its facts, a file outside is still read but said to be outside, a
// link is followed to where it really is, and a link out of the workspace is
// outside however its path reads.
func TestAppReadFileWorkspaceOutsideAndLinks(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	work := agent.Workspace
	if err := os.MkdirAll(filepath.Join(work, "internal", "parse"), 0o755); err != nil {
		t.Fatal(err)
	}
	lexer := filepath.Join(work, "internal", "parse", "lexer.go")
	if err := os.WriteFile(lexer, []byte("package parse\n\nfunc Lex() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readOK(t, backend, agent.ID, "internal/parse/lexer.go", 0, appwire.FilePageMax)
	if got.Kind != appwire.ReadText || got.Outside || got.Symlink || got.Text != "package parse\n\nfunc Lex() {}\n" || got.Next != nil || got.Lines == nil || *got.Lines != 3 || got.Size == nil || *got.Size != 29 || got.Modified == "" {
		t.Fatalf("a workspace file = %+v", got)
	}
	wantResolved, _ := filepath.EvalSymlinks(lexer)
	if got.Resolved != wantResolved {
		t.Fatalf("resolved %q, want %q", got.Resolved, wantResolved)
	}
	// Facts only.
	if facts := readOK(t, backend, agent.ID, "internal/parse/lexer.go", 0, 0); facts.Text != "" || facts.Kind != appwire.ReadText || facts.Lines == nil {
		t.Fatalf("limit 0 = %+v", facts)
	}

	elsewhere := filepath.Join(t.TempDir(), "notes.go")
	if err := os.WriteFile(elsewhere, []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := readOK(t, backend, agent.ID, elsewhere, 0, 100)
	if out.Kind != appwire.ReadText || !out.Outside || out.Text != "scratch\n" {
		t.Fatalf("an absolute path outside = %+v", out)
	}
	// Climbing out lexically is outside too.
	rel, _ := filepath.Rel(work, elsewhere)
	if climb := readOK(t, backend, agent.ID, rel, 0, 100); !climb.Outside || climb.Kind != appwire.ReadText {
		t.Fatalf("a ../ path = %+v", climb)
	}

	// A link inside to a file inside: inside, marked a link, resolved.
	if err := os.Symlink(lexer, filepath.Join(work, "lexer-link.go")); err != nil {
		t.Fatal(err)
	}
	in := readOK(t, backend, agent.ID, "lexer-link.go", 0, 100)
	if !in.Symlink || in.Outside || in.Resolved != wantResolved || in.Kind != appwire.ReadText {
		t.Fatalf("a link inside = %+v", in)
	}
	// A link inside the workspace to a file outside: outside.
	if err := os.Symlink(elsewhere, filepath.Join(work, "escape.go")); err != nil {
		t.Fatal(err)
	}
	esc := readOK(t, backend, agent.ID, "escape.go", 0, 100)
	wantElse, _ := filepath.EvalSymlinks(elsewhere)
	if !esc.Symlink || !esc.Outside || esc.Resolved != wantElse {
		t.Fatalf("a link out = %+v", esc)
	}
	// A dangling link is missing, not an error.
	if err := os.Symlink(filepath.Join(work, "never"), filepath.Join(work, "dangling.go")); err != nil {
		t.Fatal(err)
	}
	if d := readOK(t, backend, agent.ID, "dangling.go", 0, 100); d.Kind != appwire.ReadMissing || !d.Symlink || d.Text != "" {
		t.Fatalf("a dangling link = %+v", d)
	}
}

// THE STATES THAT ARE NEVER SHOWN AS TEXT: missing or deleted, a folder, a
// binary, bytes that are not UTF-8, and a file over the bound, which is not
// read at all. An unknown agent is not_found; a leading dash is just a name.
func TestAppReadFileStatesAndBounds(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	work := agent.Workspace
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(work, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := readOK(t, backend, agent.ID, "gone.go", 0, 100); got.Kind != appwire.ReadMissing || got.Outside || got.Size != nil {
		t.Fatalf("missing = %+v", got)
	}
	write("deleted.go", []byte("x\n"))
	if err := os.Remove(filepath.Join(work, "deleted.go")); err != nil {
		t.Fatal(err)
	}
	if got := readOK(t, backend, agent.ID, "deleted.go", 0, 100); got.Kind != appwire.ReadMissing {
		t.Fatalf("deleted = %+v", got)
	}
	if err := os.Mkdir(filepath.Join(work, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := readOK(t, backend, agent.ID, "src", 0, 100); got.Kind != appwire.ReadDirectory || got.Text != "" {
		t.Fatalf("a folder = %+v", got)
	}
	write("hero.png", []byte("\x89PNG\r\n\x1a\n\x00\x00"))
	if got := readOK(t, backend, agent.ID, "hero.png", 0, 100); got.Kind != appwire.ReadBinary || got.Text != "" || got.Size == nil {
		t.Fatalf("a binary = %+v", got)
	}
	write("latin1.txt", []byte("caf\xe9\n"))
	if got := readOK(t, backend, agent.ID, "latin1.txt", 0, 100); got.Kind != appwire.ReadBinary || got.Text != "" {
		t.Fatalf("invalid UTF-8 became %+v", got)
	}
	write("huge.log", bytes.Repeat([]byte("a\n"), appwire.FileReadMax/2+1))
	if got := readOK(t, backend, agent.ID, "huge.log", 0, appwire.FilePageMax); got.Kind != appwire.ReadTooLarge || got.Text != "" || got.Size == nil || *got.Size != appwire.FileReadMax+2 || got.Lines != nil {
		t.Fatalf("too large = %+v", got)
	}
	write("-rf", []byte("dash\n"))
	if got := readOK(t, backend, agent.ID, "-rf", 0, 100); got.Kind != appwire.ReadText || got.Text != "dash\n" || !filepath.IsAbs(got.Resolved) {
		t.Fatalf("a leading dash = %+v", got)
	}
	var wire *appwire.Error
	if _, err := backend.ReadFile("nope", "x", 0, 1); !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("an unknown agent answered %v", err)
	}
}

// PAGES ARE BOUNDED AND CUT CLEANLY: a page ends after a whole line when it
// can and never inside a character, next names where the following page
// starts, and an offset inside a character is refused rather than mangled.
func TestAppReadFilePagesCutAtLinesAndCharacters(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	work := agent.Workspace
	line := strings.Repeat("é", 40) + "\n" // 81 bytes
	body := strings.Repeat(line, 2000)     // 162,000 bytes: three pages
	if err := os.WriteFile(filepath.Join(work, "big.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	offset, pages := int64(0), 0
	for {
		page := readOK(t, backend, agent.ID, "big.txt", offset, appwire.FilePageMax)
		pages++
		if len(page.Text) > appwire.FilePageMax || !strings.HasSuffix(page.Text, "\n") {
			t.Fatalf("page %d is %d bytes, ends %q", pages, len(page.Text), page.Text[max(0, len(page.Text)-3):])
		}
		got.WriteString(page.Text)
		if page.Next == nil {
			break
		}
		offset = *page.Next
	}
	if got.String() != body || pages != 3 {
		t.Fatalf("%d pages rebuilt %d of %d bytes", pages, got.Len(), len(body))
	}
	// One long line: cut at a character, not inside one.
	if err := os.WriteFile(filepath.Join(work, "one.txt"), []byte(strings.Repeat("é", 40000)), 0o644); err != nil {
		t.Fatal(err)
	}
	page := readOK(t, backend, agent.ID, "one.txt", 0, 1001)
	if len(page.Text) != 1000 || page.Next == nil || *page.Next != 1000 {
		t.Fatalf("a long line page = %d bytes, next %v", len(page.Text), page.Next)
	}
	var wire *appwire.Error
	if _, err := backend.ReadFile(agent.ID, "one.txt", 1, 100); !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
		t.Fatalf("an offset inside a character answered %v", err)
	}
	if _, err := backend.ReadFile(agent.ID, "one.txt", 1<<30, 100); !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
		t.Fatalf("an offset past the end answered %v", err)
	}
}
