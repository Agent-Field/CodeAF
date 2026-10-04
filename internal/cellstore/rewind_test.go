package cellstore

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// sealStep changes the folder and the transcript, then seals one turn.
func sealStep(t *testing.T, e Engine, c cell.Cell, files map[string]string, line string) Sealed {
	t.Helper()
	writeTree(t, c.Root, files, 0o644)
	p, _ := c.Path(cell.TranscriptPath)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	s, err := e.Seal(context.Background(), c, TurnInfo{Calls: []Executed{exec1("edit", line)}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// contentDigest is the tree a turn holds, minus the chain's own files and the
// engine's directories.
func contentDigest(t *testing.T, root string) string {
	var keep []string
	for _, l := range treeDigest(t, root) {
		if !strings.HasPrefix(l, ReceiptsDir) && !strings.HasPrefix(l, BlobsDir) {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// threeTurns seals three turns and returns the content digest after each.
func threeTurns(t *testing.T) (Engine, cell.Cell, []Sealed, []string) {
	e := realEngine(t)
	c := newCell(t)
	steps := []struct {
		files map[string]string
		line  string
	}{
		{map[string]string{"a.txt": "one\n", "d/b.txt": "b1\n"}, `{"n":1}`},
		{map[string]string{"a.txt": "two\n", "c.txt": "new\n"}, `{"n":2}`},
		{map[string]string{"d/b.txt": "b3\n"}, `{"n":3}`},
	}
	var sealed []Sealed
	var digests []string
	for _, s := range steps {
		sealed = append(sealed, sealStep(t, e, c, s.files, s.line))
		digests = append(digests, contentDigest(t, c.Root))
	}
	return e, c, sealed, digests
}

func TestRewindRestoresFilesAndTranscriptAsANewTurn(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e, c, s, digests := threeTurns(t)
		rewound := mustRewind(t, e, c, s[0].Turn.ID)

		if got := contentDigest(t, c.Root); got != digests[0] {
			t.Fatalf("content differs from turn 1.\nwant:\n%s\ngot:\n%s", digests[0], got)
		}
		if tr, _ := os.ReadFile(rel(c, cell.TranscriptPath)); string(tr) != "{\"n\":1}\n" {
			t.Fatalf("transcript %q, want turn 1's", tr)
		}
		log, err := Log(c)
		if err != nil || len(log) != 4 {
			t.Fatalf("log has %d turns (%v), want 4", len(log), err)
		}
		if log[0].Turn.ID != rewound.Turn.ID || log[0].Turn.Parent != s[2].Turn.ID || log[0].Receipt.Calls[0].Tool != rewindTool {
			t.Fatalf("newest %+v, want the rewind with turn 3 as parent", log[0].Turn)
		}
		for i, want := range []string{s[2].Turn.ID, s[1].Turn.ID, s[0].Turn.ID} {
			if log[i+1].Turn.ID != want {
				t.Fatalf("history entry %d is %s, want %s: rewind must not rewrite the chain", i+1, log[i+1].Turn.ID, want)
			}
		}
	})
}

func TestRewindOfARewind(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e, c, s, digests := threeTurns(t)
		back := mustRewind(t, e, c, s[0].Turn.ID)
		mustRewind(t, e, c, s[2].Turn.ID)
		if got := contentDigest(t, c.Root); got != digests[2] {
			t.Fatalf("content differs from turn 3.\nwant:\n%s\ngot:\n%s", digests[2], got)
		}
		again := mustRewind(t, e, c, back.Turn.ID)
		if got := contentDigest(t, c.Root); got != digests[0] {
			t.Fatal("rewinding the rewind did not return to turn 1's content")
		}
		if log, _ := Log(c); len(log) != 6 || log[0].Turn.ID != again.Turn.ID {
			t.Fatalf("log has %d turns, want 6", len(log))
		}
	})
}

func TestRewindRefusedWhileACallIsIncomplete(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e, c, s, digests := threeTurns(t)
		wal, _, err := OpenWAL(e.WALPath(c))
		if err != nil {
			t.Fatal(err)
		}
		if err := wal.Begin(Intent{Tool: "deploy", ArgsHash: hashHex([]byte("x")), Started: 5, SideEffect: "external"}); err != nil {
			t.Fatal(err)
		}
		_, err = e.Rewind(context.Background(), c, s[0].Turn.ID)
		var incomplete IncompleteError
		if !errors.As(err, &incomplete) || !strings.Contains(err.Error(), "deploy") {
			t.Fatalf("err = %v, want an IncompleteError naming deploy", err)
		}
		if got := contentDigest(t, c.Root); got != digests[2] {
			t.Fatal("a refused rewind changed the tree")
		}
		if log, _ := Log(c); len(log) != 3 {
			t.Fatalf("a refused rewind sealed a turn: %d turns", len(log))
		}
	})
}

