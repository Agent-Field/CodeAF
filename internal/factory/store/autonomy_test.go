package store

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestNewJobDefaultsAutoWithoutChangingOldRecords(t *testing.T) {
	st := open(t)
	id, err := st.Add(context.Background(), factory.Item{Title: "new issue", Repo: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)
	if it.Autonomy != "auto" {
		t.Fatal("new job not Auto")
	}
	legacy, err := st.Create(factory.Item{Title: "old issue", Repo: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	read, _ := st.Get(legacy.ID)
	if factory.Automatic(read) || read.Autonomy != "" {
		t.Fatal("old record silently opted in")
	}
}
