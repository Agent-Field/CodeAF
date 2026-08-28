package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/cachedir"
)

// seedCacheDir fills the cache under the state root the fixture has already
// moved: sheetApp points AFORGE_HOME at its own temporary directory, so the
// seeding has to come AFTER the app and write where the app will look.
func seedCacheDir(t *testing.T) {
	t.Helper()
	dir := filepath.Join(cachedir.Root(), "toolchain", "npm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blob"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runCacheNote runs one /cache form through the dispatch and hands back the
// note it produced, the way the loop would: the command runs off the loop and
// its message is fed back through Update.
func runCacheNote(t *testing.T, a *app, line string) string {
	t.Helper()
	cmd := a.slash(line)
	if cmd == nil {
		return lastNote(t, a)
	}
	a.Update(cmd())
	return lastNote(t, a)
}

// THE GUARD: a bare /cache clean is always the question and never the act.
// Only the sentence typed out in full — /cache clean now — deletes anything,
// and the question that precedes it names the size and what stays safe.
func TestCacheCleanStopsAtTheQuestionUntilTypedInFull(t *testing.T) {
	a, _ := sheetApp(t)
	seedCacheDir(t)

	note := runCacheNote(t, a, "/cache clean")
	for _, want := range []string{"this deletes the shared build cache", "4.0 KB", "not touched", "/cache clean now"} {
		if !strings.Contains(note, want) {
			t.Errorf("the question is missing %q: %q", want, note)
		}
	}
	if cachedir.Size() == 0 {
		t.Fatal("/cache clean deleted without the typed confirmation")
	}

	note = runCacheNote(t, a, "/cache clean now")
	if !strings.Contains(note, "cache cleaned · 4.0 KB freed") {
		t.Fatalf("the confirmed clean did not report what left: %q", note)
	}
	if cachedir.Size() != 0 {
		t.Fatal("/cache clean now left the cache standing")
	}

	// Asked again over nothing, both forms answer with the one empty sentence
	// rather than a zero or a silence.
	if note = runCacheNote(t, a, "/cache clean"); note != cacheEmptyWord {
		t.Fatalf("clean over an empty cache said %q, want %q", note, cacheEmptyWord)
	}
}

// The reading form answers both states, and never with a figure of zero.
func TestBareCacheReadsTheSizeAndTheEmptiness(t *testing.T) {
	a, _ := sheetApp(t)
	seedCacheDir(t)

	if note := runCacheNote(t, a, "/cache"); !strings.Contains(note, "the cache holds 4.0 KB") {
		t.Fatalf("bare /cache did not say the size: %q", note)
	}
	if _, err := cachedir.Clean(); err != nil {
		t.Fatal(err)
	}
	if note := runCacheNote(t, a, "/cache"); !strings.Contains(note, "the cache is empty") {
		t.Fatalf("bare /cache over nothing did not say so: %q", note)
	}
}

// A word that is not a form changes nothing and says the two that are.
func TestCacheRefusesAnUnknownArgumentCalmly(t *testing.T) {
	a, _ := sheetApp(t)
	seedCacheDir(t)
	if note := runCacheNote(t, a, "/cache purge"); !strings.Contains(note, "/cache takes clean") {
		t.Fatalf("an unknown argument was not refused in the house shape: %q", note)
	}
	if cachedir.Size() == 0 {
		t.Fatal("an unknown argument deleted the cache")
	}
}
