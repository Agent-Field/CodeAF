package session

import (
	"strconv"
	"strings"
	"testing"
	"time"

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

// bulletLines is the skill bullets and nothing else: the section header, the
// routing sentence and the "- … and N more skills" overflow line are all not
// one skill.
func bulletLines(catalog string) []string {
	bullets := make([]string, 0, skillCatalogMaxLines)
	for _, line := range strings.Split(catalog, "\n") {
		if strings.HasPrefix(line, "- ") && !strings.Contains(line, "more skills") {
			bullets = append(bullets, line)
		}
	}
	return bullets
}

// TestSkillCatalogWindowsALargeShelf: a shelf past the cap renders exactly
// [skillCatalogMaxLines] bullets and one overflow line naming what did not fit,
// so the section can never grow with the notebook.
func TestSkillCatalogWindowsALargeShelf(t *testing.T) {
	brain := openTestBrain(t)
	for index := 0; index < 15; index++ {
		activeSkill(t, brain,
			"domain:alpha",
			"skill number "+strconv.Itoa(index)+" checks a thing",
			"/shelf/skill-"+strconv.Itoa(index),
		)
	}

	catalog := renderSkillCatalog(Config{Memory: brain, Workspace: "/srv/app"})
	if catalog == "" {
		t.Fatal("a shelf of fifteen skills rendered nothing")
	}
	bullets := bulletLines(catalog)
	if len(bullets) != skillCatalogMaxLines {
		t.Fatalf("catalog carries %d bullets, want the %d-line cap:\n%s", len(bullets), skillCatalogMaxLines, catalog)
	}
	// 15 skills, 8 shown: the overflow line names the other 7.
	if !strings.Contains(catalog, "… and 7 more skills") {
		t.Fatalf("catalog overflow line is wrong, want \"… and 7 more skills\":\n%s", catalog)
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
	if !strings.Contains(catalog, "Skills suited to a message are attached to it, and `use_skill` reaches any of them by name") {
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
