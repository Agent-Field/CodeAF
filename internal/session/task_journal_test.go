package session

// A task's transcript outlives the process, as tests: the checkpoint carries
// the journal path, a checkpoint from before it carried one is backfilled by
// the node's id from the session's own tasks/ directory — picking the run that
// IS the node and not the audit or repair beside it — and a transcript that is
// not on disk answers "" rather than a guess.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
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

// TWO NODES NEVER GET ONE JOURNAL, and this is the flake it was: five
// internal/session tests failed only when other suites were running beside
// them, and the thing they shared was this path.
//
// Every test in this package runs under one HOME (hermetic_test.go) and every
// agent in it has no session file of its own, so `session` is the constant
// "unfiled" for all of them; node ids start again at one in every graph. The
// stamp was the only thing left to tell two nodes' journals apart and it was
// good only to the second — so two tests whose first node started inside one
// second were handed the SAME path, and [newAgent] either resumed the other
// one's transcript or, while the first still held the file's flock, refused the
// second outright. A design refused that way never wrote its page, and the test
// waiting for its card waited the full sixty seconds and failed.
//
// The claim is stated as an absolute rather than as a probability, which is why
// [journalMoment] exists: minting is not allowed to answer one instant twice.
func TestEveryNodeJournalPathIsMintedOnlyOnce(t *testing.T) {
	seen := map[string]bool{}
	for round := 0; round < 2000; round++ {
		// The same arguments every time — one session name, one node id, no
		// suffix — because that is exactly the case the collision came from.
		path := taskJournalPath(Place{}, "unfiled", 1, "")
		if seen[path] {
			t.Fatalf("round %d minted %q a second time; two nodes would share one journal", round, path)
		}
		seen[path] = true
	}
}

// AND THEY ARE MINTED IN ORDER, because [findTaskJournal] finds a node's newest
// transcript by comparing names. A stamp handed out of order would make the
// older file the one a resumed session reads.
func TestNodeJournalNamesSortIntoMintingOrder(t *testing.T) {
	var last string
	for round := 0; round < 500; round++ {
		name := filepath.Base(taskJournalPath(Place{}, "unfiled", 7, ""))
		if last != "" && !(name > last) {
			t.Fatalf("round %d minted %q after %q, which sorts no later", round, name, last)
		}
		last = name
	}
}

func TestPlanBackedTaskReopensItsNestedWorkerHistory(t *testing.T) {
	agent, id, place, _, _ := completedWorkspaceApplyFixture(t, false)
	dir := filepath.Join(place.NodeJournals(), itoa64(id))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "20261026-000305.763019_worker.jsonl")
	journal := `{"type":"session","version":1,"id":"worker","model":"deepseek/deepseek-v4.1-flash"}` + "\n" + `{"type":"message","role":"assistant","content":"the actual worker history"}` + "\n"
	for name, body := range map[string]string{want: journal, filepath.Join(dir, "20261025-000305.763019_worker.jsonl"): journal, filepath.Join(dir, "20261027-000305.763019_worker.jsonl"): strings.Repeat("x", 65536), filepath.Join(dir, "20261026-000400.000000_helper.jsonl"): `{"type":"session","version":1,"id":"helper"}` + "\n"} {
		if err := os.WriteFile(name, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(place.NodeJournals(), itoa64(id+1))
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "20261026-010000.000000_worker.jsonl"), []byte(journal), 0600); err != nil {
		t.Fatal(err)
	}
	t.Run("journal", func(t *testing.T) {
		if got := agent.TaskJournal(id + 1); got != "" {
			t.Fatalf("unknown task exposed foreign worker: %q", got)
		}
		if got := agent.TaskJournal(id); got != want {
			t.Fatalf("plan-backed TaskJournal(%d)=%q, want its worker %q", id, got, want)
		}
		if report, ok := PeekReport(want); !ok || report != "the actual worker history" {
			t.Fatalf("resolved worker history is unreadable: %q", report)
		}
	})
	t.Run("task-page", func(t *testing.T) {
		page, ok := agent.PlanTaskPage("t-" + itoa64(id))
		if !ok || page.Row.Model != "deepseek/deepseek-v4.1-flash" {
			t.Fatalf("worker page lacks its recorded model: found=%v model=%q", ok, page.Row.Model)
		}
	})
	t.Run("rebuilt-index", func(t *testing.T) {
		for _, row := range RebuiltTaskRows(place.Dir) {
			if row.ID == itoa64(id) {
				if row.TranscriptURI != taskURI(want) || row.Model != "deepseek/deepseek-v4.1-flash" {
					t.Fatalf("reopened task has wrong history/model: transcript=%q model=%q", row.TranscriptURI, row.Model)
				}
				return
			}
		}
		t.Fatal("completed plan-backed task missing from rebuilt index")
	})
}

