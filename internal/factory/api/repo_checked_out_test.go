package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// The queue's repositories say whether the factory's machine knows a checkout
// of each, as `checked_out`, from the one map of checkouts the floor answers;
// one with none says nothing, which reads as false.
func TestQueueRepositoriesSayWhetherTheyAreCheckedOut(t *testing.T) {
	q := FromSnapshot(factory.Snapshot{
		Repos:     []factory.Repo{{Name: "acme/api", Watched: true}, {Name: "acme/web", Watched: true}},
		Checkouts: map[string]string{"acme/api": "/somewhere/api"},
	}, "", time.Unix(0, 0))
	if len(q.Repos) != 2 || !q.Repos[0].CheckedOut || q.Repos[1].CheckedOut {
		t.Fatalf("queue repositories = %+v", q.Repos)
	}
	on, _ := json.Marshal(q.Repos[0])
	off, _ := json.Marshal(q.Repos[1])
	if !strings.Contains(string(on), `"checked_out":true`) || strings.Contains(string(off), `"checked_out"`) {
		t.Fatalf("repositories on the wire = %s and %s", on, off)
	}
}
