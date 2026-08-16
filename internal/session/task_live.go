package session

// A RUNNING NODE'S PRESENT, KEPT SO SOMEBODY CAN ASK FOR IT.
//
// Everything else this build knows about a task is either a plan or an epitaph.
// The index is what the work CAME TO (task_index.go); the notice is its state
// and its age (task_contract.go); the report is what it said at the end. In
// between — the eleven minutes a node actually spends working — the only thing
// that existed was a STREAM: the child's events, published into the room for
// whoever happened to be subscribed at that instant (task_room.go). A surface
// can live on a stream because it is always there, folding events into a row as
// they arrive. THE MODEL CANNOT. It is not there: it is inside a turn that ends,
// and when it next thinks to ask "how is task 7 doing" the answer under the old
// arrangement was the same two facts a spinner has — running, four minutes —
// which are equally true of a node stuck on one `go test` and of a node calling
// a tool every second.
//
// So the stream is recorded as it passes: the call in flight, how many steps
// have finished, and a short ring of what the node has been saying and doing.
// Three rules hold it up.
//
//   - IT IS A TAIL, NOT AN ARCHIVE. Two hundred lines, each clipped, and the
//     whole thing thrown away with the session. The node's transcript is a real
//     session file on disk and the model already has hands to read it
//     (task_index.go's TranscriptURI); a recorder that kept everything would be
//     a second copy of the journal, in memory, for a question that is about the
//     last minute.
//
//   - IT RECORDS WHAT THE NODE SAID AND WHAT IT CALLED, NEVER WHAT ITS TOOLS
//     RETURNED. Event.Output is display-only by contract — a capped copy for a
//     surface to expand — and a tool result the model reads must come off the
//     wire, not off a rendering of it. What is here is the node's own narrative
//     and the calls it made, which is what "what is it doing" actually asks.
//
//   - IT IS FED FROM ONE PLACE: [taskRoom.publish], the funnel every one of the
//     child's events already goes through. A second tap on the child's stream
//     would be a second ordering of the same events, and the two would disagree
//     exactly when it mattered — under load.
//
// It outlives the room's close on purpose. A node that landed a second before
// the model asked still answers with the last thing it was doing, and the row
// above the answer says the work is over.

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// taskLiveRing is how many lines are kept. A screenful of terminal is
	// forty; this is five of them, so a model asking for the maximum tail gets
	// it and nothing else is held.
	taskLiveRing = 200
	// taskLiveDefaultTail is what one read answers with when the model named no
	// number, and taskLiveMaxTail the most it may ask for. They are `jobs
	// output`'s numbers said again, because "the tail of a running thing" is one
	// idea and should not have two budgets.
	taskLiveDefaultTail = 40
	taskLiveMaxTail     = taskLiveRing
	// taskLiveLineLimit clips ONE line. A node that prints minified JSON must
	// not put a screenful in the answer on the strength of a single newline.
	taskLiveLineLimit = 200
)

// taskLive is one node's rolling present. Its own mutex, taken by nothing else:
// it is written from the child's event loop and read from a tool call, and it
// must never be the reason either of them waits on the graph.
type taskLive struct {
	mu sync.Mutex
	// call is the tool in flight, in the gloss the person would read ("bash go
	// test ./…"), and "" when the node is between calls. began is when it
	// started, and it is ZERO for a call that has only been ANNOUNCED — the
	// model has finished writing it, nothing is running yet — which is the tool
	// row's own distinction and matters here for its reason: a clock started at
	// the announcement measures how long a response took to stream.
	call  string
	began time.Time
	// last is the gloss of the most recently FINISHED call, so a node between
	// calls can still say what it just did.
	last string
	// steps is how many calls have finished — the same unit the node's own
	// thresholds are counted in (task_run.go).
	steps int
	// lines is the ring, oldest first, and partial is the text the node has
	// written since its last newline. The partial is not a line yet and is not
	// in the ring; a reader is given it anyway, because a node three words into
	// a sentence is a node saying something.
	lines   []string
	partial string
}

