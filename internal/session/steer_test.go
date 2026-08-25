package session

// The laws of the splice (steer.go), one test each:
//
//	(a) a steer lands at the next model-call boundary, as user content of the
//	    SAME turn — the model reads the question, the work so far, then it
//	(b) several steers land in the order they were sent
//	(c) a steer that missed every boundary of a turn that ANSWERED falls through
//	    onto the follow-up queue and asks its own question, on the same stream
//	(d) a steer that fell through onto an INTERRUPTED turn is dropped with that
//	    queue, said out loud first and written down — never silently, and never
//	    into the transcript of the turn it missed
//	(e) the same on a turn that FAULTED
//	(f) a steer sent when nothing is running is refused
//	(g) the record says which of the two happened, and a surface reads it back
//	(h) a transcript with no steers in it loads exactly as it always did
//	(i) a node's room steer is untouched by any of it

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// heldTurn is a turn parked inside its first provider call, which is the only
// moment a steer can be sent into: entered says the step has begun, release
// lets it finish.
type heldTurn struct {
	entered chan struct{}
	release chan struct{}
}

func newHeldTurn() *heldTurn {
	return &heldTurn{entered: make(chan struct{}), release: make(chan struct{})}
}

func (h *heldTurn) wait(t *testing.T) {
	t.Helper()
	select {
	case <-h.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first step never started")
	}
}

// mustSteer sends one steer into a running turn and fails rather than returning
// an error, because every test below is about what happens AFTER it is taken.
func mustSteer(t *testing.T, agent *Agent, words string) <-chan Event {
	t.Helper()
	stream, err := agent.Steer(words)
	if err != nil {
		t.Fatalf("Steer(%q): %v", words, err)
	}
	return stream
}

// steerEvents is the three steer kinds off one stream, in the order they
// arrived, spelled as their words so a failure says what happened rather than
// which integers it saw.
func steerEvents(collected []Event) []string {
	var out []string
	for _, event := range collected {
		switch event.Kind {
		case EventSteerAccepted:
			out = append(out, "accepted:"+event.Steer.Words)
		case EventSteerConsumed:
			out = append(out, "consumed:"+event.Steer.Words)
		case EventSteerFellThrough:
			out = append(out, "fell:"+event.Steer.Words)
		}
	}
	return out
}

// userLines is every user-role message in one request, in order — the shape
// every assertion about "what the model was actually sent" is made against.
func userLines(messages []ai.Message) []string {
	var out []string
	for _, message := range messages {
		if message.Role == "user" {
			out = append(out, messageText(message))
		}
	}
	return out
}

