package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func TestOwnershipChatRoutesScratchWithoutMovingTheProject(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	project := t.TempDir()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = project
		c.Place = Place{Dir: t.TempDir(), Workspace: project}
	})
	if a.config.Workspace != project {
		t.Fatal("a borrowed project was moved")
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	chat, err := registry.Resolve(a.config.WorkspaceURI)
	if err != nil || chat.Role != workspaceidentity.Chat {
		t.Fatalf("chat identity missing: %v", err)
	}
	result, err := a.config.seat().In(project).Exec(context.Background(), executor.ExecRequest{Argv: []string{"sh", "-c", "mktemp"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(workspaceRuntimeRoot(chat.URI), "temp")
	path := strings.TrimSpace(string(result.Stdout))
	if result.Exit != 0 || filepath.Dir(path) != scratch {
		t.Fatalf("temporary file escaped chat scratch: %q, exit %d", path, result.Exit)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestOwnershipChatsShareProjectCacheAndKeepSeparateTemporaryFiles(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	project := t.TempDir()
	var values [2][]string
	for i := range values {
		a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
			c.Workspace = project
			c.Place = Place{Dir: t.TempDir(), Workspace: project}
		})
		result, err := a.config.seat().In(project).Exec(context.Background(), executor.ExecRequest{Argv: []string{"sh", "-c", "printf '%s\\n' \"$TMPDIR\" \"$XDG_CACHE_HOME\""}}, nil)
		if err != nil || result.Exit != 0 {
			t.Fatalf("chat process: %+v, %v", result, err)
		}
		values[i] = strings.Split(strings.TrimSpace(string(result.Stdout)), "\n")
	}
	if len(values[0]) != 2 || len(values[1]) != 2 || values[0][0] == values[1][0] || values[0][1] != values[1][1] {
		t.Fatalf("chat scratch must be separate and project cache shared: %v", values)
	}
}

func TestOwnershipPrototypeChatReopensWithoutProcessFlag(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
	project := t.TempDir()
	place := Place{Dir: t.TempDir(), Workspace: project}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	chat, err := registry.Register(workspaceidentity.Chat, canonicalPath(place.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveMeta(place.Dir, Meta{ID: place.ID(), ChatURI: chat.URI, Workspace: project, WorkspacePolicy: DefaultWorkspacePolicy()}); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace, c.Place = project, place })
	if a.config.WorkspaceURI != chat.URI || a.config.Workspace != project {
		t.Fatal("reopen lost existing identity or changed foreground workspace")
	}
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.WorkspacePolicy == nil || *meta.WorkspacePolicy != *DefaultWorkspacePolicy() {
		t.Fatalf("prototype chat has no persistent policy: %+v", meta.WorkspacePolicy)
	}
}
