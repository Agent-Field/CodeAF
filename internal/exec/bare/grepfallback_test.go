package bare

// A CAPABILITY THAT CANNOT WORK IS ABSENT — AND THIS ONE CAN ALWAYS WORK.
//
// `grep` used to shell out to ripgrep and, on a machine without it, answer
// `ripgrep (rg) is not available and could not be downloaded` to every call. In
// one measured ten-hour run that was every one of eleven grep calls, on a
// machine whose egress was blocked so the download the sentence promised could
// never have happened. The model learned it by failing, twice.
//
// So the verb exists everywhere. These tests drive the walking engine directly,
// because that is the half a machine WITH ripgrep would otherwise never run.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// grepTree writes a small repository to search.
func grepTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"main.go":                 "package main\n\nfunc main() {\n\tstartTheBelt()\n}\n",
		"internal/belt.go":        "package internal\n\n// startTheBelt wires the tools.\nfunc startTheBelt() {}\n",
		"internal/notes.md":       "the belt is wired at startup\n",
		".git/COMMIT_EDITMSG":     "startTheBelt everywhere\n",
		"node_modules/dep/dep.js": "function startTheBelt() {}\n",
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	// A binary file, which a search must not paste into anybody's context.
	if err := os.WriteFile(filepath.Join(root, "belt.bin"), []byte("startTheBelt\x00\x01\x02"), 0o644); err != nil {
		t.Fatalf("write the binary: %v", err)
	}
	return root
}

func TestGrepWithoutRipgrepFindsWhatRipgrepWouldFind(t *testing.T) {
	root := grepTree(t)
	matches, limitHit, err := grepByWalking(context.Background(), "startTheBelt", root, "", false, false, 100)
	if err != nil {
		t.Fatalf("the walking engine failed: %v", err)
	}
	if limitHit {
		t.Fatal("five files tripped a hundred-match cap")
	}

	found := map[string]bool{}
	for _, one := range matches {
		relative, _ := filepath.Rel(root, one.filePath)
		found[filepath.ToSlash(relative)] = true
		if one.lineNumber < 1 {
			t.Fatalf("%s reported line %d", relative, one.lineNumber)
		}
	}
	for _, want := range []string{"main.go", "internal/belt.go"} {
		if !found[want] {
			t.Fatalf("the search missed %s; it found %v", want, found)
		}
	}
	// THE NOISE IS SKIPPED, and the description says which noise: .git,
	// node_modules, vendor, and anything with a NUL byte in it.
	for _, unwanted := range []string{".git/COMMIT_EDITMSG", "node_modules/dep/dep.js", "belt.bin"} {
		if found[unwanted] {
			t.Fatalf("the search descended into %s", unwanted)
		}
	}
}

// The arguments are the SAME arguments, so every one of them has to reach the
// walking engine and mean what it means for ripgrep.
func TestGrepWithoutRipgrepHonoursTheSameArguments(t *testing.T) {
	root := grepTree(t)
	search := func(pattern, glob string, ignoreCase, literal bool, limit int) []grepMatch {
		t.Helper()
		matches, _, err := grepByWalking(context.Background(), pattern, root, glob, ignoreCase, literal, limit)
		if err != nil {
			t.Fatalf("%q failed: %v", pattern, err)
		}
		return matches
	}

	if got := search("STARTTHEBELT", "", false, false, 100); len(got) != 0 {
		t.Fatalf("a case-sensitive search matched %d lines", len(got))
	}
	if got := search("STARTTHEBELT", "", true, false, 100); len(got) == 0 {
		t.Fatal("ignoreCase found nothing")
	}
	// A literal search takes the pattern as bytes, so regex punctuation in it is
	// punctuation and not syntax.
	if got := search("startTheBelt(", "", false, true, 100); len(got) == 0 {
		t.Fatal("a literal search for startTheBelt( found nothing")
	}
	if _, _, err := grepByWalking(context.Background(), "startTheBelt(", root, "", false, false, 100); err == nil {
		t.Fatal("startTheBelt( compiled as a regex, so `literal` means nothing")
	}
	// A glob with no separator is a base-name filter, which is what `*.md`
	// means to everybody who types it.
	markdown := search("belt", "*.md", false, false, 100)
	if len(markdown) != 1 || !strings.HasSuffix(markdown[0].filePath, "notes.md") {
		t.Fatalf("the *.md filter matched %v", markdown)
	}
	// A glob with a separator is matched against the path, at any depth.
	scoped := search("startTheBelt", "internal/*.go", false, false, 100)
	if len(scoped) == 0 {
		t.Fatal("internal/*.go matched nothing")
	}
	for _, one := range scoped {
		if !strings.HasSuffix(one.filePath, filepath.Join("internal", "belt.go")) {
			t.Fatalf("internal/*.go reached outside itself: %s", one.filePath)
		}
	}
	if got := search("startTheBelt", "**/*.go", false, false, 100); len(got) < 1 {
		t.Fatal("**/*.go matched nothing")
	}
	// And the limit is the limit.
	matches, limitHit, err := grepByWalking(context.Background(), "e", root, "", false, false, 3)
	if err != nil {
		t.Fatalf("the capped search failed: %v", err)
	}
	if len(matches) != 3 || !limitHit {
		t.Fatalf("a limit of 3 returned %d matches (cap reached: %v)", len(matches), limitHit)
	}
}

// A bad pattern is a REFUSAL THE MODEL CAN ACT ON, not a crash and not silence.
func TestGrepWithoutRipgrepRefusesAnUnreadablePattern(t *testing.T) {
	_, _, err := grepByWalking(context.Background(), "(unclosed", grepTree(t), "", false, false, 100)
	if err == nil || !strings.Contains(err.Error(), "Invalid pattern") {
		t.Fatalf("an unclosed group answered %v", err)
	}
}

// THE MODEL IS NEVER TOLD TO DOWNLOAD SOMETHING IT CANNOT DOWNLOAD. Whichever
// engine this machine has, the description names it and the tool is on the belt.
func TestGrepIsOnTheBeltWhicheverEngineIsHere(t *testing.T) {
	var grep *Tool
	for index, tool := range AllTools(t.TempDir()) {
		if tool.Name == "grep" {
			grep = &AllTools(t.TempDir())[index]
			break
		}
	}
	if grep == nil {
		t.Fatal("grep is not on the belt")
	}
	if strings.Contains(grep.Description, "could not be downloaded") {
		t.Fatalf("the description still promises a download: %q", grep.Description)
	}
	if _, present := ripgrepPath(); !present && grep.Description != grepFallbackDescription {
		t.Fatalf("a machine without ripgrep describes grep as %q", grep.Description)
	}
}
