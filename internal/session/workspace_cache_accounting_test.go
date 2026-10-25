package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/cachedir"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func TestAdmittedLegacyProjectCacheKeepsItsIdentityAndBytes(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.Register(workspaceidentity.Project, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(project.URI))
	legacy := home.Join("workspace-caches", hex.EncodeToString(digest[:]), "cache")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	admitted, err := registry.Register(workspaceidentity.Cache, legacy, workspaceidentity.Links{OwnerURI: project.URI})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "compiler"), []byte("legacy cache"), 0600); err != nil {
		t.Fatal(err)
	}
	selected, err := ownedProjectCache(registry, project.URI)
	if err != nil || selected != legacy {
		t.Fatalf("legacy binding moved: %q %v", selected, err)
	}
	if got := observedWorkerCache(t, project.Path, project.URI); got != legacy {
		t.Fatalf("actual worker ignored retained cache: %s", got)
	}
	current, err := registry.Lookup(workspaceidentity.Cache, selected)
	if err != nil || current.URI != admitted.URI {
		t.Fatalf("cache identity replaced: %+v %v", current, err)
	}
	if got := cachedir.Size(); got != 12 {
		t.Fatalf("legacy cache is invisible: %d", got)
	}
	if _, err := os.Stat(filepath.Join(cachedir.Root(), "workspaces")); !os.IsNotExist(err) {
		t.Fatal("legacy project acquired a second cache pool")
	}
	if _, err := cachedir.Clean(); err != nil {
		t.Fatal(err)
	}
	selected, err = ownedProjectCache(registry, project.URI)
	if err != nil || selected != legacy {
		t.Fatalf("clean lost admitted binding: %q %v", selected, err)
	}
	current, err = registry.Lookup(workspaceidentity.Cache, selected)
	if err != nil || current.URI != admitted.URI {
		t.Fatal("clean replaced cache URI")
	}
}

func observedWorkerCache(t *testing.T, workspace, uri string) string {
	t.Helper()
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, filepath.Join(t.TempDir(), "stub-codeaf"))
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "cache proof", "1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	script := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("cache", "bash", `{"command":"printf '%s\\n' \"$XDG_CACHE_HOME\" > cache-path.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	worker, err := NewBeltWorker(Config{Workspace: workspace, WorkspaceURI: uri, Model: "test/model"}, script, store.Task("1"), store.Path(), store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	drainTurn(t, worker, "Observe the assigned cache")
	worker.Close()
	raw, err := os.ReadFile(filepath.Join(workspace, "cache-path.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

func TestLegacyRuntimeCacheKeepsIdentityWithoutCleaningRunTemp(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	chat, err := registry.Register(workspaceidentity.Chat, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run, err := registry.Register(workspaceidentity.Run, t.TempDir(), workspaceidentity.Links{OwnerURI: chat.URI})
	if err != nil {
		t.Fatal(err)
	}
	newRun, err := registry.Register(workspaceidentity.Run, t.TempDir(), workspaceidentity.Links{OwnerURI: chat.URI})
	if err != nil {
		t.Fatal(err)
	}
	newCache := observedWorkerCache(t, newRun.Path, newRun.URI)
	if rel, err := filepath.Rel(cachedir.Root(), newCache); err != nil || !filepath.IsLocal(rel) {
		t.Fatalf("new rootless worker cache outside cache owner: %s", newCache)
	}
	legacy := filepath.Join(workspaceRuntimeRoot(run.URI), "cache")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	cache, err := registry.Register(workspaceidentity.Cache, legacy, workspaceidentity.Links{OwnerURI: run.URI})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "compiler"), []byte("cache"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := observedWorkerCache(t, run.Path, run.URI); got != legacy {
		t.Fatalf("runtime cache moved: %s", got)
	}
	scratch := filepath.Join(workspaceRuntimeRoot(run.URI), "temp", "keep")
	if err := os.WriteFile(scratch, []byte("pending scratch"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := cachedir.Size(); got != 5 {
		t.Fatalf("cache accounting included scratch or missed cache: %d", got)
	}
	if _, err := cachedir.Clean(); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(scratch); err != nil || string(raw) != "pending scratch" {
		t.Fatal("cache cleaning touched scratch")
	}
	if got := observedWorkerCache(t, run.Path, run.URI); got != legacy {
		t.Fatal("clean changed legacy cache binding")
	}
	actual, err := registry.Lookup(workspaceidentity.Cache, legacy)
	if err != nil || actual.URI != cache.URI {
		t.Fatal("clean replaced legacy runtime cache identity")
	}
}

func TestOwnedChatProfileCacheUsesCacheOwnerAndRetainsLegacyBinding(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
			registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
			if err != nil {
				t.Fatal(err)
			}
			old := home.Join("workspace-caches", "profile", "cache")
			var admitted workspaceidentity.Record
			if legacy {
				if err := os.MkdirAll(old, 0700); err != nil {
					t.Fatal(err)
				}
				admitted, err = registry.Register(workspaceidentity.Cache, old)
				if err != nil {
					t.Fatal(err)
				}
			}
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
				config.Workspace = t.TempDir()
				config.Place = Place{Dir: t.TempDir(), Owned: true}
			})
			result, err := agent.config.seat().In(agent.config.Workspace).Exec(context.Background(), executor.ExecRequest{Argv: []string{"sh", "-c", `printf '%s\n' "$XDG_CACHE_HOME"`}}, nil)
			if err != nil || result.Exit != 0 {
				t.Fatalf("foreground cache process: %v", err)
			}
			path := strings.TrimSpace(string(result.Stdout))
			if legacy {
				record, err := registry.Lookup(workspaceidentity.Cache, path)
				if err != nil || record.URI != admitted.URI || path != old {
					t.Fatal("foreground chat replaced legacy profile cache")
				}
			} else if rel, err := filepath.Rel(cachedir.Root(), path); err != nil || !filepath.IsLocal(rel) {
				t.Fatalf("new foreground cache outside cache owner: %s", path)
			}
			if err := os.WriteFile(filepath.Join(path, "compiler"), []byte("cache"), 0600); err != nil {
				t.Fatal(err)
			}
			if got := cachedir.Size(); got != 5 {
				t.Fatalf("foreground cache invisible or other assets counted: %d", got)
			}
		})
	}
}
