package remote

// A JOIN GETS THE CONVERSATION IT NAMED, OR IT GETS A REFUSAL.
//
// [Hello.Join] is a hello about ONE conversation: a surface read a row off a
// disk, believes that work is running next door, and asks for a second view onto
// it. Two locks stand between the question and the answer — the host's, which
// finds the session under that transcript, and the session's, which lets the
// connection in — and the window between them is a real one. The window that
// owns the session can open something else in it right there, and until this the
// arriving connection was quietly bound to the REPLACEMENT: every guard after
// the door then compared that conversation against itself and passed, forever.
//
// The tests below stand in that window on purpose. `dialOpening` hands the test
// the door itself, so the swap happens where the host has already let go and the
// session has not yet been asked — no timing hook in the engine, and no sleep.

import (
	"strings"
	"sync/atomic"
	"testing"
)

// roomOf is the session as the room sees itself: who is attached, how many
// arrivals it has numbered, and whether the keyboard is on a surface that is
// still in it. A refused connection must move none of the three.
func roomOf(sess *Session) (attached int, arrivals uint64, driving bool) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	_, driving = sess.surfaces[sess.driver]
	return len(sess.surfaces), sess.arrivals, driving
}

// THE FAILURE, PINNED. The guest's hello names the conversation it found open;
// the owner opens another one while that hello is in flight; the guest must be
// told so rather than handed the replacement under the name it asked for.
func TestAJoinIsRefusedWhenTheConversationWasReplacedWhileTheHelloWasInFlight(t *testing.T) {
	lab := twoConversationSession()
	owner := viewOn(t, lab.sess)

	guest := dialOpening(t, func(Hello) (*Session, error) {
		// THE WINDOW ITSELF. The host matched the transcript and let go of its
		// own lock; this is where a `Session.Open` on another connection lands.
		if _, err := owner.OpenSession(replacedTranscript); err != nil {
			return nil, err
		}
		return lab.sess, nil
	})

	frame := guest.hello(Hello{
		Version: Version, Surface: "reader",
		Session: joinedTranscript, Join: true, Watch: true,
	})
	if frame.Kind != "fatal" {
		t.Fatalf("a join onto a conversation that had been replaced was answered with %q, want a refusal", frame.Kind)
	}
	// THE SENTENCE IS THE ONE THE SURFACE ALREADY READS. A guest page turns this
	// fragment into its last word about the work (internal/tui3's taskowner.go),
	// so the refusal at the door and the refusal on a call say the same thing.
	if !strings.Contains(frame.Error, "not open here any more") {
		t.Fatalf("the refusal does not say what happened: %q", frame.Error)
	}
	if !strings.Contains(frame.Error, joinedTranscript) {
		t.Fatalf("the refusal does not name the conversation that was asked for: %q", frame.Error)
	}

	attached, arrivals, driving := roomOf(lab.sess)
	if attached != 1 || arrivals != 1 {
		t.Fatalf("a refused join left %d surface(s) attached and %d arrival(s) numbered, want 1 and 1", attached, arrivals)
	}
	if !driving {
		t.Fatal("the keyboard is on nobody in the room after a join was refused")
	}
}

// AND NOTHING IS TAKEN BEFORE THE REFUSAL. [Hello.Join] and [Hello.Watch] are
// two flags for two intentions (wire.go), so a join that did not also say it was
// here to read is a driver on arrival — which means the check has to come first,
// or a connection about to be told "no" takes the keyboard off the window that
// owns the work on its way out.
func TestARefusedJoinTakesNoKeyboardOffTheWindowThatOwnsTheWork(t *testing.T) {
	lab := twoConversationSession()
	owner := viewOn(t, lab.sess)

	guest := dialOpening(t, func(Hello) (*Session, error) {
		if _, err := owner.OpenSession(replacedTranscript); err != nil {
			return nil, err
		}
		return lab.sess, nil
	})
	frame := guest.hello(Hello{
		Version: Version, Surface: "reader",
		Session: joinedTranscript, Join: true,
	})
	if frame.Kind != "fatal" {
		t.Fatalf("a join without a watch was answered with %q, want the same refusal", frame.Kind)
	}

	attached, arrivals, driving := roomOf(lab.sess)
	if attached != 1 || arrivals != 1 {
		t.Fatalf("the refused connection entered the room: %d attached, %d numbered", attached, arrivals)
	}
	if !driving {
		t.Fatal("a refused join took the keyboard off the window that owns the conversation")
	}
}

// A JOIN THAT NAMES NOTHING IS THE SAME REFUSAL. An unknown identity is not a
// match ([Session.agentOf] says so for a call), and the alternative is worse
// than an error: an unnamed join used to become a connection with no binding at
// all, which FOLLOWS the session wherever it is taken next.
func TestAJoinThatNamesNoConversationIsRefused(t *testing.T) {
	sess := heldSession(&fakeAgent{model: "a/b"})
	guest := dialSession(t, sess)

	frame := guest.hello(Hello{Version: Version, Surface: "reader", Join: true, Watch: true})
	if frame.Kind != "fatal" {
		t.Fatalf("a join naming no conversation was answered with %q, want a refusal", frame.Kind)
	}
	if !strings.Contains(frame.Error, "has to name the conversation") {
		t.Fatalf("the refusal does not say what was missing: %q", frame.Error)
	}
	if attached, arrivals, _ := roomOf(sess); attached != 0 || arrivals != 0 {
		t.Fatalf("an unnamed join entered the room: %d attached, %d numbered", attached, arrivals)
	}
}

