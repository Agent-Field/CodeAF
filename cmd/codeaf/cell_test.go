package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestCellLogAndRewindThroughTheDoor(t *testing.T) {
	if _, err := furrow.ResolveBinary(); err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, body := range []string{"one", "two"} {
		if err := os.WriteFile(filepath.Join(c.Root, "f.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := cellstore.Engine{}.Seal(context.Background(), c, cellstore.TurnInfo{})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.Turn.ID)
	}

	var out bytes.Buffer
	if err := runCellIn([]string{"rewind", ids[0][:10]}, &out, c.Root); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(c.Root, "f.txt")); string(got) != "one" {
		t.Fatalf("f.txt = %q after rewind, want one", got)
	}
	out.Reset()
	if err := runCellIn([]string{"log"}, &out, filepath.Join(c.Root, ".cell")); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], ids[1][:12]) || !strings.Contains(lines[0], "cell.rewind") {
		t.Fatalf("log:\n%s", out.String())
	}
}

func TestCellDoorRefusesShortCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"nope"}, {"rewind"}, {"log", "a", "b"}} {
		if err := runCellIn(args, &bytes.Buffer{}, t.TempDir()); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("%v: err = %v, want usage", args, err)
		}
	}
}

func TestCellRewindRestoresTheWorkspaceTheSessionSealed(t *testing.T) {
	if _, err := furrow.ResolveBinary(); err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if err := session.SaveMeta(c.Root, session.Meta{ID: "x", Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	seal := func(body string) string {
		if err := os.WriteFile(filepath.Join(workspace, "f.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := cellstore.EngineFor(workspace).Seal(context.Background(), c, cellstore.TurnInfo{})
		if err != nil {
			t.Fatal(err)
		}
		return s.Turn.ID
	}
	first := seal("one")
	seal("two")

	if err := runCellIn([]string{"rewind", first[:10]}, io.Discard, c.Root); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(workspace, "f.txt")); string(got) != "one" {
		t.Fatalf("workspace f.txt = %q after rewind, want one", got)
	}
	if _, err := os.Stat(filepath.Join(workspace, cell.StateDir)); !os.IsNotExist(err) {
		t.Fatal("the rewind wrote .cell into the workspace")
	}
}

func TestCellResolveClosesACrashedCallAndFindsBucketedCells(t *testing.T) {
	if _, err := furrow.ResolveBinary(); err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	bucket := filepath.Join(home.Join("v3", "projects"), "-work")
	c, err := cell.CreateIn(bucket, cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := cellstore.Engine{}.Seal(context.Background(), c, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	wal, _, err := cellstore.OpenWAL(cellEngine(c).WALPath(c))
	if err != nil {
		t.Fatal(err)
	}
	if err := wal.Begin(cellstore.Intent{V: 1, Tool: "bash", ArgsHash: "ab", Started: 1, SideEffect: "local"}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runCellIn([]string{"log", c.ID}, &out, t.TempDir()); err != nil || !strings.Contains(out.String(), "unfinished  bash") {
		t.Fatalf("log by id: %v\n%s", err, out.String())
	}
	if err := runCellIn([]string{"rewind", sealed.Turn.ID[:10], c.ID}, &out, t.TempDir()); err == nil {
		t.Fatal("rewind must refuse while a call is unfinished")
	}
	out.Reset()
	if err := runCellIn([]string{"resolve", c.ID}, &out, t.TempDir()); err != nil || !strings.Contains(out.String(), "resolved  bash") {
		t.Fatalf("resolve: %v\n%s", err, out.String())
	}
	if err := runCellIn([]string{"rewind", sealed.Turn.ID[:10], c.ID}, &out, t.TempDir()); err != nil {
		t.Fatalf("rewind after resolve: %v", err)
	}
}

// A CALL THAT FINISHED AND WAS NEVER SEALED IS A GAP THE CHAIN CANNOT SHOW, so
// the log says so, and stops saying so once a seal has taken the call.
func TestCellLogMarksCallsThatWereNeverSealed(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	c, err := cell.CreateIn(filepath.Join(home.Join("v3", "projects"), "-work"), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	wal, _, err := cellstore.OpenWAL(cellEngine(c).WALPath(c))
	if err != nil {
		t.Fatal(err)
	}
	in := cellstore.Intent{V: 1, Tool: "bash", ArgsHash: "ab", Started: 1, SideEffect: "local"}
	done := cellstore.Executed{Call: cellstore.Call{Tool: "bash", ArgsHash: "ab", Started: 1}}
	if err := wal.Begin(in); err != nil {
		t.Fatal(err)
	}
	if err := wal.Finish(in, done); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := writeGaps(c, &out); err != nil || !strings.Contains(out.String(), "unsealed  1 call(s)") {
		t.Fatalf("log after an unsealed call: %v\n%s", err, out.String())
	}
	if err := wal.Sealed([]cellstore.Executed{done}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := writeGaps(c, &out); err != nil || out.Len() != 0 {
		t.Fatalf("log after the call was sealed: %v\n%s", err, out.String())
	}
}

// EACH CELL'S SEAT OWNS ITS OWN WATCH: a failure on one is not on the other.
func TestEachSeatOwnsItsSealWatch(t *testing.T) {
	if _, err := furrow.ResolveBinary(); err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	bucket := filepath.Join(home.Join("v3", "projects"), "-work")
	seatOf := func() *cellstore.SealWatch {
		c, err := cell.CreateIn(bucket, cell.Options{Class: cell.Sandboxed})
		if err != nil {
			t.Fatal(err)
		}
		cfg := v3Seated(session.Config{Place: session.Place{Dir: c.Root, Workspace: t.TempDir()}})
		watch, ok := cfg.Seals.(*cellstore.SealWatch)
		if !ok {
			t.Fatalf("no watch on a sealed session: %T", cfg.Seals)
		}
		return watch
	}
	first, second := seatOf(), seatOf()
	if first == second {
		t.Fatal("two cells share one watch")
	}
	first.Report(errors.New("disk full"))
	if !first.Failing() || second.Failing() {
		t.Fatal("a failure on one cell showed on the other")
	}
}
