package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixturePlaceholder stands for the fixture tree's own absolute location in
// the registry file. Claude Code records every installPath absolutely, so a
// static fixture cannot spell one; [pluginFixture] lays the tree down in a
// temporary directory and writes the real location in.
const fixturePlaceholder = "@FIXTURE@"

// pluginFixture copies testdata/plugins into a fresh directory with the
// placeholder in every JSON file replaced by that directory, and answers it.
// The tree holds one home with a registry, three installed plugins, the
// leftovers Claude Code keeps beside them, and two projects: one that
// installs and enables a plugin of its own, and one that is empty.
func pluginFixture(t *testing.T) string {
	t.Helper()
	source := filepath.Join("testdata", "plugins")
	target := t.TempDir()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".json") {
			data = []byte(strings.ReplaceAll(string(data), fixturePlaceholder, target))
		}
		return os.WriteFile(destination, data, 0o644)
	})
	if err != nil {
		t.Fatalf("lay down the plugin fixture: %v", err)
	}
	return target
}

func byName(found []Skill, name string) []Skill {
	var out []Skill
	for _, skill := range found {
		if skill.Name == name {
			out = append(out, skill)
		}
	}
	return out
}

// An installed and enabled plugin's skills are found where the registry says
// the plugin was unpacked, as user skills read from the plugin root.
func TestDiscoverInstalledEnabledPluginSkill(t *testing.T) {
	root := pluginFixture(t)
	found := discover(t, filepath.Join(root, "elsewhere"), filepath.Join(root, "home"))
	skill, ok := byDir(found, filepath.Join("tidy", "1.0.0", "skills", "tidy-commits"))
	if !ok {
		t.Fatalf("the enabled plugin's skill was not discovered: %+v", found)
	}
	if skill.Description != "Squash and reword a branch's commits before review" {
		t.Errorf("Description = %q", skill.Description)
	}
	if skill.Scope != ScopeUser {
		t.Errorf("Scope = %q, want %q", skill.Scope, ScopeUser)
	}
	if skill.Root != ".claude/plugins" {
		t.Errorf("Root = %q, want %q", skill.Root, ".claude/plugins")
	}
	if skill.Shadowed || skill.Warning != "" {
		t.Errorf("Shadowed = %v, Warning = %q, want a clean winner", skill.Shadowed, skill.Warning)
	}
}

// Only what Claude Code itself would load: a plugin switched off, an older
// cached version of an installed plugin, a marketplace plugin nobody
// installed (even one the settings name as enabled), and a folder a manifest
// tries to reach outside its plugin are all left alone.
func TestDiscoverIgnoresPluginsClaudeCodeWouldNotLoad(t *testing.T) {
	root := pluginFixture(t)
	found := discover(t, filepath.Join(root, "elsewhere"), filepath.Join(root, "home"))
	// The control first: the same registry's live plugin IS read, so the
	// absences below are the rule at work and not a scan that reads no plugin.
	if len(byName(found, "tidy-commits")) != 1 {
		t.Fatalf("the live plugin's skill is missing, so the exclusions below prove nothing: %+v", found)
	}
	for _, name := range []string{"dormant-helper", "stale-version", "never-installed", "escaped", "project-helper", "other-half"} {
		if hits := byName(found, name); len(hits) > 0 {
			t.Errorf("%s was discovered but its plugin is not live here: %+v", name, hits)
		}
	}
}

// A manifest's own `skills` paths are the folders read; this one names the
// default skills/ folder among them, which is why tidy-commits is still found.
func TestDiscoverPluginManifestSkillFolder(t *testing.T) {
	root := pluginFixture(t)
	found := discover(t, filepath.Join(root, "elsewhere"), filepath.Join(root, "home"))
	if _, ok := byDir(found, filepath.Join("tidy", "1.0.0", "extra", "extra-notes")); !ok {
		t.Fatalf("the manifest's extra skill folder was not read: %+v", found)
	}
}

// One repository split into several plugins by its marketplace's catalog: the
// installed plugin carries the whole repository, and only the skill folders
// its catalog entry names are its skills. The catalog is read where the
// marketplace record says the marketplace lives.
func TestDiscoverPluginSkillsTheMarketplaceEntryNames(t *testing.T) {
	root := pluginFixture(t)
	found := discover(t, filepath.Join(root, "elsewhere"), filepath.Join(root, "home"))
	for _, name := range []string{"ledger-close", "sheet-merge"} {
		hits := byName(found, name)
		if len(hits) != 1 {
			t.Fatalf("%s, which the catalog names for the installed plugin, was found %d times: %+v", name, len(hits), found)
		}
		if hits[0].Plugin != "suite@split" || hits[0].Shadowed {
			t.Errorf("%s Plugin = %q, Shadowed = %v, want an unshadowed skill of suite@split", name, hits[0].Plugin, hits[0].Shadowed)
		}
	}
	if hits := byName(found, "other-half"); len(hits) > 0 {
		t.Errorf("other-half belongs to a plugin nobody installed, but was discovered: %+v", hits)
	}
}

