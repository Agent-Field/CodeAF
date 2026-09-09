package tui3

import (
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestHiddenStopUsesItsOwnTaskAndJobRoster(t *testing.T) {
	a, door, first := asyncApp(t)
	turning(a, first)
	file := a.file
	cmd, _ := a.startBeside("/tmp/lab")
	drain(t, a, cmd)
	second := door.made[0]
	turning(a, second)
	held := a.behind[a.convKey(file)]
	held.watch.noteTask(&session.TaskNotice{ID: 3, State: session.TaskRunning})
	held.watch.noteJob(&session.JobNotice{ID: 8, State: session.JobRunning})
	// The project index deliberately names a different conversation’s task.
	first.nodes = []session.TaskIndexEntry{{ID: "99", Status: string(session.TaskRunning)}}
	a.stopConversation(chatTab{key: a.convKey(file), file: file})
	if len(first.cancelled) != 2 || first.cancelled[0] != "task:3" || first.cancelled[1] != "job:8" {
		t.Fatalf("cancelled %v", first.cancelled)
	}
	if second.stops != 0 || len(second.cancelled) != 0 || !second.running {
		t.Fatal("stop crossed conversations")
	}
}

func TestHiddenJobStaysDiscoverableAndReportsItsCompletion(t *testing.T) {
	a, _, agent := asyncApp(t)
	w := &behindWatch{}
	held := &kept{conv: Conversation{Agent: agent, SessionFile: "/tmp/hidden.jsonl"}, watch: w}
	a.behind = map[string]*kept{"hidden": held, a.convKey(held.conv.SessionFile): held}
	if !w.noteJob(&session.JobNotice{ID: 4, State: session.JobRunning}) {
		t.Fatal("start did not wake the screen")
	}
	if row := a.homeTrue(session.SessionRow{Transcript: held.conv.SessionFile}); row.Presence.State != session.PresenceWorking {
		t.Fatal("Home hides working job")
	}
	if a.tabSignalFor("hidden", false) != tabWorking {
		t.Fatal("hidden job appears idle")
	}
	if row := a.hopKept(held, a.now()); !row.moving || row.note != "working" {
		t.Fatalf("job row: %+v", row)
	}
	if !w.noteJob(&session.JobNotice{ID: 4, State: session.JobDone}) {
		t.Fatal("completion did not wake the screen")
	}
	if row := a.hopKept(held, a.now()); row.moving || row.note != hopLandedWord {
		t.Fatalf("completed row: %+v", row)
	}
}

func TestHiddenWorkSnapshotCanRaceWithSettlement(t *testing.T) {
	w := &behindWatch{}
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		for i := 0; i < 100; i++ {
			w.noteTask(&session.TaskNotice{ID: 1, State: session.TaskRunning})
			w.noteTask(&session.TaskNotice{ID: 1, State: session.TaskDone})
			w.noteJob(&session.JobNotice{ID: 1, State: session.JobRunning})
			w.noteJob(&session.JobNotice{ID: 1, State: session.JobDone})
		}
	}()
	for i := 0; i < 100; i++ {
		w.workIDs()
	}
	group.Wait()
	tasks, jobs := w.workIDs()
	if len(tasks)+len(jobs) != 0 {
		t.Fatal("settled work remained in snapshot")
	}
}

func TestHomePaintUsesLiveJobStateWhileCoveringTheLastTab(t *testing.T) {
	a, _, _ := asyncApp(t)
	a.jobs = []session.JobNotice{{ID: 1, State: session.JobRunning}}
	row := session.SessionRow{Transcript: a.file, Title: "hidden job"}
	sw := switcherLine{row: &switcherRow{kind: switcherConversation, session: row, title: "hidden job"}}
	line := homeLine{kind: homeSession, row: row, sw: &sw}
	text := a.homeLine(line, 0, 120, a.pal)
	if !strings.Contains(plain(text), "working") {
		t.Fatalf("Home lost live job: %s", plain(text))
	}
}
