package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
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
		TranscriptURI: "file:///srv/home/.codeaf/v3/projects/-srv-code-api/bbbb000000000002/tasks/20260826-094113_1.jsonl",
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
	a.farRecord = func(uri string, _ int) (session.TaskRecord, error) {
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
	// The work is behind its conversation's fold, which opens shut; a person gets
	// there with `→` and so does this ([openTaskFolds]).
	openTaskFolds(a)
	width, height := a.size()
	_, hits, _, _ := a.taskSheetFrame(width, height)
	row := -1
	for y, hit := range hits {
		if _, worker := a.tasksFiltered().at(a.tasksFiltered().lay(a.taskSheetListWidth()), hit.index); hit.kind == taskSheetHitRow && worker {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatalf("the tasks place drew no row of the far machine's work:\n%s", placeText(a))
	}
	// TWICE, BECAUSE A FRAME WITH A PANE ON IT PREVIEWS BEFORE IT OPENS: the first
	// press puts the cursor on the row and the pane beside the list becomes that
	// row's record, and the second press is what opens the card (taskpane.go).
	// Where there is no pane the first press opens and the second is not made.
	cmd := a.taskSheetPress(2, row)
	if !a.taskSheet.detailOn {
		cmd = a.taskSheetPress(2, row)
	}
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

func farRoomText(a *app) string {
	rows := a.roomRows(a.bodyWidth())
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, plain(row.text))
	}
	return strings.Join(lines, "\n")
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

// The hosted roster is this conversation's slice of the far world, not an
// assertion on the remote agent (which deliberately has no local disk doors).
func TestAHostedRosterListsTheFarConversationsTasks(t *testing.T) {
	a := hostedPlaceLab(t)
	entry := farCardEntry(time.Now())
	var reads atomic.Int64
	a.farTasks = func() ([]session.TaskIndexEntry, bool) {
		reads.Add(1)
		return []session.TaskIndexEntry{entry}, true
	}
	cmd := a.loadTasks()
	msg := cmd().(tasksLoadedMsg)
	a.tasksLoaded(msg.rows)
	if reads.Load() != 1 {
		t.Fatalf("one roster load made %d far reads", reads.Load())
	}
	if node := a.tasks[9]; node == nil || node.title != "widening the pipe" {
		t.Fatalf("the far row did not become a roster node: %#v", node)
	}
	railOpenAll(a)
	if entries := a.railEntries(); len(entries) != 2 || !entries[0].head || entries[1].node == nil || entries[1].node.id != 9 {
		t.Fatalf("the hosted roster stayed empty: %#v", entries)
	}
}

// A hosted room opens immediately with one honest loading line, then replaces
// it with the journal the engine answered. The seam is counted because drawing
// and hovering the finished page must never make another round trip.
func TestAHostedRoomReadsTheFarJournalOnce(t *testing.T) {
	a := hostedPlaceLab(t)
	entry := farCardEntry(time.Now())
	a.adoptFarTaskRows([]session.TaskIndexEntry{entry})
	var reads atomic.Int64
	a.farRecord = func(uri string, tail int) (session.TaskRecord, error) {
		reads.Add(1)
		if uri != entry.TranscriptURI || tail != session.TaskJournalTail {
			t.Fatalf("room asked for %q tail %d", uri, tail)
		}
		journal := []byte(`{"type":"message","role":"user","content":"widen the pipe"}` + "\n" +
			`{"type":"message","role":"assistant","content":"widened it"}` + "\n")
		return session.TaskRecord{Journal: journal, Kept: true}, nil
	}
	a.openRoomFor(9, entry.Title)
	if a.room == nil || !a.room.loading {
		t.Fatal("the hosted room did not open while its journal was on the way")
	}
	if screen := farRoomText(a); !strings.Contains(screen, roomLoadingWord) {
		t.Fatalf("the loading room said nothing:\n%s", screen)
	}
	cmd := a.takeRoomPump()
	msg := cmd().(roomRecordMsg)
	a.farRoomRead(msg)
	for range 3 {
		_ = a.roomRows(a.bodyWidth())
	}
	if reads.Load() != 1 {
		t.Fatalf("one room made %d far reads", reads.Load())
	}
	if screen := farRoomText(a); !strings.Contains(screen, "widened it") || strings.Contains(screen, roomLoadingWord) {
		t.Fatalf("the far journal did not replace the loading line:\n%s", screen)
	}
}

// A running row has no transcript URI yet. Its id still opens the room, and
// only the room's own command reads the far journal; frames merely redraw what
// the last read supplied.
func TestAHostedRunningRoomFillsOnItsOwnBoundedBeat(t *testing.T) {
	a := hostedPlaceLab(t)
	now := time.Now()
	entry := farCardEntry(now)
	entry.Status, entry.TranscriptURI, entry.EndedAt = string(session.TaskRunning), "", time.Time{}
	a.adoptFarTaskRows([]session.TaskIndexEntry{entry})
	var reads atomic.Int64
	a.farRoomRecord = func(id uint64, tail int) (session.TaskRecord, error) {
		at := reads.Add(1)
		if id != 9 || tail != session.TaskJournalTail {
			t.Fatalf("running room asked for %d tail %d", id, tail)
		}
		if at == 1 {
			return session.TaskRecord{}, nil
		}
		journal := []byte(`{"type":"message","role":"user","content":"widen the pipe"}` + "\n" +
			`{"type":"message","role":"assistant","content":"checking the far lock now"}` + "\n")
		return session.TaskRecord{Journal: journal, Kept: true}, nil
	}
	a.openRoomFor(9, entry.Title)
	if a.room == nil || a.room.done {
		t.Fatal("the running hosted row did not open as a live room")
	}
	first := a.takeRoomPump()().(roomRecordMsg)
	cmd := a.farRoomRead(first)
	if cmd == nil || !strings.Contains(farRoomText(a), roomYetWord) {
		t.Fatalf("the empty running room did not arm its beat:\n%s", farRoomText(a))
	}
	before := reads.Load()
	for range 20 {
		a.Update(frameMsg{})
		_ = a.roomRows(a.bodyWidth())
	}
	if reads.Load() != before {
		t.Fatalf("frames made %d room reads", reads.Load()-before)
	}
	poll := a.farRoomPoll(a.room.gen)
	msg := poll().(roomRecordMsg)
	a.farRoomRead(msg)
	if text := farRoomText(a); !strings.Contains(text, "checking the far lock now") || strings.Contains(text, roomYetWord) {
		t.Fatalf("the running room did not fill from its beat:\n%s", text)
	}
}

// A HOSTED CARD NEVER OPENS A FILE ON THIS DISK. The path on the record is the
// engine's; a read of it here is either nothing or a stranger's file, and it is
// the read that produced the wrong sentence in the first place. The pin is a
// REAL journal at the far path, made on this machine — if anything in this lane
// ever falls back to the local disk, this is what it would find.
//
// A HOSTED JOB'S LOG IS THE SAME LAW ON THE OTHER SURFACE, and it is pinned
// where that surface lives now: jobpage_test.go's
// [TestAHostedJobPageNeverReadsThisDisk]. It was pinned here, against a job's
// row on the roster, for as long as a job HAD a row on the roster.
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
	a.farRecord = func(string, int) (session.TaskRecord, error) {
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
	openTaskFolds(a)
	width, height := a.size()
	_, hits, _, _ := a.taskSheetFrame(width, height)
	for y, hit := range hits {
		if _, worker := a.tasksFiltered().at(a.tasksFiltered().lay(width), hit.index); hit.kind == taskSheetHitRow && worker {
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
	want := a.host + ":/srv/home/.codeaf/v3/projects/-srv-code-api/bbbb000000000002/tasks/20260826-094113_1.jsonl"
	if !strings.Contains(text, want) {
		t.Fatalf("the card did not say whose disk its transcript is on:\n%s", text)
	}
	// The branch is unaffected: a branch is a name inside a repository rather
	// than a place on a disk, and it was never a door on either machine.
	if !strings.Contains(text, taskCardBranchWord+railSep+"task/widening") {
		t.Fatalf("the card lost its branch row:\n%s", text)
	}
}

// A STALE FAR ROW NEVER UNDOES A LANDING THE STREAM DELIVERED. The far world a
// hosted window reads is the one it last fetched, and a run's row reaches its
// index as `running` at the hand-off: the roster read the landing's own notice
// asks for adopted that row, put the landed run back to running with no age,
// and its clock climbed with no end — `39m` ten minutes after a twenty-nine
// minute run, a spinner and `1 running` on the side list.
func TestAStaleFarRowNeverUndoesTheStreamsLanding(t *testing.T) {
	a := hostedPlaceLab(t)
	born := time.Now().Add(-30 * time.Minute)
	ended := born.Add(29*time.Minute + 8*time.Second)
	now := ended.Add(2 * time.Second)
	a.clock = func() time.Time { return now }
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "widening the pipe", session.TaskRunning,
		session.TaskNotice{StartedAt: born})})
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "widening the pipe", session.TaskDone,
		session.TaskNotice{StartedAt: born, EndedAt: ended, Elapsed: ended.Sub(born), CostUSD: 1.61})})
	held := farCardEntry(now)
	held.Status, held.EndedAt, held.StartedAt, held.DurationMS, held.Cost = string(session.TaskRunning), time.Time{}, born, 0, 1.24
	a.adoptFarTaskRows([]session.TaskIndexEntry{held})
	node := a.tasks[9]
	if node.state != session.TaskDone {
		t.Fatalf("the held far row put the landed run back to %s", node.state)
	}
	now = now.Add(10 * time.Minute)
	if got := a.roomClock(node); got != "29m 8s" {
		t.Fatalf("ten minutes after the landing the run reads %q, want 29m 8s", got)
	}
	if node.cost != 1.61 {
		t.Fatalf("the held far row put the landed run's spend back to %.2f", node.cost)
	}
}