func TestResolveNeedsExactlyOneMatch(t *testing.T) {
	turns := []Turn{{ID: "abc1"}, {ID: "abd2"}}
	for ref, ok := range map[string]bool{"abc": true, "ab": false, "zz": false, "": false} {
		if _, err := Resolve(turns, ref); (err == nil) != ok {
			t.Errorf("Resolve(%q) err=%v, want ok=%v", ref, err, ok)
		}
	}
}

func mustRewind(t *testing.T, e Engine, c cell.Cell, ref string) Sealed {
	t.Helper()
	s, err := e.Rewind(context.Background(), c, ref)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// chainBytes is every chain file's content by cell-relative path.
func chainBytes(t *testing.T, c cell.Cell) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, p := range []string{TurnsPath, ReceiptsDir, BlobsDir} {
		_ = filepath.WalkDir(rel(c, p), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				raw, _ := os.ReadFile(path)
				out[path] = string(raw)
			}
			return nil
		})
	}
	return out
}

// dyingSeal is an engine that behaves like the real one except that the seal
// never happens: the process is gone after the restore.
func dyingSeal(e Engine) Engine {
	e.Transport = sealKiller{e.transport()}
	return e
}

type sealKiller struct{ Transport }

func (k sealKiller) Do(ctx context.Context, t Target, op Op) ([]byte, error) {
	if _, sealing := op.(sealOp); sealing {
		return nil, errors.New("killed before the seal")
	}
	return k.Transport.Do(ctx, t, op)
}

func TestCrashBetweenRestoreAndSealLeavesTheChainWhole(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e, c, s, digests := threeTurns(t)
		before := chainBytes(t, c)

		if _, err := dyingSeal(e).Rewind(context.Background(), c, s[0].Turn.ID); err == nil {
			t.Fatal("rewind reported success though the seal died")
		}
		// The seal wrote the rewind's receipt before it died: an unreferenced,
		// content-addressed file. Everything that was there stays byte for byte.
		after := chainBytes(t, c)
		for p, want := range before {
			if after[p] != want {
				t.Fatalf("%s changed by a rewind that never sealed", p)
			}
		}
		if log, err := Log(c); err != nil || len(log) != 3 {
			t.Fatalf("log after the crash: %d turns, %v; want the 3 sealed ones", len(log), err)
		}
		if got := contentDigest(t, c.Root); got != digests[0] {
			t.Fatal("the restore itself did not land before the crash")
		}
		// The next open is consistent: sealing what is on disk extends the old head.
		next, err := e.Seal(context.Background(), c, TurnInfo{Calls: []Executed{exec1("edit", "after crash")}})
		if err != nil || next.Turn.Parent != s[2].Turn.ID {
			t.Fatalf("next seal = %+v, %v; want a child of turn 3", next.Turn, err)
		}
	})
}

func TestRewindOfAWorkspaceSealedCellRestoresTheWorkspace(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		c := newCell(t)
		ws := t.TempDir()
		e := realEngine(t)
		e.Workspace = ws
		step := func(files map[string]string, line string) Sealed {
			writeTree(t, ws, files, 0o644)
			f, err := os.OpenFile(rel(c, cell.TranscriptPath), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.WriteString(line + "\n")
			_ = f.Close()
			s, err := e.Seal(context.Background(), c, TurnInfo{Calls: []Executed{exec1("edit", line)}})
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
		first := step(map[string]string{"a.txt": "one\n"}, `{"n":1}`)
		step(map[string]string{"a.txt": "two\n", "b.txt": "new\n"}, `{"n":2}`)

		rewound := mustRewind(t, e, c, first.Turn.ID)

		if raw, _ := os.ReadFile(filepath.Join(ws, "a.txt")); string(raw) != "one\n" {
			t.Fatalf("workspace a.txt = %q, want turn 1's", raw)
		}
		if _, err := os.Stat(filepath.Join(ws, "b.txt")); !os.IsNotExist(err) {
			t.Fatal("a file made after turn 1 survived the rewind")
		}
		if _, err := os.Stat(filepath.Join(ws, cell.StateDir)); !os.IsNotExist(err) {
			t.Fatal("the rewind wrote .cell into the workspace")
		}
		if tr, _ := os.ReadFile(rel(c, cell.TranscriptPath)); string(tr) != "{\"n\":1}\n" {
			t.Fatalf("transcript %q, want turn 1's", tr)
		}
		if log, err := Log(c); err != nil || len(log) != 3 || log[0].Turn.ID != rewound.Turn.ID {
			t.Fatalf("log = %d turns, %v; want the rewind on top of 2", len(log), err)
		}
		if left, _ := filepath.Glob(filepath.Join(e.LocalDir(c), "rewind-*")); len(left) != 0 {
			t.Fatalf("scratch left behind: %v", left)
		}
	})
}
