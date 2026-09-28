package session

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	store "github.com/Agent-Field/codeaf/internal/store"
)

// activeSkill records a candidate and activates it in one step — the only
// transition that puts a skill on the shelf the catalog reads (store's
// [Store.SkillFacts] returns ACTIVE skills, and a candidate is deliberately
// absent from every retrieval surface until its trial goes green).
func activeSkill(t *testing.T, brain *store.Store, scope, body, artifact string) store.Fact {
	t.Helper()
	candidate, err := brain.RecordSkillCandidate(store.RootID, scope, body, artifact)
	if err != nil {
		t.Fatalf("record skill candidate %q: %v", body, err)
	}
	if err := brain.ActivateSkill(candidate.Seq, artifact, ""); err != nil {
		t.Fatalf("activate skill %q: %v", body, err)
	}
	return candidate
}

// bulletLines is the described skill lines and nothing else: the section
// header, the routing sentence, the names-only line and the "- … and N more
// skills" overflow line are all not one described skill.
func bulletLines(catalog string) []string {
	bullets := make([]string, 0)
	for _, line := range strings.Split(catalog, "\n") {
		if strings.HasPrefix(line, "- ") && !strings.Contains(line, "more skills") && !strings.HasPrefix(line, skillCatalogNamesLead) {
			bullets = append(bullets, line)
		}
	}
	return bullets
}

// THE CATALOG NAMES EVERY SKILL A SHELF OF ORDINARY SIZE HOLDS. It used to
// window the shelf to eight lines scored against the workspace path, so a
// person with forty skills was shown eight, chosen by a path that says nothing
// about the message — and the model could not pick a skill it was never shown.
// Sixty skills with descriptions of an ordinary length all get their line.
func TestSkillCatalogNamesEverySkillOnAnOrdinaryShelf(t *testing.T) {
	brain := openTestBrain(t)
	for index := 0; index < 60; index++ {
		activeSkill(t, brain,
			"harness:claude",
			"skill number "+strconv.Itoa(index)+" drafts, checks and formats one kind of document for review",
			"/shelf/skill-"+strconv.Itoa(index),
		)
	}
	catalog := renderSkillCatalog(Config{Memory: brain, Workspace: "/srv/app"})
	if got := len(bulletLines(catalog)); got != 60 {
		t.Fatalf("catalog describes %d of sixty skills:\n%s", got, catalog)
	}
	if strings.Contains(catalog, "more skills") || strings.Contains(catalog, skillCatalogNamesLead) {
		t.Fatalf("a shelf that fits was cut:\n%s", catalog)
	}
}

// A SHELF PAST THE BUDGET IS BOUNDED BY BYTES, and nothing on it vanishes
// silently: the described lines stop at the budget, the names of the rest are
// listed alone within their own budget, and whatever is past both is counted.
// Every description is clipped to one line of its own budget.
func TestSkillCatalogIsBoundedByBytes(t *testing.T) {
	brain := openTestBrain(t)
	// Long enough to be clipped, and short enough for the store's own limit on
	// one fact.
	long := strings.Repeat("a very thorough description of what this skill is for ", 8)
	for index := 0; index < 300; index++ {
		activeSkill(t, brain, "harness:claude", long, "/shelf/skill-with-a-longish-name-"+strconv.Itoa(1000+index))
	}
	catalog := renderSkillCatalog(Config{Memory: brain, Workspace: "/srv/app"})
	bullets := bulletLines(catalog)
	spent := 0
	for _, line := range bullets {
		spent += len(line) + 1
		doc := line[strings.Index(line, ": ")+2:]
		if utf8.RuneCountInString(doc) > skillCatalogDocRunes {
			t.Fatalf("a description was not clipped to %d runes: %q", skillCatalogDocRunes, doc)
		}
	}
	if spent > skillCatalogBudget {
		t.Fatalf("the described lines cost %d bytes, over the %d budget", spent, skillCatalogBudget)
	}
	if !strings.Contains(catalog, skillCatalogNamesLead) {
		t.Fatalf("the skills past the budget are not named:\n%s", catalog)
	}
	if !strings.Contains(catalog, "more skills") {
		t.Fatalf("the skills past both budgets are not counted:\n%s", catalog)
	}
	if len(catalog) > len(skillCatalogHeader)+skillCatalogBudget+skillCatalogNamesBudget+len(skillCatalogNamesLead)+200 {
		t.Fatalf("the catalog is %d bytes, past both budgets", len(catalog))
	}
}

