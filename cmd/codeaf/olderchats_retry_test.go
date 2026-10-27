package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestAFailedOlderChatCopyWaitsForTheChatToChange counts copy attempts. An idle
// terminal visits every older chat each background round, and a copy seals the
// chat's whole folder, so a chat whose copy failed is not copied again until it
// has changed: the same chat and folder fail the same way.
func TestAFailedOlderChatCopyWaitsForTheChatToChange(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	original := filepath.Join(home.Dir(), "v3", "projects", "old", "old-session")
	if err := os.MkdirAll(original, 0700); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(original, "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"type":"session","version":1,"id":"old-session"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(original, session.Meta{ID: "old-session", LastUserAt: time.Now(), Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	chats := olderChats{copy: func(context.Context, string) (cell.Cell, error) {
		attempts++
		return cell.Cell{}, errors.New("files kept changing while saving this chat; try again")
	}}
	for range 5 {
		if _, _, ok := chats.visit(t.Context(), original); ok {
			t.Fatal("a failed copy was reported as made")
		}
	}
	if attempts != 1 {
		t.Fatalf("five rounds over an unchanged chat whose copy fails made %d copy attempts, want 1", attempts)
	}

	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"message","role":"user","content":"later"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	chats.visit(t.Context(), original)
	if attempts != 2 {
		t.Fatalf("a chat that changed after its copy failed made %d copy attempts in total, want 2", attempts)
	}
}

// TestAnOlderChatInTheHomeFolderIsNotCopied keeps the copier off a chat whose
// folder is the home folder. A copy seals the chat's folder, and sealing the home
// folder walks every file under it while holding the engine's one writer, so
// every open chat's tools waited behind it until they timed out.
func TestAnOlderChatInTheHomeFolderIsNotCopied(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	person := t.TempDir()
	t.Setenv("HOME", person)
	original := filepath.Join(home.Dir(), "v3", "projects", "home", "home-session")
	if err := os.MkdirAll(original, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "transcript.jsonl"), []byte(`{"type":"session","version":1,"id":"home-session"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(original, session.Meta{ID: "home-session", LastUserAt: time.Now(), Workspace: person}); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	chats := olderChats{copy: func(context.Context, string) (cell.Cell, error) {
		attempts++
		return cell.Cell{}, nil
	}}
	if _, _, ok := chats.visit(t.Context(), original); ok {
		t.Fatal("a chat in the home folder was reported as copied")
	}
	if attempts != 0 {
		t.Fatalf("a chat in the home folder made %d copy attempts, want 0", attempts)
	}
}
