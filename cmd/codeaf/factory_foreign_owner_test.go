package main

import (
	"context"
 "encoding/json"
 "os"
 "path/filepath"
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
	"time"
)

func TestReviewForeignManagerDoesNotSpendLocalBudgetAfterWorkerLeaves(t *testing.T) {
 for _, mode := range []string{"issue", "created", "legacy"} {
 t.Run(mode, func(t *testing.T) {
	st, web, profile := talkLab(t)
	local, err := st.Create(factory.Item{Repo: "web", Num: 7, Title: "local", Cap: 10})
	if err != nil {
		t.Fatal(err)
	}
	foreign := local
	if mode == "issue" { foreign.Num = 42 } else { foreign.Created = foreign.Created.Add(time.Hour) }
	foreign.Title = "foreign job"
	chat, err := managerMaker(st, web, profile)(context.Background(), foreign)
	if err != nil {
		t.Fatal(err)
	}
 if mode == "legacy" {
  body, err := json.Marshal(map[string]any{"root":chatKey(st.Root()),"job":local.ID,"repo":local.Repo})
  if err != nil { t.Fatal(err) }
  if err := os.WriteFile(filepath.Join(filepath.Dir(chat),factoryOwnerFile),body,0600); err != nil { t.Fatal(err) }
 }
	// A remote worker has finished and no longer has a live mirror here.
	budget := jobChatDoorOf(st).Chat(chat)
	if budget != nil {
		budget.Spend(.25)
	}
	got, err := st.Get(local.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := 0.0
 if mode == "legacy" { want = .25 }
 if got.ChatSpent != want {
		t.Fatalf("foreign manager charged local job $%v; want $%v", got.ChatSpent, want)
	}

 })
 }
}