func TestPlanBackedHistoryRefusesRedirectedTaskAndWorker(t *testing.T) {
	for _, redirectDirectory := range []bool{false, true} {
		t.Run(map[bool]string{false: "worker", true: "task-directory"}[redirectDirectory], func(t *testing.T) {
			agent, id, place, _, _ := completedWorkspaceApplyFixture(t, false)
			outside := t.TempDir()
			name := "20261026-000305.763019_worker.jsonl"
			if err := os.WriteFile(filepath.Join(outside, name), []byte(`{"type":"session","version":1,"id":"foreign"}`+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(place.NodeJournals(), itoa64(id))
			if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
				t.Fatal(err)
			}
			if redirectDirectory {
				if err := os.Symlink(outside, dir); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, name), filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			if got := agent.TaskJournal(id); got != "" {
				t.Fatalf("redirected worker was exposed: %q", got)
			}
			for _, row := range RebuiltTaskRows(place.Dir) {
				if row.ID == itoa64(id) && row.TranscriptURI != "" {
					t.Fatalf("redirected worker indexed: %q", row.TranscriptURI)
				}
			}
		})
	}
}

// A taken chat may retain its completed task and journal without a plan store.
// Its ordinary task room reads the replayed notice, not a PlanTaskPage or index.
func TestTakenWorkerRosterCarriesRecordedModelWithoutPlanStore(t *testing.T) {
	for _, saved := range []string{"", "recorded/prior-model"} {
		t.Run(saved, func(t *testing.T) {
			want := saved
			if want == "" {
				want = "deepseek/deepseek-v4.1-flash"
			}
			place := newPlace(t, false)
			writeCheckpoint(t, place.Tasks(), taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 2, Runs: []runRecord{{ID: 2, Title: "Extend two-root formatting helpers", State: TaskDone, PlanTask: "t-2", Report: "finished", Model: saved}}})
			dir := filepath.Join(place.NodeJournals(), "2")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(dir, "20261026-000305.763019_worker.jsonl")
			if err := os.WriteFile(journal, []byte(`{"type":"session","version":1,"id":"worker","model":"deepseek/deepseek-v4.1-flash"}`+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			agent := journalAgent(t, place)
			if _, found := agent.PlanTaskPage("t-2"); found {
				t.Fatal("fixture unexpectedly has a plan page; it must use the generic task room")
			}
			if got := agent.TaskJournal(2); got != journal {
				t.Fatalf("worker journal=%q, want %q", got, journal)
			}
			events, stop := agent.WatchTaskUpdates()
			defer stop()
			deadline := time.After(3 * time.Second)
			for {
				select {
				case event := <-events:
					if event.Task != nil && event.Task.ID == 2 {
						if event.Task.Model != want {
							t.Fatalf("generic task room receives model %q instead of recorded worker model", event.Task.Model)
						}
						return
					}
				case <-deadline:
					t.Fatal("completed worker missing from replayed roster")
				}
			}

		})
	}
}
