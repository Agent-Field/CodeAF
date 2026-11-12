package serve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestQueuedCapacityCountsArePrivateToTheJobOwner(t *testing.T) {
	c := capacitySetup(t)
	worker := c.attach(t, c.priya, "priya", "private-mac", 1)
	c.add(t, "priya")
	waiting := c.add(t, "priya")
	c.request(t, "GET", "/v1/runners/pull", worker.Credential, nil, 200, nil)
	for _, who := range []struct {
		name, token string
		owner       bool
	}{{"owner", c.priya, true}, {"teammate", c.raj, false}, {"operator", "operator-secret", false}} {
		t.Run(who.name, func(t *testing.T) {
			check := func(job api.Job) {
				t.Helper()
				disclosesCounts := strings.Contains(job.StatePresentation.Detail, "1/1 execution slots in use")
				if disclosesCounts != who.owner {
					t.Fatalf("private capacity disclosure=%v for %s: %+v", disclosesCounts, who.name, job.StatePresentation)
				}
			}
			var job api.Job
			c.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", waiting.ID), who.token, nil, 200, &job)
			check(job)
			var queue api.Queue
			c.request(t, "GET", "/v1/queue", who.token, nil, 200, &queue)
			found := false
			for _, row := range queue.Jobs {
				if row.ID == waiting.ID {
					found = true
					check(row)
				}
			}
			if !found {
				t.Fatal("the shared queued job became hidden")
			}
		})
	}
}
