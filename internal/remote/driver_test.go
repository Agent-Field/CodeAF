package remote

// driver_test.go is the room's one keyboard, proven on the wire.
//
// Every question here is a question about WHO MAY TYPE, so every one of them is
// asked with at least two connections onto one [Session] — which is the shape a
// host serves and the shape driver.go exists for. The frames are the real
// frames and the sentences are the real sentences: a test that asserted a
// paraphrase of the refusal would pass on the day somebody rewrote it into
// machinery.

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// driverOf is the "driver" frame a link is waiting for. A hand-over is fanned
// out to everybody in the room, so this is what an OLDER window sees the moment
// a newer one arrives.
func driverOf(l *link) Driver {
	l.t.Helper()
	frame := l.await(func(f Frame) bool { return f.Kind == "driver" })
	return decode[Driver](l.t, frame.Payload)
}

// ── who drives ──────────────────────────────────────────────────────────────

// The first window in an empty room has the keyboard, and nobody has to ask.
func TestTheFirstWindowInTheRoomHasTheKeyboard(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	only := dialSession(t, sess)
	welcome := decode[Welcome](t, only.hello(Hello{Version: Version, Surface: "macbook"}).Payload)
	if !welcome.Driver.Yours {
		t.Fatalf("the only window in the room was not given the keyboard: %+v", welcome.Driver)
	}
	if result := only.call(1, MethodSubmit, SubmitArgs{Text: "go"}); result.Error != "" {
		t.Fatalf("the only window could not type: %s", result.Error)
	}
}

// THE NEWEST WINDOW DRIVES. A person who walked to another machine and opened
// the conversation there is not an intruder to be refused — they are simply
// where the person now is.
func TestTheNewestWindowTakesTheKeyboardAndTheOlderOneIsTold(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})

	away := dialSession(t, sess)
	welcome := decode[Welcome](t, away.hello(Hello{Version: Version, Surface: "spark"}).Payload)
	if !welcome.Driver.Yours {
		t.Fatalf("the window that just arrived was not given the keyboard: %+v", welcome.Driver)
	}

	// And the window it arrived in front of is TOLD, on a frame of its own, so
	// that its screen stops offering a composer this same instant.
	told := driverOf(desk)
	if told.Yours {
		t.Fatalf("the older window still believed it was driving")
	}
	if told.Machine != "spark" {
		t.Fatalf("the older window was told the keyboard is on %q, want spark", told.Machine)
	}
	if told.Here {
		t.Fatalf("a window on another machine was reported as one on this one")
	}
}

// Two windows on ONE machine send one name, and that is how the engine can tell
// the desk across the room from the terminal behind this one. `another window`
// is the word aforge already uses at home for exactly this.
func TestASecondWindowOnTheSameMachineReadsAsAnotherWindow(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	first := dialSession(t, sess)
	first.hello(Hello{Version: Version, Surface: "macbook"})
	second := dialSession(t, sess)
	second.hello(Hello{Version: Version, Surface: "macbook"})

	told := driverOf(first)
	if !told.Here {
		t.Fatalf("a second window on the same machine was not reported as one: %+v", told)
	}
	if refused := notDrivingWord(told); !strings.Contains(refused, "in another window") {
		t.Fatalf("the refusal named a machine where it should have said another window: %q", refused)
	}
}

// ── the refusal ─────────────────────────────────────────────────────────────

