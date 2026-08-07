package main

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/tui"
)

// The Self place reads the craft repository the same way the resident does —
// through the ordinary craft package, at the directory beside the brain. It is
// an optional Commander capability, like the settings registry: a build or an
// entry point without a craft repository still opens Self, and the Crafts row
// reads an honest zero instead of the place refusing to open.
//
// Everything here is a read. Self never saves, reverts, or runs a workflow;
// those belong to the resident, which is the one writer the repository has.

// selfCraftLimit bounds a history read. A workflow's recent past explains its
// present; the rest is archaeology and stays where `git log` in the craft
// directory can answer it.
const selfCraftLimit = 8

// The repository handle is opened once per process rather than per poll:
// craft.Open shells git three times to settle identity, and Self polls while
// it is on screen. Opening an existing repository is idempotent, so this is a
// cache of the handle and never a second initialization.
var (
	selfCraftOnce sync.Once
	selfCraftRepo *craft.Repo
)

func (c *chatCommander) craftRepo() *craft.Repo {
	if c == nil || strings.TrimSpace(c.database) == "" {
		return nil
	}
	dir := filepath.Join(filepath.Dir(c.database), "craft")
	selfCraftOnce.Do(func() {
		repo, err := craft.Open(dir)
		if err != nil {
			return
		}
		selfCraftRepo = repo
	})
	return selfCraftRepo
}

// Crafts is the catalogue as Self lists it. A workflow that will not parse is
// already skipped by the repository with a reported reason; the surface shows
// what it could read rather than nothing.
func (c *chatCommander) Crafts() ([]tui.CraftSummary, error) {
	repo := c.craftRepo()
	if repo == nil {
		return nil, nil
	}
	summaries, err := repo.List()
	entries := make([]tui.CraftSummary, 0, len(summaries))
	for _, summary := range summaries {
		entry := tui.CraftSummary{
			Name:        summary.Name,
			Description: summary.Description,
			Commit:      summary.Commit,
			When:        summary.When,
		}
		// The step count is the one number the catalogue does not carry, and
		// it is the number that says how big a shape this is.
		if workflow, loadErr := repo.Load(summary.Name); loadErr == nil {
			entry.Steps = len(workflow.Steps)
		}
		entries = append(entries, entry)
	}
	return entries, err
}

// CraftDetail opens one workflow: its steps in file order, the bounds a run
// will actually obey, and the versions behind it.
func (c *chatCommander) CraftDetail(name string) (tui.CraftDetail, bool) {
	repo := c.craftRepo()
	if repo == nil {
		return tui.CraftDetail{}, false
	}
	workflow, err := repo.Load(name)
	if err != nil {
		return tui.CraftDetail{}, false
	}
	detail := tui.CraftDetail{
		Name:        workflow.Name,
		Description: workflow.Description,
		Commit:      workflow.Commit,
		Dir:         repo.Dir(),
		CostUSD:     workflow.Limits.CostUSD,
		WallClock:   workflow.Limits.WallClock,
	}
	if detail.CostUSD == 0 {
		detail.CostUSD = craft.DefaultRunBudgetUSD
	}
	if detail.WallClock == 0 {
		detail.WallClock = craft.DefaultWallClock
	}
	for _, step := range workflow.Steps {
		entry := tui.CraftStep{
			ID:    step.ID,
			Brief: step.Brief,
			Needs: append([]string(nil), step.Needs...),
			Model: step.Model,
			Skill: step.Skill,
		}
		if step.Verify != nil {
			entry.Verify = step.Verify.Script
		}
		detail.Steps = append(detail.Steps, entry)
	}
	if versions, historyErr := repo.History(name, selfCraftLimit); historyErr == nil {
		for _, version := range versions {
			detail.History = append(detail.History, tui.CraftVersion{
				Commit: version.Commit, When: version.When, Subject: version.Subject,
			})
		}
	}
	return detail, true
}
