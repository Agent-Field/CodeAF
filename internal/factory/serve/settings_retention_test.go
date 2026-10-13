package serve

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestAppDoneDaysControlsQueueAndJobRetentionWithoutDeletingHistory(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	f.h.config.ServedBy = "app"
	f.h.config.ProfileDir = t.TempDir()
	merged := f.now.Add(-48 * time.Hour)
	item, err := f.st.Create(factory.Item{Repo: "acme/api", Title: "merged", State: factory.StateShipped, PullRequest: &factory.PullRequest{State: factory.PRMerged, MergedAt: merged}})
	if err != nil {
		t.Fatal(err)
	}
	set := func(days int) {
		raw, _ := json.Marshal(days)
		if err := config.ApplyAppSettings(f.h.config.ProfileDir, map[string]json.RawMessage{"factory.done_days": raw}, ""); err != nil {
			t.Fatal(err)
		}
	}
	assert := func(visible bool) {
		var queue api.Queue
		f.request(t, "GET", "/v1/queue", "operator-secret", nil, 200, &queue)
		var job api.Job
		f.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", item.ID), "operator-secret", nil, 200, &job)
		if len(queue.Jobs) != 1 || queue.Jobs[0].Display.RunningVisible != visible || job.Display.RunningVisible != visible {
			t.Fatalf("queue and job retention: %+v %+v", queue.Jobs, job.Display)
		}
	}
	assert(false)
	set(7)
	assert(true)
	f.now = merged.Add(7 * 24 * time.Hour)
	assert(false)
	set(0)
	assert(false)
	if _, err := f.st.Get(item.ID); err != nil {
		t.Fatal("tidying deleted history", err)
	}
}