// A MESSAGE A PERSON PRESSED ENTER ON EITHER LANDS OR IS ANSWERED. The third
// outcome — it disappears because a window somewhere else had the keyboard — is
// the one nobody could debug from the screen.
func TestAWindowWithoutTheKeyboardIsRefusedInWordsAndNotInSilence(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})
	away := dialSession(t, sess)
	away.hello(Hello{Version: Version, Surface: "spark"})

	for _, door := range []struct {
		method  string
		payload any
	}{
		{MethodSubmit, SubmitArgs{Text: "go"}},
		{MethodFollowUp, SubmitArgs{Text: "and also"}},
		{MethodSteer, SubmitArgs{Text: "use the other file"}},
		{MethodSubmitImage, SubmitImageArgs{Text: "look"}},
		{MethodSubmitFiles, SubmitFilesArgs{Text: "here"}},
	} {
		result := desk.call(1, door.method, door.payload)
		want := "the keyboard is on spark right now — press enter here to take it back"
		if result.Error != want {
			t.Fatalf("%s from a watcher answered %q, want %q", door.method, result.Error, want)
		}
	}

	// And nothing else is closed to it. A watcher is a person watching their own
	// work, not a guest: it reads the transcript, answers cards and interrupts.
	away.ok(9, MethodSubmit, SubmitArgs{Text: "go"})
	desk.ok(2, MethodTranscript, nil)
	desk.ok(3, MethodInterrupt, nil)
}

// ── taking it back ──────────────────────────────────────────────────────────

// One round trip and never a reconnect: the connection under a take-back never
// moved, and the window that had it is told in the same breath.
func TestTakingTheKeyboardBackMovesItAndTellsTheOtherWindow(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})
	away := dialSession(t, sess)
	away.hello(Hello{Version: Version, Surface: "spark"})
	if told := driverOf(desk); told.Yours {
		t.Fatalf("the desk was not made a watcher to begin with")
	}

	desk.ok(1, MethodTake, nil)

	// The taker can type again...
	desk.ok(2, MethodSubmit, SubmitArgs{Text: "go"})
	// ...and the window that had it is now the watcher, told by a frame rather
	// than by discovering it on its next keystroke.
	told := driverOf(away)
	if told.Yours {
		t.Fatalf("both windows believe they hold the keyboard")
	}
	if told.Machine != "macbook" {
		t.Fatalf("the new watcher was told the keyboard is on %q, want macbook", told.Machine)
	}
	if result := away.call(3, MethodSubmit, SubmitArgs{Text: "no"}); result.Error == "" {
		t.Fatalf("the new watcher was allowed to type")
	}
}

// M11: the newly taken keyboard can steer, every watcher receives the steer
// lifecycle, and no watcher is told that those words opened a fresh turn.
func TestTheDriverCanSteerWithoutBroadcastingAFreshTurn(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})
	away := dialSession(t, sess)
	away.hello(Hello{Version: Version, Surface: "spark"})
	driverOf(desk)

	desk.ok(1, MethodTake, nil)
	driverOf(away)
	ref := decode[StreamRef](t, desk.ok(2, MethodSteer, SubmitArgs{Text: "use staging"}).Payload)
	if len(agent.steered) != 1 || agent.steered[0] != "use staging" {
		t.Fatalf("the newly taken keyboard steered %q", agent.steered)
	}
	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventSteerAccepted, Steer: &session.SteerNote{ID: 7, Words: "use staging"}}
	stream <- session.Event{Kind: session.EventSteerConsumed, Steer: &session.SteerNote{ID: 7, Words: "use staging"}}
	close(stream)

	var kinds []session.EventKind
	for len(kinds) < 2 {
		frame := away.await(func(frame Frame) bool { return frame.Kind == "event" && frame.ID == ref.Stream })
		kinds = append(kinds, decode[EventWire](t, frame.Payload).Unwire().Kind)
	}
	if kinds[0] != session.EventSteerAccepted || kinds[1] != session.EventSteerConsumed {
		t.Fatalf("the watcher received steer events %v", kinds)
	}
	frames := append([]Frame(nil), away.spare...)
	frames = append(frames, drain(away)...)
	for _, frame := range frames {
		if frame.Kind == "turn" {
			t.Fatal("the steer was broadcast to the watcher as a fresh turn")
		}
	}
}

// ── leaving ─────────────────────────────────────────────────────────────────

