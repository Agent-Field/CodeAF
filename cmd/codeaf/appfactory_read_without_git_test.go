package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// Read-only commands keep working when the runner cannot read Git metadata.
// A failed Git read is not a comparison base that needs preserving.
func TestFactoryReadOnlyCheckWithoutGitMetadata(t *testing.T) {
	g := newRunRig(t)
	dir := t.TempDir()
	runner := factoryrun.New(factoryrun.Options{
		Store:   g.st,
		RepoDir: func(string) string { return dir },
		Exec: map[factory.StageKind]factoryrun.Executor{
			factory.StageCheck: factoryrun.NewCheckExecutor(factoryrun.CheckOptions{}),
		},
	})
	stopAll(t, g.st, runner)
	id, err := g.st.Add(context.Background(), factory.Item{
		Repo: "api", Title: "Read without Git metadata", Tier: factory.TierOwner,
		Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, On: true, Ask: "true", Until: factory.UntilGreen, Max: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Launch(id); err != nil {
		t.Fatal(err)
	}
	job := g.waitFor(t, id, factory.StateLanded)
	if len(job.Stream.Phases) != 1 || job.Stream.Phases[0].State != factory.PhaseDone {
		t.Fatalf("read-only check did not finish: %+v", job.Stream)
	}
}
