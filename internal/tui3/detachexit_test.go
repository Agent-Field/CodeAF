package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// quitStoppingWord is the half of the quit warning about work that ends with the
// window, which is what every test written before hosting existed was asking
// for.
func quitStoppingWord(a *app) string {
	stopping, _ := a.quitWorkCounts()
	return quitWorkWord(stopping)
}

// stowAgent puts the conversation on screen into the keeper and attaches
// another, whatever kind of agent it is ([stowOne] takes only the switcher's
// own fake).
func stowAgent(t *testing.T, a *app, agent Agent, file string) {
	t.Helper()
	leaving, side := a.front(), a.detachConversation()
	a.stow(leaving, side)
	drain(t, a, a.attachConversation(Conversation{Agent: agent, SessionFile: file}, nil))
}

// hostedAgent is the shape [remote.Agent] has: it always answers the detach
// seam, and says separately whether its conversation outlives this terminal —
// true for a session host, false for a one-shot engine on a pipe. It counts what
// a quit does to it so a test can tell a detach from an ending.
type hostedAgent struct {
	*fakeAgent
	oneShot  bool
	detaches int
}

func (h *hostedAgent) WorkOutlivesExit() bool { return !h.oneShot }

func (h *hostedAgent) Detach() error {
	h.detaches++
	if h.oneShot {
		// What the real one does when the pipe is the conversation's whole life.
		h.Interrupt()
		return h.Close()
	}
	return nil
}

// A WINDOW CLOSING IS A VIEW LEAVING AND NOT WORK ENDING. Quitting used to
// interrupt and close every agent, which for a hosted conversation is a message
// to the far side saying the conversation is over — so closing a terminal on a
// running task paused it and restarted its worker on the way back.
func TestQuittingDetachesFromHostedWorkInsteadOfEndingIt(t *testing.T) {
	hosted := &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(hosted)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)

	a.leaveEverything()

	if hosted.detaches != 1 {
		t.Errorf("quitting detached %d times, want once", hosted.detaches)
	}
	if hosted.closes != 0 || hosted.stops != 0 {
		t.Errorf("quitting ended hosted work: closes=%d interrupts=%d", hosted.closes, hosted.stops)
	}

	// AND IT IS IDEMPOTENT, because a signal and a keystroke can both arrive.
	a.leaveEverything()
	if hosted.closes != 0 || hosted.stops != 0 {
		t.Errorf("the second leave ended hosted work: closes=%d interrupts=%d", hosted.closes, hosted.stops)
	}
}

// AND AN IN-PROCESS CONVERSATION STILL ENDS HERE, because this process was the
// only thing running it: detaching from it would leave the work nowhere.
func TestQuittingStillClosesAnInProcessConversation(t *testing.T) {
	local := &fakeAgent{model: "m"}
	a := newTestApp(local)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)

	a.leaveEverything()

	if local.closes != 1 || local.stops != 1 {
		t.Errorf("an in-process conversation left with closes=%d interrupts=%d, want one of each", local.closes, local.stops)
	}
}

// A TERMINAL HOLDS BOTH KINDS AT ONCE, and each one is left on its own terms:
// the conversation on screen and the ones in the keeper are asked separately.
func TestQuittingHandlesAMixOfHostedAndLocalConversations(t *testing.T) {
	hosted := &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(hosted)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	behind := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	stowOne(t, a, behind, "/tmp/lab/two/transcript.jsonl")

	a.leaveEverything()

	if hosted.detaches != 1 || hosted.closes != 0 {
		t.Errorf("the hosted conversation on screen: detaches=%d closes=%d", hosted.detaches, hosted.closes)
	}
	if !behind.closed {
		t.Error("the in-process conversation in the keeper was not closed")
	}
	if len(a.behind) != 0 {
		t.Error("the keeper survived the quit")
	}
}

// THE WARNING SAYS WHAT IS ACTUALLY ABOUT TO HAPPEN. Telling somebody a hosted
// task will stop asks them to keep a window open for a reason that is not real.
func TestTheArmedHintSaysHostedWorkKeepsRunning(t *testing.T) {
	hosted := &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(hosted)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.tasks = map[uint64]*taskNode{1: {id: 1, state: session.TaskRunning}}
	a.taskOrder = []uint64{1}

	hint := a.quitHint()
	if !strings.Contains(hint, "a task keeps running") {
		t.Errorf("the armed hint is %q about hosted work", hint)
	}
	if strings.Contains(hint, "will stop") {
		t.Errorf("the armed hint threatens hosted work: %q", hint)
	}

	// Two of them agree with themselves.
	a.tasks[2] = &taskNode{id: 2, state: session.TaskRunning}
	a.taskOrder = []uint64{1, 2}
	if hint := a.quitHint(); !strings.Contains(hint, "2 tasks keep running") {
		t.Errorf("the armed hint is %q about two hosted tasks", hint)
	}
}

