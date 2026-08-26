package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE RECORD CARD OVER --host ─────────────────────────────────────────────
//
// The tasks place learned to list the far machine's work before the CARD behind
// a row learned anything at all. So a press over --host drew the far machine's
// title, its money and its branch — and under them, read off THIS laptop's disk
// at a path that only exists on the server, `its transcript is not on this disk
// any more` about a journal sitting perfectly well over there. It is the same
// fault the whole places lane exists to end, one layer further in.
//
// What these hold it to: the card reads the FAR machine, it never opens a file
// here, it says something honest for every state of that reading, and the paths
// on it are said as the other machine's.

// farCardEntry is one row of the far machine's record, as the world walk hands
// it over: every field the card draws, and a journal path that is real over
// there and nowhere near this disk.
func farCardEntry(now time.Time) session.TaskIndexEntry {
	return session.TaskIndexEntry{
		ID: "9", Name: "widening", Label: "widening the pipe", Title: "widening the pipe",
		Status: string(session.TaskDone), Cost: 3.10, Model: "opus", FilesChanged: 3,
		Outcome: "widened the pipe", SessionID: "bbbb000000000002", EndedAt: now.Add(-time.Minute),
		ArtifactURI:   "git:task/widening",
		TranscriptURI: "file:///srv/home/.aforge/v3/projects/-srv-code-api/bbbb000000000002/tasks/20260826-094113_1.jsonl",
	}
}

// farCardLab is a hosted surface standing on the tasks place, with the far
// machine's world wired and its record answered by count.
func farCardLab(t *testing.T, answer func(uri string) (session.TaskRecord, error)) (*app, *atomic.Int64) {
	t.Helper()
	a := hostedPlaceLab(t)
	now := time.Now()
	entry := farCardEntry(now)
	a.world = func() (session.World, bool) {
		return session.World{
			Read: now,
			Projects: []session.Project{{
				Dir: "-srv-code-api", Path: "/srv/code/api", Name: "api",
				Sessions: []session.SessionRow{{
					ID: "bbbb000000000002", Title: "rewriting the importer",
					Project: "api", ProjectDir: "/srv/code/api", Workspace: "/srv/code/api",
					At: now, Created: now,
					Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{entry}},
				}},
			}},
		}, true
	}
	var asked atomic.Int64
	a.farRecord = func(uri string) (session.TaskRecord, error) {
		asked.Add(1)
		return answer(uri)
	}
	a.showPage(pageTasks)
	return a, &asked
}

// pressFarCard opens the card the way a hand does — a press on the row — and
// delivers whatever the reading answered.
func pressFarCard(t *testing.T, a *app) {
	t.Helper()
	width, height := a.size()
	_, hits, _, _ := a.taskSheetFrame(width, height)
	row := -1
	for y, hit := range hits {
		if hit.kind == taskSheetHitRow {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatalf("the tasks place drew no row of the far machine's work:\n%s", placeText(a))
	}
	cmd := a.taskSheetPress(2, row)
	if !a.taskSheet.detailOn {
		t.Fatalf("a press on a far task row opened nothing:\n%s", placeText(a))
	}
	if cmd == nil {
		t.Fatal("the card was opened with no reading behind it")
	}
	if msg := cmd(); msg != nil {
		tail, ok := msg.(taskTailMsg)
		if !ok {
			t.Fatalf("the reading answered something else: %#v", msg)
		}
		a.taskTailRead(tail)
	}
}

// The card opens over --host and its report is the FAR machine's — asked of the
// machine that owns the journal, exactly once.
func TestAHostedTaskCardReadsTheFarMachinesRecord(t *testing.T) {
	var asked string
	a, count := farCardLab(t, func(uri string) (session.TaskRecord, error) {
		asked = uri
		return session.TaskRecord{Report: "widened the pipe and re-ran the importer.", Kept: true}, nil
	})
	pressFarCard(t, a)
	if count.Load() != 1 {
		t.Fatalf("the far machine was asked %d times for one card", count.Load())
	}
	if asked != farCardEntry(time.Now()).TranscriptURI {
		t.Fatalf("the card asked about the wrong row: %q", asked)
	}
	text := placeText(a)
	if !strings.Contains(text, taskCardTailHead) || !strings.Contains(text, "re-ran the importer") {
		t.Fatalf("the card did not draw the far machine's report:\n%s", text)
	}
	// AND THE ROW'S OWN FACTS ARE STILL THE FAR MACHINE'S — they came over on the
	// walk, and a card that lost them while gaining a report would be a worse
	// card than the broken one.
	for _, want := range []string{"widening the pipe", "opus", "$3.10", "3 files changed", "task/widening"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the card lost %q:\n%s", want, text)
		}
	}
}