// THE SAME SHELF RENDERS THE SAME BYTES, whatever order the store hands it
// back in and whether a skill was just used: the section sits in the cached
// prefix, and a reordering would cost every byte cached behind it.
func TestSkillCatalogIsStableAcrossRenders(t *testing.T) {
	brain := openTestBrain(t)
	activeSkill(t, brain, "harness:claude", "writes release notes", "/shelf/zeta-notes")
	first := activeSkill(t, brain, "harness:codex", "formats a spreadsheet", "/shelf/alpha-sheets")
	activeSkill(t, brain, "harness:agents", "reviews a pull request", "/shelf/mid-review")
	before := renderSkillCatalog(Config{Memory: brain, Workspace: "/srv/app"})
	// Reading a skill's accessors is what records a use (store's
	// SkillFactAccessors), which is what moved the old recency bonus.
	if _, _, _, _, err := brain.SkillFactAccessors(first.Seq); err != nil {
		t.Fatalf("use the skill: %v", err)
	}
	after := renderSkillCatalog(Config{Memory: brain, Workspace: "/srv/app"})
	if before != after {
		t.Fatalf("one use reordered the catalog:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	bullets := bulletLines(before)
	if len(bullets) != 3 || !strings.Contains(bullets[0], "alpha-sheets") || !strings.Contains(bullets[2], "zeta-notes") {
		t.Fatalf("the catalog is not in name order:\n%s", before)
	}
}

// A WORKER THAT CANNOT FETCH A SKILL IS NOT SHOWN THE MENU. The section names
// `use_skill`, and a node on the floor of its tree has no such verb.
func TestSkillCatalogIsAbsentWhereUseSkillIs(t *testing.T) {
	brain := openTestBrain(t)
	activeSkill(t, brain, "harness:claude", "writes release notes", "/shelf/notes")
	floor := Config{Memory: brain, Workspace: "/srv/app", InTask: true}
	if floor.mayProposeTask() {
		t.Skip("this shape may hand work out, so it carries use_skill")
	}
	if got := renderSkillCatalog(floor); got != "" {
		t.Fatalf("a belt without use_skill was shown the catalog:\n%s", got)
	}
}

// TestSkillCatalogIsEmptyWithoutSkills: no active skills is zero bytes — the
// whole reason the section is conditional rather than a heading that names
// nothing on every request.
func TestSkillCatalogIsEmptyWithoutSkills(t *testing.T) {
	if catalog := renderSkillCatalog(Config{}); catalog != "" {
		t.Fatalf("a config with no store rendered %q, want the empty string", catalog)
	}
	brain := openTestBrain(t)
	if catalog := renderSkillCatalog(Config{Memory: brain, Workspace: "/srv/app"}); catalog != "" {
		t.Fatalf("an empty shelf rendered %q, want the empty string", catalog)
	}
	// A CANDIDATE IS NOT ON THE SHELF: it is recorded and never activated, so
	// the catalog must not offer a skill the trial never promoted.
	if _, err := brain.RecordSkillCandidate(store.RootID, "domain:alpha", "candidate not yet promoted", "/shelf/pending"); err != nil {
		t.Fatalf("record candidate: %v", err)
	}
	if catalog := renderSkillCatalog(Config{Memory: brain, Workspace: "/srv/app"}); catalog != "" {
		t.Fatalf("a shelf of only candidates rendered %q, want the empty string", catalog)
	}
}

// TestSkillCatalogSurfacesRelevantSkills: the scorer ranks a skill whose scope
// or doc shares words with the workspace above an unrelated shelf, so the ones
// kept under the window are the likely ones.
func TestSkillCatalogSurfacesRelevantSkills(t *testing.T) {
	brain := openTestBrain(t)
	// Ten unrelated skills, inserted FIRST so seq order would show them first.
	for index := 0; index < 10; index++ {
		activeSkill(t, brain,
			"domain:unrelated",
			"unrelated procedure number "+strconv.Itoa(index),
			"/shelf/unrelated-"+strconv.Itoa(index),
		)
	}
	// One scoped to the workspace, one whose doc shares a word with it.
	activeSkill(t, brain, "repo:/srv/app", "scoped to the working directory", "/shelf/scoped-skill")
	activeSkill(t, brain, "domain:misc", "inspects the app before delivery", "/shelf/app-check")

	catalog := renderSkillCatalog(Config{Memory: brain, Workspace: "/srv/app"})
	for _, want := range []string{"scoped-skill", "app-check"} {
		if !strings.Contains(catalog, want) {
			t.Errorf("relevant skill %q is missing from the catalog:\n%s", want, catalog)
		}
	}
	// And they are ranked ABOVE the unrelated tail, which is the whole point of
	// scoring: the first bullet is one of the two relevant ones.
	bullets := bulletLines(catalog)
	if len(bullets) == 0 {
		t.Fatal("catalog rendered no bullets")
	}
	if !strings.Contains(bullets[0], "scoped-skill") && !strings.Contains(bullets[0], "app-check") {
		t.Errorf("the top bullet is not a relevant skill: %q", bullets[0])
	}
}

func TestSkillCatalogDoesNotFalselyMatchRepoScopePaths(t *testing.T) {
	brain := openTestBrain(t)
	activeSkill(t, brain, "repo:/Users/bob/backend", "backend procedures", "/shelf/backend-skill")

	facts, err := brain.SkillFacts(store.FactActive, 10)
	if err != nil {
		t.Fatalf("SkillFacts: %v", err)
	}
	scored := scoreSkills(facts, "/Users/alice/frontend")
	if len(scored) != 1 {
		t.Fatalf("expected 1 scored skill, got %d", len(scored))
	}
	if scored[0].score >= 100 {
		t.Errorf("expected score < 100 for unrelated repo path, got %d", scored[0].score)
	}
}

// TestSkillCatalogAlwaysCarriesItsHeader: whenever there is anything to show,
// the section heading and the routing sentence are present — the model is told
// this is the shelf, that skills suited to a message are attached to it, and
// that use_skill reaches any of them by name.
func TestSkillCatalogAlwaysCarriesItsHeader(t *testing.T) {
	brain := openTestBrain(t)
	activeSkill(t, brain, "domain:alpha", "one skill on the shelf", "/shelf/only-skill")

	catalog := renderSkillCatalog(Config{Memory: brain, Workspace: "/srv/app"})
	if !strings.HasPrefix(catalog, "## Available skills\n") {
		t.Fatalf("catalog does not open on its heading:\n%s", catalog)
	}
	if !strings.Contains(catalog, "fetch it with `use_skill` (mode get) and follow it before starting") {
		t.Fatalf("catalog does not carry the routing sentence:\n%s", catalog)
	}
	if !strings.Contains(catalog, "- only-skill: one skill on the shelf") {
		t.Fatalf("catalog does not name the skill and its doc:\n%s", catalog)
	}
}

// TestSkillCatalogRendersAnImportedSkillsNameAndDoc: an agentskills folder
// reaches the shelf as a fact like any other, and the catalog — which carries
// a name and a doc and never a path — needs no change for it: the folder's
// base name is the name, the Body is the doc, and no SKILL.md path leaks
// into a section whose whole budget is one line per skill.
func TestSkillCatalogRendersAnImportedSkillsNameAndDoc(t *testing.T) {
	brain := openTestBrain(t)
	agentskillsShelfSkill(t, brain, "pdf-extract", "extract pages from PDFs")

	catalog := renderSkillCatalog(Config{Memory: brain, Workspace: "/srv/app"})
	if !strings.Contains(catalog, "- pdf-extract: extract pages from PDFs") {
		t.Fatalf("the imported skill's name and doc are missing from the catalog:\n%s", catalog)
	}
	if strings.Contains(catalog, "SKILL.md") {
		t.Fatalf("the catalog carries a path, which it must not:\n%s", catalog)
	}
}

// TestSkillCatalogRendersOnThePageWhenSkillsExist: the section is wired into
// renderSystemAt, so a conversation with a shelf reads it and one without does
// not.
func TestSkillCatalogRendersOnThePageWhenSkillsExist(t *testing.T) {
	brain := openTestBrain(t)
	activeSkill(t, brain, "domain:alpha", "audits a delivery", "/shelf/delivery-audit")

	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	withShelf := renderSystemAt(Config{Memory: brain, Workspace: "/srv/app"}, now)
	if !strings.Contains(withShelf, "## Available skills") {
		t.Fatalf("a conversation with a shelf does not read the catalog:\n%s", withShelf)
	}
	if !strings.Contains(withShelf, "delivery-audit") {
		t.Fatalf("the page does not name the shelf's skill:\n%s", withShelf)
	}
	// It sits before `# Project`, where the section belongs.
	if strings.Index(withShelf, "## Available skills") > strings.Index(withShelf, "# Project") {
		t.Fatalf("the catalog landed after `# Project`")
	}

	withoutShelf := renderSystemAt(Config{Workspace: "/srv/app"}, now)
	if strings.Contains(withoutShelf, "## Available skills") {
		t.Fatalf("a conversation with no store reads a catalog:\n%s", withoutShelf)
	}
}

// MEMORY OFF IS NOT SKILLS OFF, and this is the whole of #1379 answered in
// full rather than explained. A person with eighty-one skills on disk and
// memory off asked the chat whether it could use skills and was told codeaf
// has no such mechanism; #1382 made the chat say they were switched off. Now a
// session handed no memory and a shelf of its own reads that shelf everywhere
// the shelf is read — the catalog, the skills a message carries, and
// `use_skill` — while everything memory is stays off: no `remember` on the
// belt and no memory block.
func TestASkillShelfWorksWithMemoryOff(t *testing.T) {
	shelf := openTestBrain(t)
	agentskillsShelfSkill(t, shelf, "release-notes", "drafts release notes from merged changes")

	catalog := renderSkillCatalog(Config{Skills: shelf, Workspace: "/srv/app"})
	if !strings.Contains(catalog, "- release-notes: drafts release notes from merged changes") {
		t.Fatalf("a memory-off session with a shelf rendered no catalog line for its skill:\n%s", catalog)
	}

	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("drafted"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Skills = shelf
	})
	if !beltHas(agent, useSkillToolName) {
		t.Fatal("use_skill is not on the belt of a memory-off session that has a shelf")
	}
	if beltHas(agent, "remember") {
		t.Fatal("remember is on the belt of a session whose memory is off")
	}
	if block := agent.memoryBlock(context.Background(), "draft the release notes"); block != "" {
		t.Fatalf("a memory-off session rendered a memory block %q", block)
	}
	if out := useSkill(t, agent, `{"mode":"list"}`); !strings.Contains(out, "release-notes") {
		t.Fatalf("use_skill list on the memory-off shelf = %q", out)
	}

	events, err := agent.Submit(context.Background(), "please draft the release notes for the merged changes")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	notice := drainSkillsNotice(t, events)
	if !strings.Contains(notice, "release-notes") {
		t.Fatalf("the memory-off turn did not carry the matching skill: notice %q", notice)
	}
	if sent := userTextIn(completer.request(0)); !strings.Contains(sent, "drafts release notes from merged changes") {
		t.Fatalf("the message the model read does not carry the skill:\n%s", sent)
	}
}

// AND A SESSION WITH NEITHER A MEMORY NOR A SHELF STILL PAYS NOTHING: no
// section, and no sentence about a setting. The one door that used to explain
// the gap now closes it, so there is nothing left to explain.
func TestNoShelfAtAllRendersNothing(t *testing.T) {
	if got := renderSkillCatalog(Config{Workspace: "/srv/app"}); got != "" {
		t.Fatalf("a session with no shelf rendered %q, want the empty string", got)
	}
}
