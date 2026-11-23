package resident

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestReconcileStampSkipsScansAndSharesConcurrentPass(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	dir := filepath.Join(home, ".claude", "skills", "pdf")
	writeImportedSkill(t, dir, "pdf", "Fill forms")
	script := filepath.Join(dir, "scripts", "run.py")
	writeTreeFile(t, script, "print(1)")
	graph := openStore(t)
	cache := settledCache(16)
	check := func(scans uint64) {
		t.Helper()
		var wg sync.WaitGroup
		for range 24 {
			wg.Go(func() { reconcileImportedSkillsWith(graph, project, home, cache) })
		}
		wg.Wait()
		if cache.imports.scans != scans {
			t.Fatalf("scans=%d want %d", cache.imports.scans, scans)
		}
	}
	check(1)
	reads := cache.reads.Load()
	check(1)
	if cache.reads.Load() != reads {
		t.Fatal("unchanged pass read skill content")
	}
	first, ok := importedFactByArtifact(t, graph, dir)
	if !ok {
		t.Fatal("initial import absent")
	}
	info, _ := os.Stat(script)
	writeTreeFile(t, script+".new", "print(2)")
	if err := os.Chtimes(script+".new", info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(script+".new", script); err != nil {
		t.Fatal(err)
	}
	check(2)
	changed, ok := importedFactByArtifact(t, graph, dir)
	if !ok || changed.Digest == first.Digest {
		t.Fatal("nested replacement was not imported")
	}
	if err := os.Truncate(script, 0); err != nil {
		t.Fatal(err)
	}
	check(3)
	second := filepath.Join(home, ".claude", "skills", "new")
	writeImportedSkill(t, second, "new", "New skill")
	check(4)
	if _, ok := importedFactByArtifact(t, graph, second); !ok {
		t.Fatal("new skill missing")
	}
	if err := os.Rename(second, second+"-moved"); err != nil {
		t.Fatal(err)
	}
	check(5)
	if _, ok := importedFactByArtifact(t, graph, second); ok {
		t.Fatal("removed skill remains active")
	}
}

func TestImportStampTracksExternalPluginAndControlFiles(t *testing.T) {
	home, project, plugin := t.TempDir(), t.TempDir(), t.TempDir()
	dir := filepath.Join(plugin, "skills", "pdf")
	writeImportedSkill(t, dir, "pdf", "PDF tools")
	writeTreeFile(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"),
		`{"plugins":{"tools@local":[{"scope":"user","installPath":"`+plugin+`"}]}}`)
	settings := filepath.Join(home, ".claude", "settings.json")
	writeTreeFile(t, settings, `{"enabledPlugins":{"tools@local":true}}`)
	graph := openStore(t)
	cache := settledCache(16)
	reconcileImportedSkillsWith(graph, project, home, cache)
	if _, ok := importedFactByArtifact(t, graph, dir); !ok {
		t.Fatal("plugin not imported")
	}
	reconcileImportedSkillsWith(graph, project, home, cache)
	if cache.imports.scans != 1 {
		t.Fatal("quiet plugin scanned again")
	}
	writeTreeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: pdf\ndescription: Changed description\n---\n")
	reconcileImportedSkillsWith(graph, project, home, cache)
	if cache.imports.scans != 2 {
		t.Fatal("external plugin edit did not trigger a scan")
	}
	writeTreeFile(t, settings, `{"enabledPlugins":{"tools@local":false}}`)
	reconcileImportedSkillsWith(graph, project, home, cache)
	if _, ok := importedFactByArtifact(t, graph, dir); ok {
		t.Fatal("disabled plugin remains active")
	}
	if cache.imports.scans != 3 {
		t.Fatal("plugin control file did not trigger a scan")
	}
}