// AND A MIXED TERMINAL SAYS BOTH THINGS, in the order they cost: what ends
// first, what survives after it.
func TestTheArmedHintSeparatesWorkThatEndsFromWorkThatSurvives(t *testing.T) {
	// The in-process conversation with two running nodes goes into the keeper,
	// and the hosted one with a node of its own is the one on screen.
	a := newTestApp(&busyAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.file = "/tmp/lab/two/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	hosted := &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}
	stowAgent(t, a, hosted, "/tmp/lab/one/transcript.jsonl")
	a.tasks = map[uint64]*taskNode{1: {id: 1, state: session.TaskRunning}}
	a.taskOrder = []uint64{1}

	hint := a.quitHint()
	if !strings.Contains(hint, "2 tasks will stop") {
		t.Errorf("the armed hint lost the in-process tasks: %q", hint)
	}
	if !strings.Contains(hint, "a task keeps running") {
		t.Errorf("the armed hint lost the hosted task: %q", hint)
	}
	if strings.Index(hint, "will stop") > strings.Index(hint, "keeps running") {
		t.Errorf("the armed hint puts what survives before what ends: %q", hint)
	}
}

// LEAVING KEEPS THE PERSON'S WORDS AND THE CONVERSATION'S QUESTIONS. The draft
// and everything parked behind it go to disk on the way out, and a hosted
// conversation's pending question is not answered or cancelled by the window
// going away — nothing on the quit path touches it.
func TestQuittingSavesTheDraftAndLeavesAQuestionStanding(t *testing.T) {
	hosted := &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(hosted)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.draftFile = t.TempDir() + "/draft"
	a.input.setText("half a sentence")
	a.parks = []parked{{text: "and one waiting"}}
	raiseAsk(a, 7, "bash")

	a.quit()

	saved := readDraft(a.draftFile)
	if !strings.Contains(saved, "half a sentence") || !strings.Contains(saved, "and one waiting") {
		t.Errorf("the draft written on the way out is %q", saved)
	}
	if askCount(a) != 1 {
		t.Errorf("quitting left %d questions standing, want the one it was asked", askCount(a))
	}
	if hosted.closes != 0 || hosted.stops != 0 {
		t.Error("quitting ended the hosted conversation the question belongs to")
	}
}

// AND A SIGNAL TAKES THE SAME ROAD AS THE KEY. A closed terminal window arrives
// as SIGHUP, which this package forwards as [sigQuitMsg] (tui3.go's
// [forwardSignals]) — the road the live proof came down, where a hosted task was
// paused and its worker restarted because the signal ended the conversation.
func TestASignalDetachesFromHostedWorkToo(t *testing.T) {
	hosted := &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(hosted)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.draftFile = t.TempDir() + "/draft"
	a.input.setText("half a sentence")

	drive(t, a, sigQuitMsg{})

	if hosted.detaches != 1 {
		t.Errorf("the signal detached %d times, want once", hosted.detaches)
	}
	if hosted.closes != 0 || hosted.stops != 0 {
		t.Errorf("the signal ended hosted work: closes=%d interrupts=%d", hosted.closes, hosted.stops)
	}
	if saved := readDraft(a.draftFile); !strings.Contains(saved, "half a sentence") {
		t.Errorf("the signal left the draft as %q", saved)
	}
}

// AND IT STILL CLOSES A CONVERSATION THIS PROCESS IS RUNNING, which is the half
// of the signal path that was always right.
func TestASignalStillClosesAnInProcessConversation(t *testing.T) {
	local := &fakeAgent{model: "m"}
	a := newTestApp(local)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)

	drive(t, a, sigQuitMsg{})

	if local.closes != 1 || local.stops != 1 {
		t.Errorf("the signal left it with closes=%d interrupts=%d, want one of each", local.closes, local.stops)
	}
}

