package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDocPrintsAPlainFileWithNoCallAndNoBill(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	name := filepath.Join(dir, "notes.txt")
	os.WriteFile(name, []byte("plain text, read locally\n"), 0o644)

	said, err := captureStdout(t, func() error { return runDoc([]string{name}) })
	if err != nil {
		t.Fatalf("doc refused a plain file: %v", err)
	}
	if !strings.Contains(said, "plain text, read locally") {
		t.Errorf("doc printed %q, want the file's own text", said)
	}
}

func TestRunDocRefusesWhatIsNeitherPlainNorADocumentItReads(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	name := filepath.Join(dir, "thing.bin")
	os.WriteFile(name, []byte{0x00, 0x01, 0x02, 0x03}, 0o644)

	err := runDoc([]string{name})
	if err == nil {
		t.Fatal("doc accepted a binary file it can neither read locally nor bill for")
	}
	if !strings.Contains(err.Error(), "none of those") {
		t.Errorf("the refusal says %q, want the tool's own sentence about formats", err.Error())
	}
}

func TestParsePageRange(t *testing.T) {
	for _, asked := range []struct {
		in         string
		from, to   int
		wantBroken bool
	}{
		{in: "", from: 0, to: 0},
		{in: "3", from: 3, to: 3},
		{in: "3-5", from: 3, to: 5},
		{in: " 4 - 9 ", from: 4, to: 9},
		{in: "0", wantBroken: true},
		{in: "5-2", wantBroken: true},
		{in: "x", wantBroken: true},
	} {
		from, to, err := parsePageRange(asked.in)
		if asked.wantBroken {
			if err == nil {
				t.Errorf("parsePageRange(%q) accepted a range it should refuse", asked.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePageRange(%q) refused: %v", asked.in, err)
			continue
		}
		if from != asked.from || to != asked.to {
			t.Errorf("parsePageRange(%q) = %d-%d, want %d-%d", asked.in, from, to, asked.from, asked.to)
		}
	}
}

func TestRunWebRefusesEverythingThatIsNotSearchOrFetch(t *testing.T) {
	err := runWeb([]string{})
	if err == nil || !strings.Contains(err.Error(), "search") {
		t.Errorf("a bare `codeaf web` says %v, which never names the two shapes", err)
	}
	err = runWeb([]string{"sniff", "x"})
	if err == nil || !strings.Contains(err.Error(), "neither") {
		t.Errorf("an unknown subcommand says %v, which never says so", err)
	}
	err = runWeb([]string{"search"})
	if err == nil || !strings.Contains(err.Error(), "QUERY") {
		t.Errorf("a search with no query says %v, which never names what is missing", err)
	}
	err = runWeb([]string{"fetch"})
	if err == nil || !strings.Contains(err.Error(), "URL") {
		t.Errorf("a fetch with no URL says %v, which never names the shape", err)
	}
}

func TestSessionPathOfTakesTheFileOffTheToolResult(t *testing.T) {
	got := sessionPathOf("/tmp/picture.png — 1024×768 png, 41KB, generated on some-model")
	if got != "/tmp/picture.png" {
		t.Errorf("sessionPathOf = %q, want the path the result opened with", got)
	}
}