// The keyboard is never left on a window that has gone, so the last window
// standing can always type. A FORGOTTEN WINDOW STILL SHOWING THE WORK IS A
// FEATURE — this is what happens when somebody finally closes it.
func TestClosingTheDrivingWindowHandsTheKeyboardOnToTheNewestLeft(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})
	away := dialSession(t, sess)
	away.hello(Hello{Version: Version, Surface: "spark"})
	driverOf(desk)

	if err := away.end(); err != nil {
		t.Fatalf("the driving window ended with %v", err)
	}
	if told := driverOf(desk); !told.Yours {
		t.Fatalf("the window left behind was not handed the keyboard: %+v", told)
	}
	desk.ok(1, MethodSubmit, SubmitArgs{Text: "go"})
}

// A REDIAL IS AN ATTACH THE PERSON DID NOT MAKE. The lid they closed in one city
// reconnecting half an hour later must not pull the keyboard off the machine
// they are sitting at — which is what [Hello.Back] is for.
func TestALinkComingBackDoesNotStealTheKeyboardFromWhereThePersonWent(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})
	away := dialSession(t, sess)
	away.hello(Hello{Version: Version, Surface: "spark"})
	driverOf(desk)

	// The desk's link dies and comes back, saying it has been here before.
	if err := desk.end(); err != nil {
		t.Fatalf("the desk's link ended with %v", err)
	}
	back := dialSession(t, sess)
	welcome := decode[Welcome](t, back.hello(Hello{Version: Version, Surface: "macbook", Back: true}).Payload)
	if welcome.Driver.Yours {
		t.Fatalf("a link coming back took the keyboard off the machine the person walked to")
	}
	if welcome.Driver.Machine != "spark" {
		t.Fatalf("the returning window was told the keyboard is on %q, want spark", welcome.Driver.Machine)
	}

	// And a returning link that finds the keyboard going spare does take it,
	// because there is nobody to take it from.
	if err := away.end(); err != nil {
		t.Fatalf("the far window ended with %v", err)
	}
	if told := driverOf(back); !told.Yours {
		t.Fatalf("the last window standing was left unable to type: %+v", told)
	}
}

// A WATCHER NEVER TAKES THE KEYBOARD, AND `Back` WAS NOT ENOUGH.
//
// A surface opened to READ one task of a conversation running next door
// ([Hello.Watch]) must not end up driving it. `Back` gets the first half right
// and the second half exactly wrong: a returning surface takes the keyboard the
// moment it is going spare, and going spare is the ordinary state of a
// conversation whose window has stepped away for a second. So the reader is
// refused it on arrival, refused it when the driver leaves, refused it through
// [MethodTake], and refused when it types anyway — in words about itself, because
// nothing about the conversation is in the way.
func TestAWatchingSurfaceIsNeverGivenTheKeyboardEvenWhenItIsGoingSpare(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})

	// The reader arrives NEWEST, which is what would ordinarily take the keyboard.
	reader := dialSession(t, sess)
	welcome := decode[Welcome](t, reader.hello(Hello{Version: Version, Surface: "macbook", Watch: true}).Payload)
	if welcome.Driver.Yours {
		t.Fatal("a reading surface was handed the keyboard on arrival")
	}
	if told := driverOf(desk); !told.Yours {
		t.Fatalf("a reading surface took the keyboard off the window that owns the work: %+v", told)
	}

	// The window that owns the work goes away. There is nobody left who may type,
	// and the room is left with NO driver rather than with the reader.
	if err := desk.end(); err != nil {
		t.Fatalf("the desk's link ended with %v", err)
	}
	if told := driverOf(reader); told.Yours {
		t.Fatalf("a reading surface inherited a keyboard nobody was holding: %+v", told)
	}
	// ...AND ASKING FOR IT OUTRIGHT IS REFUSED IN WORDS RATHER THAN ANSWERED WITH
	// A DRIVER. [MethodTake] is not on the reader's allow-list, so the refusal
	// lands one layer ABOVE the hand-over rule — the call never reaches
	// [Session.take] at all — and the result carries a sentence instead of a
	// [Driver]. That is the stronger of the two guarantees and it is the one the
	// wire actually gives, so it is what is demanded here: a test that decoded a
	// Driver out of this frame would be reading an empty payload and calling the
	// zero value a pass.
	took := reader.call(1, MethodTake, nil)
	if took.Error == "" {
		t.Fatalf("a reading surface was allowed to ask for the keyboard: %+v", took)
	}
	if !strings.Contains(took.Error, watchingWord) {
		t.Fatalf("the refusal is %q, want the reader's own words %q", took.Error, watchingWord)
	}
	if !strings.Contains(took.Error, MethodTake) {
		t.Fatalf("the refusal does not name the door that was tried: %q", took.Error)
	}
	if len(took.Payload) != 0 {
		t.Fatalf("a refused take still answered with a driver: %s", took.Payload)
	}
}

