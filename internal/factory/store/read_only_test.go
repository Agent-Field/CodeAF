package store_test

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// P5, P9: a spending read may run under an item lock without migrating aliases,
// rewriting the document, or changing the data used for lifetime spending.
func TestReadOnlyDoesNotLockOrRepair(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.Create(factory.Item{Repo: "api", SpentBefore: 2, ChatSpent: 1, Stream: &factory.Stream{Spent: 3}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(st.ItemPath(job.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"owner/api"}); err != nil {
		t.Fatal(err)
	}
	unchanged := errors.New("read only")
	done := make(chan error, 1)
	go func() {
		done <- st.Update(job.ID, func(*factory.Item) error {
			got, err := st.ReadOnly(job.ID)
			if err != nil {
				return err
			}
			if got.Repo != "api" || factory.JobLifetimeSpent(got) != 5 {
				return errors.New("read repaired the alias or changed spending")
			}
			return unchanged
		})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, unchanged) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read-only read acquired an item lock")
	}
	after, err := os.ReadFile(st.ItemPath(job.ID))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("document changed: %s, %v", after, err)
	}
	if _, err := st.ReadOnly(job.ID + 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing document = %v", err)
	}
	if err := os.WriteFile(st.ItemPath(job.ID+1), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadOnly(job.ID + 1); err == nil {
		t.Fatal("unreadable document was accepted")
	}
}
