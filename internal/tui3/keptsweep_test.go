package tui3

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// THE SWEEP: what gives an open conversation's cost back without a person asking.
//
// The keeper refuses nothing however many conversations somebody opens, which is
// the owner's own ruling — so the only honest way to bound what a window holds is
// to let go of the ones nothing would be lost from. These tests are the list of
// what "nothing would be lost" means, one claim at a time.

// askingAgent is a conversation stopped on a question. No other double on this
// surface answers [session.Agent.NeedsPerson], and the sweep asks that door
// directly rather than trusting the watcher's cached copy of it.
type askingAgent struct{ *fakeAgent }

func (askingAgent) NeedsPerson() bool { return true }

// coldKept is one conversation this window holds that is free to let go of: a
// watcher with nothing running on it, an empty box, and a detach far enough back
// that letting go of it is no surprise.
func coldKept(agent Agent, file string, left time.Time) *kept {
	return &kept{
		conv:  Conversation{Agent: agent, SessionFile: file},
		side:  &aside{since: left, title: "the cold one"},
		watch: &behindWatch{key: file, quit: make(chan struct{})},
	}
}

// fillKeeper puts `count` cold conversations in the keeper, oldest first on the
// previous-stack, and hands back the agents in that order so a test can ask which
// of them was let go of.
func fillKeeper(a *app, count int, left time.Time) []*switchAgent {
	if a.behind == nil {
		a.behind = map[string]*kept{}
	}
	agents := make([]*switchAgent, 0, count)
	for i := 0; i < count; i++ {
		file := "/tmp/lab/cold" + itoa(i) + "/transcript.jsonl"
		agent := &switchAgent{fakeAgent: &fakeAgent{model: "m"}, left: make(chan struct{})}
		agents = append(agents, agent)
		a.behind[file] = coldKept(agent, file, left.Add(time.Duration(i)*time.Minute))
		a.prev = append(a.prev, file)
	}
	return agents
}

// awaitLeft waits until the sweep's off-frame close has actually run. The
// keeper forgets the conversation on the calling goroutine; Interrupt and Close
// happen afterwards, and reading those counts before this returns is a race.
func awaitLeft(t *testing.T, agent *switchAgent) {
	t.Helper()
	if agent.left == nil {
		t.Fatal("awaitLeft needs an agent whose Close signals left")
	}
	select {
	case <-agent.left:
	case <-time.After(2 * time.Second):
		t.Fatal("the sweep did not let go of the conversation")
	}
}

// hostedLeave is a hosted conversation the sweep lets go of, with a way for a
// test to wait until Detach has actually run — that call now happens off the
// frame, the same as Interrupt and Close on an in-process conversation.
type hostedLeave struct {
	*hostedAgent
	left chan struct{}
	once sync.Once
}

func (h *hostedLeave) Detach() error {
	err := h.hostedAgent.Detach()
	h.once.Do(func() { close(h.left) })
	return err
}

// blockLeave is a conversation whose Close waits until the test unblocks it,
// which is how [TestTheSweepDoesNotWaitOnTheAgentLeaving] can tell the sweep
// returned without waiting on the close.
type blockLeave struct {
	*switchAgent
	started chan struct{}
	block   chan struct{}
}

func (b *blockLeave) Close() error {
	close(b.started)
	<-b.block
	return b.switchAgent.Close()
}

// Past the ceiling the coldest quiet conversation is let go of, and it is the one
// left longest ago rather than any other.
func TestPastTheCeilingTheKeeperLetsGoOfTheColdestQuietConversation(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.file = "/tmp/lab/front/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.clock = func() time.Time { return now }

	// One more than the ceiling holds, counting the one on screen, and every one
	// of them cold.
	agents := fillKeeper(a, keptCeiling, now.Add(-2*time.Hour))
	a.sweepKept()
	awaitLeft(t, agents[0])

	if got := a.openCount(); got != keptCeiling {
		t.Fatalf("the sweep left this window holding %d conversations, and the ceiling is %d", got, keptCeiling)
	}
	if agents[0].closes == 0 {
		t.Fatal("the conversation left longest ago is still open — the sweep took another one, or none")
	}
	for i, agent := range agents[1:] {
		if agent.closes != 0 {
			t.Fatalf("the sweep let go of cold%d as well, and one under the ceiling is enough", i+1)
		}
	}
	if a.behind["/tmp/lab/cold0/transcript.jsonl"] != nil {
		t.Fatal("the conversation the sweep let go of is still in the keeper")
	}
	for _, key := range a.prev {
		if key == "/tmp/lab/cold0/transcript.jsonl" {
			t.Fatal("the conversation the sweep let go of is still on the way back")
		}
	}
	// AND THE PERSON IS TOLD, in the words that say what became of it.
	if got := plain(lastNote(t, a)); !strings.Contains(got, keptSweptNameWord) || !strings.Contains(got, keptSweptWord) {
		t.Fatalf("the sweep said %q", got)
	}
}

