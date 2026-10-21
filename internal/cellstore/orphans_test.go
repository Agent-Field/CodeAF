package cellstore

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// startGroup starts a sleeping process as the leader of its own group and
// returns it with a channel that closes when it is gone.
func startGroup(t *testing.T) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "60") //codeaf:plumbing test double for a tool's process
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	gone := make(chan struct{})
	go func() { _ = cmd.Wait(); close(gone) }()
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); <-gone })
	return cmd, gone
}

// deadProcess is an owner that no longer exists, with the identity it had.
func deadProcess(t *testing.T) (pid int, start uint64) {
	t.Helper()
	cmd, gone := startGroup(t)
	pid, start = cmd.Process.Pid, processgroup.StartOf(cmd.Process.Pid)
	_ = cmd.Process.Kill()
	<-gone
	return pid, start
}

// crashWithGroup leaves the WAL a killed session would: an intent for a shell
// call, and the group it started, owned by owner.
func crashWithGroup(t *testing.T, wal string, group *exec.Cmd, owner int, ownerStart uint64) {
	t.Helper()
	w, _, err := OpenWAL(wal)
	if err != nil {
		t.Fatal(err)
	}
	in := Intent{V: walV, Tool: "bash", ArgsHash: "h", Started: 7, SideEffect: "local", Brief: "sleep 60"}
	rec := GroupRec{PGID: group.Process.Pid, Start: processgroup.StartOf(group.Process.Pid), Owner: owner, OwnerStart: ownerStart}
	if err := w.Begin(in); err != nil {
		t.Fatal(err)
	}
	if err := w.Started(in, rec); err != nil {
		t.Fatal(err)
	}
}

func reopen(t *testing.T, wal string) *Recorder {
	t.Helper()
	r, _ := newRecorder(t, newCell(t), &stubExec{}, wal)
	return r
}

func TestReopenEndsTheGroupItsDeadOwnerLeft(t *testing.T) {
	group, gone := startGroup(t)
	owner, ownerStart := deadProcess(t)
	wal := filepath.Join(t.TempDir(), "wal")
	crashWithGroup(t, wal, group, owner, ownerStart)

	reopen(t, wal)

	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("the orphaned group was still running after reopen")
	}
}

func TestReopenNeverTouchesAGroupItsOwnerStillRuns(t *testing.T) {
	group, gone := startGroup(t)
	self := processgroup.StartOf(selfPID())
	wal := filepath.Join(t.TempDir(), "wal")
	crashWithGroup(t, wal, group, selfPID(), self)

	reopen(t, wal)

	select {
	case <-gone:
		t.Fatal("a group whose owner is alive was ended")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestReopenNeverTouchesAGroupWhoseLeaderIsNotTheOneRecorded(t *testing.T) {
	group, gone := startGroup(t)
	owner, ownerStart := deadProcess(t)
	wal := filepath.Join(t.TempDir(), "wal")
	crashWithGroup(t, wal, group, owner, ownerStart)
	staleStart := processgroup.StartOf(group.Process.Pid) + 1
	rewriteGroupStart(t, wal, staleStart)

	reopen(t, wal)

	select {
	case <-gone:
		t.Fatal("a group whose recorded identity no longer matches was ended")
	case <-time.After(300 * time.Millisecond):
	}
}

func rewriteGroupStart(t *testing.T, wal string, start uint64) {
	t.Helper()
	recs, err := (&WAL{path: wal}).read()
	if err != nil {
		t.Fatal(err)
	}
	for i := range recs {
		if recs[i].Group != nil {
			recs[i].Group.Start = start
		}
	}
	if err := (&WAL{path: wal}).rewrite(recs); err != nil {
		t.Fatal(err)
	}
}

// A shell call that a kill -9 leaves behind: the recorder was told the group by
// the tool, and the process died before the call did.
func TestAroundRecordsTheGroupTheToolStarted(t *testing.T) {
	group, _ := startGroup(t)
	wal := filepath.Join(t.TempDir(), "wal")
	r, _ := newRecorder(t, newCell(t), &stubExec{}, wal)
	ctx, spawns := executor.WithSpawns(context.Background())
	call := executor.Call{Tool: "bash", Args: []byte(`{"command":"sleep 60"}`), Spawns: spawns}
	func() {
		defer func() { _ = recover() }()
		_ = r.Around(ctx, call, executor.EffectLocal, AgentRun, func() ([]byte, bool) {
			executor.Spawned(ctx, group.Process.Pid)
			panic("harness died")
		})
	}()

	_, rec, err := OpenWAL(wal)
	if err != nil || len(rec.Incomplete) != 1 {
		t.Fatalf("incomplete %+v, err %v", rec.Incomplete, err)
	}
	got := rec.Incomplete[0]
	if got.Brief != "sleep 60" || len(got.Groups) != 1 || got.Groups[0].PGID != group.Process.Pid || got.Groups[0].Owner != selfPID() {
		t.Fatalf("intent %+v", got)
	}
}

func TestInterruptedSaysWhichCallWasCutOffAndClosesOnce(t *testing.T) {
	group, _ := startGroup(t)
	owner, ownerStart := deadProcess(t)
	wal := filepath.Join(t.TempDir(), "wal")
	crashWithGroup(t, wal, group, owner, ownerStart)

	cut := reopen(t, wal).Interrupted()
	lines := cut.Lines()
	if len(lines) != 1 || !strings.Contains(lines[0], "bash: sleep 60") || !strings.Contains(lines[0], "stopped when codeaf was killed — not run again") {
		t.Fatalf("lines %q", lines)
	}
	if note := cut.Note(); !strings.Contains(note, "bash: sleep 60") || !strings.Contains(note, "Do not assume any of them succeeded") {
		t.Fatalf("note %q", note)
	}
	if err := cut.Close(); err != nil {
		t.Fatal(err)
	}
	again := reopen(t, wal).Interrupted()
	if len(again.Lines()) != 0 || again.Note() != "" {
		t.Fatal("a closed call was told again")
	}
}

func TestAnOlderLogWithoutBriefOrGroupsStillReplays(t *testing.T) {
	wal := filepath.Join(t.TempDir(), "wal")
	old, _ := json.Marshal(map[string]any{"V": 1, "op": "intent",
		"intent": map[string]any{"V": 1, "tool": "bash", "args_hash": "h", "started": 1, "side_effect": "local"}})
	if err := writeLog(wal, string(old)+"\n"); err != nil {
		t.Fatal(err)
	}
	lines := reopen(t, wal).Interrupted().Lines()
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "bash — stopped") {
		t.Fatalf("lines %q", lines)
	}
}

func TestBriefOfReadsTheArgumentThatNamesTheCall(t *testing.T) {
	for args, want := range map[string]string{
		`{"command":"ls   -la\n/tmp"}`: "ls -la /tmp",
		`{"path":"a/b.go","x":"y"}`:    "a/b.go",
		`{"other":1}`:                  "",
		`not json`:                     "",
	} {
		if got := briefOf([]byte(args)); got != want {
			t.Errorf("briefOf(%s) = %q, want %q", args, got, want)
		}
	}
	if long := briefOf([]byte(`{"command":"` + strings.Repeat("x", 200) + `"}`)); len([]rune(long)) != briefMax {
		t.Errorf("long brief has %d runes, want %d", len([]rune(long)), briefMax)
	}
}

func selfPID() int { return syscall.Getpid() }

func writeLog(path, text string) error {
	return os.WriteFile(path, []byte(text), 0o600)
}
