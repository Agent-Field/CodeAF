package main

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
	"time"
)

func TestReviewForeignManagerDoesNotSpendLocalBudgetAfterWorkerLeaves(t *testing.T) {
	st, web, profile := talkLab(t)
	local, err := st.Create(factory.Item{Repo: "web", Num: 7, Title: "local", Cap: 10})
	if err != nil {
		t.Fatal(err)
	}
	foreign := local
	foreign.Num = 42
	foreign.Created = foreign.Created.Add(time.Hour)
	foreign.Title = "foreign job"
	chat, err := managerMaker(st, web, profile)(context.Background(), foreign)
	if err != nil {
		t.Fatal(err)
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
	if got.ChatSpent != 0 {
		t.Fatalf("foreign manager charged local job $%v; want $0", got.ChatSpent)
	}
}
