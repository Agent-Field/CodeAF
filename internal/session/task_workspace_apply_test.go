package session

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/home"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTaskWorkspaceExplicitReviewAndApply(t *testing.T) {
	for _, repository := range []bool{false, true} {
		t.Run(map[bool]string{false: "folder", true: "repository"}[repository], func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
			t.Setenv("CODEAF_TASK_BELT", "bash")
			t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
			source := t.TempDir()
			if repository {
				source = newTestRepo(t)
			} else {
				writeFile(t, filepath.Join(source, "shared.txt"), "the original line\n")
			}
			place := Place{Dir: t.TempDir(), Workspace: source}
			engine := newBeltRunDouble("done")
			engine.real = true
			registerBeltRunEngine(t, engine)
			agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place, c.SessionFile = source, place, place.Transcript() })
			id, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "write private output", WorkspaceSetRequest{Primary: "work", Roots: []WorkspaceRootRequest{{Source: source, Slot: "work", Access: WorkspaceWrite}}})
			if err != nil {
				t.Fatal(err)
			}
			<-engine.entered
			engine.mu.Lock()
			copyDir := engine.spec.Workspace
			engine.mu.Unlock()
			if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil {
				t.Fatal("running task was applied")
			}
			writeFile(t, filepath.Join(copyDir, "shared.txt"), "reviewed private result\n")
			endBeltRun(t, agent, engine)
			// Edits made after the worker committed must also be reviewed and applied.
			writeFile(t, filepath.Join(copyDir, "shared.txt"), "reviewed private result after completion\n")
			writeFile(t, filepath.Join(copyDir, "new.txt"), "untracked private result\n")
			if got := readFile(t, filepath.Join(source, "shared.txt")); got != "the original line\n" {
				t.Fatalf("source changed without apply: %q", got)
			}
			writer, _ := newTestAgent(t, &scriptedCompleter{steps: []step{bashCall("writer-proof", "printf completed"), finalText("done")}}, func(c *Config) {
				c.Workspace = copyDir
				c.AskConsent = false
				c.SessionFile = filepath.Join(t.TempDir(), "writer.jsonl")
			})
			events, err := writer.Submit(context.Background(), "run the local command")
			if err != nil {
				t.Fatal(err)
			}
			collect(t, events)
			if !taskStorageHasUncertainWriter() {
				t.Fatal("ordinary shell did not retain writer uncertainty")
			}
			review, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work")
			if err != nil {
				t.Fatal(err)
			}
			if review.Destination != source || !strings.Contains(review.Diff, "reviewed private result") || !strings.Contains(review.Diff, "untracked private result") || len(review.Changed) != 2 || !slices.Contains(review.Changed, "shared.txt") || !slices.Contains(review.Changed, "new.txt") {
				t.Fatalf("review did not show actual selected root changes: %+v", review)
			}
			// Later edits stay private; apply uses exactly the immutable reviewed snapshot.
			writeFile(t, filepath.Join(copyDir, "shared.txt"), "later private edit\n")
			if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, filepath.Join(source, "shared.txt")); got != "reviewed private result after completion\n" {
				t.Fatalf("explicit apply did not deliver reviewed output: %q", got)
			}
			if got := readFile(t, filepath.Join(source, "new.txt")); got != "untracked private result\n" {
				t.Fatalf("untracked result not applied: %q", got)
			}
			if !taskStorageHasUncertainWriter() {
				t.Fatal("apply cleared permanent retirement uncertainty")
			}
			if got := readFile(t, filepath.Join(copyDir, "shared.txt")); got != "later private edit\n" {
				t.Fatalf("original private copy changed: %q", got)
			}
			kept, _ := runRowOf(agent.graph(), id)
			if _, err := runCopyTree(kept.WorkspaceSet.Roots[0].Copy, place); err != nil {
				t.Fatalf("original copy URI no longer reopens: %v", err)
			}
			if kept.WorkspaceSet.Roots[0].Landing != "" {
				t.Fatal("apply persisted future landing permission")
			}
			if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil {
				t.Fatal("already applied root was applied twice")
			}
			if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err == nil {
				t.Fatal("already applied root offered a new unapplicable review")
			}

		})
	}
}

func TestTaskWorkspaceApplyPreservesConflictingOriginalAndPrivateCopy(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "shared.txt"), "baseline\n")
	place := Place{Dir: t.TempDir(), Workspace: source}
	engine := newBeltRunDouble("done")
	engine.real = true
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place, c.SessionFile = source, place, place.Transcript() })
	id, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "keep both versions", WorkspaceSetRequest{Primary: "work", Roots: []WorkspaceRootRequest{{Source: source, Slot: "work", Access: WorkspaceWrite}}})
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	engine.mu.Lock()
	copyDir := engine.spec.Workspace
	engine.mu.Unlock()
	writeFile(t, filepath.Join(copyDir, "shared.txt"), "private version\n")
	endBeltRun(t, agent, engine)
	if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, "shared.txt"), "person changed original\n")
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil {
		t.Fatal("conflicting apply succeeded")
	}
	if got := readFile(t, filepath.Join(source, "shared.txt")); got != "person changed original\n" {
		t.Fatalf("person's change lost: %q", got)
	}
	if got := readFile(t, filepath.Join(copyDir, "shared.txt")); got != "private version\n" {
		t.Fatalf("private result lost: %q", got)
	}
	if _, err := os.Stat(copyDir); err != nil {
		t.Fatal(err)
	}
}

