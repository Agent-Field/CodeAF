package session

// A task's transcript outlives the process, as tests: the checkpoint carries
// the journal path, a checkpoint from before it carried one is backfilled by
// the node's id from the session's own tasks/ directory — picking the run that
// IS the node and not the audit or repair beside it — and a transcript that is
// not on disk answers "" rather than a guess.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// journalAgent is one conversation over a session folder, with the scripted
// runner every store test uses: nothing here needs a provider or git.
func journalAgent(t *testing.T, place Place) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = place.Transcript()
		config.Place = place
	})
	return agent
}

// touchJournal puts one transcript-shaped file in the session's tasks/
// directory and answers its path.
func touchJournal(t *testing.T, place Place, name string) string {
	t.Helper()
	dir := place.NodeJournals()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(`{"type":"session","version":1,"id":"n1"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// THE PATH IS ON THE CHECKPOINT, and a session opened again over the same
// folder answers the same file — which is the whole of what lets a finished
// task's room replay after a restart.
func TestTheCheckpointCarriesTheJournalAndAResumeAnswersIt(t *testing.T) {
	place := newPlace(t, false)
	agent := journalAgent(t, place)
	graph := agent.graph()
	graph.mu.Lock()
	graph.run = func(*TaskNode) {}
	graph.mu.Unlock()
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Add the greeting", brief: "write hello.txt", acceptance: "the file is there"})

	journal := touchJournal(t, place, "20260824-101500_"+itoa64(id)+".jsonl")
	node := graph.node(id)
	node.setJournal(journal)
	node.finish("wrote the file", nil, "", "")
	graph.complete(node, TaskDone)
	waitDoneNode(t, node)

	record := awaitRecord(t, place.Tasks(), id, func(record taskRecord) bool {
		return record.State == TaskDone && record.Journal != ""
	}, "done with its journal written down")
	if record.Journal != journal {
		t.Fatalf("the checkpoint carries journal %q, want %q", record.Journal, journal)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}

	resumed := journalAgent(t, place)
	if got := resumed.TaskJournal(id); got != journal {
		t.Fatalf("after a resume TaskJournal(%d) = %q, want %q", id, got, journal)
	}
}

// A CHECKPOINT FROM BEFORE THE FIELD EXISTED IS BACKFILLED BY ID: the newest
// `<stamp>_<id>.jsonl` in the session's tasks/ directory, with the audit and
// repair transcripts beside it passed over, and the answer written back onto
// the checkpoint so the next life does not look again.
func TestAnOldCheckpointFindsTheJournalByIdAndPicksTheMainTranscript(t *testing.T) {
	place := newPlace(t, false)
	writeCheckpoint(t, place.Tasks(), taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 7,
		Nodes: []taskRecord{{
			ID: 7, Title: "Fix the reconciler", Brief: "fix it", Acceptance: "tests pass",
			State: TaskDone, Report: "fixed", Noted: true,
		}},
	})
	touchJournal(t, place, "20260824-090000_7.jsonl")
	want := touchJournal(t, place, "20260824-100000_7.jsonl")
	touchJournal(t, place, "20260824-100200_7-audit-9c1a2f.jsonl")
	touchJournal(t, place, "20260824-100400_7-repair1.jsonl")
	touchJournal(t, place, "20260824-110000_17.jsonl")

	agent := journalAgent(t, place)
	if got := agent.TaskJournal(7); got != want {
		t.Fatalf("TaskJournal(7) = %q, want the newest main transcript %q", got, want)
	}
	record := awaitRecord(t, place.Tasks(), 7, func(record taskRecord) bool {
		return record.Journal != ""
	}, "with the found journal written down")
	if record.Journal != want {
		t.Fatalf("the checkpoint was backfilled with %q, want %q", record.Journal, want)
	}
}

// A TRANSCRIPT THAT IS NOT ON DISK ANSWERS "" — never a name nobody wrote.
func TestAMissingJournalAnswersNothing(t *testing.T) {
	place := newPlace(t, false)
	writeCheckpoint(t, place.Tasks(), taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 3,
		Nodes: []taskRecord{{
			ID: 3, Title: "Grind", Brief: "b", Acceptance: "a",
			State: TaskDone, Report: "done", Noted: true,
		}},
	})
	touchJournal(t, place, "20260824-100200_3-audit-9c1a2f.jsonl")

	agent := journalAgent(t, place)
	if got := agent.TaskJournal(3); got != "" {
		t.Fatalf("TaskJournal(3) = %q with only an audit on disk, want \"\"", got)
	}
	if got := agent.TaskJournal(99); got != "" {
		t.Fatalf("TaskJournal(99) = %q for a node that does not exist, want \"\"", got)
	}
}

// ONE DIRECTORY, TWO READERS. Where a new journal is minted and where an old
// one is looked for are the same function, on both layouts.
func TestJournalsAreMintedAndFoundInTheSameDirectory(t *testing.T) {
	place := newPlace(t, false)
	if minted := filepath.Dir(taskJournalPath(place, "abc", 1, "")); minted != taskJournalDir(place, "abc") {
		t.Fatalf("a folder session mints into %q and looks in %q", minted, taskJournalDir(place, "abc"))
	}
	legacy := filepath.Join(home.Dir(), "v3", "tasks", "abc")
	if got := taskJournalDir(Place{}, "abc"); got != legacy {
		t.Fatalf("the legacy layout looks in %q, want %q", got, legacy)
	}
	if minted := filepath.Dir(taskJournalPath(Place{}, "abc", 1, "")); minted != legacy {
		t.Fatalf("the legacy layout mints into %q, want %q", minted, legacy)
	}
}