// AN OLD ENGINE IS REFUSED AT THE DOOR RATHER THAN TRUSTED WITH THE FLAGS.
//
// [Hello.Join] and [Hello.Watch] are omitempty booleans, which is precisely what
// an older build DISCARDS in silence — and a build that discarded them would boot
// a conversation to answer a question about running work, and hand this reader
// the keyboard, before any welcome existed to check. The version gate is what
// stops it, it runs before `Open` and before `attach`, and this is the test that
// the gate is in front of both.
func TestAJoinOrWatchHelloFromAnotherProtocolIsRefusedBeforeAnythingOpens(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})

	var opened atomic.Int64
	old := dialOpening(t, func(Hello) (*Session, error) { opened.Add(1); return sess, nil })

	frame := old.hello(Hello{Version: Version - 1, Surface: "reader", Join: true, Watch: true})
	if frame.Kind != "fatal" {
		t.Fatalf("a hello from another protocol was answered with %q, want a refusal", frame.Kind)
	}
	if !strings.Contains(frame.Error, "protocol") {
		t.Fatalf("the refusal does not say what was wrong with the hello: %q", frame.Error)
	}
	if got := opened.Load(); got != 0 {
		t.Fatalf("a refused hello reached the conversation door %d times", got)
	}
	// AND NOTHING ABOUT THE OWNER MOVED, WHICH IS PROVEN BY A SILENCE AND A CALL.
	//
	// THE SILENCE IS THE POINT AND IT CANNOT BE AWAITED. A hand-over is what
	// produces a `driver` frame, and the whole claim here is that no hand-over
	// happened — so waiting for one is waiting for the thing whose absence is the
	// pass, and it can only ever time out. What is asked instead is the fact that
	// frame would have carried: the desk can still type, which is what holding the
	// keyboard MEANS on this wire, and it is answered rather than awaited.
	desk.ok(1, MethodSubmit, SubmitArgs{Text: "go"})
	// AND THE REFUSED CONNECTION IS OVER, on its own side, having opened nothing.
	if err := old.end(); err == nil {
		t.Fatal("a refused hello left the engine serving that connection")
	}
}

// A WATCHER CHANGES NOTHING, EVEN THROUGH A DOOR THAT NEVER TYPES.
//
// The driver rule covers the methods that put WORDS into a conversation, which is
// not the same set as the methods that change one: a model, an effort rung, a
// permission, a resolve, a revive, opening another session in this one, closing
// it. Every one of those was open to a reader before [Hello.Watch], and every one
// of them acts on somebody else's work. The guard is an ALLOW-LIST, so this test
// asks for things a reader has no business asking for and expects a refusal in
// its own words rather than a change.
func TestAWatchingSurfaceIsRefusedEveryDoorThatChangesAnything(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})
	reader := dialSession(t, sess)
	reader.hello(Hello{Version: Version, Surface: "reader", Watch: true})

	was := agent.model
	for id, call := range []struct {
		method  string
		payload any
	}{
		{MethodSetModel, "someone/else"},
		{MethodSubmit, "go"},
		{MethodInterrupt, nil},
		{MethodSessionNew, nil},
		{MethodTaskStop, TaskStopArgs{ID: "7"}},
	} {
		frame := reader.call(uint64(id+1), call.method, call.payload)
		if frame.Error == "" {
			t.Fatalf("%s was allowed on a reading surface", call.method)
		}
		if !strings.Contains(frame.Error, watchingWord) {
			t.Fatalf("%s was refused with %q, want the reader's own sentence", call.method, frame.Error)
		}
	}
	if agent.model != was {
		t.Fatalf("a reading surface moved the model to %q", agent.model)
	}
	// AND THE ONE DOOR IT EXISTS FOR IS OPEN. A guard that refused this too would
	// be a connection with nothing to do.
	if frame := reader.call(90, MethodTaskRoom, TaskRoomArgs{ID: 7, Tail: 4}); frame.Error != "" &&
		strings.Contains(frame.Error, watchingWord) {
		t.Fatalf("the reader was refused the one thing it is for: %v", frame.Error)
	}
	// The window that owns the work still drives and is untouched.
	if told := driverOf(desk); !told.Yours {
		t.Fatalf("the reader took the keyboard: %+v", told)
	}
}

