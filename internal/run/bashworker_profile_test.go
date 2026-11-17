package run_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestCrewWorkerReadsCurrentProfileBeforeEachCall(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	t.Setenv("CODEAF_HOME", t.TempDir())
	profile, workspace := t.TempDir(), t.TempDir()
	file := filepath.Join(profile, "config.json")
	if err := os.WriteFile(file, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var refused bool
	s := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"printf BEFORE > before.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(file, 0o700); err != nil {
				t.Fatal(err)
			}
			return toolReply(`{"command":"printf AFTER > after.txt"}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			for _, m := range messages {
				text := messageContent(m)
				if m.Role == "tool" && strings.Contains(text, file) && strings.Contains(text, "saved approval rules could not be read") {
					refused = true
				}
			}
			return nil, errors.New("API error (401): fixture complete")
		},
	}}
	store := runOpenStore(t)
	factory := run.CrewFactory(store, workspace, profile, run.Seats{One: "test/model"}, "", false, func(string) session.Completer { return s })
	worker := factory(*store.Task(store.RootID()))
	_, err := worker.Run(runContext(t), *store.Task(store.RootID()))
	if err == nil {
		t.Fatal("scripted provider should end with its authentication refusal")
	}
	t.Logf("scripted provider ending: %v", err)
	if _, err := os.Stat(filepath.Join(workspace, "before.txt")); err != nil {
		t.Fatalf("readable worker failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "after.txt")); !os.IsNotExist(err) {
		t.Errorf("worker wrote after failed read: %v", err)
	}
	if !refused {
		t.Fatal("worker lost config filename and failed-read reason")
	}
}
