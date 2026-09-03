package main

// The three surfaces this file is about — the jobs section in the home column, a
// job's own page, and a task room — are the ones the polish audit could not
// capture, because nothing in the fixture put anything on them. So the whole
// point of these tests is that they read the seeding back THROUGH THE ENGINE:
// a real [session.Agent] is built on the seeded conversation's folder and its
// roster lane is subscribed, which is exactly the door internal/tui3 opens and
// exactly what it draws from. A fixture that only its own reader can read is a
// fixture that proves nothing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// roomAgent seeds a demo home, then opens the conversation that holds the work
// on a real agent and answers everything its roster lane replays.
//
// NO NETWORK HAPPENS HERE. [session.New] builds a client and renders a prompt
// and sends nothing, and the graph below is written so that a resumed frontier
// finds nothing ready to start — which is itself one of the things under test
// ([TestTheFixtureOpensWithoutStartingAnyWork]).
func roomAgent(t *testing.T) (nodes []session.TaskNotice, jobs []session.JobNotice, agent *session.Agent) {
	t.Helper()
	dir := t.TempDir()
	if _, err := seedDemoHome(dir, time.Now()); err != nil {
		t.Fatalf("seed the demo home: %v", err)
	}
	world := session.ReadWorld(filepath.Join(dir, ".aforge", "v3", "projects"))
	var found session.SessionRow
	for _, row := range world.Sessions() {
		if row.Title == roomTalkTitle {
			found = row
		}
	}
	if found.Dir == "" {
		t.Fatalf("no conversation called %q on the seeded home", roomTalkTitle)
	}
	place := session.Place{Dir: found.Dir, Workspace: found.Workspace}
	agent, err := session.New(session.Config{
		Workspace: found.Workspace,
		Model:     "anthropic/claude-sonnet-4",
		APIKey:    "not-a-key-nothing-here-sends-anything",
		// A BASE IS REQUIRED AND NOTHING IS SENT TO IT. [session.New] builds the
		// client and renders the prompt and makes no request; this test never
		// opens a turn, and the graph below is written so that recovery starts
		// nothing.
		BaseURL:     "https://openrouter.ai/api/v1",
		SessionFile: place.Transcript(),
		Place:       place,
	})
	if err != nil {
		t.Fatalf("open %q: %v", roomTalkTitle, err)
	}
	t.Cleanup(func() { agent.Close() })

	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	// The replay is sent onto the lane before WatchTaskUpdates returns, so
	// everything owed is already queued; the deadline is only here so a lane that
	// went quiet fails as a timeout rather than as a hang.
	deadline := time.After(20 * time.Second)
	for {
		select {
		case event := <-lane:
			switch {
			case event.Job != nil:
				jobs = append(jobs, *event.Job)
			case event.Task != nil:
				nodes = append(nodes, *event.Task)
			}
			if len(nodes) >= len(demoGraph) && len(jobs) >= len(demoJobs) {
				return nodes, jobs, agent
			}
		case <-deadline:
			t.Fatalf("the roster lane replayed %d nodes and %d jobs, wanted %d and %d",
				len(nodes), len(jobs), len(demoGraph), len(demoJobs))
		}
	}
}

func TestTheFixtureHasATaskRoomWithAFamilyInIt(t *testing.T) {
	nodes, _, agent := roomAgent(t)

	states := map[session.TaskState]int{}
	byID := map[uint64]session.TaskNotice{}
	for _, node := range nodes {
		states[node.State]++
		byID[node.ID] = node
	}
	// The shape a room is worth opening on: work that finished, work nobody
	// could sign off, work that stopped, and work parked behind it.
	if states[session.TaskDone] < 2 {
		t.Fatalf("the graph came back with %d done pieces, so the room has nothing settled in it: %v", states[session.TaskDone], states)
	}
	if states[session.TaskUnverified] < 1 {
		t.Fatal("nothing in the graph needs the person's look, which is the state the whole `needs your look` language points at")
	}
	if states[session.TaskFailed] < 1 {
		t.Fatalf("nothing in the graph stopped, so the room's failed shape never draws: %v", states)
	}
	if states[session.TaskQueued] < 1 {
		t.Fatalf("nothing in the graph is waiting, so the column is all history: %v", states)
	}

	// A FAMILY AND NOT A LIST. The audit's whole complaint was that the fixture
	// drew one row, so the depth is asserted rather than the count.
	deepest := 0
	for _, node := range nodes {
		depth := 0
		for at := node; at.Parent != 0; depth++ {
			parent, held := byID[at.Parent]
			if !held {
				t.Fatalf("node %d was handed out by %d, which is not on the roster", node.ID, at.Parent)
			}
			at = parent
		}
		if depth > deepest {
			deepest = depth
		}
	}
	if deepest < 3 {
		t.Fatalf("the deepest family on the roster is %d handings deep; a fold that only ever sees two levels is a fold nothing tests", deepest)
	}

	// AND EVERY SETTLED PIECE HAS A ROOM TO OPEN. The room replays the node's own
	// journal through the engine's reader, so a path the engine cannot find is a
	// room that opens on a blank page with the transcript sitting beside it.
	replayed := 0
	for _, node := range nodes {
		path := agent.TaskJournal(node.ID)
		if path == "" {
			continue
		}
		record := session.ReadTranscript(path)
		if record.Unreadable != "" {
			t.Fatalf("the room of node %d could not be read: %s", node.ID, record.Unreadable)
		}
		if len(record.Entries) < 4 {
			t.Fatalf("the room of node %d replays %d entries — too few to say anything about how a room draws", node.ID, len(record.Entries))
		}
		calls := 0
		for _, entry := range record.Entries {
			if entry.Tool != "" {
				calls++
			}
		}
		if calls == 0 {
			t.Fatalf("the room of node %d replays no tool calls, so its rows are prose and nothing expands", node.ID)
		}
		replayed++
	}
	if replayed < 4 {
		t.Fatalf("only %d nodes have a journal a room can replay", replayed)
	}
}