// A LIVE FAR ROW THE STREAM HAS NOT NAMED COUNTS FROM THE ROW'S OWN START. A
// node adopted from a live row had no anchor at all, and the side list counted
// from the zero instant: `2562047h 47m`.
func TestALiveFarRowCountsFromItsOwnStart(t *testing.T) {
	a := hostedPlaceLab(t)
	now := time.Now()
	a.clock = func() time.Time { return now }
	live := farCardEntry(now)
	live.Status, live.EndedAt, live.TranscriptURI = string(session.TaskRunning), time.Time{}, ""
	live.StartedAt, live.DurationMS = now.Add(-3*time.Minute), int64(time.Minute/time.Millisecond)
	a.adoptFarTaskRows([]session.TaskIndexEntry{live})
	if got := strings.Split(plain(a.railTelemetry(a.tasks[9], 40)), railSep)[0]; got != "3m" {
		t.Fatalf("a live far row three minutes in reads %q on the side list, want 3m", got)
	}
	// AND ONE THAT NAMES NO START DRAWS NO AGE rather than one counted from the
	// zero instant.
	b := hostedPlaceLab(t)
	b.clock = func() time.Time { return now }
	live.StartedAt = time.Time{}
	b.adoptFarTaskRows([]session.TaskIndexEntry{live})
	if got := plain(b.railTelemetry(b.tasks[9], 40)); strings.Contains(got, "h") {
		t.Fatalf("a live far row with no start reads %q on the side list", got)
	}
}
