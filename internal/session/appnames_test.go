package session

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAppNamesUseFirstMessageWithoutCallingTitleModel(t *testing.T) {
	completer := namedTurn(t, "answer", "AI title must not be used")
	agent, path := titleAgent(t, completer, func(cfg *Config) { cfg.DeterministicNames = true })
	lane, leave := agent.WatchTitle()
	defer leave()
	text := "Investigate billing timeout with Unicode 字符 and errors"
	stream, err := agent.Submit(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	agent.waitForTitle()
	if got := agent.Title(); got != FirstMessageName(text) {
		t.Fatalf("title %q", got)
	}
	if completer.asks() != 0 {
		t.Fatal("app bought an AI title")
	}
	select {
	case event := <-lane:
		if event.Kind != EventTitleChanged || event.Text != FirstMessageName(text) {
			t.Fatalf("title event: %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no title event")
	}
	if err := agent.Rename("Billing investigation"); err != nil {
		t.Fatal(err)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"title":"Billing investigation"`) {
		t.Fatal("rename missing from journal")
	}
	reopened, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) { cfg.SessionFile = path; cfg.DeterministicNames = true })
	if reopened.Title() != "Billing investigation" {
		t.Fatalf("rename did not survive reopen: %q", reopened.Title())
	}
}

func TestAppRenameRejectsInvalidNamesAndLateAITitles(t *testing.T) {
	agent, _ := titleAgent(t, &scriptedCompleter{}, nil)
	for _, name := range []string{"", strings.Repeat("字", 41), "line\nbreak", "nul\x00byte"} {
		if err := agent.Rename(name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if err := agent.Rename(strings.Repeat("字", 40)); err != nil {
		t.Fatal(err)
	}
	agent.publishTitle(context.Background(), conversationTitle{full: "late AI name"})
	if agent.Title() != strings.Repeat("字", 40) {
		t.Fatal("late naming errand overwrote person")
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if err := agent.Rename("closed"); err == nil {
		t.Fatal("renamed a closed engine")
	}
}

func TestAppNamesPreserveLiteralWordsAndUnicodeAcrossAllReaders(t *testing.T) {
	for _, name := range []string{"full: exact words", "Name this session in ≤8 words", strings.Repeat("字", 40)} {
		t.Run(name, func(t *testing.T) {
			agent, path := titleAgent(t, &scriptedCompleter{steps: oneTurn("answer")}, func(cfg *Config) { cfg.DeterministicNames = true })
			stream, err := agent.Submit(context.Background(), "Opening text")
			if err != nil {
				t.Fatal(err)
			}
			for range stream {
			}
			agent.waitForTitle()
			if err := agent.Rename(name); err != nil {
				t.Fatal(err)
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			summary, ok := Peek(path)
			if !ok || summary.Title != name {
				t.Fatalf("peek lost literal name: %q", summary.Title)
			}
			reopened, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) { cfg.SessionFile = path })
			if reopened.Title() != name {
				t.Fatalf("replay lost literal name: %q", reopened.Title())
			}
			dir := t.TempDir()
			if err := SaveMeta(dir, Meta{ID: "123", Title: name, LiteralName: true}); err != nil {
				t.Fatal(err)
			}
			meta, err := LoadMeta(dir)
			if err != nil || meta.Title != name {
				t.Fatalf("metadata lost literal name: %q, %v", meta.Title, err)
			}
		})
	}
}
