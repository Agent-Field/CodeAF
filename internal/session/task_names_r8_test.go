package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

type sameNameTaskComputers struct{ testTaskComputers }

func (sameNameTaskComputers) List(context.Context) ([]TaskComputer, error) {
	return []TaskComputer{
		{Device: "dev_0123456789abcdef0123456789ab1111", Name: "mac mini", CanTakeTasks: true},
		{Device: "dev_0123456789abcdef0123456789ab2222", Name: "mac mini", CanTakeTasks: true},
	}, nil
}

// R7-12 and contract 6 require an ambiguous tool name to list distinct choices.
func TestTwoComputersWithOneNameAreToldApart(t *testing.T) {
	taskComputerChoiceHome(t)
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.ProfileDir = t.TempDir()
		c.TaskComputers = sameNameTaskComputers{}
		if err := config.WriteTaskRunsOn(c.ProfileDir, "dev_0123456789abcdef0123456789ab1111"); err != nil {
			t.Fatal(err)
		}
	})
	args := taskOnOtherComputer()
	args.Computer = "mac mini"
	raw, _ := json.Marshal(args)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	text, bad, err := a.stageTask(ctx, raw).Commit(ctx)
	if err != nil || !bad || !strings.Contains(text, "more than one") || !strings.Contains(text, "mac mini #1111") || !strings.Contains(text, "mac mini #2222") {
		t.Fatalf("ambiguous proposal: %q, %v, %v", text, bad, err)
	}
	p, err := a.resolveTaskComputer(context.Background(), "mac mini #2222", taskStand{mode: TaskModeWorktree}, "", false)
	if err != nil || p.computer == nil || p.computer.Device != "dev_0123456789abcdef0123456789ab2222" {
		t.Fatalf("second computer = %+v, %v", p.computer, err)
	}
}
