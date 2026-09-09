package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE PHASE CLOCK, AS A TURN MOVES IT ─────────────────────────────────────
//
// Three laws are pinned here, and each of them is a defect that was reported
// rather than a shape somebody liked:
//
//   - A TURN SAYS WHAT IT IS DOING BETWEEN REQUESTS, and everything it says it
//     also finishes saying. A phase left open is a clock a surface goes on
//     drawing for work that ended.
//   - A SIDE ERRAND NEVER OWNS THE CLOCK. The status line belongs to the answer
//     somebody is reading, never to whichever call happened to answer last.
//   - ONE MECHANISM ANSWERS ONE SILENCE. This package no longer keeps a hedge
//     of its own; the transport's does, and a silent stream is asked for once.

// phaseLog is every phase this package posted, in order.
type phaseLog struct {
	mu   sync.Mutex
	news []PhaseNews
}

func (l *phaseLog) add(news PhaseNews) {
	l.mu.Lock()
	l.news = append(l.news, news)
	l.mu.Unlock()
}

func (l *phaseLog) all() []PhaseNews {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]PhaseNews, len(l.news))
	copy(out, l.news)
	return out
}

// watchPhases registers a reader for the length of one test and puts back
// whatever was there, exactly as a surface closing over another one does.
func watchPhases(t *testing.T) *phaseLog {
	t.Helper()
	log := &phaseLog{}
	previous := OnPhaseNews(log.add)
	t.Cleanup(func() { OnPhaseNews(previous) })
	return log
}

// phaseWords is the phases in the order they were posted, with the ends written
// as the empty string they are, so a failure message reads as the story.
func phaseWords(news []PhaseNews) []string {
	words := make([]string, 0, len(news))
	for _, one := range news {
		words = append(words, string(one.Phase))
	}
	return words
}

