package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/cachedir"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/manual"
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

// The guard itself: every answer to the prompt that is not [cacheCleanWord] —
// a y, an empty line, a closed stdin — keeps the cache. A destructive prompt
// fails closed.
func TestCacheCleanKeepsEverythingUnlessTheWordIsTyped(t *testing.T) {
	seedCache(t)
	_ = captureAside(t)
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
	commentary := captureAside(t)
	var out strings.Builder
	if err := runCacheWith([]string{"clean"}, strings.NewReader(cacheCleanWord+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	text := commentary.String() + out.String()
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
	commentary := captureAside(t)
	var out strings.Builder
	if err := runCacheWith([]string{"clean", "--yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(commentary.String()+out.String(), `Type "`) {
		t.Fatalf("--yes still asked:\n%s%s", commentary.String(), out.String())
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

// Row 28. The prompt was on stdout, which is where the answer goes, so
// `aforge cache clean | tee clean.log` put the question in the log file and
// left the person looking at a blank terminal waiting for a word they could not
// see. The receipt stays on stdout: that IS the answer.
func TestTheCacheQuestionIsAskedOffTheAnswerStream(t *testing.T) {
	seedCache(t)
	commentary := captureAside(t)
	var answer strings.Builder
	if err := runCacheWith([]string{"clean"}, strings.NewReader(cacheCleanWord+"\n"), &answer); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"This deletes the shared build cache", "not touched", `Type "` + cacheCleanWord + `"`} {
		if !strings.Contains(commentary.String(), want) {
			t.Errorf("the question is missing %q from the aside:\n%s", want, commentary.String())
		}
		if strings.Contains(answer.String(), want) {
			t.Errorf("stdout carries %q, which is part of the question and not the answer:\n%s", want, answer.String())
		}
	}
	if !strings.Contains(answer.String(), "freed") {
		t.Errorf("the receipt is not on stdout, where a script reads it:\n%s", answer.String())
	}
}

// Row 21. One destructive gesture, two surfaces, one word. The chat cannot pass
// a flag, so it has always wanted `/cache clean now`; the terminal wanted the
// word "clean", and a person who had learned one typed it at the other's
// prompt — at the one prompt in the product that deletes gigabytes.
func TestBothSurfacesConfirmTheCacheDeletionWithTheSameWord(t *testing.T) {
	page, found := manual.Chat().Page("commands")
	if !found {
		t.Fatal("the manual has no `commands` page to check the chat's word against")
	}
	if !strings.Contains(page, "/cache clean "+cacheCleanWord) {
		t.Fatalf("the chat's confirming word is not %q — the terminal prompt and the chat have drifted apart again", cacheCleanWord)
	}
	if !strings.Contains(usageText, `type "`+cacheCleanWord+`"`) {
		t.Fatalf("`aforge --help` does not tell a person to type %q before the cache is deleted", cacheCleanWord)
	}
	seedCache(t)
	_ = captureAside(t)
	var answer strings.Builder
	if err := runCacheWith([]string{"clean"}, strings.NewReader("clean\n"), &answer); err != nil {
		t.Fatal(err)
	}
	if cachedir.Size() == 0 {
		t.Fatalf("the old word still deletes the cache — there are two confirming words again")
	}
	if !strings.Contains(answer.String(), "kept — nothing was deleted.") {
		t.Fatalf("a word that is not %q did not keep the cache:\n%s", cacheCleanWord, answer.String())
	}
}
