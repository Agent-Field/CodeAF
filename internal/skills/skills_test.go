package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

// The fixture trees: home holds the shape cases with a project that has no
// skill roots at all, shadow holds one name in three folders, and roots holds
// one name in two same-scope folders. Everything under testdata is static so
// a discovery answer never depends on the machine running the test.
func discover(t *testing.T, projectDir, homeDir string) []Skill {
	t.Helper()
	found, err := Discover(Options{ProjectDir: projectDir, HomeDir: homeDir})
	if err != nil {
		t.Fatalf("Discover(%q, %q): %v", projectDir, homeDir, err)
	}
	return found
}

func byDir(found []Skill, suffix string) (Skill, bool) {
	for _, skill := range found {
		if strings.HasSuffix(skill.Dir, suffix) {
			return skill, true
		}
	}
	return Skill{}, false
}

func TestDiscoverValidUserSkill(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "empty"), filepath.Join("testdata", "home"))
	skill, ok := byDir(found, filepath.Join(".claude", "skills", "pdf"))
	if !ok {
		t.Fatalf("pdf skill not discovered: %+v", found)
	}
	if skill.Name != "pdf" {
		t.Errorf("Name = %q, want %q", skill.Name, "pdf")
	}
	if skill.Description != "Fill, flatten and redact PDF forms" {
		t.Errorf("Description = %q", skill.Description)
	}
	if !filepath.IsAbs(skill.Dir) {
		t.Errorf("Dir = %q, want an absolute path", skill.Dir)
	}
	if skill.Scope != ScopeUser {
		t.Errorf("Scope = %q, want %q", skill.Scope, ScopeUser)
	}
	if skill.Root != ".claude/skills" {
		t.Errorf("Root = %q, want %q", skill.Root, ".claude/skills")
	}
	if skill.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want the size of the folder's files", skill.SizeBytes)
	}
	if skill.Warning != "" {
		t.Errorf("Warning = %q, want none", skill.Warning)
	}
	if skill.Shadowed {
		t.Errorf("Shadowed = true, want false")
	}
}

// The one common malformation: an unquoted description containing a colon,
// which YAML would otherwise refuse to parse at all.
func TestDiscoverRepairsColonInDescription(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "empty"), filepath.Join("testdata", "home"))
	skill, ok := byDir(found, filepath.Join(".claude", "skills", "colon-description"))
	if !ok {
		t.Fatalf("colon-description skill not discovered: %+v", found)
	}
	if skill.Description != "Parses invoices and receipts: extracts totals and dates" {
		t.Errorf("Description = %q, want the repaired value", skill.Description)
	}
	if skill.Warning != "" {
		t.Errorf("Warning = %q, want none", skill.Warning)
	}
}

// A name that does not match its folder loads, with the mismatch said out
// loud — the spec's client guide asks for leniency here, not a refusal.
func TestDiscoverNameMismatchLoadsWithWarning(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "empty"), filepath.Join("testdata", "home"))
	skill, ok := byDir(found, filepath.Join(".claude", "skills", "mismatched-folder"))
	if !ok {
		t.Fatalf("mismatched-folder not discovered: %+v", found)
	}
	if skill.Name != "different-name" {
		t.Errorf("Name = %q, want the frontmatter name", skill.Name)
	}
	if skill.Warning == "" || !strings.Contains(skill.Warning, "does not match") {
		t.Errorf("Warning = %q, want the mismatch named", skill.Warning)
	}
}

func TestDiscoverLongNameLoadsWithWarning(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "empty"), filepath.Join("testdata", "home"))
	skill, ok := byDir(found, filepath.Join(".claude", "skills", "too-long-name"))
	if !ok {
		t.Fatalf("too-long-name not discovered: %+v", found)
	}
	if skill.Warning == "" || !strings.Contains(skill.Warning, "1-64") {
		t.Errorf("Warning = %q, want the field limit named", skill.Warning)
	}
}

// A missing description skips the skill, and the result says why rather than
// failing the whole scan.
func TestDiscoverMissingDescriptionSkipsWithWarning(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "empty"), filepath.Join("testdata", "home"))
	skill, ok := byDir(found, filepath.Join(".claude", "skills", "missing-description"))
	if !ok {
		t.Fatalf("missing-description folder missing from the result: %+v", found)
	}
	if skill.Description != "" {
		t.Errorf("Description = %q, want empty", skill.Description)
	}
	if skill.Warning == "" || !strings.Contains(skill.Warning, "description") {
		t.Errorf("Warning = %q, want the missing description named", skill.Warning)
	}
}

