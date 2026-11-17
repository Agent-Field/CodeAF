package syncsetup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// openStateRecords are the conversation records a person sees the moment a moved
// chat opens: the transcript, and the task and artifact files beside it.
func openStateRecords(t *testing.T, c cell.Cell) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	state := filepath.Join(c.Root, cell.StateDir)
	for _, sub := range []string{"tasks", "artifacts"} {
		entries, err := os.ReadDir(filepath.Join(state, sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if raw, err := os.ReadFile(filepath.Join(state, sub, e.Name())); err == nil {
				out[sub+"/"+e.Name()] = raw
			}
		}
	}
	if raw, err := os.ReadFile(transcriptOf(c)); err == nil {
		out["transcript.jsonl"] = raw
	}
	return out
}

func assertRecordsMatch(t *testing.T, step string, want, got map[string][]byte) {
	t.Helper()
	if len(want) == 0 {
		t.Fatalf("%s: the source holds no records to compare", step)
	}
	for name, w := range want {
		g, ok := got[name]
		switch {
		case !ok:
			t.Errorf("%s: %s is missing on the receiving computer", step, name)
		case !bytes.Equal(g, w):
			t.Errorf("%s: %s differs from the source's (%d bytes, source has %d): the receiving computer kept an older copy", step, name, len(g), len(w))
		}
	}
}

// sameLengthEdit rewrites file in place with the same number of bytes and
// different content, as a task file or a transcript line looks after the far
// side has updated it.
func sameLengthEdit(t *testing.T, path, from, to string) {
	t.Helper()
	if len(from) != len(to) {
		t.Fatalf("fixture: %q and %q differ in length", from, to)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(from)) {
		t.Fatalf("fixture: %s holds no %q", path, from)
	}
	mustNil(t, os.WriteFile(path, bytes.Replace(raw, []byte(from), []byte(to), 1), 0o600))
}

// Contract: whenever a chat moves, the records it opens on are the ones the
// computer it came from last wrote, byte for byte. A computer that already holds
// an older copy never keeps it: a task or artifact file changed on the other
// computer replaces the old one even when the two have the same length, and a
// transcript whose middle was rewritten in place is the new transcript.
// Local-edit protection is a separate contract; nothing is edited here after the
// chat opens.
func TestMovesBackAndForthAlwaysOpenOnTheSourcesRecordsNeverAnOlderLocalCopy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	ctx := context.Background()
	h := newTwoHomes(t)
	seedTree(t, h.work)

	a := h.openA()
	// A transcript big enough to hold several chunks, so a same-length rewrite
	// lands inside one chunk and leaves its neighbours alone.
	for i := 0; i < 600; i++ {
		appendTo(t, transcriptOf(h.cell), fmt.Sprintf(`{"type":"message","role":"user","content":"line-%04d %s"}`+"\n", i, strings.Repeat("q", 240)))
	}
	state := filepath.Join(h.cell.Root, cell.StateDir)
	mustNil(t, os.MkdirAll(filepath.Join(state, "tasks"), 0o700))
	mustNil(t, os.MkdirAll(filepath.Join(state, "artifacts"), 0o700))
	task := filepath.Join(state, "tasks", "t1.jsonl")
	art := filepath.Join(state, "artifacts", "plan.txt")
	mustNil(t, os.WriteFile(task, []byte(`{"id":1,"owner":"tower-v1","notes":"first"}`+"\n"), 0o600))
	mustNil(t, os.WriteFile(art, []byte("plan written on tower, version one\n"), 0o600))
	a.mustSay("turn one on the first computer")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(ctx))

	step := "A to B (B holds nothing yet)"
	wantRecords := openStateRecords(t, h.cell)
	var shownTranscript []byte
	contB := h.continuerB()
	contB.opt.OnShown = func(got Continued) { shownTranscript, _ = os.ReadFile(transcriptOf(got.Taken.Cell)) }
	toB, err := contB.Take(ctx, h.cell.ID)
	mustNil(t, err)
	mustNil(t, toB.Wait(ctx))
	assertRecordsMatch(t, step, wantRecords, openStateRecords(t, toB.Taken.Cell))
	if !bytes.Equal(shownTranscript, wantRecords["transcript.jsonl"]) {
		t.Errorf("%s: the transcript at the moment the chat opened was not the source's", step)
	}

	// B works: it rewrites a task and an artifact with the same length and
	// changes a line in the middle of the transcript without changing its size.
	b := h.openOn(h.b, h.engB, toB.Taken.Cell, workspaceOf(toB.Taken.Cell.Root), nameB)
	bState := filepath.Join(toB.Taken.Cell.Root, cell.StateDir)
	sameLengthEdit(t, filepath.Join(bState, "tasks", "t1.jsonl"), "tower-v1", "laptop-2")
	sameLengthEdit(t, filepath.Join(bState, "artifacts", "plan.txt"), "version one", "version two")
	sameLengthEdit(t, transcriptOf(toB.Taken.Cell), "line-0300", "line-0X00")
	b.mustSay("turn two on the second computer")
	h.durable(h.cell.ID, toB.Taken.Cell)
	mustNil(t, b.drive.Close(ctx))

	step = "B to A (A holds the older copy it started with)"
	wantRecords = openStateRecords(t, toB.Taken.Cell)
	contA := h.continuerA()
	contA.opt.OnShown = func(got Continued) { shownTranscript, _ = os.ReadFile(transcriptOf(got.Taken.Cell)) }
	backA, err := contA.Take(ctx, h.cell.ID)
	mustNil(t, err)
	mustNil(t, backA.Wait(ctx))
	assertRecordsMatch(t, step, wantRecords, openStateRecords(t, backA.Taken.Cell))
	if !bytes.Equal(shownTranscript, wantRecords["transcript.jsonl"]) {
		t.Errorf("%s: the transcript at the moment the chat opened was not the source's", step)
	}

	// A works again, the same way.
	a = h.openA()
	aState := filepath.Join(backA.Taken.Cell.Root, cell.StateDir)
	sameLengthEdit(t, filepath.Join(aState, "tasks", "t1.jsonl"), "laptop-2", "tower-v3")
	sameLengthEdit(t, filepath.Join(aState, "artifacts", "plan.txt"), "version two", "version 333")
	sameLengthEdit(t, transcriptOf(backA.Taken.Cell), "line-0450", "line-0Y50")
	a.mustSay("turn three on the first computer")
	h.durable(h.cell.ID, backA.Taken.Cell)
	mustNil(t, a.drive.Close(ctx))

	step = "A to B again (B holds the copy from the first move)"
	wantRecords = openStateRecords(t, backA.Taken.Cell)
	contB = h.continuerB()
	contB.opt.OnShown = func(got Continued) { shownTranscript, _ = os.ReadFile(transcriptOf(got.Taken.Cell)) }
	toB2, err := contB.Take(ctx, h.cell.ID)
	mustNil(t, err)
	mustNil(t, toB2.Wait(ctx))
	assertRecordsMatch(t, step, wantRecords, openStateRecords(t, toB2.Taken.Cell))
	if !bytes.Equal(shownTranscript, wantRecords["transcript.jsonl"]) {
		t.Errorf("%s: the transcript at the moment the chat opened was not the source's", step)
	}
}

