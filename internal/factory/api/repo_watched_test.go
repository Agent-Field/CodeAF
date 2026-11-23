package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// The queue's repositories carry whether each is watched as the floor said,
// never a constant, and there is no second "on" beside it: listed is on.
func TestQueueRepositoriesSayWhetherTheyAreWatched(t *testing.T) {
	q := FromSnapshot(factory.Snapshot{Repos: []factory.Repo{{Name: "acme/api", Watched: true}, {Name: "acme/web"}}}, "", time.Unix(0, 0))
	if len(q.Repos) != 2 || !q.Repos[0].Watched || q.Repos[1].Watched {
		t.Fatalf("queue repositories = %+v", q.Repos)
	}
	data, err := json.Marshal(q.Repos[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"watched":false`) || strings.Contains(string(data), `"on"`) {
		t.Fatalf("repository on the wire = %s", data)
	}
	back := ToSnapshot(q, nil)
	if !back.Repos[0].Watched || back.Repos[1].Watched {
		t.Fatalf("repositories read back = %+v", back.Repos)
	}
}
