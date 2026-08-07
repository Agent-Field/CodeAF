package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const releaseNotesFile = `name: release-notes
description: turn a range of commits into user-facing release notes
params:
  - name: since
    description: the tag to start from
    required: true
steps:
  - id: collect
    brief: Collect the commits since {{since}} and group them by area.
  - id: write
    brief: Write the release notes from the grouped commits.
    needs: [collect]
`

// openCraftRepo is the real repository, in a temporary directory. The forge's
// whole product is a version in git, so nothing here is faked: a commit
// message that does not carry its evidence is the failure this catches.
func openCraftRepo(t *testing.T) *craft.Repo {
	t.Helper()
	repo, err := craft.Open(t.TempDir())
	if err != nil {
		t.Skipf("craft repository unavailable: %v", err)
	}
	return repo
}

// forgeJob lands one finished job and distills it, which is the only path a
// craft is ever written on.
func forgeJob(t *testing.T, graph *store.Store, reconciler *Reconciler, id, intent string, fail bool) {
	t.Helper()
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: id, Brief: intent, Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "forge", Intent: intent}); err != nil {
		t.Fatalf("splice %s: %v", id, err)
	}
	claim, won, err := graph.Claim(id, "worker")
	if err != nil || !won {
		t.Fatalf("claim %s: won=%t err=%v", id, won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if fail {
		err = graph.Fail(claim, "the verifier never passed")
	} else {
		err = graph.Complete(claim, "the notes are in /tmp/notes.md")
	}
	if err != nil {
		t.Fatalf("settle %s: %v", id, err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}
}

// A job whose shape looked reusable leaves a version behind, and that version
// says what it was forged from. git history IS the version history: the
// evidence lives in the commit or nowhere.
func TestDistilledCraftIsSavedWithItsEvidence(t *testing.T) {
	graph := openStore(t)
	repo := openCraftRepo(t)
	if _, err := graph.TouchSeen("tui", "forge", store.SeenAttached); err != nil {
		t.Fatal(err)
	}
	distill := func(context.Context, string, string, bool) ([]Learned, error) {
		return []Learned{
			{Scope: "repo:aforge", Kind: store.FactLesson, Body: "the changelog lives in docs/"},
			{Craft: &CraftCandidate{Name: "release-notes", YAML: releaseNotesFile}},
		}, nil
	}
	reconciler := New(graph, nil, nil).
		WithDistiller(distill).
		WithCraftMind(NewCraftMind(repo, repo.Dir(), nil, nil))
	forgeJob(t, graph, reconciler, "notes", "write the release notes since v1.2", false)

	workflow, err := repo.Load("release-notes")
	if err != nil {
		t.Fatalf("load the forged craft: %v", err)
	}
	if len(workflow.Steps) != 2 || workflow.Commit == "" {
		t.Fatalf("forged workflow = %+v", workflow)
	}
	versions, err := repo.History("release-notes", 2)
	if err != nil || len(versions) != 1 {
		t.Fatalf("history = %+v err=%v", versions, err)
	}
	if !strings.Contains(versions[0].Subject, "release-notes: forged from job notes") ||
		!strings.Contains(versions[0].Subject, "write the release notes since v1.2") {
		t.Fatalf("commit subject = %q", versions[0].Subject)
	}
	// The ordinary memory rode out of the same call and still landed.
	facts, err := graph.ActiveFacts("repo:aforge", 10)
	if err != nil || len(facts) != 1 {
		t.Fatalf("notebook = %+v err=%v", facts, err)
	}
	if !strings.Contains(craftSessionMessages(t, graph), "⚒ forged: the release-notes craft") {
		t.Fatalf("the forge was invisible: %q", craftSessionMessages(t, graph))
	}
}

// The parser's errors were written for this reader. One repair round is what
// they are worth: the errors name every breach at once, so a writer that can
// use them fixes the file in one edit.
func TestInvalidCraftGetsOneRepairRound(t *testing.T) {
	graph := openStore(t)
	repo := openCraftRepo(t)
	distill := func(context.Context, string, string, bool) ([]Learned, error) {
		return []Learned{{Craft: &CraftCandidate{Name: "release-notes", YAML: `name: release-notes
steps:
  - id: collect
    breif: Collect the commits.
`}}}, nil
	}
	var problems []string
	repair := func(_ context.Context, candidate CraftCandidate, problem string) (CraftCandidate, error) {
		problems = append(problems, problem)
		return CraftCandidate{Name: candidate.Name, YAML: releaseNotesFile}, nil
	}
	reconciler := New(graph, nil, nil).
		WithDistiller(distill).
		WithCraftMind(NewCraftMind(repo, repo.Dir(), nil, repair))
	forgeJob(t, graph, reconciler, "notes", "write the release notes since v1.2", false)

	if len(problems) != 1 {
		t.Fatalf("repair rounds = %d, want exactly one", len(problems))
	}
	if !strings.Contains(problems[0], "breif") || !strings.Contains(problems[0], "step") {
		t.Fatalf("the repair was not given the parser's own words: %q", problems[0])
	}
	if _, err := repo.Load("release-notes"); err != nil {
		t.Fatalf("the repaired craft was not saved: %v", err)
	}
}

// A candidate still invalid after its one round is dropped where the next
// attempt will find it. The user never asked for a workflow, so a failed
// attempt at one is the resident's own business to remember.
func TestInvalidCraftAfterRepairBecomesALesson(t *testing.T) {
	graph := openStore(t)
	repo := openCraftRepo(t)
	broken := "name: release-notes\nsteps:\n  - id: collect\n"
	distill := func(context.Context, string, string, bool) ([]Learned, error) {
		return []Learned{{Craft: &CraftCandidate{Name: "release-notes", YAML: broken}}}, nil
	}
	rounds := 0
	repair := func(_ context.Context, candidate CraftCandidate, _ string) (CraftCandidate, error) {
		rounds++
		return CraftCandidate{Name: candidate.Name, YAML: broken}, nil
	}
	reconciler := New(graph, nil, nil).
		WithDistiller(distill).
		WithCraftMind(NewCraftMind(repo, repo.Dir(), nil, repair))
	forgeJob(t, graph, reconciler, "notes", "write the release notes since v1.2", false)

	if rounds != 1 {
		t.Fatalf("repair rounds = %d, want exactly one", rounds)
	}
	if _, err := repo.Load("release-notes"); err == nil {
		t.Fatal("an invalid craft reached the shelf")
	}
	facts, err := graph.ActiveFacts(CraftSurvivalKey("release-notes"), 10)
	if err != nil || len(facts) != 1 {
		t.Fatalf("lesson facts = %+v err=%v", facts, err)
	}
	if facts[0].Kind != store.FactLesson ||
		!strings.Contains(facts[0].Body, "tried to forge the release-notes craft") ||
		!strings.Contains(facts[0].Body, "neither a brief nor a verify") {
		t.Fatalf("lesson = %+v", facts[0])
	}
}

// A craft run that goes wrong is the only reason a craft changes. The distiller
// is told which version ran and how it went, and its updated file lands as the
// next version with the reason in the commit.
func TestCraftRefinementSavesASecondVersion(t *testing.T) {
	graph := openStore(t)
	repo := openCraftRepo(t)
	first, err := craft.Parse([]byte(releaseNotesFile))
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.Save(first, "forged from job earlier: write the release notes")
	if err != nil {
		t.Fatal(err)
	}

	refined := strings.Replace(releaseNotesFile,
		"    brief: Write the release notes from the grouped commits.",
		"    brief: Write the release notes from the grouped commits, newest first.", 1)
	var outcomes []string
	distill := func(_ context.Context, _, outcome string, _ bool) ([]Learned, error) {
		outcomes = append(outcomes, outcome)
		return []Learned{{Craft: &CraftCandidate{Name: "release-notes", YAML: refined}}}, nil
	}
	reconciler := New(graph, nil, nil).
		WithDistiller(distill).
		WithCraftMind(NewCraftMind(repo, repo.Dir(), nil, nil))

	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "run", Brief: "write the release notes since v1.2", Stage: 1,
	}}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "forge", Intent: "write the release notes since v1.2",
		Craft: "release-notes@" + commit,
	}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("run", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fail(claim, "the notes came out oldest first"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}

	if len(outcomes) != 1 || !strings.Contains(outcomes[0], "ran the release-notes craft") ||
		!strings.Contains(outcomes[0], "it failed") {
		t.Fatalf("the distiller was not told what ran: %+v", outcomes)
	}
	versions, err := repo.History("release-notes", 5)
	if err != nil || len(versions) != 2 {
		t.Fatalf("history = %+v err=%v", versions, err)
	}
	if !strings.Contains(versions[0].Subject, "release-notes: refined after a failed run") {
		t.Fatalf("refinement subject = %q", versions[0].Subject)
	}
	if !strings.Contains(versions[0].Body, "from job run") {
		t.Fatalf("refinement body = %q", versions[0].Body)
	}
	workflow, err := repo.Load("release-notes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(workflow.Steps[1].Brief, "newest first") {
		t.Fatalf("the refined step did not land: %+v", workflow.Steps[1])
	}
	// The failed run still counts against the version that ran it.
	if record := reconciler.craftSurvival("release-notes@" + commit); record.Against != 1 {
		t.Fatalf("survival = %+v", record)
	}
}

func craftSessionMessages(t *testing.T, graph *store.Store) string {
	t.Helper()
	messages, err := graph.Messages("forge", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, message := range messages {
		bodies = append(bodies, message.Body)
	}
	return strings.Join(bodies, "\n---\n")
}
