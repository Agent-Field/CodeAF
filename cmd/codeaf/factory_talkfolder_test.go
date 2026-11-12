package main

// Where a job's own conversation works when no checkout of its repository is
// known, and what its brief says about the time before Start: never the
// window's folder (the app's own home, where a manager once cloned the
// repository itself), the factory's clone when there is one, else an empty
// folder of the job's own.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAJobsChatWithNoCheckoutWorksInAFolderOfItsOwn(t *testing.T) {
	st, web, profile := talkLab(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "fix the ledger", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := talkMaker(st, web, profile)(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := session.LoadMeta(filepath.Dir(chat))
	if err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(st.Root(), "chats", strconv.Itoa(it.ID))
	if meta.Workspace != own || meta.Workspace == web {
		t.Fatalf("the conversation works in %q, want the job's own %q and never the window's %q", meta.Workspace, own, web)
	}
	if info, err := os.Stat(own); err != nil || !info.IsDir() {
		t.Fatalf("the job's own folder was not made: %v", err)
	}
	data, err := os.ReadFile(chat)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), talkBlockBrief) {
		t.Fatalf("the brief does not say what happens before Start or who clones:\n%s", data)
	}
	for _, line := range []string{
		"Before the job starts, what the person says here is the brief.",
		"Start the job only when asked.",
		"The runner clones the repository when the job starts; never clone it yourself.",
	} {
		if !strings.Contains(talkBlockBrief, line) {
			t.Errorf("the brief lacks %q", line)
		}
	}
}

func TestAJobsChatWorksInTheFactorysCloneWhenThereIsOne(t *testing.T) {
	st, web, _ := talkLab(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Title: "fix the ledger", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	clone := factoryCloneFolder(st.Root(), "acme/api")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := talkFolder(st, factoryRepoDirs(st, web), it)
	if err != nil || got != clone {
		t.Fatalf("folder %q, %v; want the factory's clone %q", got, err, clone)
	}
	if _, err := os.Stat(filepath.Join(st.Root(), "chats")); !os.IsNotExist(err) {
		t.Fatalf("a folder of the job's own was made beside a clone: %v", err)
	}
}
