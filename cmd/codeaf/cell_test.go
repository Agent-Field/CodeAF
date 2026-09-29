package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/furrow"
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
