package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// A ROW SAYS ITS STATE ONCE, WHOEVER WROTE THE RECORD IT IS READ FROM.
//
// The engine grades a node somebody stopped with the outcome `stopped`
// (internal/session's taskGradeOutcome), and this page used to hang that
// outcome off its own state word — so the row drew `stopped · stopped`, one
// reading said twice because two writers each thought they were the one saying
// it. [session.TaskStatus.RowWord] is the join now, and it is the only place
// the word and its reason are punctuated together.
func TestARowSaysItsStateWordOnce(t *testing.T) {
	now := time.Date(2026, time.September, 11, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		why     string
		entry   session.TaskIndexEntry
		runs    bool
		forbids string
	}{{
		why: "a stopped node whose record grades it `stopped`",
		entry: session.TaskIndexEntry{
			ID: "1", Label: "Upgraded model v2", Title: "Upgraded model v2", SessionID: "room-a",
			Status: string(session.TaskFailed), Ending: session.TaskEndingStopped,
			Outcome: "stopped", EndedAt: now.Add(-time.Hour),
		},
		forbids: "stopped" + rowSep + "stopped",
	}, {
		why: "a row that claims to be running with nobody behind it",
		entry: session.TaskIndexEntry{
			ID: "2", Label: "Fit the OU skill model", Title: "Fit the OU skill model", SessionID: "room-a",
			Status: string(session.TaskRunning),
		},
		forbids: taskRecordStoppedWord + rowSep + taskRecordStoppedWord,
	}} {
		item := tasksItem{entry: tc.entry, runs: tc.runs}
		row := plain(tasksRow(tasksLine{kind: tasksLineTask, item: item}, 120, now, newPalette(tokens.NoColor, false), false))
		word := taskStateWord(tc.entry, tc.runs)
		if strings.Contains(row, tc.forbids) {
			t.Fatalf("%s draws\n  %s\nand says %q twice", tc.why, row, word)
		}
		if strings.Count(row, word) != 1 {
			t.Fatalf("%s draws\n  %s\nand says %q %d times, want once", tc.why, row, word, strings.Count(row, word))
		}
	}
}

// THE ONE JOIN IS THE ENGINE'S. A surface that composed the pair itself would be
// a second spelling of it, which is how the two halves came to disagree.
func TestTheRowTakesItsStateAndReasonFromTheOneJoin(t *testing.T) {
	now := time.Date(2026, time.September, 11, 9, 0, 0, 0, time.UTC)
	entry := session.TaskIndexEntry{
		ID: "3", Label: "Wire the seam", Title: "Wire the seam", SessionID: "room-a",
		Status: string(session.TaskFailed), Ending: session.TaskEndingWire,
		Outcome: "the call never came back", FilesChanged: 2, EndedAt: now.Add(-time.Hour),
	}
	item := tasksItem{entry: entry}
	want := "2 files" + rowSep + item.status().RowWord()
	if got := tasksMiddle(item); got != want {
		t.Fatalf("the row reads %q, want %q", got, want)
	}
}

// A CONVERSATION ROOT NAMES ITS FOLDER ONLY WHERE THE FOLDER IS NEWS.
//
// The mission-control ruling retired `~` as a project name and the project tag
// on rows in this window's own folder (docs/design/home-mission-control/DESIGN.md
// §1, "What is retired"). This page drew both until #884: a chat opened in a
// home directory arrived wearing a lone `~` at the right of its name.
func TestAConversationRootWearsAFolderTagOnlyWhereItIsNews(t *testing.T) {
	now := time.Date(2026, time.September, 11, 9, 0, 0, 0, time.UTC)
	pal := newPalette(tokens.NoColor, false)
	draw := func(chat tasksChat, folder string) string {
		line := tasksLine{kind: tasksLineChat, chat: chat}
		return plain(tasksChatRow(line, 100, now, folder, "/home/pat", pal, false))
	}
	root := func(title, project, dir string) tasksChat {
		return tasksChat{
			key:   tasksChatKey("room-a"),
			row:   session.SessionRow{ID: "room-a", Title: title, Project: project, ProjectDir: dir, Workspace: dir},
			title: title, at: now.Add(-time.Hour),
		}
	}
	if got := draw(root("Clever Bet Prediction Model", "~", "/home/pat"), "/home/pat/code/pricing"); strings.Contains(got, "~") {
		t.Fatalf("a chat in the home directory still wears a tag:\n  %s", got)
	}
	if got := draw(root("Crafting a Multi-Page Website", "pricing", "/home/pat/code/pricing"), "/home/pat/code/pricing"); strings.Contains(got, "pricing") {
		t.Fatalf("a chat in this window's own folder still wears its tag:\n  %s", got)
	}
	if got := draw(root("AI Influencers", "aforge", "/home/pat/code/aforge"), "/home/pat/code/pricing"); !strings.Contains(got, "aforge") {
		t.Fatalf("another project's chat lost the one fact that places it:\n  %s", got)
	}
}

// AND THE WINDOW'S OWN FOLDER IS READ OFF ITS OWN CONVERSATION ROW.
//
// [app.taskSheetSelfRow] knows what this conversation is CALLED and nothing
// about which project bucket it belongs to; the world scan knows the bucket and
// nothing about a journal this session has not finished writing. The two meet in
// [tasksConversationRows], so the reading takes the folder from there — asking
// [tasksMine.row] directly got the half with no folder on it, and every row of
// the folder a person was sitting in went on wearing its name.
func TestTheReadingKnowsTheFolderThisWindowIsSittingIn(t *testing.T) {
	now := time.Date(2026, time.September, 11, 9, 0, 0, 0, time.UTC)
	mine := tasksMine{row: session.SessionRow{
		ID: "room-a", Title: "Sweeping the Frame Budget",
		Transcript: "/tmp/room-a/transcript.jsonl", Open: true, Live: true,
	}}
	world := session.World{Projects: []session.Project{{
		Name: "aforge", Sessions: []session.SessionRow{{
			ID: "room-a", Title: "Sweeping the Frame Budget", Transcript: "/tmp/room-a/transcript.jsonl",
			Project: "aforge", ProjectDir: "/home/pat/code/aforge", At: now.Add(-time.Hour),
		}},
	}}, Read: now}
	r := readTasks(world, mine, session.LastDays(now, 14), time.Time{}, now)
	if r.folder != "/home/pat/code/aforge" {
		t.Fatalf("the reading thinks this window is in %q, want the folder its own conversation names", r.folder)
	}
	if page := tasksPage(r, 100); strings.Contains(page, "aforge") && !strings.Contains(page, "Sweeping") {
		t.Fatalf("the page drew the folder it is already in:\n%s", page)
	}
}