// journalEntries reads a session file back as its lines.
func journalEntries(t *testing.T, path string) []sessionEntry {
	t.Helper()
	var entries []sessionEntry
	for _, line := range readLines(t, path) {
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("journal line %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

// ── (a) the splice lands at the boundary, as the same turn's user content ───

func TestASteerLandsAtTheNextBoundaryAsSameTurnUserContent(t *testing.T) {
	held := newHeldTurn()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(held.entered)
			<-held.release
			return toolResponse("call-1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("counted them too"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	turn := mustSubmit(t, agent, "list the workspace")
	held.wait(t)
	steered := mustSteer(t, agent, "count the files while you are there")
	close(held.release)

	collect(t, turn)
	fromSteer := collect(t, steered)

	if completer.requests() != 2 {
		t.Fatalf("requests = %d, want 2 — the steer must ride the SAME turn", completer.requests())
	}
	second := completer.request(1)
	// The question, the work so far, then the correction: one turn, in order.
	if got, want := rolesOf(second), []string{"system", "user", "assistant", "tool", "user"}; !equalStrings(got, want) {
		t.Fatalf("second request roles = %v, want %v — the steer lands at the step boundary", got, want)
	}
	if got, want := userLines(second), []string{"list the workspace", "count the files while you are there"}; !equalStrings(got, want) {
		t.Fatalf("user content = %v, want %v — the question, then the steer", got, want)
	}
	// And the person watching their own correction is told it landed.
	if got, want := steerEvents(fromSteer), []string{
		"accepted:count the files while you are there",
		"consumed:count the files while you are there",
	}; !equalStrings(got, want) {
		t.Fatalf("steer events = %v, want %v", got, want)
	}
	// Nothing was interrupted and nothing was thrown away: the turn ended
	// normally, on its own stream.
	if last := fromSteer[len(fromSteer)-1]; last.Kind != EventTurnDone {
		t.Fatalf("steer stream ended with %v, want EventTurnDone", last.Kind)
	}
}

// ── (b) several steers keep the order they were typed in ────────────────────

// Two sent inside one step arrive together at the boundary that step ends on,
// and one sent after it arrives at the next — which is the same rule twice
// (steer.go): every steer lands at the FIRST boundary after it was sent.
func TestSteersLandInTheOrderTheyWereSent(t *testing.T) {
	first := newHeldTurn()
	second := newHeldTurn()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(first.entered)
			<-first.release
			return toolResponse("call-1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(second.entered)
			<-second.release
			return toolResponse("call-2", "ls", `{"path":"src"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("all three folded in"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	turn := mustSubmit(t, agent, "look around")
	first.wait(t)
	mustSteer(t, agent, "one")
	mustSteer(t, agent, "two")
	close(first.release)

	second.wait(t)
	mustSteer(t, agent, "three")
	close(second.release)
	collect(t, turn)

	if completer.requests() != 3 {
		t.Fatalf("requests = %d, want 3", completer.requests())
	}
	// Two typed in one step batch at that step's boundary, in order.
	if got, want := userLines(completer.request(1)), []string{"look around", "one", "two"}; !equalStrings(got, want) {
		t.Fatalf("second request user content = %v, want %v", got, want)
	}
	// The third waited for the boundary after the step it was typed in.
	if got, want := userLines(completer.request(2)), []string{"look around", "one", "two", "three"}; !equalStrings(got, want) {
		t.Fatalf("third request user content = %v, want %v", got, want)
	}
}

// ── (c) fall-through on a turn that answered ────────────────────────────────

// A steer typed at a turn whose last request has already gone out has no
// boundary left. It must not vanish, and it must not be written into the turn
// it missed: it becomes an ordinary waiting message, and the stream the person
// is already holding carries the turn it starts.
func TestASteerThatMissedItsBoundaryBecomesAWaitingMessage(t *testing.T) {
	held := newHeldTurn()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(held.entered)
			<-held.release
			// A text-only answer: this turn has no next step to steer into.
			return textResponse("here is the list"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("and here are the counts"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	turn := mustSubmit(t, agent, "list the workspace")
	held.wait(t)
	steered := mustSteer(t, agent, "count them as well")
	close(held.release)

	collect(t, turn)
	fromSteer := collect(t, steered)

	if got, want := steerEvents(fromSteer), []string{
		"accepted:count them as well",
		"fell:count them as well",
	}; !equalStrings(got, want) {
		t.Fatalf("steer events = %v, want %v — it steered nothing and must say so", got, want)
	}
	// ONE CHANNEL, ONE STORY: the turn it steered, the fall-through, then the
	// turn its own words started.
	var turns int
	for _, event := range fromSteer {
		if event.Kind == EventTurnDone {
			turns++
		}
	}
	if turns != 2 {
		t.Fatalf("EventTurnDone on the steer's stream = %d, want 2 — the turn it missed and the turn it became", turns)
	}
	// And the second turn is an ORDINARY question: its own turn, opening on the
	// person's words, with nothing pretending they steered the first one.
	if completer.requests() != 2 {
		t.Fatalf("requests = %d, want 2", completer.requests())
	}
	if got, want := userLines(completer.request(1)),
		[]string{"list the workspace", "count them as well"}; !equalStrings(got, want) {
		t.Fatalf("follow-up request user content = %v, want %v", got, want)
	}
	if got := messageText(lastMessage(agent)); got != "and here are the counts" {
		t.Fatalf("last message = %q, want the answer to the waiting message", got)
	}
}

// ── (d) fall-through onto a turn somebody stopped ───────────────────────────

// An interrupted turn drops its follow-up queue, because a drain must never
// resurrect a turn somebody stopped ([Agent.nextFollowUpLocked]). A steer that
// falls through onto one is an ordinary waiting message from that instant, and
// goes with the rest of the queue — but it is SAID on the stream and WRITTEN
// DOWN first, and it is never put into the transcript of the turn it missed.
func TestASteerFallsThroughWhenTheTurnIsInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	held := newHeldTurn()
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(held.entered)
			<-held.release
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })

	turn := mustSubmit(t, agent, "start the long thing")
	held.wait(t)
	steered := mustSteer(t, agent, "actually stop at the parser")
	close(held.release)
	agent.Interrupt()

	collect(t, turn)
	fromSteer := collect(t, steered)

	if got, want := steerEvents(fromSteer), []string{
		"accepted:actually stop at the parser",
		"fell:actually stop at the parser",
	}; !equalStrings(got, want) {
		t.Fatalf("steer events = %v, want %v", got, want)
	}
	// Not in the transcript of the turn it missed: the model never read it, and
	// a record that held it would say the model had.
	agent.mu.Lock()
	said := userLines(agent.messages)
	agent.mu.Unlock()
	for _, line := range said {
		if line == "actually stop at the parser" {
			t.Fatalf("the steer was written into the interrupted turn: %v", said)
		}
	}
	// A stop stops everything said to that turn, and no turn ran on it.
	if completer.requests() != 1 {
		t.Fatalf("requests = %d, want 1 — a drain must not restart a stopped turn", completer.requests())
	}
	// But the record says it was sent and that it steered nothing.
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !recordsAFallThrough(t, path, "actually stop at the parser") {
		t.Fatal("the journal holds no record of the steer that fell through")
	}
}

// recordsAFallThrough reports whether the file holds the honest line for one
// steer: sent, aimed at a turn, never carried by a request of it.
func recordsAFallThrough(t *testing.T, path, words string) bool {
	t.Helper()
	for _, entry := range journalEntries(t, path) {
		if entry.Type != "steer" || entry.Content != words {
			continue
		}
		if entry.Steer == nil || entry.Steer.Consumed {
			t.Fatalf("fall-through line does not say it fell through: %+v", entry.Steer)
		}
		if entry.Steer.At == "" {
			t.Fatal("fall-through line does not say when the person sent it")
		}
		return true
	}
	return false
}

// ── (e) fall-through on a turn that faulted ─────────────────────────────────

func TestASteerFallsThroughWhenTheTurnFaults(t *testing.T) {
	held := newHeldTurn()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(held.entered)
			<-held.release
			return nil, errors.New("insufficient_quota")
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	turn := mustSubmit(t, agent, "start it")
	held.wait(t)
	steered := mustSteer(t, agent, "and skip the tests")
	close(held.release)

	collected := collect(t, turn)
	var faulted bool
	for _, event := range collected {
		if event.Kind == EventError {
			faulted = true
		}
	}
	if !faulted {
		t.Fatalf("turn events = %v, want EventError among them", kinds(collected))
	}
	if got, want := steerEvents(collect(t, steered)), []string{
		"accepted:and skip the tests",
		"fell:and skip the tests",
	}; !equalStrings(got, want) {
		t.Fatalf("steer events = %v, want %v", got, want)
	}
	if completer.requests() != 1 {
		t.Fatalf("requests = %d, want 1 — a faulted turn starts nothing else", completer.requests())
	}
}

// ── (f) a steer with nothing to steer is refused ────────────────────────────

// The caller had a plain send available and did not use it, so the honest
// answer is that there was nothing running — not a turn started behind their
// back under a different name.
func TestSteeringAnIdleSessionIsRefused(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, nil)

	stream, err := agent.Steer("go left instead")
	if !errors.Is(err, ErrNothingToSteer) {
		t.Fatalf("Steer while idle = %v, want ErrNothingToSteer", err)
	}
	if stream != nil {
		t.Fatal("a refused steer must hand back no stream to wait on")
	}
	if completer.requests() != 0 {
		t.Fatalf("requests = %d, want 0 — a refusal sends nothing", completer.requests())
	}
	// And an empty one is refused before the question is even asked.
	if _, err := agent.Steer("   "); err == nil {
		t.Fatal("an empty steer must be refused")
	}
	// A steer AFTER the turn it was meant for has ended is the same refusal, and
	// this is the case a surface actually hits: the person typed while the answer
	// was still drawing and pressed enter a beat late.
	collect(t, mustSubmit(t, agent, "ask something"))
	if _, err := agent.Steer("too late"); !errors.Is(err, ErrNothingToSteer) {
		t.Fatalf("Steer after the turn = %v, want ErrNothingToSteer", err)
	}
}

// ── (g) the record says whether the model read it ───────────────────────────

// A steer is an ordinary user message in the transcript, because that is what
// the model has to read it as. The journal keeps the one bit that says it did
// not open the turn it sits in, and a surface reads it back off the display
// entry — which is what lets a turn be drawn as a trunk with elbow rows.
func TestTheRecordSaysASteerWasPartOfTheTurnItSteered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	held := newHeldTurn()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(held.entered)
			<-held.release
			return toolResponse("call-1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })

	turn := mustSubmit(t, agent, "list the workspace")
	held.wait(t)
	sent := time.Now()
	mustSteer(t, agent, "and count them")
	close(held.release)
	collect(t, turn)

	// The live surface: the question is the trunk, the steer is an elbow on it.
	entries := agent.Transcript()
	var trunk, elbow *DisplayEntry
	for index := range entries {
		switch entries[index].Text {
		case "list the workspace":
			trunk = &entries[index]
		case "and count them":
			elbow = &entries[index]
		}
	}
	if trunk == nil || elbow == nil {
		t.Fatalf("transcript is missing the question or the steer: %+v", entries)
	}
	if trunk.Steer != nil {
		t.Fatal("the message that OPENED the turn must not be marked a steer")
	}
	if elbow.Steer == nil {
		t.Fatal("the steer is drawn as an ordinary question: nothing says it was part of the turn")
	}
	if !elbow.Steer.Consumed {
		t.Fatal("the steer landed, and the record must say so")
	}
	if elbow.Steer.At.Before(sent.Add(-time.Second)) || elbow.Steer.At.After(time.Now()) {
		t.Fatalf("steer instant = %v, want the moment it was sent", elbow.Steer.At)
	}

	// And the same, read back off the file after the process is gone.
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var found bool
	for _, entry := range journalEntries(t, path) {
		if entry.Type == "message" && entry.Content == "and count them" {
			found = true
			if entry.Steer == nil || !entry.Steer.Consumed || entry.Steer.At == "" {
				t.Fatalf("journal line for the steer = %+v", entry.Steer)
			}
		}
		if entry.Type == "message" && entry.Content == "list the workspace" && entry.Steer != nil {
			t.Fatal("the turn's opening question is marked as a steer in the file")
		}
	}
	if !found {
		t.Fatal("the steer is not in the journal at all")
	}

	// A resume reads the mark back, so the elbow survives the process.
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	steers := 0
	for _, message := range replayed.messages {
		if messageText(message) == "and count them" {
			if _, marked := replayed.steers[noteKey(message)]; !marked {
				t.Fatal("the replay lost the steer's mark")
			}
			steers++
		}
	}
	if steers != 1 {
		t.Fatalf("replayed steers = %d, want 1", steers)
	}
}

// ── (h) a conversation with no steers in it is untouched ────────────────────

// The mark is new; the file is not. A transcript written before steering
// existed carries no steer line and no steer field, and it must replay into
// exactly the messages and exactly the display entries it always did.
func TestATranscriptWithNoSteersLoadsExactlyAsItAlwaysDid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	journal, _, err := openSessionFile(path, "/w", "m", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	journal.appendMessage(textMessage("user", "what does this do"))
	journal.appendMessage(textMessage("assistant", "it reads the file"))
	journal.appendNote(textMessage("user", "task 3 finished"))
	if err := journal.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if got, want := rolesOf(replayed.messages), []string{"user", "assistant", "user"}; !equalStrings(got, want) {
		t.Fatalf("replayed roles = %v, want %v", got, want)
	}
	if len(replayed.steers) != 0 {
		t.Fatalf("replayed steers = %v, want none in a file that has none", replayed.steers)
	}

	reopened, restored, err := openSessionFile(path, "/w", "m", "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	entries := shapeEntries(restored.messages, reopened)
	if got, want := len(entries), 3; got != want {
		t.Fatalf("entries = %d, want %d", got, want)
	}
	for _, entry := range entries {
		if entry.Steer != nil {
			t.Fatalf("an old line came back marked as a steer: %+v", entry)
		}
	}
	// And the marks that were already there still work: the session's own line
	// is still an aside and not the person's words.
	if entries[2].Role != "aside" {
		t.Fatalf("note role = %q, want aside", entries[2].Role)
	}
}

// ── (i) the room's steer is a different act and is untouched ────────────────

// [Agent.SteerTask] puts a line into a running NODE — another agent, its own
// transcript — and it shares this package's steering lane with the splice
// without sharing its marks. A line steered at a node is not a splice into
// anybody's turn, and nothing about it may start to behave like one.
func TestNodeSteeringIsNotATurnSplice(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	if !agent.enqueueSteeredLine("the config lives under etc/") {
		t.Fatal("the node's own steering lane refused a line")
	}
	agent.mu.Lock()
	queued := append([]userMessage(nil), agent.steering...)
	agent.mu.Unlock()
	if len(queued) != 1 {
		t.Fatalf("queued = %d, want the one steered line", len(queued))
	}
	if !queued[0].steered {
		t.Fatal("a line steered at a node lost its own mark")
	}
	if queued[0].steer != nil {
		t.Fatal("a line steered at a node was marked as a turn splice")
	}
	// It drains as it always did: into the transcript, as the person's words,
	// with no steer mark on the journal line and no lift out of the queue.
	agent.mu.Lock()
	agent.liftSteersLocked(nil)
	landed, owed := agent.drainSteeringLocked(nil)
	agent.mu.Unlock()
	if landed != 1 || !owed {
		t.Fatalf("drain = (%d, %v), want the line landed and an answer owed", landed, owed)
	}
	if got := messageText(lastMessage(agent)); got != "the config lives under etc/" {
		t.Fatalf("transcript tail = %q, want the steered line", got)
	}
	for _, entry := range agent.Transcript() {
		if entry.Steer != nil {
			t.Fatalf("a node's steered line came back marked as a splice: %+v", entry)
		}
	}
	if strings.TrimSpace(messageText(lastMessage(agent))) == "" {
		t.Fatal("the steered line reached the transcript empty")
	}
}
