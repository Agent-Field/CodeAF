package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
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
		row := plain(tasksRow(tasksLine{kind: tasksLineTask, item: item}, 120, now, tasksSort{}, newPalette(tokens.NoColor, false), false))
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
// The explicit project column includes the current project and home directory.
func TestConversationProjectColumnIncludesCurrentAndHomeProjects(t *testing.T) {
	now := time.Date(2026, time.September, 11, 9, 0, 0, 0, time.UTC)
	pal := newPalette(tokens.NoColor, false)
	draw := func(chat tasksChat, folder string) string {
		line := tasksLine{kind: tasksLineChat, chat: chat}
		return plain(tasksChatRow(line, 100, now, folder, "/home/pat", tasksSort{}, pal, false))
	}
	root := func(title, project, dir string) tasksChat {
		return tasksChat{
			key:   tasksChatKey("room-a"),
			row:   session.SessionRow{ID: "room-a", Title: title, Project: project, ProjectDir: dir, Workspace: dir},
			title: title, at: now.Add(-time.Hour),
		}
	}
	if got := draw(root("Clever Bet Prediction Model", "~", "/home/pat"), "/home/pat/code/pricing"); !strings.Contains(got, "~") {
		t.Fatalf("a chat in the home directory lost its project column:\n  %s", got)
	}
	if got := draw(root("Crafting a Multi-Page Website", "pricing", "/home/pat/code/pricing"), "/home/pat/code/pricing"); !strings.Contains(got, "pricing") {
		t.Fatalf("a chat in this window's own folder lost its project column:\n  %s", got)
	}
	if got := draw(root("AI Influencers", "codeaf", "/home/pat/code/codeaf"), "/home/pat/code/pricing"); !strings.Contains(got, "codeaf") {
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
		Name: "codeaf", Sessions: []session.SessionRow{{
			ID: "room-a", Title: "Sweeping the Frame Budget", Transcript: "/tmp/room-a/transcript.jsonl",
			Project: "codeaf", ProjectDir: "/home/pat/code/codeaf", At: now.Add(-time.Hour),
		}},
	}}, Read: now}
	r := readTasks(world, mine, session.LastDays(now, 14), tasksSort{}, time.Time{}, now)
	if r.folder != "/home/pat/code/codeaf" {
		t.Fatalf("the reading thinks this window is in %q, want the folder its own conversation names", r.folder)
	}
	// The project is visible independently of the conversation title.
	if page := tasksPage(r, 100); !strings.Contains(page, "codeaf") {
		t.Fatalf("the project column lost the current folder:\n%s", page)
	}
}

// THE WORD IS THE LAST THING A ROW GIVES UP, and a list of work is read to find
// out whether anything needs a person.
//
// Two rows lost it before this, at ordinary widths, and for two different
// reasons. A row nobody is behind said `incomplete` in the NOTE field, which
// ranked second — then the word moved into the fact that ranked fifth, behind the
// age and the conversation the work came out of, so at eighty-five columns the
// page kept `The Skill Model Rebuild` and dropped the one fact anybody would have
// acted on. And a `your call` row that changed no files had only ONE spelling to
// offer, because its file count and its state word collapsed into the same
// string — and a field with one spelling either fits or goes (rowfit.go, law 4),
// so a hundred-column frame drew its age and nothing else.
func TestTheStateWordIsTheLastThingARowGivesUp(t *testing.T) {
	now := time.Date(2026, time.September, 11, 9, 0, 0, 0, time.UTC)
	pal := newPalette(tokens.NoColor, false)
	for _, tc := range []struct {
		why  string
		item tasksItem
	}{{
		why: "a row that claims to be running with nobody behind it",
		item: tasksItem{
			entry: session.TaskIndexEntry{
				ID: "1", SessionID: "room-a", Status: string(session.TaskRunning),
				Label: "Rebuild the skill model from the 2024 backtest",
			},
			row: session.SessionRow{ID: "room-a", Title: "The Skill Model Rebuild"},
		},
	}, {
		why: "a landing nobody could check that changed no files",
		item: tasksItem{
			entry: session.TaskIndexEntry{
				ID: "2", SessionID: "room-a", Status: string(session.TaskUnverified),
				Label:   "Put the annual toggle on the pricing page and check the copy",
				EndedAt: now.Add(-time.Hour),
			},
			row: session.SessionRow{ID: "room-a", Title: "The Skill Model Rebuild"},
		},
	}} {
		word := taskStateWord(tc.item.entry, tc.item.runs)
		// DOWN TO THE FLOOR THE COLUMN HAS, and no further. The state is a COLUMN
		// now (spec.md §2) and a column is all of the page or none of it: at
		// ninety cells it is drawn on every row, and under ninety it goes from
		// every row at once and the sort key keeps its own cells. What a narrow
		// frame gives a person instead is the CURSOR's row, which grows the state
		// and the reason under it ([tasksReasonLine]) — one row said whole rather
		// than twenty rows each missing the same word.
		for _, width := range []int{120, 100, tasksStateFloor} {
			row := plain(tasksRow(tasksLine{kind: tasksLineTask, item: tc.item}, width, now, tasksSort{}, pal, false))
			if !strings.Contains(row, word) {
				t.Fatalf("at %d columns %s reads\n  %s\nand has given up %q, which is the one thing the list is read for",
					width, tc.why, strings.TrimRight(row, " "), word)
			}
		}
		under := plain(tasksReasonLine(tc.item, tasksStateFloor-1, pal))
		if !strings.Contains(under, word) {
			t.Fatalf("under %d columns %s says %q on no line at all, and the cursor's own line is where it goes:\n  %s",
				tasksStateFloor, tc.why, word, under)
		}
	}
}
