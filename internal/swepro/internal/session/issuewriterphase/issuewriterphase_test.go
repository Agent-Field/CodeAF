package issuewriterphase

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type testPromptOps struct {
	mu       sync.Mutex
	requests []PromptRequest
	onPrompt func(PromptRequest) error
}

func (ops *testPromptOps) ResolvePromptParts(
	_ context.Context, template string,
) ([]any, error) {
	return []any{template}, nil
}

func (ops *testPromptOps) Prompt(_ context.Context, input any) (any, error) {
	request := input.(PromptRequest)
	ops.mu.Lock()
	ops.requests = append(ops.requests, request)
	ops.mu.Unlock()
	if ops.onPrompt != nil {
		return nil, ops.onPrompt(request)
	}
	return nil, nil
}

func TestDispatchIssueWriterPhasePreservesTaskOrder(t *testing.T) {
	issuesDir := filepath.Join(t.TempDir(), "issues")
	prompt := &testPromptOps{}
	prompt.onPrompt = func(request PromptRequest) error {
		reminder := request.Parts[0].(PromptPart).Text
		for _, name := range []string{"first", "third"} {
			path := filepath.Join(issuesDir, name+".md")
			if contains(reminder, path) {
				if err := os.WriteFile(path, []byte("# issue"), 0o644); err != nil {
					return err
				}
			}
		}
		return errors.New("prompt errors are swallowed")
	}
	idCalls := 0
	var idMu sync.Mutex
	result, err := DispatchIssueWriterPhase(context.Background(), Input{
		Workspace: "/repo", ParentSessionID: "parent", PromptOps: prompt,
		Tasks: []DAGTaskInput{
			{TaskKey: "first", Title: "First"},
			{TaskKey: "second", Title: "Second"},
			{TaskKey: "third", Title: "Third"},
		},
		IssuesDir: issuesDir, ArchPath: "/arch.md", ProductPath: "/product.md",
	}, Dependencies{
		Models: ModelResolverFunc(func(string) []string { return []string{"p/model/sub"} }),
		Sessions: SessionCreatorFunc(func(_ context.Context, _, _ string) (string, error) {
			return "session", nil
		}),
		NewMessageID: func() string {
			idMu.Lock()
			defer idMu.Unlock()
			idCalls++
			return "id"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || result.Written != 2 || idCalls != 6 {
		t.Fatalf("result=%#v idCalls=%d", result, idCalls)
	}
	if got := result.WrittenByTaskKey.Keys(); len(got) != 2 ||
		got[0] != "first" || got[1] != "third" {
		t.Fatalf("written order=%v", got)
	}
	if len(result.Failed) != 1 || result.Failed[0].TaskKey != "second" {
		t.Fatalf("failures=%#v", result.Failed)
	}
}

func TestDispatchIssueWriterPhaseNoTasksAndNoModel(t *testing.T) {
	result, err := DispatchIssueWriterPhase(context.Background(), Input{}, Dependencies{})
	if err != nil || result.Total != 0 || result.WrittenByTaskKey.Len() != 0 {
		t.Fatalf("empty result=%#v err=%v", result, err)
	}
	result, err = DispatchIssueWriterPhase(context.Background(), Input{
		Tasks: []DAGTaskInput{{TaskKey: "task", Title: "Task"}},
	}, Dependencies{
		Models: ModelResolverFunc(func(string) []string { return nil }),
	})
	if err != nil || len(result.Failed) != 1 ||
		result.Failed[0].Reason != "no HIGH-tier model available" {
		t.Fatalf("no-model result=%#v err=%v", result, err)
	}
}

func TestDispatchIssueWriterPhaseAcceptsDirectoryAndTraversal(t *testing.T) {
	root := t.TempDir()
	issuesDir := filepath.Join(root, "issues")
	directoryPath := filepath.Join(issuesDir, "directory.md")
	if err := os.MkdirAll(directoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	traversalPath := filepath.Join(issuesDir, "../escape.md")
	if err := os.WriteFile(traversalPath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := DispatchIssueWriterPhase(context.Background(), Input{
		PromptOps: &testPromptOps{},
		Tasks: []DAGTaskInput{
			{TaskKey: "directory", Title: "Directory"},
			{TaskKey: "../escape", Title: "Escape"},
		},
		IssuesDir: issuesDir,
	}, Dependencies{
		Models: ModelResolverFunc(func(string) []string { return []string{"p/m"} }),
		Sessions: SessionCreatorFunc(func(context.Context, string, string) (string, error) {
			return "session", nil
		}),
	})
	if err != nil || result.Written != 2 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	path, _ := result.WrittenByTaskKey.Get("../escape")
	if path != traversalPath {
		t.Fatalf("traversal path=%q want %q", path, traversalPath)
	}
}

func contains(value, fragment string) bool {
	return len(fragment) == 0 || len(value) >= len(fragment) &&
		func() bool {
			for i := 0; i+len(fragment) <= len(value); i++ {
				if value[i:i+len(fragment)] == fragment {
					return true
				}
			}
			return false
		}()
}