// taskLiveState is the recorder copied out, so nothing renders under its lock.
type taskLiveState struct {
	// Call is the tool in flight, or "" between calls.
	Call string
	// Since is when that call began, zero when it is only announced.
	Since time.Time
	// Last is the previous finished call, and Steps how many have finished.
	Last  string
	Steps int
	// Lines is the tail asked for, oldest first.
	Lines []string
}

// record folds one of the child's events in. It is the whole write side.
func (l *taskLive) record(event Event) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	switch event.Kind {
	case EventTextDelta:
		l.write(event.Text)
	case EventToolAnnounced:
		// Named, not clocked: see [taskLive.began].
		l.call, l.began = taskLiveCall(event), time.Time{}
	case EventToolBegin:
		l.call, l.began = taskLiveCall(event), time.Now()
		l.push("· " + l.call)
	case EventToolEnd:
		l.finished()
	case EventToolFailed:
		// A FAILURE'S HINT IS THE FAILURE, not a gloss of the call: the loop
		// puts the result's first line there (loop.go), so the line is built
		// from the tool's name and that, and the call's target is on the "·"
		// line this one answers.
		failed := strings.TrimSpace(event.Tool)
		if failed == "" {
			failed = "a call"
		}
		if hint := strings.TrimSpace(event.Hint); hint != "" {
			failed += ": " + hint
		}
		l.push("✗ " + failed)
		l.finished()
	case EventError:
		// A turn that ended badly is not a finished CALL, so it clears what was
		// in flight without counting a step: the step count is the node's own
		// threshold unit (task_run.go) and must mean the same thing here.
		if event.Err != nil {
			l.push("! " + event.Err.Error())
		}
		if l.call != "" {
			l.last = l.call
		}
		l.call, l.began = "", time.Time{}
	}
}

// finished closes the call in flight and counts it.
//
// ONE CALL AT A TIME IS A SIMPLIFICATION, and it is the surface's own: a batch
// of parallel calls leaves this holding the last one that began, and the first
// end clears it. internal/tui3's rail folds the same stream the same way, and
// two answers to "what is it doing" that disagreed during a batch would be
// worse than one that says the last thing it started. The step COUNT is exact
// either way, because every end is counted.
func (l *taskLive) finished() {
	if l.call != "" {
		l.last = l.call
	}
	l.steps++
	l.call, l.began = "", time.Time{}
}

// write folds a streamed chunk of the node's own words in, one line per
// newline. Text is kept because it is the node THINKING OUT LOUD — "the guard
// is missing; I will add it and a test" — which is the one thing in the stream
// that says why the calls around it are happening.
func (l *taskLive) write(text string) {
	if text == "" {
		return
	}
	l.partial += text
	for {
		at := strings.IndexByte(l.partial, '\n')
		if at < 0 {
			return
		}
		line, rest := l.partial[:at], l.partial[at+1:]
		l.partial = rest
		if trimmed := strings.TrimRight(line, " \t\r"); strings.TrimSpace(trimmed) != "" {
			l.push(trimmed)
		}
	}
}

// push appends one line, clipped, dropping the oldest when the ring is full.
func (l *taskLive) push(line string) {
	l.lines = append(l.lines, clip(line, taskLiveLineLimit))
	if len(l.lines) > taskLiveRing {
		// Copied down rather than resliced: a slice that only ever moves its
		// start keeps the whole backing array alive for the life of the node.
		copy(l.lines, l.lines[len(l.lines)-taskLiveRing:])
		l.lines = l.lines[:taskLiveRing]
	}
}

