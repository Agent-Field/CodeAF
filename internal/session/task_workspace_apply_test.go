package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
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
			// The shipped policy must work without the retired prototype override.
			if err := SaveMeta(place.Dir, Meta{ID: place.ID(), Workspace: source, WorkspacePolicy: DefaultWorkspacePolicy()}); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "")
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
	return completedWorkspaceApplyFixtureWithInput(t, repository, nil)
}

func completedWorkspaceApplyFixtureWithInput(t *testing.T, repository bool, setup func(string)) (*Agent, uint64, Place, string, string) {
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
	if setup != nil {
		setup(source)
	}
	place := Place{Dir: t.TempDir(), Workspace: source}
	if err := SaveMeta(place.Dir, Meta{ID: place.ID(), Workspace: source, WorkspacePolicy: DefaultWorkspacePolicy()}); err != nil {
		t.Fatal(err)
	}
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
				if err := SaveMeta(otherPlace.Dir, Meta{ID: otherPlace.ID(), Workspace: foreign, WorkspacePolicy: DefaultWorkspacePolicy()}); err != nil {
					t.Fatal(err)
				}
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
	_, command, ok := strings.Cut(review.Diff, "Complete reviewed diff: ")
	if !ok {
		t.Fatal("complete reviewed command missing")
	}
	writeFile(t, filepath.Join(copyDir, "large.txt"), "later mutable private edit\n")
	output, err := exec.Command("sh", "-c", strings.TrimSpace(command)).Output()
	if err != nil {
		t.Fatalf("complete immutable diff command failed: %v", err)
	}
	if !strings.Contains(string(output), "+large immutable line") || strings.Contains(string(output), "later mutable private edit") {
		t.Fatal("complete diff did not inspect the exact reviewed snapshot")
	}
}

func TestTaskWorkspaceApplyPreservesReviewedPortableMetadata(t *testing.T) {
	for _, kind := range []string{"mode-and-crlf", "empty-directory", "directory-mode"} {
		t.Run(kind, func(t *testing.T) {
			agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, false)
			writeFile(t, filepath.Join(copyDir, "shared.txt"), "reviewed bytes\n")
			switch kind {
			case "mode-and-crlf":
				writeFile(t, filepath.Join(copyDir, ".gitattributes"), "shared.txt text eol=lf\n")
				writeFile(t, filepath.Join(copyDir, "shared.txt"), "reviewed bytes\r\n")
				if err := os.Chmod(filepath.Join(copyDir, "shared.txt"), 0640); err != nil {
					t.Fatal(err)
				}
			case "empty-directory":
				if err := os.Mkdir(filepath.Join(copyDir, "empty"), 0750); err != nil {
					t.Fatal(err)
				}
			case "directory-mode":
				if err := os.Mkdir(filepath.Join(copyDir, "nested"), 0750); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(copyDir, "nested", "result.txt"), "nested result\n")
			}
			if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
				t.Fatal(err)
			}
			if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "mode-and-crlf":
				if got := readFile(t, filepath.Join(source, "shared.txt")); got != "reviewed bytes\r\n" {
					t.Errorf("reviewed raw EOL changed: %q", got)
				}
				info, err := os.Stat(filepath.Join(source, "shared.txt"))
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0640 {
					t.Fatalf("reviewed file mode=%04o want0640", info.Mode().Perm())
				}
			case "empty-directory", "directory-mode":
				name := "empty"
				if kind == "directory-mode" {
					name = "nested"
				}
				info, err := os.Stat(filepath.Join(source, name))
				if err != nil {
					t.Fatalf("reviewed directory missing: %v", err)
				}
				if info.Mode().Perm() != 0750 {
					t.Fatalf("reviewed directory mode=%04o want0750", info.Mode().Perm())
				}
			}
		})
	}
}

