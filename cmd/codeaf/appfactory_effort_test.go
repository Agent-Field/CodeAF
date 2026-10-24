package main

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"os"
	"path/filepath"
	"testing"
)

func TestFactoryChatEffortIsDurableAndRepeatedReadsDoNotMoveJob(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Kind: factory.KindIssue, Num: 12, Repo: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err = os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = markFactoryChat(st, it, file); err != nil {
		t.Fatal(err)
	}
	b := &appBackend{}
	if err = b.saveJobEffort(file, "high"); err != nil {
		t.Fatal(err)
	}
	high, _ := st.Get(it.ID)
	if high.Effort != "high" {
		t.Fatal("effort not stored")
	}
	if err = b.saveJobEffort(file, "high"); err != nil {
		t.Fatal(err)
	}
	same, _ := st.Get(it.ID)
	if !same.Changed.Equal(high.Changed) {
		t.Fatal("reading the current effort changed the job again")
	}
}
