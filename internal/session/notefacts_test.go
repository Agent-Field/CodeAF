package session

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// asideAfterDrain drains whatever is queued and hands back the one aside entry
// the transcript now ends with.
func asideAfterDrain(t *testing.T, agent *Agent) DisplayEntry {
	t.Helper()
	if landed := agent.drainSteering(nil); landed != 1 {
		t.Fatalf("%d notes drained, want 1", landed)
	}
	entries := agent.Transcript()
	last := entries[len(entries)-1]
	if last.Role != "aside" {
		t.Fatalf("last entry is %q, want aside: %#v", last.Role, entries)
	}
	return last
}

// replayedAside is the same aside as a reopened session and a detached reading
// of the file draw it, so a test can hold all three to one answer.
func replayedAside(t *testing.T, agent *Agent, path string) (reopened, detached DisplayEntry) {
	t.Helper()
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	last := func(entries []DisplayEntry) DisplayEntry {
		t.Helper()
		for index := len(entries) - 1; index >= 0; index-- {
			if entries[index].Role == "aside" {
				return entries[index]
			}
		}
		t.Fatalf("no aside in %#v", entries)
		return DisplayEntry{}
	}
	return last(reopen(t, path).Transcript()), last(ReadTranscript(path).Entries)
}

// A RUN'S REPORT NAMES THE TASK IT IS ABOUT, live and after a reopen, without a
// surface reading the sentence. The run's landing is the one task ending that
// does not travel with a reply tag, so its row id rides the note's own facts.
func TestARunReportAsideCarriesItsTaskIDLiveAndOnReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the run", "1", "Repair", "repair the parser")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := &beltRun{store: store, root: store.RootID(), row: 7}

	agent.deliverBeltRunLanding(run, RunSummary{Outcome: beltRunOutcomeDone, Result: "all green"}, RunLanding{})

	want := func(where string, entry DisplayEntry) {
		t.Helper()
		if entry.Role != "aside" || !reflect.DeepEqual(entry.TaskIDs, []string{"7"}) || entry.AsideKind != NoteKindTask || entry.AsideTitle != "" {
			t.Fatalf("%s: run report aside = %#v, want TaskIDs [7] and kind task", where, entry)
		}
	}
	entries := agent.Transcript()
	want("live", entries[len(entries)-1])
	reopened, detached := replayedAside(t, agent, path)
	want("reopened", reopened)
	want("detached", detached)
}

// EVERY KIND OF NOTE SAYS WHAT WROTE IT, live and on replay, and a job's
// aside carries the job's own label as its title.
func TestAsideKindNamesTheAuthorLiveAndOnReplay(t *testing.T) {
	cases := []struct {
		name  string
		send  func(*Agent)
		kind  string
		title string
	}{
		{"job", func(a *Agent) { a.enqueueJobEnding("job 3 exited 0", "dev server") }, NoteKindJob, "dev server"},
		{"job with no name", func(a *Agent) { a.enqueueJobNote("job 4 exited 1") }, NoteKindJob, ""},
		{"watch tick", func(a *Agent) { a.enqueueWatchNote("checks", "watch checks · 2 pending", false) }, NoteKindWatch, ""},
		{"ordinary note", func(a *Agent) { a.enqueueAmbientNote("be careful with the parser") }, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) { cfg.SessionFile = path })
			c.send(agent)
			check := func(where string, entry DisplayEntry) {
				t.Helper()
				if entry.AsideKind != c.kind || entry.AsideTitle != c.title {
					t.Fatalf("%s: kind %q title %q, want %q %q", where, entry.AsideKind, entry.AsideTitle, c.kind, c.title)
				}
			}
			check("live", asideAfterDrain(t, agent))
			reopened, detached := replayedAside(t, agent, path)
			check("reopened", reopened)
			check("detached", detached)
		})
	}
}

// A WATCH'S FIRING AND A RESUME'S ACCOUNT ARE NAMED AS WELL.
func TestWatchFiringAndResumeAccountCarryTheirKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })

	agent.enqueueWatchNote("checks", "watch checks fired: all green", true)
	if got := asideAfterDrain(t, agent); got.AsideKind != NoteKindWatch {
		t.Fatalf("firing aside kind = %q", got.AsideKind)
	}

	account := userText("work the last process left open")
	account.facts = noteFacts{Kind: NoteKindResume}
	agent.enqueueNote(account)
	if got := asideAfterDrain(t, agent); got.AsideKind != NoteKindResume {
		t.Fatalf("resume aside kind = %q", got.AsideKind)
	}
	reopened, _ := replayedAside(t, agent, path)
	if reopened.AsideKind != NoteKindResume {
		t.Fatalf("reopened resume aside kind = %q", reopened.AsideKind)
	}
}

