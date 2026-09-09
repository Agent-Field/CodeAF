package remote

// THE NAME OF A SEND HAS TO CROSS THE WIRE, OR THE ONE FAILURE THAT MATTERS
// OVER A WIRE HAS NO ANSWER.
//
// The words cross, the engine takes them, and the answer is lost. Without a
// name on the send the surface can only send it again — a second correction on
// the worker's queue — or drop it. These are the four facts that make the third
// answer possible: the name travels, the repeat comes back recognised, an
// engine that does not keep names says so AT THE DOOR rather than after the
// damage, and a call nobody answered is told apart from a refusal.

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// namedAgent is an engine that keeps the identity on a send, and answers a
// repeat of one with the receipt already on its record.
type namedAgent struct {
	*fakeAgent
	seen map[string]uint64
	next uint64
	// sources records what actually crossed, in order.
	sources []session.SteerSource
}

func (a *namedAgent) SteerRepeatKnown() bool { return true }

func (a *namedAgent) SteerTaskFrom(id uint64, line string, from session.SteerSource) (session.SteerReceipt, error) {
	a.sources = append(a.sources, from)
	key := fmt.Sprintf("%s/%d", from.Scope, from.Seq)
	if a.seen == nil {
		a.seen = map[string]uint64{}
	}
	if direction, ok := a.seen[key]; ok {
		return session.SteerReceipt{Again: true, Direction: direction,
			Landing: "already on the task's record from the same message — nothing was sent a second time"}, nil
	}
	a.next++
	a.seen[key] = a.next
	a.steered = append(a.steered, fmt.Sprintf("%d:%s", id, line))
	return session.SteerReceipt{Direction: a.next, Landing: session.SteerDelivered(false)}, nil
}

