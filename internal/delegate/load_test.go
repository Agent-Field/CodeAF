package delegate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDelegate installs one delegate under dir: its manifest, its page, and a
// program that exists (an empty executable script) unless bin says otherwise.
func writeDelegate(t *testing.T, dir, name, manifest, page string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if page != "" {
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func writeProgram(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

const goodManifest = `{
  "name": "fake",
  "description": "a fake delegate for the tests",
  "bin": "./fake.sh",
  "argv": ["run", "--dir", "{{workspace}}", "--max-cost", "{{cost_usd}}", "--max-hours", "{{hours}}", "--", "{{brief}}"],
  "env": {"FAKE_KEY": "{{key}}"},
  "lands": "tree"
}`

func TestLoadAdmitsAManifestWithItsPageAndItsProgram(t *testing.T) {
	dir := t.TempDir()
	writeDelegate(t, dir, "fake", goodManifest, "# fake\n\n## /fake — what it does\n")
	writeProgram(t, filepath.Join(dir, "fake.sh"), "exit 0\n")
	registry, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := registry.Find("fake")
	if !ok {
		t.Fatalf("fake is not in the registry: refusals %v absent %v", registry.Refusals(), registry.Absent())
	}
	if m.BinPath != filepath.Join(dir, "fake.sh") || !m.LandsTree() {
		t.Fatalf("manifest = %+v", m)
	}
	if got := registry.Names(); len(got) != 1 || got[0] != "fake" {
		t.Fatalf("names = %v", got)
	}
}

// A machine with no delegates gets an empty registry AND the folder, so the
// person who goes to add one finds it waiting.
func TestLoadIsEmptyWhenTheFolderDoesNotExistAndMakesIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nowhere")
	registry, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Empty() {
		t.Fatalf("registry = %+v, want empty", registry)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("the folder was not made: %v", err)
	}
}

func TestLoadRefusesAManifestWithoutItsPageAndNamesTheCommand(t *testing.T) {
	dir := t.TempDir()
	writeDelegate(t, dir, "fake", goodManifest, "")
	writeProgram(t, filepath.Join(dir, "fake.sh"), "exit 0\n")
	registry, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Find("fake"); ok {
		t.Fatal("a delegate with no manual page was admitted")
	}
	refusals := registry.Refusals()
	if len(refusals) != 1 || !strings.Contains(refusals[0].Reason, "fake.md") || !strings.HasSuffix(refusals[0].Reason, "not added") {
		t.Fatalf("refusals = %v", refusals)
	}
}

func TestLoadRefusesAPageThatDoesNotSayTheCommand(t *testing.T) {
	dir := t.TempDir()
	writeDelegate(t, dir, "fake", goodManifest, "# fake\n\nIt does things.\n")
	writeProgram(t, filepath.Join(dir, "fake.sh"), "exit 0\n")
	registry, _ := Load(dir)
	refusals := registry.Refusals()
	if len(refusals) != 1 || refusals[0].String() != "fake: its manual page does not say /fake — not added" {
		t.Fatalf("refusals = %v", refusals)
	}
}

func TestLoadKeepsAnAbsentProgramApartFromARefusal(t *testing.T) {
	dir := t.TempDir()
	manifest := strings.Replace(goodManifest, `"./fake.sh"`, `"no-such-program-on-any-path"`, 1)
	writeDelegate(t, dir, "fake", manifest, "## /fake\n")
	registry, _ := Load(dir)
	if len(registry.Refusals()) != 0 {
		t.Fatalf("refusals = %v, want none: the file is fine", registry.Refusals())
	}
	absent := registry.Absent()
	if len(absent) != 1 || absent[0].String() != "fake: no-such-program-on-any-path is not on this machine" {
		t.Fatalf("absent = %v", absent)
	}
	if _, ok := registry.Find("fake"); ok {
		t.Fatal("an absent delegate was offered")
	}
}

func TestLoadRefusesTheThingsValidateRefuses(t *testing.T) {
	cases := map[string]string{
		"a stray placeholder": strings.Replace(goodManifest, "{{brief}}", "{{prompt}}", 1),
		"no brief":            strings.Replace(goodManifest, `"--", "{{brief}}"`, `"--"`, 1),
		"a bad name":          strings.Replace(goodManifest, `"name": "fake"`, `"name": "Fake Thing"`, 1),
		"an unknown lands":    strings.Replace(goodManifest, `"lands": "tree"`, `"lands": "branch"`, 1),
		"an unknown field":    strings.Replace(goodManifest, `"lands": "tree"`, `"lands": "tree", "reader": "senior-dev"`, 1),
		"not json":            "{",
	}
	for name, manifest := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeDelegate(t, dir, "fake", manifest, "## /fake\n")
			writeProgram(t, filepath.Join(dir, "fake.sh"), "exit 0\n")
			registry, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(registry.Refusals()) != 1 {
				t.Fatalf("refusals = %v, want one", registry.Refusals())
			}
			if _, ok := registry.Find("fake"); ok {
				t.Fatal("admitted")
			}
		})
	}
}

func TestLoadRefusesAFileWhoseNameDisagreesWithItsManifest(t *testing.T) {
	dir := t.TempDir()
	writeDelegate(t, dir, "other", goodManifest, "## /other\n")
	registry, _ := Load(dir)
	refusals := registry.Refusals()
	if len(refusals) != 1 || !strings.Contains(refusals[0].Reason, `other.json but the manifest says its name is "fake"`) {
		t.Fatalf("refusals = %v", refusals)
	}
}