// A BATCH OF NOTES FROM ONE AUTHOR KEEPS IT, AND A MIXED BATCH NAMES NONE: an
// honest "task" or "job" can only be said when every note agrees.
func TestMergedNoteFactsKeepOnlyWhatEveryNoteAgreesOn(t *testing.T) {
	jobs := mergeNoteFacts([]noteFacts{{Kind: NoteKindJob, Title: "build"}, {Kind: NoteKindJob, Title: "build"}})
	if jobs.Kind != NoteKindJob || jobs.Title != "build" {
		t.Fatalf("two endings of one job merged to %#v", jobs)
	}
	twoJobs := mergeNoteFacts([]noteFacts{{Kind: NoteKindJob, Title: "build"}, {Kind: NoteKindJob, Title: "lint"}})
	if twoJobs.Kind != NoteKindJob || twoJobs.Title != "" {
		t.Fatalf("two different jobs merged to %#v, want kind job and no title", twoJobs)
	}
	mixed := mergeNoteFacts([]noteFacts{taskFacts(3), {Kind: NoteKindJob, Title: "build"}})
	if mixed.Kind != "" || mixed.Title != "" || !reflect.DeepEqual(mixed.Tasks, []string{"3"}) {
		t.Fatalf("a task and a job merged to %#v, want no kind, no title and the task id", mixed)
	}
}

// AN OLDER JOURNAL WITH NO NOTE FACTS REPLAYS WITH THE ZERO VALUE: the emptiness
// law, not a kind guessed out of the sentence.
func TestAJournalWithNoNoteFactsReplaysAsAnUnnamedAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeLines(t, path,
		`{"type":"session","version":1,"id":"old","cwd":"/tmp","model":"test","timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"message","role":"user","content":"job 3 exited 0","note":true,"timestamp":"2026-01-01T00:00:01Z"}`,
	)
	entries := ReadTranscript(path).Entries
	if len(entries) != 1 || entries[0].Role != "aside" {
		t.Fatalf("entries = %#v, want one aside", entries)
	}
	if entries[0].AsideKind != "" || entries[0].AsideTitle != "" || entries[0].TaskIDs != nil {
		t.Fatalf("an older journal invented %#v", entries[0])
	}
}

// ── A STOPPED TURN IS INTERRUPTED, NOT FAILED ───────────────────────────────

// stoppedBatch drives one bash call that the person stops mid-run, then lets the
// turn finish, and hands back the agent and the journal path.
func stoppedBatch(t *testing.T, door StopDoor) (*Agent, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	started, done := make(chan struct{}), make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-slow", "bash", `{"command":"sleep 30"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("stopped"), nil },
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
	events := mustSubmit(t, agent, "wait a long time")
	go func() {
		defer close(done)
		for event := range events {
			if event.Kind == EventToolBegin {
				close(started)
				break
			}
		}
		for range events { //nolint:revive // draining is the point
		}
	}()
	<-started
	agent.InterruptFor(door)
	<-done
	return agent, path
}

// A CALL THE PERSON'S STOP CUT SHORT IS INTERRUPTED AND NEVER FAILED, live and
// on every way of reading the file back.
func TestAStoppedCallIsInterruptedAndNotFailedLiveAndOnReplay(t *testing.T) {
	agent, path := stoppedBatch(t, StopByPerson)
	check := func(where string, entries []DisplayEntry) {
		t.Helper()
		entry := toolEntryFor(t, entries, "call-slow")
		if !entry.Interrupted || entry.Failed {
			t.Fatalf("%s: Interrupted=%v Failed=%v, want interrupted and not failed", where, entry.Interrupted, entry.Failed)
		}
	}
	check("live", agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	check("reopened", reopen(t, path).Transcript())
	check("detached", ReadTranscript(path).Entries)
}

// A WINDOW TAKING THE CONVERSATION OVER IS NOT THE PERSON STOPPING: the same cut
// call is not marked interrupted by them.
func TestACallCutByAnotherDoorIsNotMarkedStoppedByThePerson(t *testing.T) {
	agent, _ := stoppedBatch(t, StopByTakeover)
	if entry := toolEntryFor(t, agent.Transcript(), "call-slow"); entry.Interrupted {
		t.Fatal("a takeover's cut was drawn as the person's stop")
	}
}

// AN OLDER JOURNAL WITH AN ABORTED CALL AND NO `stopped` LINE REPLAYS AS IT
// ALWAYS DID: not interrupted, and still not invented as failed.
func TestAJournalWithNoStoppedLinesReplaysAsNotInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeLines(t, path,
		`{"type":"session","version":1,"id":"old","cwd":"/tmp","model":"test","timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"message","role":"user","content":"run it","timestamp":"2026-01-01T00:00:01Z"}`,
		`{"type":"message","role":"assistant","toolCalls":[{"id":"call-1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"sleep 9\"}"}}],"timestamp":"2026-01-01T00:00:02Z"}`,
		`{"type":"message","role":"tool","toolCallId":"call-1","content":"Command aborted","timestamp":"2026-01-01T00:00:03Z"}`,
	)
	entry := toolEntryFor(t, ReadTranscript(path).Entries, "call-1")
	if entry.Interrupted || entry.Failed {
		t.Fatalf("an older journal invented Interrupted=%v Failed=%v", entry.Interrupted, entry.Failed)
	}
	if !strings.Contains(entry.Output, "Command aborted") {
		t.Fatalf("the aborted output was lost: %q", entry.Output)
	}
}
