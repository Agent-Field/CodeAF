package session

// Awareness keyed by the repository as well as by the project folder, and the
// worker harness's runs carrying the files they touched onto the project's
// record (taskrepo.go, task_run_belt.go, taskdelta.go).
//
// Measured on 2026-09-24: of 26 cases where two chats touched the same file
// within a day, the block could show 9. The other 17 were one repository
// reached from chats filed under different project folders, and no run on the
// worker harness had ever written down which files it touched. These tests are
// those two holes, plus the law that a reading which could not look must not
// read the same as a reading that found nothing.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// repoScopeAgent is a conversation in folder mine of the bucket it names, whose
// working directory is workspace, built the way the other delta tests build one.
func repoScopeAgent(t *testing.T, mine, workspace string) *Agent {
	t.Helper()
	if err := os.MkdirAll(mine, 0o700); err != nil {
		t.Fatalf("session folder: %v", err)
	}
	agent := &Agent{config: Config{Workspace: workspace, Place: Place{Dir: mine, Workspace: workspace}}}
	t.Cleanup(agent.SettleWrites)
	agent.messages = []ai.Message{textMessage("system", "base")}
	agent.system = "base"
	return agent
}

// landedOn is one finished row another chat wrote, about work on ground.
func landedOn(id, session, title, ground string, ended time.Time, files ...string) TaskIndexEntry {
	row := indexRow(id, session, title, string(TaskDone))
	row.EndedAt = ended
	row.Ground = ground
	row.Files = files
	row.FilesChanged = len(files)
	return row
}