func TestDiscoverMissingNameSkipsWithWarning(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "empty"), filepath.Join("testdata", "home"))
	skill, ok := byDir(found, filepath.Join(".claude", "skills", "no-name"))
	if !ok {
		t.Fatalf("no-name folder missing from the result: %+v", found)
	}
	if skill.Name != "" {
		t.Errorf("Name = %q, want empty", skill.Name)
	}
	if skill.Warning == "" || !strings.Contains(skill.Warning, "name") {
		t.Errorf("Warning = %q, want the missing name named", skill.Warning)
	}
}

func TestDiscoverUnparseableFrontmatterSkipsWithWarning(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "empty"), filepath.Join("testdata", "home"))
	skill, ok := byDir(found, filepath.Join(".claude", "skills", "broken-frontmatter"))
	if !ok {
		t.Fatalf("broken-frontmatter folder missing from the result: %+v", found)
	}
	if skill.Warning == "" || !strings.Contains(skill.Warning, "YAML") {
		t.Errorf("Warning = %q, want the parse failure named", skill.Warning)
	}
}

// A folder with no SKILL.md is not a skill and not a complaint: the promoted
// command folders on the resident's own shelf live exactly this way.
func TestDiscoverFolderWithoutSkillMDIsIgnored(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "empty"), filepath.Join("testdata", "home"))
	if _, ok := byDir(found, filepath.Join(".claude", "skills", "plain-folder")); ok {
		t.Errorf("a folder with no SKILL.md appeared in the result: %+v", found)
	}
}

// A SKILL.md one level deeper than a direct child is never reached: nothing
// is walked past the direct children of a skills root.
func TestDiscoverDirectChildrenOnly(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "empty"), filepath.Join("testdata", "home"))
	for _, suffix := range []string{
		filepath.Join(".claude", "skills", "nested"),
		filepath.Join(".claude", "skills", "nested", "inner"),
	} {
		if _, ok := byDir(found, suffix); ok {
			t.Errorf("a nested skill was discovered at %s: %+v", suffix, found)
		}
	}
}

// Project shadows user, and within the user scope the earlier root wins, so
// a name held three times has one winner and two shadows — none dropped.
func TestDiscoverProjectShadowsUser(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "shadow", "project"), filepath.Join("testdata", "shadow", "home"))
	if len(found) != 3 {
		t.Fatalf("discovered %d skills, want the three pdf folders: %+v", len(found), found)
	}
	winner, ok := byDir(found, filepath.Join("testdata", "shadow", "project", ".claude", "skills", "pdf"))
	if !ok || winner.Shadowed {
		t.Fatalf("the project copy is not the winner: %+v", found)
	}
	if winner.Scope != ScopeProject {
		t.Errorf("winner Scope = %q, want %q", winner.Scope, ScopeProject)
	}
	if winner.Description != "The project copy of the PDF skill" {
		t.Errorf("winner Description = %q", winner.Description)
	}
	for _, suffix := range []string{
		filepath.Join("testdata", "shadow", "home", ".claude", "skills", "pdf"),
		filepath.Join("testdata", "shadow", "home", ".codex", "skills", "pdf"),
	} {
		shadowed, ok := byDir(found, suffix)
		if !ok {
			t.Fatalf("the shadowed copy at %s was dropped: %+v", suffix, found)
		}
		if !shadowed.Shadowed {
			t.Errorf("the copy at %s is not marked shadowed", suffix)
		}
		if shadowed.Scope != ScopeUser {
			t.Errorf("shadowed copy Scope = %q, want %q", shadowed.Scope, ScopeUser)
		}
	}
}

// Within one scope the first root in issue #1277's order owns the name.
func TestDiscoverFirstRootWinsWithinScope(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "roots", "project"), filepath.Join("testdata", "empty"))
	winner, ok := byDir(found, filepath.Join(".codeaf", "skills", "duplicate"))
	if !ok || winner.Shadowed {
		t.Fatalf("the .codeaf copy is not the winner: %+v", found)
	}
	loser, ok := byDir(found, filepath.Join(".claude", "skills", "duplicate"))
	if !ok {
		t.Fatalf("the .claude copy was dropped: %+v", found)
	}
	if !loser.Shadowed {
		t.Errorf("the .claude copy is not marked shadowed")
	}
}

func TestDiscoverNeedsAtLeastOneBase(t *testing.T) {
	if _, err := Discover(Options{}); err == nil {
		t.Fatal("Discover with no directories at all did not error")
	}
}