// Contract: a chat that moves opens with its project's own rules already in
// the workspace. The system prompt reads AGENTS.md and CLAUDE.md from the
// workspace root the moment the agent is built, which is before the rest of the
// workspace has arrived; those two small files therefore come in the same
// request set as the conversation records, without another round trip.
func TestMovedChatOpensWithTheWorkspaceInstructionFilesAlreadyOnDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	ctx := context.Background()
	h := newTwoHomes(t)
	seedTree(t, h.work)
	rules := "# house rules\nrun the whole gate before you say done\n"
	claude := "# more rules\nnever commit to main\n"
	mustNil(t, os.WriteFile(filepath.Join(h.work, "AGENTS.md"), []byte(rules), 0o644))
	mustNil(t, os.WriteFile(filepath.Join(h.work, "CLAUDE.md"), []byte(claude), 0o644))

	a := h.openA()
	a.mustSay("first turn")
	h.durable(h.cell.ID, h.cell)
	a.mustSay("second turn")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(ctx))

	rows, _, err := h.b.RowsAt(ctx)
	mustNil(t, err)
	if len(rows) != 1 {
		t.Fatalf("RowsAt returned %d rows, want 1", len(rows))
	}
	row := rows[0]
	rec := newRecordedRelay(t, h.b.Dir, h.b.Store, 400*time.Millisecond)
	rec.blockBackground()
	h.b.Dir = recordedDir{Client: h.b.Dir, rec: rec}
	h.b.Store = recordedStore{Store: h.b.Store, rec: rec}

	cont := h.continuerB()
	var shownRounds int
	var shownAgents, shownClaude string
	var shownRest bool
	cont.opt.OnShown = func(got Continued) {
		shownRounds, _, _, _, _ = rec.snapshot()
		work := workspaceOf(got.Taken.Cell.Root)
		raw, _ := os.ReadFile(filepath.Join(work, "AGENTS.md"))
		shownAgents = string(raw)
		raw, _ = os.ReadFile(filepath.Join(work, "CLAUDE.md"))
		shownClaude = string(raw)
		_, statErr := os.Stat(filepath.Join(work, "README.md"))
		shownRest = statErr == nil
	}
	seen := &directory.LeaseExpectation{Device: row.DeviceID, Fence: row.Fence}
	got, err := cont.TakeSeen(ctx, row.Cell, seen)
	mustNil(t, err)
	t.Cleanup(func() {
		rec.unblock()
		if got.Wait != nil {
			_ = got.Wait(context.Background())
		}
	})

	if shownAgents != rules || shownClaude != claude {
		t.Errorf("at the moment the chat opened AGENTS.md = %q and CLAUDE.md = %q; want the project's own rules already there", shownAgents, shownClaude)
	}
	if shownRounds == 0 || shownRounds > 2 {
		t.Errorf("the chat opened after %d serial relay rounds, want at most 2 even with the instruction files", shownRounds)
	}
	if shownRest {
		t.Errorf("the rest of the workspace had already arrived when the chat opened; the instruction files must not drag it in")
	}
	rec.unblock()
	mustNil(t, got.Wait(ctx))
	assertSameTree(t, h.work, workspaceOf(got.Taken.Cell.Root))
}