// A ONE-SHOT ENGINE ANSWERS THE DETACH SEAM AND ITS WORK STILL STOPS. Reading
// the seam itself as "this keeps running" would promise survival to exactly the
// connection that cannot offer it — `aforge engine` on a pipe, and a --host
// launch against one — so the warning asks for the lifetime instead of the
// capability.
func TestAOneShotRemoteIsWarnedAboutHonestly(t *testing.T) {
	oneShot := &hostedAgent{fakeAgent: &fakeAgent{model: "m"}, oneShot: true}
	a := newTestApp(oneShot)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.tasks = map[uint64]*taskNode{1: {id: 1, state: session.TaskRunning}}
	a.taskOrder = []uint64{1}

	if hint := a.quitHint(); !strings.Contains(hint, "a task will stop") {
		t.Errorf("the armed hint is %q about a one-shot engine's task", hint)
	}
	if hint := a.quitHint(); strings.Contains(hint, "keeps running") {
		t.Errorf("the armed hint promised survival on a one-shot engine: %q", hint)
	}

	// And leaving still goes through the agent's own door, which ends it.
	a.leaveEverything()
	if oneShot.detaches != 1 {
		t.Errorf("leaving asked the agent %d times", oneShot.detaches)
	}
	if oneShot.closes != 1 || oneShot.stops != 1 {
		t.Errorf("a one-shot conversation left with closes=%d interrupts=%d", oneShot.closes, oneShot.stops)
	}
}

// ── the real client, not a marker ───────────────────────────────────────────

// servedAgent is [fakeAgent] with the one method an engine wants that the
// surface never calls ([remote.WrappedAgent] is the union of the two).
type servedAgent struct{ *fakeAgent }

func (s *servedAgent) ReasoningLevels() map[string]string { return s.levels }

func (s *servedAgent) ResolveStanding(uint64, session.StandingAnswer) {}

func (s *servedAgent) Steer(string) (<-chan session.Event, error) { return nil, nil }

func (s *servedAgent) RewindPoints() []session.RewindPoint { return nil }

func (s *servedAgent) RewindAt(int) ([]session.DisplayEntry, error) { return nil, nil }

// remoteFront dials a real [remote.Agent] at a real served conversation over an
// in-memory pipe and puts it in front of the surface. `persistent` is the fact
// the engine states about its own lifetime, which is the whole subject here.
func remoteFront(t *testing.T, persistent bool) (*app, *remote.Agent) {
	t.Helper()
	scripted := &servedAgent{fakeAgent: &fakeAgent{model: "m"}}
	surface, engine := remote.Pipe()
	go func() {
		_ = remote.ServeAttach(engine, engine, remote.AttachOptions{
			Open: func(remote.Hello) (*remote.Session, error) {
				return remote.NewSession(&remote.Engine{Agent: scripted, Workspace: "/tmp/lab"}, persistent), nil
			},
		})
		_ = engine.Close()
	}()
	client, err := remote.Dial(surface, "loopback", remote.Hello{Version: remote.Version})
	if err != nil {
		_ = surface.Close()
		t.Fatalf("dial the conversation: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	agent := client.Agent()
	a := newTestApp(agent)
	a.file = "/tmp/lab/one/transcript.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.tasks = map[uint64]*taskNode{1: {id: 1, state: session.TaskRunning}}
	a.taskOrder = []uint64{1}
	return a, agent
}

// THE WARNING IS WRITTEN FROM THE ENGINE'S OWN STATEMENT OF ITS LIFETIME, and
// this is the case that catches a surface reading the detach seam instead: every
// remote agent answers Detach, and only the hosted one keeps working afterwards.
func TestTheArmedHintFollowsARealClientsLifetime(t *testing.T) {
	hostedApp, hosted := remoteFront(t, true)
	if !hosted.WorkOutlivesExit() {
		t.Fatal("a session host's agent said its work ends with the window")
	}
	if hint := hostedApp.quitHint(); !strings.Contains(hint, "a task keeps running") {
		t.Errorf("the armed hint is %q on a hosted conversation", hint)
	}

	oneShotApp, oneShot := remoteFront(t, false)
	if oneShot.WorkOutlivesExit() {
		t.Fatal("a one-shot engine's agent claimed to outlive the window")
	}
	hint := oneShotApp.quitHint()
	if !strings.Contains(hint, "a task will stop") {
		t.Errorf("the armed hint is %q on a one-shot engine", hint)
	}
	if strings.Contains(hint, "keeps running") {
		t.Errorf("the armed hint promised survival on a one-shot engine: %q", hint)
	}
}
