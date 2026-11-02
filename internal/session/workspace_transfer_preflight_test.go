package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func TestTransferRootPreflightRequiresExistingAliasesWithoutMutatingRegistry(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	place := newPlace(t, true)
	if err := os.MkdirAll(filepath.Join(place.Dir, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	primary := t.TempDir()
	uri := func(id string) string { return "codeaf-workspace://11111111111111111111111111111111/" + id }
	first := uri("22222222222222222222222222222222")
	second := uri("33333333333333333333333333333333")
	save := func(roots ...string) {
		t.Helper()
		document := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: uint64(len(roots))}
		for index, root := range roots {
			id := uint64(index + 1)
			document.Runs = append(document.Runs, runRecord{ID: id, State: TaskDone, Copy: &TaskCopyRecord{URI: uri([]string{"44444444444444444444444444444444", "55555555555555555555555555555555"}[index]), ChatURI: uri("66666666666666666666666666666666"), ProjectURI: root, GroundURI: root, Ground: primary, Dir: filepath.Join(place.Trees(), []string{"1", "2"}[index]), Mode: TaskModeMirror}})
		}
		raw, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(place.Tasks(), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save(first)
	if err := ValidateTaskRestoreRoots(place, primary); err != nil {
		t.Fatalf("single unknown primary rejected: %v", err)
	}
	save(first, second)
	if err := ValidateTaskRestoreRoots(place, primary); err == nil {
		t.Fatal("two unknown roots silently merged")
	}
	registryPath := home.Join("workspace-identities")
	if _, err := os.Stat(registryPath); !os.IsNotExist(err) {
		t.Fatal("preflight created a registry to reject unknown roots")
	}
	registry, err := workspaceidentity.Open(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := registry.Register(workspaceidentity.Project, primary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Adopt(workspaceidentity.Record{URI: first, Role: workspaceidentity.Project}, primary, workspaceidentity.Links{}); err != nil {
		t.Fatal(err)
	}
	save(canonical.URI, first)
	before, err := os.ReadFile(filepath.Join(registryPath, "registry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskRestoreRoots(place, primary); err != nil {
		t.Fatalf("explicit aliases rejected: %v", err)
	}
	if err := ValidateTaskRestoreRoots(place, t.TempDir()); err == nil {
		t.Fatal("known aliases reassigned to a different receiving folder")
	}
	if err := ValidateForkTaskRoots(place, t.TempDir()); err != nil {
		t.Fatalf("historical fork could not recognize existing source aliases: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(registryPath, "registry.sqlite"))
	if err != nil || string(after) != string(before) {
		t.Fatal("preflight changed admitted root bindings")
	}
	stray := filepath.Join(place.Dir, ".cell", "trees", "3")
	if err := os.MkdirAll(stray, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stray, "tree.json"), []byte(`{"head":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskRestoreRoots(place, primary); err == nil {
		t.Fatal("unlisted carried task silently assigned to primary root")
	}
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	if err := ValidateTaskRestoreRoots(place, primary); err != nil {
		t.Fatal("ordinary profiles changed their legacy behavior")
	}
}

func TestTransferRootPreflightRejectsNewWorkspaceSetBeforeLegacyRestore(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	place := newPlace(t, true)
	document := taskDocument{Type: taskDocumentType, Version: taskWorkspaceSetFileVersion, Seq: 1, Runs: []runRecord{
		{ID: 1, State: TaskQueued, PendingRun: &PendingRunRecord{Brief: "fixture", WorkspaceSet: &WorkspaceSetRequest{Primary: "a", Roots: []WorkspaceRootRequest{{Source: "/fixture/a", Slot: "a", Access: WorkspaceWrite}, {Source: "/fixture/b", Slot: "b", Access: WorkspaceRead}}}}},
	}}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(place.Tasks(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskRestoreRoots(place, t.TempDir()); err == nil || !strings.Contains(err.Error(), "singleton restore path") {
		t.Fatalf("pending workspace set bypassed the singleton transfer gate: %v", err)
	}
}
