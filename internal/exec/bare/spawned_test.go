package bare

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// A bash call tells its seat which process group it started, so the record around
// the call can end that group if codeaf dies first. The group leads itself: its id
// is its leader's pid.
func TestABashCallReportsTheGroupItStarted(t *testing.T) {
	var bash Tool
	for _, tool := range AllTools(t.TempDir()) {
		if tool.Name == "bash" {
			bash = tool
		}
	}
	ctx, spawns := executor.WithSpawns(context.Background())
	var groups []int
	spawns.Watch(func(pgid int) { groups = append(groups, pgid) })
	args, _ := json.Marshal(map[string]any{"command": "true"})

	if _, bad, err := bash.Execute(ctx, args); bad || err != nil {
		t.Fatalf("bash failed: %v %v", bad, err)
	}
	if len(groups) != 1 || groups[0] <= 0 {
		t.Fatalf("groups %v, want one positive process group id", groups)
	}
}