// ── the client half ─────────────────────────────────────────────────────────

// The real client against the real engine over an in-memory pipe: what a
// surface actually holds, and what it is woken by.
func TestTheSurfaceLearnsItHasBecomeAWatcherWithNobodyTouchingIt(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	first, err := loopSession(sess, Hello{Surface: "macbook"})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer first.Close()
	if !first.Client.Driver().Yours {
		t.Fatalf("the first surface did not hold the keyboard")
	}

	// The wake is taken BEFORE the second surface arrives, which is the whole
	// point of it: nothing happens on this machine, and this surface still has
	// to stop drawing a composer.
	woken := first.Client.DriverChanged()

	second, err := loopSession(sess, Hello{Surface: "spark"})
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer second.Close()

	select {
	case <-woken:
	case <-time.After(5 * time.Second):
		t.Fatalf("the surface was never woken when the keyboard moved")
	}
	driver := first.Client.Driver()
	if driver.Yours || driver.Machine != "spark" {
		t.Fatalf("the watching surface holds %+v", driver)
	}

	// Typing at it is refused in the engine's own words, and the words name the
	// way back.
	if _, err := first.Client.Agent().Submit(t.Context(), "go"); err == nil {
		t.Fatalf("the watching surface was allowed to type")
	} else if !strings.Contains(err.Error(), "press enter here to take it back") {
		t.Fatalf("the refusal did not say how to get back: %v", err)
	}

	// And taking it back is one call on the connection that was already open.
	if err := first.Client.Take(); err != nil {
		t.Fatalf("take: %v", err)
	}
	if !first.Client.Driver().Yours {
		t.Fatalf("the surface that took the keyboard does not believe it has it")
	}
	if _, err := first.Client.Agent().Submit(t.Context(), "go"); err != nil {
		t.Fatalf("the surface that took the keyboard could not type: %v", err)
	}
	agent.finish(agent.stream(0))
}

// loopSession is [Loopback] onto a conversation that already exists — the same
// difference [dialSession] is to [dial], and for the same reason.
func loopSession(sess *Session, hello Hello) (*Loop, error) {
	surface, engine := Pipe()
	served := make(chan error, 1)
	go func() {
		served <- ServeAttach(engine, engine, AttachOptions{
			Open: func(Hello) (*Session, error) { return sess, nil },
		})
		_ = engine.Close()
	}()
	client, err := Dial(surface, "loopback", hello)
	if err != nil {
		_ = surface.Close()
		return nil, err
	}
	return &Loop{Client: client, Served: served, surface: surface}, nil
}

// ── the name on the wire ────────────────────────────────────────────────────

// A BOUNDARY THAT TRUSTS ITS INPUT IS NOT A BOUNDARY. This string arrives from
// another machine and ends up in a line on somebody's screen, so an escape
// sequence in it would be an escape sequence in their terminal.
func TestAMachineNameFromTheWireIsMadeSafeToDraw(t *testing.T) {
	for _, probe := range []struct{ said, want string }{
		{"spark", "spark"},
		{"  spark  ", "spark"},
		{"spa\x1b[2Jrk", "spa[2Jrk"},
		{"spark\nmacbook", "sparkmacbook"},
		{strings.Repeat("x", 200), strings.Repeat("x", machineNameMost)},
		{"", ""},
	} {
		if got := machineLabel(probe.said); got != probe.want {
			t.Errorf("machineLabel(%q) = %q, want %q", probe.said, got, probe.want)
		}
	}
}