// writePresenceIn writes one live window into a bucket, working in workspace.
func writePresenceIn(t *testing.T, bucket, id, workspace string, tasks ...PresenceTask) {
	t.Helper()
	dir := filepath.Join(bucket, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("window folder: %v", err)
	}
	raw, err := json.Marshal(SessionPresence{
		Schema:       presenceSchema,
		SessionID:    id,
		Workspace:    workspace,
		PID:          4242,
		UpdatedAt:    time.Now().Add(-time.Second),
		State:        PresenceWorking,
		RunningTasks: tasks,
	})
	if err != nil {
		t.Fatalf("marshal presence: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, presenceName), raw, 0o600); err != nil {
		t.Fatalf("write presence: %v", err)
	}
}

// A RUN ON THE WORKER HARNESS WRITES DOWN EVERY FILE IT TOUCHED. Its row in the
// project's record is the fact `<elsewhere>`, the `tasks` tool and the sessions
// rows already read for the older engine's tasks, and a run left it out: 0 of
// 138 measured. The list is the working copy's own diff against the commit the
// run was cut from, so a file a worker committed itself counts as surely as one
// the landing committed for it.
func TestABeltRunsRecordNamesEveryFileItTouched(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	conversation := beltRunCommittedRepo(t)
	dir := filepath.Join(t.TempDir(), "chat")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	double := newBeltRunDouble("the change is made")
	double.real = true
	double.work = func(workspace string) {
		// One worker commits its own work mid-run; the other leaves its write for
		// the landing to commit.
		if err := os.WriteFile(filepath.Join(workspace, "committed.txt"), []byte("a worker's own commit\n"), 0o644); err != nil {
			t.Errorf("worker write: %v", err)
		}
		if out, err := git(workspace, "add", "committed.txt"); err != nil {
			t.Errorf("worker add: %v\n%s", err, out)
		}
		if out, err := git(workspace, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "worker"); err != nil {
			t.Errorf("worker commit: %v\n%s", err, out)
		}
		if err := os.WriteFile(filepath.Join(workspace, "made.txt"), []byte("left for the landing\n"), 0o644); err != nil {
			t.Errorf("worker write: %v", err)
		}
	}
	registerBeltRunEngine(t, double)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: dir}
	})
	if err := agent.startKnownTaskRun(context.Background(), 71, "make the change", "brief", nil, taskStand{dir: conversation, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-double.entered
	endBeltRun(t, agent, double)

	var row *TaskIndexEntry
	for _, entry := range ReadTaskIndex(agent.config.taskIndexFile()) {
		if entry.ID == "71" {
			entry := entry
			row = &entry
		}
	}
	if row == nil {
		t.Fatalf("the run left no row in the project's record %s", agent.config.taskIndexFile())
	}
	got := strings.Join(row.Files, ",")
	if !strings.Contains(got, "committed.txt") || !strings.Contains(got, "made.txt") || row.FilesChanged != 2 {
		t.Fatalf("the run's row names %q (%d changed), want committed.txt and made.txt", got, row.FilesChanged)
	}
	if row.Status != string(TaskDone) || row.EndedAt.IsZero() || row.Title != "make the change" {
		t.Fatalf("the run's row is %+v, want a landed done row with its title", row)
	}
	if row.Ground != canonicalPath(conversation) {
		t.Fatalf("the run's row names ground %q, want %q", row.Ground, canonicalPath(conversation))
	}
}

// TWO CHATS ON ONE REPOSITORY SEE EACH OTHER, WHATEVER FOLDER EACH WAS FILED
// UNDER. The chat here works in a linked worktree of the repository; the other
// chat was launched elsewhere and its work was about the repository itself. A
// chat on an unrelated repository in a third folder is not news here.
func TestElsewhereSeesTheSameRepositoryFromAnotherProjectFolder(t *testing.T) {
	repo := newTestRepo(t)
	worktree := filepath.Join(t.TempDir(), "side")
	mustGit(t, repo, "worktree", "add", "-b", "side", worktree)
	unrelated := newTestRepo(t)

	root := t.TempDir()
	mine := filepath.Join(root, "proj-here", "mine")
	home := filepath.Join(root, "proj-home")
	third := filepath.Join(root, "proj-third")
	for _, bucket := range []string{home, third} {
		if err := os.MkdirAll(bucket, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	appendTaskIndex(filepath.Join(home, taskIndexName),
		landedOn("1", "theirs", "Sweep the call sites", repo, time.Now().Add(-time.Minute), "internal/session/agent.go"))
	appendTaskIndex(filepath.Join(third, taskIndexName),
		landedOn("2", "strangers", "Rewrite the unrelated thing", unrelated, time.Now().Add(-time.Minute), "other.go"))
	writePresenceIn(t, home, "live-one", repo,
		PresenceTask{ID: "5", Title: "Port the parser", State: string(TaskRunning), Files: []string{"internal/parser/parse.go"}})
	writePresenceIn(t, third, "live-two", unrelated,
		PresenceTask{ID: "6", Title: "Survey the unrelated loaders", State: string(TaskRunning)})

	agent := repoScopeAgent(t, mine, worktree)
	agent.refreshElsewhere(context.Background())
	block := agent.elsewhereText
	if !strings.Contains(block, "Sweep the call sites") {
		t.Fatalf("landed work on the same repository from another project folder is missing:\n%s", block)
	}
	if !strings.Contains(block, "Port the parser") {
		t.Fatalf("live work on the same repository from another project folder is missing:\n%s", block)
	}
	if !strings.Contains(block, "in proj-home") {
		t.Fatalf("a row from another project folder does not say which one:\n%s", block)
	}
	if strings.Contains(block, "unrelated") {
		t.Fatalf("work on an unrelated repository was told as news:\n%s", block)
	}
}

// SHARED FILES RANK FIRST, THEN THE SAME REPOSITORY, THEN THE SAME PROJECT
// FOLDER, and the cap is the block's own. A landing that shares a file with this
// chat's own work is kept even when six newer landings in the same folder would
// have pushed it past the cap by age alone.
func TestElsewhereRanksSharedFilesThenRepositoryThenFolder(t *testing.T) {
	repo := newTestRepo(t)
	root := t.TempDir()
	bucket := filepath.Join(root, "proj-here")
	mine := filepath.Join(bucket, "mine")
	home := filepath.Join(root, "proj-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mine, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	index := filepath.Join(bucket, taskIndexName)
	// This chat's own landed work, in the file the oldest landing shares.
	appendTaskIndex(index, landedOn("9", "mine", "My own fix", repo, now.Add(-3*time.Hour), "pkg/shared.go"))
	appendTaskIndex(index, landedOn("1", "theirs", "Shared file work", "", now.Add(-2*time.Hour), "pkg/shared.go"))
	for i := 0; i < deltaLandedRows+1; i++ {
		appendTaskIndex(index, landedOn(string(rune('a'+i)), "theirs", "Folder work "+string(rune('A'+i)), "", now.Add(-time.Duration(i+1)*time.Minute)))
	}
	appendTaskIndex(filepath.Join(home, taskIndexName),
		landedOn("3", "elsewhere", "Repository work", repo, now.Add(-90*time.Minute), "pkg/other.go"))

	agent := repoScopeAgent(t, mine, repo)
	agent.refreshElsewhere(context.Background())
	block := agent.elsewhereText
	shared := strings.Index(block, "Shared file work")
	sameRepo := strings.Index(block, "Repository work")
	folder := strings.Index(block, "Folder work A")
	if shared < 0 || sameRepo < 0 || folder < 0 {
		t.Fatalf("the block is missing a ranked row (shared %d, repository %d, folder %d):\n%s", shared, sameRepo, folder, block)
	}
	if !(shared < sameRepo && sameRepo < folder) {
		t.Fatalf("the rows are not ranked shared files, then repository, then folder:\n%s", block)
	}
	if got := strings.Count(block, "\n- "); got != deltaLandedRows {
		t.Fatalf("the block names %d landings, want the cap of %d:\n%s", got, deltaLandedRows, block)
	}
}

// A REPOSITORY THAT COULD NOT BE RESOLVED IS SAID, NOT SILENT. A working
// directory whose git link points at nothing is not the same fact as a folder
// that is not a repository, and a block that went quiet over it would read as
// "nobody else is on this repository".
func TestAnUnresolvableRepositoryIsSaidRatherThanReadAsNoOverlap(t *testing.T) {
	root := t.TempDir()
	mine := filepath.Join(root, "proj-here", "mine")
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, ".git"), []byte("gitdir: "+filepath.Join(root, "gone", ".git", "worktrees", "x")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := repoScopeAgent(t, mine, workspace)
	agent.refreshElsewhere(context.Background())
	block := agent.elsewhereText
	if block == "" {
		t.Fatal("a repository that could not be resolved produced no block, which reads as no overlap")
	}
	if !strings.Contains(block, "could not") {
		t.Fatalf("the block does not say the repository could not be resolved:\n%s", block)
	}
}

// A ROW WHOSE FILE LIST COULD NOT BE READ SAYS SO. It must not read like a row
// that simply named no files, which is what a run that touched nothing reads as.
func TestALandingWhoseFilesCouldNotBeReadSaysSo(t *testing.T) {
	bucket := t.TempDir()
	mine := filepath.Join(bucket, "mine")
	line := `{"id":"4","name":"port-the-parser","label":"Port the parser","title":"Port the parser","status":"done","outcome":"","filesChanged":0,"filesUnread":"the run's starting commit is not on record","endedAt":"` +
		time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano) + `","sessionId":"theirs"}` + "\n"
	if err := os.MkdirAll(bucket, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, taskIndexName), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := repoScopeAgent(t, mine, "/work/codeaf")
	agent.refreshElsewhere(context.Background())
	if !strings.Contains(agent.elsewhereText, "Port the parser · done · files unknown") {
		t.Fatalf("a landing whose files could not be read reads like one that named none:\n%s", agent.elsewhereText)
	}
}