// AND THE ORDINARY JOIN STILL ARRIVES, which is the whole capability this guard
// must not take away: the conversation named is the conversation open, and the
// welcome says so in the field the surface checks before it draws a row
// (cmd/aforge's chatv3_taskowner.go hands [Welcome.SessionFile] back for exactly
// that).
func TestAJoinOntoTheConversationItNamedStillArrives(t *testing.T) {
	agent := &fakeAgent{model: "a/b", title: "the one being read"}
	engine := engineOn(agent)
	sess := NewSession(engine, true)

	reader := dialSession(t, sess)
	frame := reader.hello(Hello{
		Version: Version, Surface: "reader",
		Session: engine.SessionFile, Join: true, Watch: true,
	})
	if frame.Kind != "welcome" {
		t.Fatalf("a join onto the conversation that is open was answered with %q: %s", frame.Kind, frame.Error)
	}
	welcome := decode[Welcome](t, frame.Payload)
	if welcome.SessionFile != engine.SessionFile {
		t.Fatalf("the welcome names %q, want the conversation the join asked for", welcome.SessionFile)
	}
	if attached, _, _ := roomOf(sess); attached != 1 {
		t.Fatalf("%d surface(s) attached after one join arrived, want 1", attached)
	}
}

// A PIPE ENGINE HAS NOTHING TO JOIN, AND SAYS SO BEFORE IT BOOTS ANYTHING.
//
// `aforge engine` on a pipe opens one conversation for one connection, and that
// conversation is the connection's whole life ([Serve]). Answering a join by
// booting one would do the two things the flag exists to prevent at once: start
// a model to answer a question about work somebody believes is already running,
// and bind the connection to a transcript nobody asked for.
func TestAPipeEngineRefusesAJoinWithoutBootingAConversation(t *testing.T) {
	var booted atomic.Int64
	l := dial(t, func(Hello) (*Engine, error) {
		booted.Add(1)
		return engineOn(&fakeAgent{model: "a/b"}), nil
	})

	frame := l.hello(Hello{
		Version: Version, Surface: "reader",
		Session: "/home/somebody/.aforge/v3/sessions/-home-somebody-api/one.jsonl",
		Join:    true, Watch: true,
	})
	if frame.Kind != "fatal" {
		t.Fatalf("a join to a pipe engine was answered with %q, want a refusal", frame.Kind)
	}
	if got := booted.Load(); got != 0 {
		t.Fatalf("a refused join booted %d conversation(s)", got)
	}
}

// ── which conversation a correction is checked against ──────────────────────

// BOTH NAMES A CONNECTION CARRIES ARE ENFORCED, and neither overrides the other.
//
// The hole this closes is an ordering one: [server.invoke] checks the binding
// ([Session.serving]) and the steer branch then takes the lock AGAIN to pick the
// agent. A swap landing between the two used to leave the decision entirely to
// what the caller claimed — and a caller claiming nothing was answered by the
// replacement's task of the same number.
func TestACorrectionIsCheckedAgainstBothTheBindingAndTheClaim(t *testing.T) {
	const other = "/srv/app/.aforge/two.jsonl"
	for _, c := range []struct {
		name    string
		joined  string
		claimed string
		want    string
		agreed  bool
	}{
		{
			name:    "a bound connection claiming nothing is held to its binding",
			joined:  joinedTranscript,
			claimed: "",
			want:    joinedTranscript,
			agreed:  true,
		},
		{
			name:    "a bound connection claiming its own conversation is served",
			joined:  joinedTranscript,
			claimed: joinedTranscript,
			want:    joinedTranscript,
			agreed:  true,
		},
		{
			name:    "a bound connection claiming another conversation is refused",
			joined:  joinedTranscript,
			claimed: other,
			agreed:  false,
		},
		{
			name:    "an ordinary window is checked against its claim, which is how it follows its session",
			joined:  "",
			claimed: other,
			want:    other,
			agreed:  true,
		},
		{
			name:    "an ordinary window claiming nothing is a caller making no claim",
			joined:  "",
			claimed: "",
			want:    "",
			agreed:  true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			want, agreed := steerConversation(c.joined, c.claimed)
			if agreed != c.agreed {
				t.Fatalf("agreed = %v, want %v", agreed, c.agreed)
			}
			if agreed && want != c.want {
				t.Fatalf("the correction would go to %q, want %q", want, c.want)
			}
		})
	}
}

// AND A BOUND CONNECTION'S UNNAMED CORRECTION IS STILL DELIVERED. A surface
// written before [TaskSteerArgs.Session] existed sends the id and the words and
// nothing else; the binding answers for it, and the correction reaches the
// conversation that connection joined.
func TestABoundConnectionsUnnamedCorrectionIsDeliveredToTheConversationItJoined(t *testing.T) {
	agent := &fakeAgent{model: "a/b"}
	engine := engineOn(agent)
	sess := NewSession(engine, true)

	// A join WITHOUT a watch, because a watcher may not steer at all (driver.go)
	// and the binding is the thing under test.
	window := dialSession(t, sess)
	window.hello(Hello{
		Version: Version, Surface: "macbook",
		Session: engine.SessionFile, Join: true,
	})

	result := window.ok(1, MethodTaskSteer, TaskSteerArgs{ID: 7, Text: "CSV instead of JSON"})
	if steered := decode[TaskSteered](t, result.Payload); steered.Elsewhere {
		t.Fatal("a bound connection's correction to its own conversation was refused as somebody else's")
	}
	if len(agent.steered) != 1 || !strings.HasPrefix(agent.steered[0], "7:") {
		t.Fatalf("the engine was told %v, want the one correction", agent.steered)
	}
}