// AND IT NEVER OPENS A FILE ON THIS DISK. The path on the row is the engine's; a
// read of it here is either nothing or a stranger's file, and it is the read that
// produced the wrong sentence in the first place. The pin is a REAL journal at
// the far path, made on this machine — if anything in this lane ever falls back
// to the local disk, this is what it would find.
func TestAHostedTaskCardNeverReadsThisDisk(t *testing.T) {
	root := t.TempDir()
	journal := filepath.Join(root, "transcript.jsonl")
	if err := os.WriteFile(journal, []byte(`{"type":"message","role":"assistant","content":"THIS LAPTOP'S FILE"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := hostedPlaceLab(t)
	now := time.Now()
	entry := farCardEntry(now)
	entry.TranscriptURI = "file://" + journal
	a.world = func() (session.World, bool) {
		return session.World{Read: now, Projects: []session.Project{{
			Dir: "-srv-code-api", Path: "/srv/code/api", Name: "api",
			Sessions: []session.SessionRow{{
				ID: "bbbb000000000002", Title: "rewriting the importer",
				Project: "api", ProjectDir: "/srv/code/api", Workspace: "/srv/code/api",
				At: now, Created: now,
				Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{entry}},
			}},
		}}}, true
	}
	a.farRecord = func(string) (session.TaskRecord, error) {
		return session.TaskRecord{Report: "the far machine's word", Kept: true}, nil
	}
	a.showPage(pageTasks)
	pressFarCard(t, a)
	if text := placeText(a); strings.Contains(text, "THIS LAPTOP'S FILE") {
		t.Fatalf("the card read a journal on this disk:\n%s", text)
	}
}

// A HOSTED SURFACE WITH NO SEAM ASKS NOBODY, which is the same safety net
// [app.worldOf] keeps over the walk: a build whose door forgot to wire the
// reading must draw nothing about this disk rather than a file on it.
func TestAHostedTaskCardWithNoSeamReadsNothing(t *testing.T) {
	a, _ := farCardLab(t, nil)
	a.farRecord = nil
	pressFarCard(t, a)
	text := placeText(a)
	if !strings.Contains(text, taskCardTailUnread+a.host) {
		t.Fatalf("a hosted card with no seam said nothing about it:\n%s", text)
	}
}

// WHILE THE READING IS ON ITS WAY THE CARD SAYS SO, and it says which machine it
// is waiting on. At home the journal opens in a millisecond and the emptiness law
// is right about a fact that is merely late; down an ssh pipe a card that stood
// empty and then grew a paragraph reads as a card that was wrong first.
func TestAHostedTaskCardSaysItIsStillReading(t *testing.T) {
	a, _ := farCardLab(t, func(string) (session.TaskRecord, error) {
		return session.TaskRecord{}, nil
	})
	width, height := a.size()
	_, hits, _, _ := a.taskSheetFrame(width, height)
	for y, hit := range hits {
		if hit.kind == taskSheetHitRow {
			a.taskSheetPress(2, y)
			break
		}
	}
	text := placeText(a)
	if !strings.Contains(text, taskCardTailReading+a.host) {
		t.Fatalf("a card waiting on the far machine said nothing:\n%s", text)
	}
	if strings.Contains(text, taskCardTailGone) {
		t.Fatalf("a card that had not been answered claimed the journal was gone:\n%s", text)
	}
}

// AND A FAR MACHINE THAT REFUSES IS NOT A JOURNAL THAT IS GONE. Every other fact
// on the card came over on the walk and is still true; only this one thing could
// not be had, and the line says exactly that.
func TestAHostedTaskCardSaysWhenTheFarMachineWouldNotAnswer(t *testing.T) {
	a, _ := farCardLab(t, func(string) (session.TaskRecord, error) {
		return session.TaskRecord{}, errors.New("engine: no")
	})
	pressFarCard(t, a)
	text := placeText(a)
	if !strings.Contains(text, taskCardTailUnread+a.host) {
		t.Fatalf("a refused reading said nothing:\n%s", text)
	}
	if strings.Contains(text, taskCardTailGone) {
		t.Fatalf("a refused reading was drawn as a deleted journal:\n%s", text)
	}
}

// A journal the FAR machine no longer holds says so about THAT machine. The
// sentence names it, because "not on this disk" said about a file on a server is
// the same lie in the other direction.
func TestAHostedTaskCardNamesTheMachineAJournalIsGoneFrom(t *testing.T) {
	a, _ := farCardLab(t, func(string) (session.TaskRecord, error) {
		return session.TaskRecord{Kept: false}, nil
	})
	pressFarCard(t, a)
	text := placeText(a)
	if !strings.Contains(text, taskCardTailGoneOn+a.host+taskCardTailGoneEnd) {
		t.Fatalf("a journal gone from the far machine was not said of it:\n%s", text)
	}
	if strings.Contains(text, taskCardTailGone) {
		t.Fatalf("the far card used this disk's sentence:\n%s", text)
	}
}

// A journal that IS still there and simply held no closing word says NOTHING at
// all. `Kept` is the machine that owns the file answering the question this line
// used to guess at from an empty string.
func TestAKeptJournalWithNoReportSaysNothing(t *testing.T) {
	a, _ := farCardLab(t, func(string) (session.TaskRecord, error) {
		return session.TaskRecord{Kept: true}, nil
	})
	pressFarCard(t, a)
	text := placeText(a)
	if strings.Contains(text, taskCardTailGone) || strings.Contains(text, taskCardTailGoneOn) {
		t.Fatalf("a journal that is still there was called gone:\n%s", text)
	}
	if strings.Contains(text, taskCardTailHead) {
		t.Fatalf("a card with no report drew the report's heading:\n%s", text)
	}
}

// THE PATHS ON A FAR CARD ARE THE FAR MACHINE'S, SAID WITH ITS NAME AND NEVER
// OFFERED AS A DOOR. A click here would ask this laptop for a file on the server,
// and `~` collapses against THIS home directory — which on a machine whose home
// has the same shape is a path claiming to be one here.
func TestAHostedTaskCardSaysItsPathsAreTheFarMachines(t *testing.T) {
	a, _ := farCardLab(t, func(string) (session.TaskRecord, error) {
		return session.TaskRecord{Report: "done", Kept: true}, nil
	})
	// A WIDE FRAME, because the assertion is about the whole path: this card
	// prints paths in full and cuts what will not fit ([taskCardShown] says why
	// it does not abbreviate instead), and a hundred columns cuts this one.
	a.width = 160
	pressFarCard(t, a)
	text := placeText(a)
	want := a.host + ":/srv/home/.aforge/v3/projects/-srv-code-api/bbbb000000000002/tasks/20260826-094113_1.jsonl"
	if !strings.Contains(text, want) {
		t.Fatalf("the card did not say whose disk its transcript is on:\n%s", text)
	}
	// The branch is unaffected: a branch is a name inside a repository rather
	// than a place on a disk, and it was never a door on either machine.
	if !strings.Contains(text, taskCardBranchWord+railSep+"task/widening") {
		t.Fatalf("the card lost its branch row:\n%s", text)
	}
}