// Every reason not to let go of a conversation, one row at a time. A row that
// says false is a conversation somebody would come back to something missing
// from.
func TestTheSweepLeavesEveryConversationSomethingWouldBeLostFrom(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.clock = func() time.Time { return now }
	cold := now.Add(-2 * time.Hour)

	tests := []struct {
		name  string
		held  *kept
		quiet bool
	}{
		{name: "nothing running, nothing waiting, an empty box, left two hours ago", quiet: true,
			held: coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)},
		{name: "a turn still streaming in it", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.watch.turning.Store(true)
			return held
		}()},
		{name: "a task node still working in it", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.watch.tasking.Store(true)
			return held
		}()},
		{name: "a background job still running in it", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.watch.jobbing.Store(true)
			return held
		}()},
		{name: "a question waiting on the person", held: coldKept(askingAgent{&fakeAgent{model: "m"}}, "a", cold)},
		{name: "the watcher still says it is waiting", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.watch.waits.Store(true)
			return held
		}()},
		{name: "a turn that landed in there and has not been seen", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.watch.finished.Add(1)
			return held
		}()},
		{name: "an unsent sentence still in its box", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.side.draft = "and then check the migration"
			return held
		}()},
		{name: "a message still waiting for its answer", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.side.parks = []parked{{text: "and then check the migration"}}
			return held
		}()},
		{name: "a waiting message whose submit is crossing", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.side.parkSending = true
			return held
		}()},
		{name: "a picture still on its box", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.side.chips = []chip{{}}
			return held
		}()},
		{name: "a message of theirs that has not settled", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.side.sends = []outboxSnapshot{{}}
			return held
		}()},
		{name: "another window has asked for it", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.watch.takeover.Store(true)
			return held
		}()},
		{name: "another window has already opened it", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.watch.moved.Store(true)
			return held
		}()},
		{name: "left only a minute ago", held: coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", now.Add(-time.Minute))},
		{name: "no watcher on it, so nothing about it is known", held: func() *kept {
			held := coldKept(&switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "a", cold)
			held.watch = nil
			return held
		}()},
	}
	for _, test := range tests {
		if got := a.keptQuiet(test.held, now); got != test.quiet {
			t.Errorf("%s: the sweep would let go of it = %v, and it is %v", test.name, got, test.quiet)
		}
	}
}

// A window whose held conversations are all busy holds every one of them, however
// far past the ceiling that is. THE CEILING IS NOT A CAP: nothing is refused for
// it, and nothing somebody is waiting on is closed to honour it.
func TestTheCeilingNeverClosesWorkAndNeverRefusesAConversation(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.file = "/tmp/lab/front/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.clock = func() time.Time { return now }

	agents := fillKeeper(a, keptCeiling+8, now.Add(-2*time.Hour))
	for _, key := range a.prev {
		a.behind[key].watch.tasking.Store(true)
	}
	a.sweepKept()

	if got := a.openCount(); got != keptCeiling+9 {
		t.Fatalf("this window holds %d conversations, and every one of them was working", got)
	}
	for i, agent := range agents {
		if agent.closes != 0 {
			t.Fatalf("cold%d was closed with work running in it", i)
		}
	}
	// And a door onto another one still opens: the ceiling is never a refusal.
	stowOne(t, a, &switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "/tmp/lab/another/transcript.jsonl")
	if got := a.openCount(); got != keptCeiling+10 {
		t.Fatalf("opening one more past the ceiling left %d open", got)
	}
}

// A conversation that has just stopped working is the other moment the sweep
// can act: this surface has no idle ticker, so a window left holding cold
// conversations would otherwise only collect one when somebody opens another.
func TestAConversationFinishingCollectsAColdOne(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.file = "/tmp/lab/front/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.clock = func() time.Time { return now }

	agents := fillKeeper(a, keptCeiling, now.Add(-2*time.Hour))
	for _, key := range a.prev {
		a.behind[key].watch.tasking.Store(true)
	}
	a.sweepKept()
	if agents[0].closes != 0 {
		t.Fatal("a conversation still working was collected")
	}

	oldest := "/tmp/lab/cold0/transcript.jsonl"
	a.behind[oldest].watch.tasking.Store(false)
	// The returned command waits on the stir lane; the sweep itself runs here.
	_ = a.behindStir(behindStirMsg{key: oldest})
	awaitLeft(t, agents[0])

	if agents[0].closes == 0 {
		t.Fatal("a conversation finishing did not collect a cold one")
	}
	if a.behind[oldest] != nil {
		t.Fatal("the conversation the sweep let go of is still in the keeper")
	}
}