func TestATurnsPhasesArriveInOrderAndEveryOneIsClosed(t *testing.T) {
	log := watchPhases(t)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "read", `{"path":"note.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("read it"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("hello from disk\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	collect(t, mustSubmit(t, agent, "read note.txt"))

	news := log.all()
	if len(news) == 0 {
		t.Fatal("the turn posted no phases at all")
	}

	// THE TOOL ROUND IS THE ONE A PERSON WAITS THROUGH LONGEST, so it is the one
	// this insists on, named as the surface names it.
	var running *PhaseNews
	for i := range news {
		if news[i].Phase == provider.PhaseRunning {
			running = &news[i]
			break
		}
	}
	if running == nil {
		t.Fatalf("no running phase for the tool round; phases were %v", phaseWords(news))
	}
	if running.Detail != "read" {
		t.Fatalf("running detail = %q, want the tool's own name", running.Detail)
	}
	if running.Since.IsZero() {
		t.Fatal("the running phase carries no start, so nothing can count up from it")
	}

	// A plain answer closes directly; it does not buy another model call to
	// certify itself or advertise a checking phase that never ran.
	for _, one := range news {
		if one.Phase == provider.PhaseChecking {
			t.Fatalf("a plain answer started an unrequested check; phases were %v", phaseWords(news))
		}
	}

	// EVERY PHASE IS CLOSED, AND NONE IS OPENED OVER ANOTHER. A post with no
	// phase is the end of a story; a second one changes nothing, which is why
	// the turn's own deferred end is free to fire after the last gate has
	// already closed its own.
	//
	// A REPEAT OF THE SAME PHASE FROM THE SAME MOMENT IS THE BEAT AND NOT A NEW
	// STAGE (phasenews.go's [phaseHeldBeat]). A held stage says itself again
	// while it lasts, and it is the SAME sentence each time — same word, same
	// start — so what this law is really about is a DIFFERENT phase begun while
	// one was still open, and that is what it now says.
	var open *PhaseNews
	for index := range news {
		one := news[index]
		if one.Phase == "" {
			open = nil
			continue
		}
		if open != nil {
			if one.Phase == open.Phase && one.Since.Equal(open.Since) {
				continue
			}
			t.Fatalf("post %d opened a phase over the top of one still running; phases were %v",
				index, phaseWords(news))
		}
		open = &news[index]
	}
	if open != nil {
		t.Fatalf("the turn ended with a clock still running; phases were %v", phaseWords(news))
	}
}

// windowsOpen replaces this process's record of open conversations for the
// length of one test, and puts back what was there.
//
// TWO THINGS IN THIS PACKAGE READ IT — what a second of a task node's wait is
// worth ([Agent.turnLambda]) and which of the two leaf roles its calls are made
// under ([Agent.laneRole]) — so a test that leaves the answer to whichever
// OTHER test last opened a window is a test that passes alone and fails in the
// suite. That is exactly how the pair below was found.
func windowsOpen(t *testing.T, open bool) {
	t.Helper()
	liveSessionsMu.Lock()
	held := liveSessions
	liveSessions = map[string]liveWindow{}
	if open {
		liveSessions["a room somebody is sitting in"] = liveWindow{opened: time.Now()}
	}
	liveSessionsMu.Unlock()
	t.Cleanup(func() {
		liveSessionsMu.Lock()
		liveSessions = held
		liveSessionsMu.Unlock()
	})
}

func TestAHiddenRolesWorkNeverOwnsThePhaseClock(t *testing.T) {
	script := func() []step {
		return []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-1", "read", `{"path":"note.txt"}`), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("read it"), nil
			},
		}
	}

	// A TASK NODE WITH NOBODY IN THE ROOM. Its turn is the identical loop making
	// the identical calls, and the only thing that differs is that nobody is
	// reading this stream — which is the whole of what [lane.Role.Visible] is
	// for.
	windowsOpen(t, false)
	hidden := watchPhases(t)
	completer := &scriptedCompleter{steps: script()}
	agent, workspace := newTestAgent(t, completer, func(config *Config) { config.InTask = true })
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("hello from disk\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	collect(t, mustSubmit(t, agent, "read note.txt"))

	posted := 0
	for _, one := range hidden.all() {
		if one.Phase == "" {
			continue
		}
		posted++
		if one.Role.Visible() {
			t.Fatalf("a task node's %q phase claimed the clock as role %q", one.Phase, one.Role)
		}
		if one.Role != lane.RoleLeafUnattended {
			t.Fatalf("a task node's phase named role %q, want the unattended leaf", one.Role)
		}
	}
	if posted == 0 {
		t.Fatal("the task node posted no phases, so nothing was proved about them")
	}
}

func TestAConversationsOwnTurnDoesOwnThePhaseClock(t *testing.T) {
	log := watchPhases(t)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "read", `{"path":"note.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("read it"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("hello from disk\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	collect(t, mustSubmit(t, agent, "read note.txt"))

	for _, one := range log.all() {
		if one.Phase == "" {
			continue
		}
		if !one.Role.Visible() {
			t.Fatalf("the conversation's own %q phase read as hidden, role %q", one.Phase, one.Role)
		}
	}
}

// TestASilentStreamIsAskedForExactlyOnce is the retired hedge.
//
// This loop used to keep a duplicate-request-on-silence mechanism of its own: a
// fixed eight-second timer that sent the WHOLE prompt a second time, into the
// same pool, out of nobody's budget. The transport now answers the same silence
// better (internal/provider's hedge.go over internal/lane's watch.go) — a
// deadline derived from the serving lane's own posterior, a named alternative
// lane, a process-wide budget, the loser cancelled and both arms folded back
// into the ledger. Two mechanisms racing one silence was two bills for one
// answer, so the blind one went.
//
// A stream that says nothing and then answers is therefore ONE request, and it
// says nothing to the person about trying again, because nothing was tried
// twice.
func TestASilentStreamIsAskedForExactlyOnce(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			timer := time.NewTimer(80 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
				provider.Emit(ctx, provider.StreamDelta, "late")
				return textResponse("late"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	collected := collect(t, mustSubmit(t, agent, "take your time"))

	if got := completer.requests(); got != 1 {
		t.Fatalf("requests = %d, want one — this package no longer duplicates a silent request", got)
	}
	for _, event := range collected {
		if event.Kind == EventRetrying {
			t.Fatalf("a silent stream was announced as a retry: %q", event.Text)
		}
	}
	if got := messageText(lastMessage(agent)); got != "late" {
		t.Fatalf("transcript answer = %q, want the one late answer", got)
	}
}

// TestACompactionPassIsVisibleWhileItRuns is the promise [EventCompacting] was
// declared with, made true.
//
// The event has said in its own doc comment since the day it was written that a
// pass is visible while it runs, and internal/tui3 handles it in three places on
// the strength of that — but nothing anywhere in this repository ever SENT it,
// so a turn that stopped to tidy itself drew the finished line and never the
// work. It is paired: a surface opens a row on the first and settles it on the
// second, so a pass that announced itself and then said nothing would leave that
// row open for the rest of the session.
func TestACompactionPassIsVisibleWhileItRuns(t *testing.T) {
	log := watchPhases(t)
	long := strings.Repeat("working through the parser. ", 40)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(long), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		// A 200-token window puts the threshold at 100 and the keep-recent tail
		// at 50, so one long reply is already too much.
		config.ContextWindow = 200
		config.CompactEnabled = true
	})

	collected := collect(t, mustSubmit(t, agent, "start"))

	began, settled := -1, -1
	for index := range collected {
		switch collected[index].Kind {
		case EventCompacting:
			if began < 0 {
				began = index
				if collected[index].Hint == "" {
					t.Fatal("the pass announced itself without saying how big it was")
				}
			}
		case EventCompacted:
			if settled < 0 {
				settled = index
			}
		}
	}
	if began < 0 {
		t.Fatalf("no EventCompacting; events = %v", kinds(collected))
	}
	if settled < began {
		t.Fatalf("EventCompacted did not follow EventCompacting; events = %v", kinds(collected))
	}

	tidying := false
	for _, one := range log.all() {
		if one.Phase == provider.PhaseTidying {
			tidying = true
		}
	}
	if !tidying {
		t.Fatalf("the pass drew no clock; phases were %v", phaseWords(log.all()))
	}
}

// ── THE BEAT ────────────────────────────────────────────────────────────────

// heldSince is how many times one phase was said, and the moment every one of
// those posts counted from. A held phase says itself again while it lasts and
// every repeat is the SAME sentence, so a second start moment in this set is the
// beat restarting a stage rather than continuing it.
func heldSince(news []PhaseNews, phase provider.Phase) (said int, starts map[time.Time]bool) {
	starts = map[time.Time]bool{}
	for _, one := range news {
		if one.Phase != phase {
			continue
		}
		said++
		starts[one.Since] = true
	}
	return said, starts
}

// TestAHeldPhaseKeepsSayingItselfAndStopsWhenItEnds is the defect this beat was
// written for, measured: the route judge at the end of a turn — one
// tellPhase(PhaseChecking, …) in loop.go — ran for a quarter of an hour and the
// screen drew it for fifteen seconds, because a surface drops a phase it has not
// heard again for [provider.PhaseWindow] and nothing in this package was saying
// it again.
//
// The beat is shortened to milliseconds here for the obvious reason: what is
// being proved is that a HELD phase repeats and a FINISHED one does not, and
// neither of those facts is about the length of the interval.
func TestAHeldPhaseKeepsSayingItselfAndStopsWhenItEnds(t *testing.T) {
	log := watchPhases(t)
	held := phaseHeldBeat
	phaseHeldBeat = 2 * time.Millisecond
	t.Cleanup(func() { phaseHeldBeat = held })

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	// A STAGE THAT HAS ALREADY OUTLASTED THE WINDOW. This is the shape the
	// quarter-hour reading had: begun long ago, still running, and owed a clock
	// that says how long it has really been.
	began := time.Now().Add(-provider.PhaseWindow - time.Minute)
	agent.tellPhase(provider.PhaseChecking, "whether the work is finished", began)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if said, _ := heldSince(log.all(), provider.PhaseChecking); said >= 4 {
			break
		}
		if time.Now().After(deadline) {
			said, _ := heldSince(log.all(), provider.PhaseChecking)
			t.Fatalf("a stage held open said itself %d times; a surface drops one it has not heard for %s",
				said, provider.PhaseWindow)
		}
		time.Sleep(time.Millisecond)
	}

	// AND EVERY BEAT IS THE SAME STAGE, counting from where it really began. A
	// beat that moved the start would draw a clock that restarted every few
	// seconds, which reads as work going round in circles rather than work
	// lasting.
	_, starts := heldSince(log.all(), provider.PhaseChecking)
	if len(starts) != 1 {
		t.Fatalf("the stage was said from %d different starts, want the one it began at", len(starts))
	}
	if !starts[began] {
		t.Fatalf("the beat re-said the stage from a start of its own, not from %s", began)
	}

	agent.endPhase()
	settled, _ := heldSince(log.all(), provider.PhaseChecking)
	// SEVERAL BEATS' WORTH OF SILENCE. A beat that outlived its phase would put
	// a finished stage back on the screen for a whole window, which is the same
	// defect as a stale clock and harder to see.
	time.Sleep(20 * time.Millisecond)
	if after, _ := heldSince(log.all(), provider.PhaseChecking); after != settled {
		t.Fatalf("the beat said the stage %d more times after it ended", after-settled)
	}
	last := log.all()
	if len(last) == 0 || last[len(last)-1].Phase != "" {
		t.Fatalf("the stage did not take itself off the screen; phases were %v", phaseWords(last))
	}
}

// TestTheBeatIsComfortablyInsideTheWindowASurfaceDrops is the one-source-of-truth
// law between two packages, checked rather than commented.
//
// internal/tui3 draws nothing for a phase it has not heard again for
// [provider.PhaseWindow]. A beat at or near that window is a stage that blinks
// on a busy frame; a beat past it is a stage that goes dark while it is still
// running, which is the whole defect.
func TestTheBeatIsComfortablyInsideTheWindowASurfaceDrops(t *testing.T) {
	if phaseHeldBeat <= 0 {
		t.Fatalf("phaseHeldBeat = %s, which is not a beat at all", phaseHeldBeat)
	}
	if phaseHeldBeat*2 >= provider.PhaseWindow {
		t.Fatalf("phaseHeldBeat = %s against a %s window: a single dropped beat takes a live stage "+
			"off the screen", phaseHeldBeat, provider.PhaseWindow)
	}
}
