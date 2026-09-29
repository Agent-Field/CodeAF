package cellstore

import (
	"context"
	"errors"
	"os"
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
}

func TestRewindOfARewind(t *testing.T) {
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
}

func TestRewindRefusedWhileACallIsIncomplete(t *testing.T) {
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
