package serve

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// Checking a split's parent reads the saved job: it asks GitHub nothing about
// the parent's author and writes nothing to the parent. A person's own read of
// that job still fills its author access.
func TestSplitFromValidationDoesNotHydrateParent(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	if err := f.st.SetRepos([]string{"owner/api"}); err != nil {
		t.Fatal(err)
	}
	add := func(num int) factory.Item {
		t.Helper()
		it, err := f.st.Create(factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Repo: "owner/api", Num: num, Title: "split parent", Cap: 1})
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	parent, child := add(7), add(0)
	if err := f.st.Update(parent.ID, func(it *factory.Item) error { it.Author = "author"; return nil }); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(f.st.ItemPath(parent.ID))
	if err != nil {
		t.Fatal(err)
	}
	var lookups atomic.Int32
	f.st.SetTierResolver(func(context.Context, factory.Item) (factory.Tier, time.Time, error) {
		lookups.Add(1)
		return factory.TierCollab, time.Time{}, nil
	})
	body := api.NewJob{Repo: "owner/api", Title: "Smaller fix", SplitFrom: &parent.ID}
	r := authRequest(httptest.NewRequest(http.MethodPost, "/v1/jobs", nil))
	r.Header.Set("Authorization", "Bearer operator-secret")
	w := httptest.NewRecorder()
	// Checked and stamped directly, so neither request settlement nor create
	// admission can read the parent first.
	if !f.h.checkNewJobExtras(w, r, &body) {
		t.Fatalf("split validation: %d %s", w.Code, w.Body)
	}
	if err := f.h.stampNewJobExtras(r, child.ID, body); err != nil {
		t.Fatal(err)
	}
	child, err = f.st.GetSaved(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(f.st.ItemPath(parent.ID))
	if err != nil || !bytes.Equal(before, after) || lookups.Load() != 0 || child.SplitFrom != parent.ID {
		t.Fatalf("split changed its parent: lookups=%d split_from=%d err=%v", lookups.Load(), child.SplitFrom, err)
	}
	if read, err := f.st.Get(parent.ID); err != nil || read.Tier != factory.TierCollab || lookups.Load() != 1 {
		t.Fatalf("a person's read did not fill author access: tier=%q lookups=%d err=%v", read.Tier, lookups.Load(), err)
	}
}