// Opening another conversation is what collects a cold one — the sweep is wired
// to the keystroke that grows the keeper, and the conversation just left is never
// what it takes.
func TestOpeningAnotherConversationCollectsAColdOneAndNeverTheOneJustLeft(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	front := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(front)
	a.file = "/tmp/lab/front/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.clock = func() time.Time { return now }
	agents := fillKeeper(a, keptCeiling-1, now.Add(-2*time.Hour))

	stowOne(t, a, &switchAgent{fakeAgent: &fakeAgent{model: "m"}}, "/tmp/lab/another/transcript.jsonl")
	awaitLeft(t, agents[0])

	if got := a.openCount(); got != keptCeiling {
		t.Fatalf("this window holds %d conversations after opening one past the ceiling", got)
	}
	if agents[0].closes == 0 {
		t.Fatal("opening another conversation collected nothing")
	}
	if front.closes != 0 {
		t.Fatal("the conversation the person had just left was collected")
	}
}

// A hosted conversation is let go of by DETACHING: its engine keeps the turn, the
// tasks and the journal, and the sentence says so rather than claiming a close.
func TestASweptHostedConversationIsDetachedAndSaysItsWorkKeepsRunning(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.file = "/tmp/lab/front/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.clock = func() time.Time { return now }

	hosted := &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}
	left := make(chan struct{})
	fillKeeper(a, keptCeiling-1, now.Add(-time.Hour))
	file := "/tmp/lab/hosted/transcript.jsonl"
	a.behind[file] = coldKept(&hostedLeave{hostedAgent: hosted, left: left}, file, now.Add(-3*time.Hour))
	a.prev = append([]string{file}, a.prev...)

	a.sweepKept()
	select {
	case <-left:
	case <-time.After(2 * time.Second):
		t.Fatal("the sweep did not detach the hosted conversation")
	}

	if hosted.detaches != 1 {
		t.Fatalf("the hosted conversation was detached %d times", hosted.detaches)
	}
	if hosted.closes != 0 {
		t.Fatal("the sweep closed a hosted conversation, which ends work its engine is still running")
	}
	if got := plain(lastNote(t, a)); !strings.Contains(got, keptSweptOnWord) {
		t.Fatalf("the sweep said %q about a conversation whose work keeps running", got)
	}
}

// THE CLOSE IS NOT THIS KEYSTROKE. [app.sweepKept] runs on the update path, and
// Interrupt-and-Close can wait; if the sweep called [leaveAgent] itself, a
// conversation whose Close blocked would stall the window until it returned.
func TestTheSweepDoesNotWaitOnTheAgentLeaving(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.file = "/tmp/lab/front/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.clock = func() time.Time { return now }

	agents := fillKeeper(a, keptCeiling, now.Add(-2*time.Hour))
	started := make(chan struct{})
	block := make(chan struct{})
	slow := &blockLeave{switchAgent: agents[0], started: started, block: block}
	a.behind["/tmp/lab/cold0/transcript.jsonl"].conv.Agent = slow

	returned := make(chan struct{})
	go func() {
		a.sweepKept()
		close(returned)
	}()
	defer close(block)

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("the sweep stayed on this keystroke waiting for the conversation to close")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the sweep never started leaving the conversation")
	}
	if a.behind["/tmp/lab/cold0/transcript.jsonl"] != nil {
		t.Fatal("the keeper still holds the conversation it is supposed to have let go of")
	}
}

// The words themselves: what the sweep says carries no machinery vocabulary and
// tells the person where the conversation went.
func TestTheSweepSaysWhereTheConversationWent(t *testing.T) {
	for _, said := range []string{keptSweptWord, keptSweptOnWord, keptSweptNameWord} {
		for _, banned := range []string{"evict", "cap", "limit", "LRU", "memory"} {
			if strings.Contains(strings.ToLower(said), strings.ToLower(banned)) {
				t.Errorf("%q says %q, which is machinery and not something a person reads", said, banned)
			}
		}
	}
	if !strings.Contains(keptSweptWord, "home") {
		t.Errorf("%q does not say how to get the conversation back", keptSweptWord)
	}
}

// And the keeper's own door still ends a conversation for real when a person asks
// for that: the shared bookkeeping did not turn ctrl+w into a detach.
func TestClosingAKeptConversationStillEndsIt(t *testing.T) {
	a := newTestApp(&switchAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.stirs = make(chan behindStirMsg, stirDepth)
	agent := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	file := "/tmp/lab/one/transcript.jsonl"
	a.behind = map[string]*kept{file: coldKept(agent, file, time.Now())}
	a.prev = []string{file}

	if !a.closeKept(file) {
		t.Fatal("the keeper would not close a conversation it holds")
	}
	if agent.closes != 1 || agent.stopDoor != session.StopByLeaving {
		t.Fatalf("closes=%d door=%v", agent.closes, agent.stopDoor)
	}
	if len(a.behind) != 0 || len(a.prev) != 0 {
		t.Fatal("the closed conversation is still remembered")
	}
}
