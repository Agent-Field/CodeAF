package resident

import (
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

func countImportedActiveFolder(t *testing.T, st *store.Store, artifact string) int {
	facts, err := st.SkillFacts(store.FactActive, 500)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range facts {
		if filepath.Clean(f.Artifact) == filepath.Clean(artifact) {
			n++
		}
	}
	return n
}

// A project-scoped Claude Code plugin keeps its skills under the home's plugin
// cache, outside the project directory.
func TestImportProjectPluginSkillIsNotDuplicatedPerLaunch(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	install := filepath.Join(homeDir, ".claude", "plugins", "cache", "mk", "tool", "1.0.0")
	skillDir := filepath.Join(install, "skills", "lint")
	writeImportedSkill(t, skillDir, "lint", "Lints things")
	reg := `{"version":2,"plugins":{"tool@mk":[{"scope":"project","projectPath":"` + projectDir + `","installPath":"` + install + `"}]}}`
	writeTreeFile(t, filepath.Join(homeDir, ".claude", "plugins", "installed_plugins.json"), reg)
	writeTreeFile(t, filepath.Join(projectDir, ".claude", "settings.json"), `{"enabledPlugins":{"tool@mk":true}}`)

	st := openStore(t)
	for i := 0; i < 3; i++ {
		ReconcileImportedSkills(st, projectDir, homeDir)
	}
	if n := countImportedActiveFolder(t, st, skillDir); n != 1 {
		t.Fatalf("active facts for project plugin skill after 3 launches = %d, want 1", n)
	}
}

// Opening codeaf in the home directory must not retire another project's skills.
func TestImportHomeWorkspaceDoesNotRetireNestedProjectSkills(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := filepath.Join(homeDir, "work", "app")
	skillDir := filepath.Join(projectDir, ".claude", "skills", "report")
	writeImportedSkill(t, skillDir, "report", "Weekly report")
	st := openStore(t)
	ReconcileImportedSkills(st, projectDir, homeDir)
	if countImportedActiveFolder(t, st, skillDir) != 1 {
		t.Fatal("setup: not imported")
	}
	ReconcileImportedSkills(st, homeDir, homeDir)
	if n := countImportedActiveFolder(t, st, skillDir); n != 1 {
		t.Fatalf("pass opened in home retired the nested project's skill: active=%d", n)
	}
}

// Sibling-but-nested: a pass in a parent project retires a child project's facts.
func TestImportParentProjectDoesNotRetireChildProjectSkills(t *testing.T) {
	homeDir := t.TempDir()
	parent := t.TempDir()
	child := filepath.Join(parent, "packages", "web")
	skillDir := filepath.Join(child, ".claude", "skills", "deploy")
	writeImportedSkill(t, skillDir, "deploy", "Deploys web")
	st := openStore(t)
	ReconcileImportedSkills(st, child, homeDir)
	ReconcileImportedSkills(st, parent, homeDir)
	if n := countImportedActiveFolder(t, st, skillDir); n != 1 {
		t.Fatalf("parent pass retired child's skill: active=%d", n)
	}
}

// A slower workspace scan must finish publishing before a later scan reads
// the same home. Otherwise its older digest can replace the newly edited skill.
func TestImportSerializesSharedHomeScanAndPublication(t *testing.T) {
	homeDir, firstProject, nextProject := t.TempDir(), t.TempDir(), t.TempDir()
	skillDir := filepath.Join(homeDir, ".claude", "skills", "pdf")
	writeImportedSkill(t, skillDir, "pdf", "Original description")
	graph := openStore(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	var workers sync.WaitGroup
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() { unblock(); workers.Wait() }()
	workers.Add(1)
	go func() {
		defer workers.Done()
		runImportPass(graph, firstProject, homeDir, func(dir string) (string, error) {
			sum, err := contentDigest(dir)
			close(entered)
			<-release
			return sum, err
		}, true)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first scan did not reach the skill")
	}
	writeImportedSkill(t, skillDir, "pdf", "Updated description")
	nextDone := make(chan struct{})
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer close(nextDone)
		importPass(graph, nextProject, homeDir, settledCache(8))
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		importPasses.mu.Lock()
		gate := importPasses.shelves[graph]
		queued := gate != nil && gate.holders == 2
		importPasses.mu.Unlock()
		if queued {
			break
		}
		select {
		case <-nextDone:
			t.Fatal("later scan published while the older scan was still pending")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("later scan never queued at the shelf gate")
		}
		runtime.Gosched()
	}
	unblock()
	workers.Wait()
	fact, ok := importedFactByArtifact(t, graph, skillDir)
	if !ok || fact.Body != "Updated description" || countImportedActiveFolder(t, graph, skillDir) != 1 {
		t.Fatalf("newer skill did not remain authoritative: %+v, present=%v", fact, ok)
	}
}
