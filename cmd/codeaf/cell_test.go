package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/furrow"
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
