package session

import (
	"context"
	"errors"
	"testing"
)

type fakeStagePRBaseDoor struct {
	fakeStageDoor
	enabled bool
	calls   int
	err     error
}

func (d *fakeStagePRBaseDoor) CanSyncPRBase() bool { return d.enabled }

func (d *fakeStagePRBaseDoor) SyncPRBase(context.Context) (string, error) {
	d.calls++
	return "Merged the current base into the PR checkout.", d.err
}

func TestStagePRBaseSyncRequiresEnabledDoor(t *testing.T) {
	const name = "factory_sync_pr_base"
	for _, door := range []StageDoor{nil, &fakeStageDoor{}, &fakeStagePRBaseDoor{}} {
		if stageAgent(t, door).offers(name) {
			t.Fatalf("%T offers %s without sync authority", door, name)
		}
	}
	if !stageAgent(t, &fakeStagePRBaseDoor{enabled: true}).offers(name) {
		t.Fatalf("enabled Follow PR stage lacks %s", name)
	}
	reader, node, _ := appNodeFixture(t, true)
	node.spec.quick.reading = true
	reader.config.Stage = &fakeStagePRBaseDoor{enabled: true}
	reader.tools = reader.belt()
	if reader.offers(name) {
		t.Fatalf("read-only task offers %s", name)
	}
	if got := ActionCategoryForTool(name); got != ActionTransfer {
		t.Fatalf("%s action family = %q, want %q", name, got, ActionTransfer)
	}
}

func TestStagePRBaseSyncUsesOnlyTheBoundCheckout(t *testing.T) {
	door := &fakeStagePRBaseDoor{enabled: true}
	tool := stageToolNamed(t, stageAgent(t, door), "factory_sync_pr_base")
	for _, args := range []string{`{"branch":"dev"}`, `null`, `[]`} {
		if _, isErr := callStageTool(t, tool, args); !isErr {
			t.Errorf("accepted arguments %s", args)
		}
	}
	if door.calls != 0 {
		t.Fatal("invalid arguments reached the runner")
	}
	if out, isErr := callStageTool(t, tool, `{}`); isErr || out != "Merged the current base into the PR checkout." || door.calls != 1 {
		t.Fatalf("sync returned %q, error %v, calls %d", out, isErr, door.calls)
	}
	door.err = errors.New("The PR head changed. Refresh it first.")
	if out, isErr := callStageTool(t, tool, `{}`); !isErr || out != door.err.Error() || door.calls != 2 {
		t.Fatalf("refusal returned %q, error %v, calls %d", out, isErr, door.calls)
	}
	door.enabled = false
	if _, isErr := callStageTool(t, tool, `{}`); !isErr || door.calls != 2 {
		t.Fatalf("sync remained callable after losing authority: error %v, calls %d", isErr, door.calls)
	}
}
