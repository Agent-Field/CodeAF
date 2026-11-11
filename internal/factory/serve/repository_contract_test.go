package serve

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestRepositoryIdentityContractAndViewerFiltering(t *testing.T) {
	for _, key := range []string{"api", "acme/api"} {
		t.Run(key, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			job, err := st.Create(factory.Item{Repo: "api", Places: []string{"api"}, Title: "existing job", State: factory.StateQueued, Stream: plannedStream("main.go")})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.Create(factory.Item{Repo: "acme/web", Title: "hidden job", State: factory.StateQueued}); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for _, args := range [][]string{{"init", dir}, {"-C", dir, "remote", "add", "origin", "https://github.com/acme/api.git"}} {
				if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
					t.Fatalf("git: %v %s", err, out)
				}
			}
			if err := st.SetCheckout(key, dir); err != nil {
				t.Fatal(err)
			}
			if err := st.SetRepos([]string{"acme/api", "acme/web"}); err != nil {
				t.Fatal(err)
			}
			h, clock, g := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true}, "nobody": {}})
			h.seam = factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(st.CheckoutDir))
			h.config.Store = st
			h.config.CodeOwners = func(_ context.Context, repo string) ([]factory.CodeOwner, error) {
				if repo != "acme/api" && repo != "acme/web" {
					t.Fatalf("ownership read used checkout key %q", repo)
				}
				return []factory.CodeOwner{{Pattern: "*", Handles: []string{"priya"}}}, nil
			}
			owner := queueAs(t, h, "floor-secret", false)
			if len(owner.Repos) != 2 || len(owner.Jobs) != 2 {
				t.Fatalf("duplicate repository: %+v", owner)
			}
			viewer := signViewer(t, h, g, clock, "priya")
			q := queueAs(t, h, viewer, false)
			if api.Contract != 2 || len(q.Repos) != 1 || q.Repos[0].Name != "acme/api" || len(q.Jobs) != 1 || q.Jobs[0].ID != job.ID || q.Jobs[0].Repo != "acme/api" || !reflect.DeepEqual(q.Jobs[0].Owners, []string{"priya"}) || q.Capacity.Queued != 1 {
				t.Fatalf("contract or visible identity = %+v", q)
			}
			if q.Desk == nil || !reflect.DeepEqual(q.Desk.Checkouts, map[string]string{"acme/api": dir}) || q.Repos[0].Checkout != dir {
				t.Fatalf("checkout wire keys = %+v", q.Desk)
			}
			w := identityRequest(h, "GET", "/v1/jobs/1", "", viewer, nil)
			var detail api.Job
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.Repo != "acme/api" {
				t.Fatalf("job detail = %d %s", w.Code, w.Body)
			}
			if w := identityRequest(h, "GET", "/v1/jobs/2", "", viewer, nil); w.Code != 404 {
				t.Fatalf("hidden detail = %d %s", w.Code, w.Body)
			}
			nobody := signViewer(t, h, g, clock, "nobody")
			if q := queueAs(t, h, nobody, false); len(q.Jobs) != 0 || len(q.Repos) != 0 || q.Reason != api.ReasonNoVisibleRepos {
				t.Fatalf("hidden queue = %+v", q)
			}
			raw, err := os.ReadFile(st.ItemPath(job.ID))
			if err != nil {
				t.Fatal(err)
			}
			var stored struct{ Item factory.Item }
			if json.Unmarshal(raw, &stored) != nil || stored.Item.Repo != "acme/api" {
				t.Fatalf("legacy job was not persisted: %s", raw)
			}
		})
	}
}