// The plugin skill carries the key Claude Code files its plugin under.
func TestDiscoverPluginSkillNamesItsPlugin(t *testing.T) {
	root := pluginFixture(t)
	found := discover(t, filepath.Join(root, "elsewhere"), filepath.Join(root, "home"))
	skill, ok := byDir(found, filepath.Join("tidy", "1.0.0", "skills", "tidy-commits"))
	if !ok {
		t.Fatalf("the enabled plugin's skill was not discovered: %+v", found)
	}
	if skill.Plugin != "tidy@market" {
		t.Errorf("Plugin = %q, want %q", skill.Plugin, "tidy@market")
	}
}

// A hand-kept skill owns its name over a plugin's, and the plugin's over
// Codex's bundled one: three copies of pdf, one winner, two shadows.
func TestDiscoverPluginSkillRanksBelowHandKeptAndAboveSystem(t *testing.T) {
	root := pluginFixture(t)
	found := discover(t, filepath.Join(root, "elsewhere"), filepath.Join(root, "home"))
	copies := byName(found, "pdf")
	if len(copies) != 3 {
		t.Fatalf("found %d copies of pdf, want the hand-kept, plugin and system ones: %+v", len(copies), copies)
	}
	want := []struct {
		suffix   string
		shadowed bool
	}{
		{filepath.Join(".claude", "skills", "pdf"), false},
		{filepath.Join("tidy", "1.0.0", "skills", "pdf"), true},
		{filepath.Join(".codex", "skills", ".system", "pdf"), true},
	}
	for index, expect := range want {
		if !strings.HasSuffix(copies[index].Dir, expect.suffix) {
			t.Errorf("copy %d is %s, want the one at %s", index, copies[index].Dir, expect.suffix)
			continue
		}
		if copies[index].Shadowed != expect.shadowed {
			t.Errorf("copy at %s Shadowed = %v, want %v", expect.suffix, copies[index].Shadowed, expect.shadowed)
		}
	}
}

// A plugin installed for one project is a project skill there and nothing
// anywhere else, and the project's local settings layer over the person's:
// the plugin the home settings switch off is on in the project that turns it
// on.
func TestDiscoverProjectPluginAndLayeredSettings(t *testing.T) {
	root := pluginFixture(t)
	found := discover(t, filepath.Join(root, "project"), filepath.Join(root, "home"))
	helper, ok := byDir(found, filepath.Join("projonly", "1.0.0", "skills", "project-helper"))
	if !ok {
		t.Fatalf("the project's own plugin skill was not discovered: %+v", found)
	}
	if helper.Scope != ScopeProject {
		t.Errorf("project-helper Scope = %q, want %q", helper.Scope, ScopeProject)
	}
	if _, ok := byDir(found, filepath.Join("dormant", "1.0.0", "skills", "dormant-helper")); !ok {
		t.Errorf("the plugin this project's local settings enable was not discovered: %+v", found)
	}
}

// Codex's bundled skills live one folder deeper than its skills root, and
// they are found there as user skills of their own root.
func TestDiscoverCodexSystemSkills(t *testing.T) {
	root := pluginFixture(t)
	found := discover(t, filepath.Join(root, "elsewhere"), filepath.Join(root, "home"))
	skill, ok := byDir(found, filepath.Join(".codex", "skills", ".system", "image-lite"))
	if !ok {
		t.Fatalf("the Codex system skill was not discovered: %+v", found)
	}
	if skill.Root != ".codex/skills/.system" {
		t.Errorf("Root = %q, want %q", skill.Root, ".codex/skills/.system")
	}
	if skill.Scope != ScopeUser || skill.Shadowed {
		t.Errorf("Scope = %q, Shadowed = %v, want an unshadowed user skill", skill.Scope, skill.Shadowed)
	}
}

// A skill folder that is a link to a directory is a skill folder, reported at
// the link, and weighed at the folder it names. A link to nothing is not.
func TestDiscoverFollowsLinkedSkillFolders(t *testing.T) {
	found := discover(t, filepath.Join("testdata", "links", "project"), filepath.Join("testdata", "links", "home"))
	skill, ok := byDir(found, filepath.Join(".claude", "skills", "linked"))
	if !ok {
		t.Fatalf("the linked skill folder was not discovered: %+v", found)
	}
	if skill.Name != "linked" || skill.Shadowed {
		t.Errorf("Name = %q, Shadowed = %v, want the unshadowed linked skill", skill.Name, skill.Shadowed)
	}
	if skill.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want the size of the folder the link names", skill.SizeBytes)
	}
	if _, ok := byDir(found, filepath.Join(".claude", "skills", "dangling")); ok {
		t.Errorf("a link to nothing was discovered: %+v", found)
	}
}