func TestTaskWorkspaceStartsWithPortableSourceRepresentation(t *testing.T) {
	for _, repository := range []bool{false, true} {
		t.Run(map[bool]string{false: "folder", true: "repository"}[repository], func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
			t.Setenv("CODEAF_TASK_BELT", "bash")
			source := t.TempDir()
			if repository {
				source = newTestRepo(t)
			}
			writeFile(t, filepath.Join(source, ".gitattributes"), "shared.txt text eol=lf\n")
			writeFile(t, filepath.Join(source, "shared.txt"), "input\r\n")
			if err := os.Chmod(filepath.Join(source, "shared.txt"), 0640); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(source, "empty"), 0750); err != nil {
				t.Fatal(err)
			}
			place := Place{Dir: t.TempDir(), Workspace: source}
			engine := newBeltRunDouble("done")
			engine.real = true
			registerBeltRunEngine(t, engine)
			agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place, c.SessionFile = source, place, place.Transcript() })
			_, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "inspect input", WorkspaceSetRequest{Primary: "work", Roots: []WorkspaceRootRequest{{Source: source, Slot: "work", Access: WorkspaceWrite}}})
			if err != nil {
				t.Fatal(err)
			}
			<-engine.entered
			engine.mu.Lock()
			copyDir := engine.spec.Workspace
			engine.mu.Unlock()
			defer endBeltRun(t, agent, engine)
			if got := readFile(t, filepath.Join(copyDir, "shared.txt")); got != "input\r\n" {
				t.Errorf("private input raw EOL changed: %q", got)
			}
			for name, want := range map[string]os.FileMode{"shared.txt": 0640, "empty": 0750} {
				info, err := os.Stat(filepath.Join(copyDir, name))
				if err != nil {
					t.Errorf("private input %s: %v", name, err)
					continue
				}
				if info.Mode().Perm() != want {
					t.Errorf("private input %s mode=%04o want%04o", name, info.Mode().Perm(), want)
				}
			}
		})
	}
}

func TestTaskWorkspaceApplyUsesExplicitDirectoryDeletionCoverage(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "newly-ignored-retained", true: "explicit-empty-directory-deletion"}[remove], func(t *testing.T) {
			agent, id, _, source, copyDir := completedWorkspaceApplyFixtureWithInput(t, false, func(source string) {
				if err := os.Mkdir(filepath.Join(source, "keep"), 0750); err != nil {
					t.Fatal(err)
				}
			})
			if remove {
				if err := os.Remove(filepath.Join(copyDir, "keep")); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, filepath.Join(copyDir, ".gitignore"), "keep/\n")
			}
			if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
				t.Fatal(err)
			}
			if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(filepath.Join(source, "keep"))
			if remove {
				if !os.IsNotExist(err) {
					t.Fatalf("explicit empty directory deletion not applied: %v", err)
				}
			} else if err != nil || info.Mode().Perm() != 0750 {
				t.Fatalf("newly ignored directory was changed: %v %v", info, err)
			}
		})
	}
}

func TestTaskWorkspaceApplyRejectsTamperedPortableMetadata(t *testing.T) {
	agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, false)
	paths, err := filepath.Glob(home.Join("workspace-apply", "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("review receipt missing: %v", err)
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var ticket taskWorkspaceReviewTicket
	if err = json.Unmarshal(raw, &ticket); err != nil {
		t.Fatal(err)
	}
	entry := ticket.Portable.Entries["shared.txt"]
	entry.Mode = 0600
	ticket.Portable.Entries["shared.txt"] = entry
	if err = os.Chmod(filepath.Join(ticket.Place, "portable", "shared.txt"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(ticket)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(paths[0], raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil {
		t.Fatal("tampered portable metadata acquired landing authority")
	}
	if got := readFile(t, filepath.Join(source, "shared.txt")); got != "the original line\n" {
		t.Fatalf("original changed: %q", got)
	}
	if got := readFile(t, filepath.Join(copyDir, "shared.txt")); got != "private result\n" {
		t.Fatalf("original private copy changed: %q", got)
	}
}

func TestTaskWorkspaceReviewRefusesNewOrChangedWithheldOutput(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "changed"}[existing], func(t *testing.T) {
			agent, id, _, source, copyDir := completedWorkspaceApplyFixtureWithInput(t, false, func(source string) {
				if existing {
					writeFile(t, filepath.Join(source, ".env"), "FIXTURE_VALUE=original-private-input\n")
				}
			})
			paths, _ := filepath.Glob(home.Join("workspace-apply", "*.json"))
			before, err := os.ReadFile(paths[0])
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(copyDir, ".env"), "FIXTURE_VALUE=changed-private-output\n")
			if _, err = agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err == nil || !strings.Contains(err.Error(), "withheld") {
				t.Fatalf("withheld output did not refuse review: %v", err)
			}
			after, err := os.ReadFile(paths[0])
			if err != nil || string(after) != string(before) {
				t.Fatal("failed review replaced prior usable receipt")
			}
			if got := readFile(t, filepath.Join(copyDir, ".env")); got != "FIXTURE_VALUE=changed-private-output\n" {
				t.Fatal("private output changed")
			}
			if existing {
				if got := readFile(t, filepath.Join(source, ".env")); got != "FIXTURE_VALUE=original-private-input\n" {
					t.Fatal("original withheld input changed")
				}
			} else if _, err = os.Stat(filepath.Join(source, ".env")); !os.IsNotExist(err) {
				t.Fatal("withheld output reached original")
			}
		})
	}
}