func TestTheFixtureHasBackgroundJobsWithLogsToRead(t *testing.T) {
	_, jobs, _ := roomAgent(t)

	states := map[session.JobState]int{}
	longest := 0
	for _, job := range jobs {
		states[job.State]++
		if job.LogPath == "" {
			t.Fatalf("job %d came back with no log, which is the whole of what a job leaves behind", job.ID)
		}
		raw, err := os.ReadFile(job.LogPath)
		if err != nil {
			t.Fatalf("the log of job %d is not on the disk: %v", job.ID, err)
		}
		if lines := strings.Count(string(raw), "\n"); lines > longest {
			longest = lines
		}
		if job.Label() == "" {
			t.Fatalf("job %d has nothing to be drawn under", job.ID)
		}
	}
	if states[session.JobDone] < 1 {
		t.Fatalf("no job finished cleanly, so the column's `done` word never draws: %v", states)
	}
	if states[session.JobFailed] < 1 {
		t.Fatalf("no job failed, so the `exited` word and the failed page never draw: %v", states)
	}
	if states[session.JobStopped] < 1 {
		t.Fatalf("no job was left running when the window closed, so `stopped` never draws: %v", states)
	}
	// A LOG WORTH SCROLLING. A job page draws a tail; a fixture whose longest log
	// fits on one screen cannot show where the tail starts or what it does when
	// the terminal is sixty columns wide.
	if longest < 200 {
		t.Fatalf("the longest job log is %d lines, which fits on a screen — the job page's scroll has nothing to do", longest)
	}
}

// A RESUMED DEMO HOME MUST NOT START WORKING. Recovery turns the frontier, so a
// queued node with nothing in front of it would be real work against a real
// model the first time anybody opened the demo. Everything queued here waits on
// the piece that needs a person, so the frontier finds nothing.
func TestTheFixtureOpensWithoutStartingAnyWork(t *testing.T) {
	nodes, _, _ := roomAgent(t)
	for _, node := range nodes {
		if node.State == session.TaskRunning {
			t.Fatalf("opening the demo home started node %d (%q) — a fixture that spends money when it is looked at",
				node.ID, node.Title)
		}
	}
}

// THE HOSTILE NAMES. The polish wave is about what a row does when it does not
// fit, and every name this fixture used to carry was comfortably short — so the
// demo could not show the defect at any width.
func TestTheFixtureHasNamesThatDoNotFit(t *testing.T) {
	dir := t.TempDir()
	if _, err := seedDemoHome(dir, time.Now()); err != nil {
		t.Fatalf("seed the demo home: %v", err)
	}
	world := session.ReadWorld(filepath.Join(dir, ".aforge", "v3", "projects"))

	wide := ""
	for _, row := range world.Sessions() {
		// A NAME WHOSE CELLS AND RUNES DISAGREE. Full-width CJK and an emoji are
		// two cells each and a combining accent is none, so a surface counting
		// runes gets a different answer from one counting cells — which is the
		// only way to tell the two apart from the outside.
		if ansi.StringWidth(row.Title) > len([]rune(row.Title)) {
			wide = row.Title
		}
	}
	if wide == "" {
		t.Fatal("no conversation on this home has a name wider in cells than it is in runes, so nothing can show a wide-character cut")
	}
	if !strings.ContainsRune(wide, '́') {
		t.Fatalf("the wide name %q carries no combining mark, so a reader that cuts between a letter and its accent still passes", wide)
	}

	long, labels := "", 0
	for _, project := range world.Projects {
		for _, entry := range session.ReadTaskIndex(filepath.Join(project.Dir, "tasks.jsonl")) {
			if len(entry.Title) > len(long) {
				long = entry.Title
			}
			// AND THE ROW'S OWN LABEL IS STILL THE ENGINE'S. A long title is only
			// a useful fixture if the label beside it is cut the way the engine
			// cuts one; a 101-character label would be a width no real row has.
			if len([]rune(entry.Label)) > demoTaskLabelLimit {
				t.Fatalf("the work %q carries a %d-character label, longer than anything the engine writes",
					entry.Title, len([]rune(entry.Label)))
			}
			labels++
		}
	}
	if labels == 0 {
		t.Fatal("no work at all is in the project indexes")
	}
	if len([]rune(long)) < 90 {
		t.Fatalf("the longest piece of work on this home is %d characters — short enough to fit, so no row can show what it does when it does not", len([]rune(long)))
	}
}