// A machine with no name of its own says nothing about one, and the sentence
// falls back to the word that is true either way.
func TestAWindowWithNoNameIsStillAnotherWindow(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	first := dialSession(t, sess)
	first.hello(Hello{Version: Version})
	second := dialSession(t, sess)
	second.hello(Hello{Version: Version})

	told := driverOf(first)
	if !told.Here || told.Machine != "" {
		t.Fatalf("a nameless window was drawn as something: %+v", told)
	}
	want := "the keyboard is in another window right now — press enter here to take it back"
	if got := notDrivingWord(told); got != want {
		t.Fatalf("the refusal reads %q, want %q", got, want)
	}
}

// A turn's events still reach a watcher, which is the whole reason the older
// windows stay attached rather than being closed: a forgotten window still
// showing the work is a feature.
func TestAWatcherKeepsReceivingTheTurnItCannotStart(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})
	away := dialSession(t, sess)
	away.hello(Hello{Version: Version, Surface: "spark"})
	driverOf(desk)

	ref := decode[StreamRef](t, away.ok(1, MethodSubmit, SubmitArgs{Text: "go"}).Payload)

	// THE WATCHER IS TOLD THE TURN STARTED, BEFORE ITS FIRST EVENT. A surface
	// only draws a stream it knows about, so without this the events below go
	// past a watching window in silence — the whole promise of staying attached,
	// unkept ([Turn]).
	told := decode[Turn](t, desk.await(func(f Frame) bool { return f.Kind == "turn" }).Payload)
	if told.Stream != ref.Stream {
		t.Fatalf("the watcher was told about stream %d, want %d", told.Stream, ref.Stream)
	}
	if told.Said != "go" {
		t.Fatalf("the watcher was told the turn opened on %q — a reply with no question above it", told.Said)
	}

	stream := agent.stream(0)
	stream <- session.Event{Kind: session.EventTextDelta, Text: "hello"}

	frame := desk.await(func(f Frame) bool { return f.Kind == "event" && f.ID == ref.Stream })
	if text := decode[EventWire](t, frame.Payload).Unwire().Text; text != "hello" {
		t.Fatalf("the watcher was given %q", text)
	}

	// And the window that STARTED it is not told about it: it drew that turn the
	// moment its own submit answered, and a second adoption would draw the reply
	// twice.
	for _, frame := range drain(away) {
		if frame.Kind == "turn" {
			t.Fatalf("the window that started the turn was told about its own turn")
		}
	}
}

// drain is every frame waiting on a link right now, without waiting for more.
func drain(l *link) []Frame {
	var seen []Frame
	for {
		select {
		case frame, ok := <-l.frames:
			if !ok {
				return seen
			}
			seen = append(seen, frame)
		default:
			return seen
		}
	}
}

// The surface half of the same fact: a watcher is handed the turn on the same
// kind of channel its own submit would have answered with.
func TestAWatchingSurfaceIsHandedTheTurnItDidNotStart(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	watcher, err := loopSession(sess, Hello{Surface: "macbook"})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer watcher.Close()
	driver, err := loopSession(sess, Hello{Surface: "spark"})
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer driver.Close()

	if _, err := driver.Client.Agent().Submit(t.Context(), "say something"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	select {
	case turn := <-watcher.Client.Follow():
		if turn.Said != "say something" {
			t.Fatalf("the watcher was handed a turn opened on %q", turn.Said)
		}
		agent.stream(0) <- session.Event{Kind: session.EventTextDelta, Text: "something"}
		if ev := <-turn.Events; ev.Text != "something" {
			t.Fatalf("the turn handed over carried %q", ev.Text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watching surface was never handed the turn")
	}
	agent.finish(agent.stream(0))
}