func TestTaskWorkspaceApplyRetainsUnchangedWithheldInput(t *testing.T) {
	agent, id, _, source, copyDir := completedWorkspaceApplyFixtureWithInput(t, false, func(source string) {
		writeFile(t, filepath.Join(source, ".env"), "FIXTURE_VALUE=original-private-input\n")
	})
	if got := readFile(t, filepath.Join(copyDir, ".env")); got != "FIXTURE_VALUE=original-private-input\n" {
		t.Fatal("local private input is unavailable")
	}
	if out, err := git(copyDir, "ls-files", "--", ".env"); err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("withheld input entered history: %q %v", out, err)
	}
	paths, _ := filepath.Glob(home.Join("workspace-apply", "*.json"))
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var ticket taskWorkspaceReviewTicket
	if err = json.Unmarshal(raw, &ticket); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(ticket.Place, "portable", ".env")); !os.IsNotExist(err) {
		t.Fatal("withheld input entered portable payload")
	}
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(source, ".env")); got != "FIXTURE_VALUE=original-private-input\n" {
		t.Fatal("withheld original input was changed by apply")
	}
}

func TestTaskWorkspaceDoorsRefuseInheritedGitRouting(t *testing.T) {
	agent, id, _, source, _ := completedWorkspaceApplyFixture(t, false)
	foreign := newTestRepo(t)
	config := filepath.Join(foreign, ".git", "config")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"GIT_DIR", "GIT_CONFIG"} {
		t.Run(name, func(t *testing.T) {
			value := filepath.Join(foreign, ".git")
			if name == "GIT_CONFIG" {
				value = config
			}
			t.Setenv(name, value)
			if _, err = agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err == nil || !strings.Contains(err.Error(), "unset") {
				t.Fatalf("review routing refusal is not actionable: %v", err)
			}
			if _, err = agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil || !strings.Contains(err.Error(), "unset") {
				t.Fatalf("apply routing refusal is not actionable: %v", err)
			}
			after, err := os.ReadFile(config)
			if err != nil || string(after) != string(before) {
				t.Fatal("foreign Git config changed")
			}
			if got := readFile(t, filepath.Join(source, "shared.txt")); got != "the original line\n" {
				t.Fatal("original changed under inherited Git routing")
			}

		})
	}
}

func TestTaskWorkspaceFamilyRetainsIgnoredRuntimeInputs(t *testing.T) {
	agent, id, _, source, copyDir := completedWorkspaceApplyFixtureWithInput(t, false, func(source string) {
		writeFile(t, filepath.Join(source, ".gitignore"), "cache/\n")
		writeFile(t, filepath.Join(source, "cache", "input.txt"), "local build input\n")
	})
	if got := readFile(t, filepath.Join(copyDir, "cache", "input.txt")); got != "local build input\n" {
		t.Fatal("ignored local input unavailable")
	}
	if out, err := git(copyDir, "ls-files", "--", "cache"); err != nil || !strings.Contains(out, "cache/input.txt") {
		t.Fatalf("family history lost runtime input: %q %v", out, err)
	}
	child := filepath.Join(t.TempDir(), "child")
	if out, err := git(copyDir, "worktree", "add", "--detach", child, "HEAD"); err != nil {
		t.Fatalf("restore child world: %s %v", out, err)
	}
	if got := readFile(t, filepath.Join(child, "cache", "input.txt")); got != "local build input\n" {
		t.Fatal("child lost ignored runtime input")
	}
	paths, _ := filepath.Glob(home.Join("workspace-apply", "*.json"))
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var ticket taskWorkspaceReviewTicket
	if err = json.Unmarshal(raw, &ticket); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(ticket.Place, "portable", "cache", "input.txt")); got != "local build input\n" {
		t.Fatal("portable family world lost ignored runtime input")
	}
	if _, err = agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(source, "cache", "input.txt")); got != "local build input\n" {
		t.Fatal("ignored original input changed")
	}
}

func TestTaskWorkspaceExplicitApplyToMain(t *testing.T) {
	agent, id, _, source, copyDir := completedWorkspaceApplyFixtureWithInput(t, true, func(source string) {
		mustGit(t, source, "branch", "-m", "main")
	})
	if _, err := agent.ReviewTaskWorkspaceRoot(t.Context(), id, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ApplyTaskWorkspaceRoot(t.Context(), id, "work"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(source, "shared.txt")); got != "private result\n" {
		t.Fatalf("explicit main apply did not publish reviewed bytes: %q", got)
	}
	if got := gitOut(t, source, "branch", "--show-current"); strings.TrimSpace(got) != "main" {
		t.Fatalf("explicit apply changed the chosen branch: %q", got)
	}
	if got := readFile(t, filepath.Join(copyDir, "shared.txt")); got != "private result\n" {
		t.Fatalf("explicit apply did not retain private result: %q", got)
	}
}