// THE NAME TRAVELS AND THE REPEAT IS RECOGNISED. The second ask delivers
// nothing and answers the receipt the first one earned.
func TestANamedSendCrossesTheWireAndItsRepeatIsRecognised(t *testing.T) {
	far := &namedAgent{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	agent := loop.Client.Agent()

	if !agent.SteerRepeatKnown() {
		t.Fatal("an engine that keeps send names did not say so at the door, so no surface will ever ask it twice")
	}
	said := time.Now().Add(-time.Minute).Round(time.Millisecond).UTC()
	from := session.SteerSource{Scope: "window-1", Seq: 3, At: said}

	first, err := agent.SteerTaskFrom(9, "CSV instead of JSON", from)
	if err != nil {
		t.Fatalf("SteerTaskFrom: %v", err)
	}
	if first.Again || first.Direction == 0 {
		t.Fatalf("first send = %+v, want a delivery with a receipt", first)
	}
	again, err := agent.SteerTaskFrom(9, "CSV instead of JSON", from)
	if err != nil {
		t.Fatalf("asking again: %v", err)
	}
	if !again.Again {
		t.Fatalf("the repeat came back as %+v — the surface would draw a second delivery that never happened", again)
	}
	if again.Direction != first.Direction {
		t.Fatalf("the repeat answered direction %d, want %d", again.Direction, first.Direction)
	}
	if len(far.sources) != 2 {
		t.Fatalf("the engine saw %d sends, want two asks", len(far.sources))
	}
	for i, source := range far.sources {
		if source.Scope != "window-1" || source.Seq != 3 {
			t.Fatalf("ask %d crossed as %+v, want the surface's own name for the send", i, source)
		}
		if !source.At.Equal(said) {
			t.Fatalf("ask %d crossed at %s, want the instant the person pressed enter (%s)", i, source.At, said)
		}
	}
	if !reflect.DeepEqual(far.steered, []string{"9:CSV instead of JSON"}) {
		t.Fatalf("the words were delivered %v — a repeat must deliver nothing", far.steered)
	}
}

// AND AN ENGINE THAT DOES NOT KEEP NAMES SAYS SO BEFORE ANYTHING IS SENT, which
// is the only moment the answer is worth anything: after the link dies there is
// nobody left to ask. It still steers, exactly as it always did.
func TestAnEngineWithNoNamesSaysSoAtTheDoorAndStillSteers(t *testing.T) {
	far := &fakeAgent{}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	agent := loop.Client.Agent()

	if agent.SteerRepeatKnown() {
		t.Fatal("an engine with no identity door claimed it recognises a repeat; a surface would then ask it twice and correct the worker twice")
	}
	receipt, err := agent.SteerTaskFrom(17, "check the lock",
		session.SteerSource{Scope: "window-1", Seq: 1, At: time.Now()})
	if err != nil {
		t.Fatalf("a named send to an engine with no names: %v", err)
	}
	if receipt.Again {
		t.Fatalf("receipt = %+v, want the ordinary delivery an older engine gives", receipt)
	}
	if !reflect.DeepEqual(far.steered, []string{"17:check the lock"}) {
		t.Fatalf("engine calls = %v, want the words delivered through the door it has", far.steered)
	}
}

// THE HANDLE OUTLIVES THE CONVERSATION, AND THE SEND MUST NOT.
//
// This is the bug the conversation name exists for, staged over a real wire with
// no pointer swapped anywhere: ONE client, ONE agent handle held across
// `Session.Open` and `Session.New`, exactly as cmd/aforge's chatv3_host.go holds
// it (its Resume calls OpenSession and hands back the same agent). A correction
// typed at task 7 of the first conversation must never be delivered to task 7 of
// whatever replaced it.
func TestASendKeptAcrossASessionSwapIsRefusedRatherThanReAimed(t *testing.T) {
	first := &namedAgent{fakeAgent: &fakeAgent{}}
	second := &namedAgent{fakeAgent: &fakeAgent{}}
	third := &namedAgent{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{
			Agent: first, Workspace: "/srv/app", SessionFile: "/srv/one.jsonl",
			Open: func(string) (WrappedAgent, bool, error) { return second, true, nil },
			Fresh: func() (WrappedAgent, string, error) {
				return third, "/srv/three.jsonl", nil
			},
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	// THE HANDLE IS TAKEN ONCE and used for every send below. Re-reading it would
	// be the test doing the very thing the surface cannot do.
	agent := loop.Client.Agent()

	from := session.SteerSource{Scope: "window-1", Seq: 4, At: time.Now(), Conversation: "/srv/one.jsonl"}
	if _, err := agent.SteerTaskFrom(7, "use the staging bucket", from); err != nil {
		t.Fatalf("the first send: %v", err)
	}
	if !reflect.DeepEqual(first.steered, []string{"7:use the staging bucket"}) {
		t.Fatalf("the first conversation saw %v, want the correction it was addressed to", first.steered)
	}

	// ── the person opens another conversation behind the same handle ──
	if _, err := loop.Client.OpenSession("/srv/two.jsonl"); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	_, err = agent.SteerTaskFrom(7, "use the staging bucket", from)
	if !errors.Is(err, session.ErrNotThatConversation) {
		t.Fatalf("a send held across /resume came back as %v, want a refusal naming the conversation", err)
	}
	if len(second.steered) != 0 || len(second.sources) != 0 {
		t.Fatalf("task 7 of the conversation that REPLACED it was corrected: %v", second.steered)
	}

	// The same handle still steers the conversation that IS open, which is what
	// makes the refusal above a check and not a wall.
	here := session.SteerSource{Scope: "window-1", Seq: 5, At: time.Now(), Conversation: "/srv/two.jsonl"}
	if _, err := agent.SteerTaskFrom(7, "leave the index alone", here); err != nil {
		t.Fatalf("a send for the open conversation: %v", err)
	}
	if !reflect.DeepEqual(second.steered, []string{"7:leave the index alone"}) {
		t.Fatalf("the open conversation saw %v", second.steered)
	}

	// ── and /new is the same fact by another door ──
	if _, err := loop.Client.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, err := agent.SteerTaskFrom(7, "leave the index alone", here); !errors.Is(err, session.ErrNotThatConversation) {
		t.Fatalf("a send held across /new came back as %v, want a refusal", err)
	}
	if len(third.steered) != 0 {
		t.Fatalf("the fresh conversation's task 7 was corrected: %v", third.steered)
	}
}

// AND A SEND THAT NAMES NOTHING IS CHECKED AGAINST NOTHING, because every client
// written before this one names nothing and must keep steering.
func TestASendThatNamesNoConversationStillCrosses(t *testing.T) {
	far := &namedAgent{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/one.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	if _, err := loop.Client.Agent().SteerTaskFrom(7, "read the errata",
		session.SteerSource{Scope: "window-1", Seq: 1, At: time.Now()}); err != nil {
		t.Fatalf("a send with no conversation on it: %v", err)
	}
	if !reflect.DeepEqual(far.steered, []string{"7:read the errata"}) {
		t.Fatalf("engine calls = %v", far.steered)
	}
}

// AN ENGINE THAT DOES NOT ENFORCE THE BINDING IS NEVER SENT A BOUND SEND, and
// this is the failure that makes it necessary: such an engine reads Session as
// a field it has never heard of and delivers to whatever conversation it has
// open. So NOTHING IS PUT ON THE WIRE. An unknown owner is not permission.
func TestAnOldPeerIsNeverSentABoundCorrection(t *testing.T) {
	client, e := newEngine(t)
	if client.Welcome().SteerOwner {
		t.Fatal("the fixture engine claims to enforce the binding, so it cannot stand in for an older one")
	}

	_, err := client.Agent().SteerTaskFrom(7, "use the staging bucket",
		session.SteerSource{Scope: "window-1", Seq: 1, At: time.Now(), Conversation: "/srv/one.jsonl"})
	if !errors.Is(err, session.ErrNotThatConversation) {
		t.Fatalf("a bound send to an engine that cannot check it came back as %v", err)
	}
	if !errors.Is(err, session.ErrConversationUnchecked) {
		t.Fatalf("the refusal does not say the check could not be made: %v", err)
	}
	if calls := e.calls(MethodTaskSteer); len(calls) != 0 {
		t.Fatalf("%d correction(s) crossed to an engine that would not have checked them", len(calls))
	}
	// AND IT IS NOT AN UNANSWERED CALL. Nothing was sent, so there is nothing to
	// ask again about.
	if errors.Is(err, session.ErrSendUnanswered) {
		t.Fatalf("a send that never left was reported as one nobody answered: %v", err)
	}

	// The same engine still takes an unbound send, which is the compatibility
	// this refusal is narrow enough to keep.
	e.answers[MethodTaskSteer] = TaskSteered{Waiting: true}
	if _, err := client.Agent().SteerTaskFrom(7, "use the staging bucket",
		session.SteerSource{Scope: "window-1", Seq: 1, At: time.Now()}); err != nil {
		t.Fatalf("an unbound send to the same engine: %v", err)
	}
	if calls := e.calls(MethodTaskSteer); len(calls) != 1 {
		t.Fatalf("the unbound send made %d calls, want one", len(calls))
	}
}

// AN ENGINE'S OWN UNKNOWN OUTCOME CROSSES AS AN UNKNOWN OUTCOME. It is not only
// dead links that leave a correction undecided: the engine can take one onto a
// node's record and then be unable to say whether it stayed there. Carried as an
// error string it would arrive as an ordinary refusal, the surface would hand the
// words back, and the next enter would send them under a new name.
func TestAnUncertainOutcomeCrossesAsAnUnansweredSend(t *testing.T) {
	client, e := newEngine(t)
	e.answers[MethodTaskSteer] = TaskSteered{Uncertain: true}

	_, err := client.Agent().SteerTaskFrom(7, "make it CSV",
		session.SteerSource{Scope: "window-1", Seq: 1, At: time.Now()})
	if err == nil {
		t.Fatal("an undecided correction came back as a receipt")
	}
	if !errors.Is(err, session.ErrSendUnanswered) {
		t.Fatalf("an undecided correction reads as a decided failure: %v", err)
	}
	if errors.Is(err, session.ErrNotThatConversation) {
		t.Fatalf("an undecided correction reads as a wrong-conversation refusal: %v", err)
	}
}

// A CALL NOBODY ANSWERED IS TOLD APART FROM A REFUSAL. It is the whole basis of
// asking again: a refusal is a decision and repeating it sends the same words at
// the same closed door, and a silence may be a delivery whose answer was lost.
func TestASendNobodyAnsweredIsMarkedUnanswered(t *testing.T) {
	client, e := newEngine(t)
	e.silent[MethodTaskSteer] = true

	waited := make(chan error, 1)
	go func() {
		_, err := client.Agent().SteerTask(7, "the config lives under etc/")
		waited <- err
	}()
	// Give the call time to be written and registered, then cut the pipe under
	// it: the link dying with a send outstanding is exactly the case.
	time.Sleep(20 * time.Millisecond)
	_ = e.conn.Close()

	select {
	case err := <-waited:
		if err == nil {
			t.Fatal("a send outlived the connection")
		}
		if !errors.Is(err, session.ErrSendUnanswered) {
			t.Fatalf("a send whose link died reads as a decided failure: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a send on a dead pipe hung instead of failing")
	}
}

// AND THE ENGINE'S OWN REFUSAL IS NOT ONE. It read the call and said no, so
// nothing crossed and nothing may be asked again.
func TestAnEnginesRefusalIsNotAnUnansweredSend(t *testing.T) {
	client, e := newEngine(t)
	e.fails[MethodTaskSteer] = "task 7 is done, not running"

	_, err := client.Agent().SteerTask(7, "the config lives under etc/")
	if err == nil {
		t.Fatal("a refused send came back as a receipt")
	}
	if errors.Is(err, session.ErrSendUnanswered) {
		t.Fatalf("the engine's own refusal was read as a silence: %v", err)
	}
	if err.Error() != "task 7 is done, not running" {
		t.Fatalf("the engine's sentence was rewritten: %v", err)
	}
}