// state copies the recorder out with at most tail lines, newest kept.
func (l *taskLive) state(tail int) taskLiveState {
	if l == nil {
		return taskLiveState{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	lines := l.lines
	// The unterminated line is shown but never stored: the next delta continues
	// it, and a ring that had already taken it would say it twice.
	if partial := strings.TrimSpace(l.partial); partial != "" {
		lines = append(append(make([]string, 0, len(lines)+1), lines...),
			clip(strings.TrimRight(l.partial, " \t\r"), taskLiveLineLimit))
	}
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	out := taskLiveState{Call: l.call, Since: l.began, Last: l.last, Steps: l.steps}
	out.Lines = append(out.Lines, lines...)
	return out
}

// activity is the one-line answer WITHOUT the tail. It is separate from
// [taskLive.state] because a row asks this question and only this question, and
// copying two hundred lines to render twenty characters is a cost paid on every
// list of every task.
//
// A nil recorder answers "" rather than "starting": no room has ever been
// opened on that node, so there is nothing to say about it, and a queued node's
// row already says queued.
func (l *taskLive) activity() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	snapshot := taskLiveState{Call: l.call, Since: l.began, Last: l.last, Steps: l.steps}
	l.mu.Unlock()
	return snapshot.activity()
}

// taskLiveCall is one call in one line: the session's own gloss when there is
// one, the tool's name and its arguments when there is not. It is the event's
// text and not a rendering of its own — internal/tui3's rail says the same
// sentence about the same call, and two spellings of "read session.go" would be
// two vocabularies for one fact.
func taskLiveCall(event Event) string {
	if hint := strings.TrimSpace(event.Hint); hint != "" {
		return hint
	}
	name := strings.TrimSpace(event.Tool)
	if args := strings.TrimSpace(firstLine(event.Args)); args != "" {
		return clip(name+" "+args, hintLimit)
	}
	if name == "" {
		return "a call"
	}
	return name
}

// activity is what the node is doing, in the ONE LINE a row has for it.
//
// Three answers and no fourth: a call with a clock on it, a call that has been
// written but not started, or the gap between calls — which is the node
// thinking, and is worth saying with the step count beside it so that "nothing
// visible is happening" and "nothing has happened for four minutes" are
// different sentences.
func (s taskLiveState) activity() string {
	switch {
	case s.Call != "" && !s.Since.IsZero():
		if span := taskSpanWord(time.Since(s.Since)); span != "" {
			return s.Call + " · " + span
		}
		return s.Call
	case s.Call != "":
		return s.Call + " · about to start"
	case s.Steps > 0 && s.Last != "":
		return fmt.Sprintf("thinking after %s · %d %s so far",
			s.Last, s.Steps, taskStepWord(s.Steps))
	case s.Steps > 0:
		return fmt.Sprintf("thinking · %d %s so far", s.Steps, taskStepWord(s.Steps))
	default:
		return "starting"
	}
}

func taskStepWord(n int) string {
	if n == 1 {
		return "step"
	}
	return "steps"
}

// ── the read ────────────────────────────────────────────────────────────────

// taskLiveRead is ONE node's state, off the GRAPH: its row as the index would
// draw it, its live cost, and its recorder's tail. The bool is false for an id
// this session's graph has never held.
//
// It never touches tasks.jsonl. The file is what landed work came to, written
// once at the transition, and a running node is by definition not in it — a
// read that fell back to the index would answer a question about the present
// with the most recent thing that was over.
func (a *Agent) taskLiveRead(id uint64, tail int) (TaskIndexEntry, taskLiveState, bool) {
	node := a.taskNode(id)
	if node == nil {
		return TaskIndexEntry{}, taskLiveState{}, false
	}
	// The spend is read BEFORE the graph lock, exactly as [TaskNode.notice]
	// reads it: it asks the room for the child and the child for its usage, and
	// taking those under the graph's lock would be a second lock order in a
	// package that has one.
	cost := node.spend()
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()

	node.graph.mu.Lock()
	entry := node.indexEntryLocked(session)
	live := node.room.recorder()
	node.graph.mu.Unlock()

	// A LIVE FIGURE WHERE THE INDEX HAS NONE. An index row for a running node
	// carries no price on purpose — the frozen number is not written until the
	// node lands — but a model deciding whether to let work keep going is
	// exactly the reader that needs what it has cost so far.
	if cost > 0 {
		entry.Cost = cost
	}
	return entry, live.state(tail), true
}

// recorder is the room's live recorder, nil-safe for a node that has no room —
// one that is still queued, or one whose room closed with it in an earlier life
// of this session.
func (r *taskRoom) recorder() *taskLive {
	if r == nil {
		return nil
	}
	return r.live
}
