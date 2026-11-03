package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

type workstreamForbiddenFactory struct {
	t *testing.T
}

func (d workstreamForbiddenFactory) Add(context.Context, factory.Item) (int, error) {
	d.t.Fatal("propose_jobs called Factory.Add")
	return 0, nil
}

func (d workstreamForbiddenFactory) Preview(context.Context, int, ItemChange) (factory.Item, factory.Item, error) {
	d.t.Fatal("propose_jobs called FactoryItem.Preview")
	return factory.Item{}, factory.Item{}, nil
}

func (d workstreamForbiddenFactory) Apply(context.Context, int, ItemChange) (factory.Item, error) {
	d.t.Fatal("propose_jobs called FactoryItem.Apply")
	return factory.Item{}, nil
}

func (d workstreamForbiddenFactory) EditRun(context.Context, string, factory.RunEdit) (factory.Item, []string, error) {
	d.t.Fatal("propose_jobs called FactoryRun.EditRun")
	return factory.Item{}, nil, nil
}

func (d workstreamForbiddenFactory) StartRun(context.Context, string) (string, error) {
	d.t.Fatal("propose_jobs called FactoryRun.StartRun")
	return "", nil
}

func (d workstreamForbiddenFactory) BankStage(context.Context, string, factory.Kind, string) error {
	d.t.Fatal("propose_jobs called Recipe.BankStage")
	return nil
}

func (d workstreamForbiddenFactory) BankPolicy(context.Context, string, string) error {
	d.t.Fatal("propose_jobs called Recipe.BankPolicy")
	return nil
}

func (d workstreamForbiddenFactory) BankHabit(context.Context, string, string) error {
	d.t.Fatal("propose_jobs called Recipe.BankHabit")
	return nil
}

func (d workstreamForbiddenFactory) Snapshot(context.Context) (factory.Snapshot, error) {
	d.t.Fatal("propose_jobs called Floor.Snapshot")
	return factory.Snapshot{}, nil
}

func (d workstreamForbiddenFactory) Mark(context.Context, []int, bool) error {
	d.t.Fatal("propose_jobs called Floor.Mark")
	return nil
}

func (d workstreamForbiddenFactory) Chat(string) JobChat {
	d.t.Fatal("propose_jobs called JobChat.Chat")
	return nil
}

func (d workstreamForbiddenFactory) Spend(float64) {
	d.t.Fatal("propose_jobs called JobBudget.Spend")
}

func (d workstreamForbiddenFactory) Refusal() string {
	d.t.Fatal("propose_jobs called JobBudget.Refusal")
	return ""
}

func (d workstreamForbiddenFactory) Report(context.Context, factory.StageResult) error {
	d.t.Fatal("propose_jobs called Stage.Report")
	return nil
}

func (d workstreamForbiddenFactory) Edit(context.Context, factory.PlanEdit) error {
	d.t.Fatal("propose_jobs called Stage.Edit")
	return nil
}

// Contract (c) puts failing doors behind every Factory capability during the call.
func TestWorkstreamJobsDoNotCallAnyFactoryDoor(t *testing.T) {
	door := &workstreamFakeDoor{}
	forbidden := workstreamForbiddenFactory{t: t}
	a := &Agent{config: Config{Workstream: door, Factory: forbidden, FactoryItem: forbidden, FactoryRun: forbidden, Recipe: forbidden, Floor: forbidden, JobChat: forbidden, JobBudget: forbidden, Stage: forbidden}}
	for _, tool := range a.workstreamTools() {
		if tool.Name != "propose_jobs" {
			continue
		}
		got, failed, err := tool.Execute(context.Background(), json.RawMessage(`{"jobs":[{"title":"Draft"}]}`))
		if err != nil || failed || got != "Added 1 draft jobs to wave 1 of the plan. They are drafts: nothing runs until the person launches them." {
			t.Fatalf("draft proposal: %q %v %v", got, failed, err)
		}
		if len(door.saves) != 1 || door.saves[0].Waves[0].Items[0].Kind != "draft" {
			t.Fatalf("not a draft: %+v", door.saves)
		}
		return
	}
	t.Fatal("missing propose_jobs")
}
