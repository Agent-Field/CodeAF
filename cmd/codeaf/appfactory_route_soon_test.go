package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/route"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// noPoll is a poll source that never arrives: a factory with no watched
// GitHub repository, whose poll never runs the routing pass for it.
func noPoll(context.Context) *factorygithub.Source { return nil }

// routedBy is the routing rule on job id, "" while none routed it.
func routedBy(t *testing.T, floor *store.Store, id int) string {
	t.Helper()
	it, err := floor.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return it.Route
}

// A JOB ALREADY WAITING IS ROUTED WHEN THE RULES CHANGE AND WHEN THE RUNNER
// COMES ON, not on the next entry. The factory watches no repository, so no
// poll runs the pass: saving a table routes the job filed before it, and a
// runner that comes on routes the job filed while it was off.
func TestWaitingJobsAreRoutedWhenTheTableIsSavedAndWhenTheRunnerStarts(t *testing.T) {
	quickReads(t)
	world := newReadLoopsWorld(t)
	s := readLoopsSetup(t, world, readLoopsPin)
	s.backend.local.pollSource = noPoll
	first := s.start()
	if !first.Runner {
		t.Fatalf("the start did not bring the runner on: %+v", first)
	}
	host := s.backend.local.host
	job, err := host.floor.Create(factory.Item{Origin: "jira", SourceRef: "jira:FL-1", Kind: factory.KindIssue, Tier: factory.TierOwner, State: factory.StateNew, Title: "Filed before the rule"})
	if err != nil {
		t.Fatal(err)
	}
	settle()
	if got := routedBy(t, host.floor, job.ID); got != "" {
		t.Fatalf("routed by %q with no table", got)
	}
	rules := []route.Rule{{Name: "jira", When: route.Match{Sources: []string{"jira"}}, Then: route.Action{Repo: "acme/api", Start: route.StartBacklog}}}
	var saved appwire.FactoryRoutes
	s.must("factory.routes_set", map[string]any{"factory": first.Factory, "rules": rules}, &saved)
	eventually(t, "the saved table routes the waiting job", func() bool { return routedBy(t, host.floor, job.ID) == "jira" })

	// THE RUNNER COMING ON ROUTES WHAT WAS FILED WHILE IT WAS OFF.
	root := host.floor.Root()
	s.backend.Close()
	floor, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	later, err := floor.Create(factory.Item{Origin: "jira", SourceRef: "jira:FL-2", Kind: factory.KindIssue, Tier: factory.TierOwner, State: factory.StateNew, Title: "Filed while off"})
	if err != nil {
		t.Fatal(err)
	}
	w, _ := reopened(t, s.backend.factoryHub())
	closeQuietly(t, w.backend)
	w.backend.local.pollSource = noPoll
	w.backend.local.newIntake = world.intake
	var resumed appwire.FactoryLocalStatus
	w.must("factory.local_status", map[string]any{"caller": "w1"}, &resumed)
	if !resumed.Runner {
		t.Fatalf("resume: %+v", resumed)
	}
	next := w.backend.local.host
	eventually(t, "the runner coming on routes the job filed while it was off", func() bool { return routedBy(t, next.floor, later.ID) == "jira" })
}