func completedWorkspaceApplyFixture(t *testing.T, repository bool) (*Agent, uint64, Place, string, string) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_FURROW", filepath.Join(t.TempDir(), "unavailable-native-engine"))
	source := t.TempDir()
	if repository {
		source = newTestRepo(t)
	} else {
		writeFile(t, filepath.Join(source, "shared.txt"), "the original line\n")
	}
	place := Place{Dir: t.TempDir(), Workspace: source}
	engine := newBeltRunDouble("done")
	engine.real = true
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place, c.SessionFile = source, place, place.Transcript() })
	id, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "review safely", WorkspaceSetRequest{Primary: "work", Roots: []WorkspaceRootRequest{{Source: source, Slot: "work", Access: WorkspaceWrite}}})
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	engine.mu.Lock()
	copyDir := engine.spec.Workspace
	engine.mu.Unlock()
	writeFile(t, filepath.Join(copyDir, "shared.txt"), "private result\n")
	endBeltRun(t, agent, engine)
	if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	return agent, id, place, source, copyDir
}

func TestTaskWorkspaceApplyRejectsAlteredStageReceipt(t *testing.T) {
	for _, kind := range []string{"cleanup-path", "stage-destination", "comparison-baseline"} {
		t.Run(kind, func(t *testing.T) {
			agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, kind != "comparison-baseline")
			// The local receipt is a persisted input. Corrupt it without changing the
			// ownership registry and exercise the exported apply door.
			paths, err := filepath.Glob(home.Join("workspace-apply", "*.json"))
			if err != nil || len(paths) != 1 {
				t.Fatalf("review receipts: %v %v", paths, err)
			}
			raw, err := os.ReadFile(paths[0])
			if err != nil {
				t.Fatal(err)
			}
			var ticket taskWorkspaceReviewTicket
			if err := json.Unmarshal(raw, &ticket); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "cleanup-path":
				ticket.Stage.Dir = copyDir
			case "stage-destination":
				foreign := newTestRepo(t)
				// A second authentic review can be transplanted without inventing any URI.
				otherPlace := Place{Dir: t.TempDir(), Workspace: foreign}
				engine := newBeltRunDouble("done")
				engine.real = true
				registerBeltRunEngine(t, engine)
				other, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place, c.SessionFile = foreign, otherPlace, otherPlace.Transcript() })
				otherID, _, _, err := other.StartTaskInWorkspaceSet(context.Background(), "foreign root", WorkspaceSetRequest{Primary: "work", Roots: []WorkspaceRootRequest{{Source: foreign, Slot: "work", Access: WorkspaceWrite}}})
				if err != nil {
					t.Fatal(err)
				}
				<-engine.entered
				engine.mu.Lock()
				foreignCopy := engine.spec.Workspace
				engine.mu.Unlock()
				writeFile(t, filepath.Join(foreignCopy, "foreign.txt"), "unselected root\n")
				endBeltRun(t, other, engine)
				if _, err := other.ReviewTaskWorkspaceRoot(context.Background(), otherID, "work"); err != nil {
					t.Fatal(err)
				}
				all, _ := filepath.Glob(home.Join("workspace-apply", "*.json"))
				for _, path := range all {
					if path == paths[0] {
						continue
					}
					data, _ := os.ReadFile(path)
					var foreignTicket taskWorkspaceReviewTicket
					if err := json.Unmarshal(data, &foreignTicket); err != nil {
						t.Fatal(err)
					}
					ticket.Stage, ticket.Place, ticket.Snapshot = foreignTicket.Stage, foreignTicket.Place, foreignTicket.Snapshot
				}
			case "comparison-baseline":
				// A forged metadata baseline must not turn changed originals into consent.
				_ = os.Remove(filepath.Join(ticket.Stage.Dir, codeafDroppings, groundBaselineRecord))
				writeFile(t, filepath.Join(source, "shared.txt"), "person changed original\n")
			}
			raw, err = json.Marshal(ticket)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths[0], raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil {
				t.Fatalf("altered %s receipt was applied", kind)
			}
			expected := "the original line\n"
			if kind == "comparison-baseline" {
				expected = "person changed original\n"
			}
			if got := readFile(t, filepath.Join(source, "shared.txt")); got != expected {
				t.Fatalf("original changed: %q", got)
			}
			if got := readFile(t, filepath.Join(copyDir, "shared.txt")); got != "private result\n" {
				t.Fatalf("private copy changed: %q", got)
			}
		})
	}
}

func TestTaskWorkspaceReviewReplacesItsOwnedStagingCopy(t *testing.T) {
	agent, id, _, _, copyDir := completedWorkspaceApplyFixture(t, true)
	before, err := git(copyDir, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
			t.Fatal(err)
		}
	}
	after, err := git(copyDir, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(before, "worktree ") != strings.Count(after, "worktree ") {
		t.Fatalf("repeated review leaked worktrees:\nbefore:%s\nafter:%s", before, after)
	}
}

func TestTaskWorkspaceLargeReviewNamesItsImmutableDiff(t *testing.T) {
	agent, id, _, _, copyDir := completedWorkspaceApplyFixture(t, true)
	writeFile(t, filepath.Join(copyDir, "large.txt"), strings.Repeat("large immutable line\n", 20000))
	review, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work")
	if err != nil {
		t.Fatal(err)
	}
	if !review.DiffTruncated || !strings.Contains(review.Diff, "git -C ") || !strings.Contains(review.Diff, review.Snapshot) || !strings.Contains(review.Diff, " diff ") {
		t.Fatal("large review did not provide exact immutable diff inspection")
	}
}
