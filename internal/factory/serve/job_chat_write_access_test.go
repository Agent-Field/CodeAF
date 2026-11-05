package serve_test

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestJobReplyWithoutWriteAccessIsDefinite(t *testing.T) {
	for _, door := range []string{"queue", "runner"} {
		t.Run(door, func(t *testing.T) {
			f := writeAccessJobs(t, door)
			it := f.create(t, factory.Item{Kind: factory.KindIssue, Tier: factory.TierStranger, State: factory.StateNew})
			w := f.post(t, it, "reply", `{"words":"Explain the job."}`, 409)
			var refusal struct {
				Error  string `json:"error"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &refusal); err != nil {
				t.Fatal(err)
			}
			if refusal.Error != (run.WriteAccessRefusal{}).Error() || refusal.Reason != "conflict" {
				t.Fatalf("reply refusal=%+v", refusal)
			}
			_ = f.read(t, it)
			saved, err := f.st.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Talk != "" || len(saved.Thread) != 0 || factory.JobSpent(saved) != 0 {
				t.Fatal("refused reply started work")
			}
		})
	}
}
