package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/cachedir"
	"github.com/Agent-Field/aforge-v2/internal/home"
)

func seedCache(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	dir := filepath.Join(cachedir.Root(), "toolchain", "go-mod")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blob"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The guard itself: every answer to the prompt that is not the word "clean" —
// a y, an empty line, a closed stdin — keeps the cache. A destructive prompt
// fails closed.
func TestCacheCleanKeepsEverythingUnlessTheWordIsTyped(t *testing.T) {
	seedCache(t)
	for _, answer := range []string{"y\n", "yes\n", "\n", ""} {
		var out strings.Builder
		if err := runCacheWith([]string{"clean"}, strings.NewReader(answer), &out); err != nil {
			t.Fatalf("answer %q errored: %v", answer, err)
		}
		if !strings.Contains(out.String(), "kept — nothing was deleted.") {
			t.Fatalf("answer %q did not report keeping:\n%s", answer, out.String())
		}
		if cachedir.Size() == 0 {
			t.Fatalf("answer %q deleted the cache", answer)
		}
	}
}

// The word proceeds, the blast radius was said first, and the receipt names
// what left.
func TestCacheCleanActsOnTheTypedWord(t *testing.T) {
	seedCache(t)
	var out strings.Builder
	if err := runCacheWith([]string{"clean"}, strings.NewReader("clean\n"), &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"This deletes the shared build cache", "not touched", "4.0 KB freed"} {
		if !strings.Contains(text, want) {
			t.Errorf("the clean transcript is missing %q:\n%s", want, text)
		}
	}
	if cachedir.Size() != 0 {
		t.Fatal("the cache survived a confirmed clean")
	}
}

// --yes is the scripted door: no prompt, no stdin read, same receipt.
func TestCacheCleanYesSkipsTheQuestion(t *testing.T) {
	seedCache(t)
	var out strings.Builder
	if err := runCacheWith([]string{"clean", "--yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Type \"clean\"") {
		t.Fatalf("--yes still asked:\n%s", out.String())
	}
	if cachedir.Size() != 0 {
		t.Fatal("--yes did not clean")
	}
}

// The reading form answers over both states, and an empty cache is a sentence
// rather than a zero.
func TestBareCacheAnswersBothWays(t *testing.T) {
	seedCache(t)
	var out strings.Builder
	if err := runCacheWith(nil, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "4.0 KB") {
		t.Fatalf("bare cache did not say the size:\n%s", out.String())
	}
	if _, err := cachedir.Clean(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runCacheWith(nil, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "the cache is empty") {
		t.Fatalf("an empty cache did not say so:\n%s", out.String())
	}
}
