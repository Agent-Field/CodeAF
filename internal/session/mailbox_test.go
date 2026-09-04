package session

// WHO TOOK IT, WHO SAID IT, AND WHAT HAPPENS WHEN NOBODY IS THERE.
//
// Every test here is about one seam — the instant a message is handed to a
// conversation (mailbox.go) — and each pins a way that seam used to lose a
// message or lie about one.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// pieceUnder admits one sub-task under a parent and hands back its node, in the
// state a landing delivery finds it in: finished, with a report, and its news
// not yet handed to anybody.
func pieceUnder(t *testing.T, graph *TaskGraph, parent uint64, title string) *TaskNode {
	t.Helper()
	id := graph.reserve()
	graph.admit(id, taskSpec{parent: parent, depth: 2, title: title, brief: "b", acceptance: "a"})
	node := graph.node(id)
	if node == nil {
		t.Fatalf("piece %d was admitted and is not in the graph", id)
	}
	return node
}

// queuedText is everything on one agent's steering queue, as the model would
// read it.
func queuedText(agent *Agent) []string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	var out []string
	for _, message := range agent.steering {
		out = append(out, message.text())
	}
	return out
}

// ── a report reaches a reader or it reaches the person ──────────────────────

// A PART'S REPORT GOES TO THE WORKER THAT ASKED FOR IT, and that is the
// ordinary case this file starts from: the parent's worker is standing in the
// room and the news is its to fold in.
func TestAPartsReportGoesToTheWorkerThatAskedForIt(t *testing.T) {
	nest := newNest(t, nil, nil)
	part := pieceUnder(t, nest.graph, nest.parent.id, "currency")

	nest.session.deliverTaskNote(part, part.attemptNow(), part.resultTag(), "task 2 finished: currency")

	if got := queuedText(nest.node); len(got) != 1 || !strings.Contains(got[0], "currency") {
		t.Fatalf("the parent's worker holds %#v, want the report it asked for", got)
	}
	if got := queuedText(nest.session); len(got) != 0 {
		t.Fatalf("the conversation was told about work it did not commission: %#v", got)
	}
	if !part.reported() {
		t.Fatal("a report a live worker took is not marked as handed over")
	}
}

// AND IT REACHES THE PERSON WHEN THE WORKER HAS STOPPED READING. The runner
// withdraws the seat the moment its reading is over (task_child_run.go), and
// before this the report was enqueued onto that agent anyway — queued on
// somebody who would never drain it, then marked and checkpointed as announced,
// so nothing ever said it again in this life or any later one.
func TestAPartsReportReachesThePersonWhenTheSeatIsEmpty(t *testing.T) {
	nest := newNest(t, nil, nil)
	part := pieceUnder(t, nest.graph, nest.parent.id, "currency")
	nest.parent.openRoom().speaking(nil)

	nest.session.deliverTaskNote(part, part.attemptNow(), part.resultTag(), "task 2 finished: currency")

	if got := queuedText(nest.node); len(got) != 0 {
		t.Fatalf("the withdrawn worker was handed %#v, and nothing will ever drain it", got)
	}
	if got := queuedText(nest.session); len(got) != 1 || !strings.Contains(got[0], "currency") {
		t.Fatalf("the conversation holds %#v, want the report nobody else could read", got)
	}
	if !part.reported() {
		t.Fatal("a report the conversation took is not marked as handed over")
	}
}

// AND A WORKER THAT CLOSED BETWEEN THE CHOICE AND THE HAND-OVER IS NOT THE
// READER EITHER. The old road asked whether the agent was open and then
// enqueued to it; a close in that window answered false and the answer was
// dropped on the floor.
func TestAPartsReportSkipsAWorkerThatHasClosed(t *testing.T) {
	nest := newNest(t, nil, nil)
	part := pieceUnder(t, nest.graph, nest.parent.id, "arithmetic")
	if err := nest.node.Close(); err != nil {
		t.Fatalf("closing the worker: %v", err)
	}

	nest.session.deliverTaskNote(part, part.attemptNow(), part.resultTag(), "task 2 finished: arithmetic")

	if got := queuedText(nest.session); len(got) != 1 || !strings.Contains(got[0], "arithmetic") {
		t.Fatalf("the conversation holds %#v, want the report a closed worker could not take", got)
	}
}

// AND THE RECEIPT NAMES THE CONVERSATION THAT TOOK IT, session and all. A task
// number is minted per session and repeats across them, so a receipt saying
// "task 1" would stop meaning anything the moment two transcripts are read
// together — which is exactly what a later cross-session router would have to
// resolve.
func TestAReceiptNamesTheSessionAndTheTaskThatTookTheMessage(t *testing.T) {
	nest := newNest(t, nil, nil)
	seat := roomSeat{at: conversationOf(nest.parent), room: nest.parent.openRoom()}

	got := seat.accept(delivery{origin: fromRuntime, kind: msgNotice, note: userText("a line")})

	if !got.accepted() {
		t.Fatalf("the seat refused a live worker's message: %v", got.state)
	}
	if got.to.task != nest.parent.id {
		t.Fatalf("the receipt names task %d, want the node that was addressed (%d)", got.to.task, nest.parent.id)
	}
	if got.to.session != nest.session.journalID() {
		t.Fatalf("the receipt names session %q, want %q", got.to.session, nest.session.journalID())
	}
	if !strings.Contains(got.to.String(), fmt.Sprintf("task %d", nest.parent.id)) {
		t.Fatalf("an address reads %q, want the task it names in it", got.to.String())
	}
	if seat.address().task != nest.parent.id {
		t.Fatal("a seat cannot say which conversation it is the seat of")
	}
}

// AND A CLOSED READER IS A REFUSAL AND NOT AN ACCEPTANCE. "Nobody is there" and
// "the reader cannot take anything any more" are different facts, and a caller
// that treats either as delivered loses the message.
func TestAClosedReaderRefusesRatherThanAccepts(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := agent.accept(delivery{origin: fromRuntime, kind: msgNotice, note: userText("a line")})

	if got.accepted() || got.state != deliveryClosed {
		t.Fatalf("a closed agent answered %v, want a refusal", got.state)
	}
	if got.reader != nil {
		t.Fatal("a refused delivery named a reader")
	}
}

// ── the window itself, held open ────────────────────────────────────────────

// THE WITHDRAWAL AND THE DELIVERY MEET ON ONE LOCK, and this holds that meeting
// open rather than hoping to land in it.
//
// The delivery is started while the room's lock is held here, so it is stopped
// exactly where the reader is resolved. The seat is emptied inside that window —
// which is what the runner's own withdrawal does — and the report must then be
// somewhere a person will read it, with nothing left on the queue of a worker
// that has stopped reading.
func TestAReportThatRacesTheWithdrawalIsNotSwallowed(t *testing.T) {
	nest := newNest(t, nil, nil)
	part := pieceUnder(t, nest.graph, nest.parent.id, "currency")
	room := nest.parent.openRoom()

	room.mu.Lock()
	delivered := make(chan struct{})
	go func() {
		defer close(delivered)
		nest.session.deliverTaskNote(part, part.attemptNow(), part.resultTag(), "task 2 finished: currency")
	}()
	// The delivery is now blocked on the room's lock with its reader unresolved.
	// The withdrawal wins the race, under the same lock, exactly as the runner's
	// own [taskRoom.speaking] would.
	waitBlocked(t, delivered)
	room.child = nil
	room.mu.Unlock()

	select {
	case <-delivered:
	case <-time.After(10 * time.Second):
		t.Fatal("the delivery never finished")
	}
	if got := queuedText(nest.node); len(got) != 0 {
		t.Fatalf("the withdrawn worker was handed %#v", got)
	}
	if got := queuedText(nest.session); len(got) != 1 {
		t.Fatalf("the conversation holds %#v, want the one report that lost the race", got)
	}
}

// AND THE DELIVERY THAT WINS THAT RACE IS OWED NEWS THE RUNNER STILL READS. A
// report is not held on the steering mark a person's line wears, so a runner
// that asked only about that mark on its way out left this queued and unread.
func TestAReportThatWinsTheRaceIsOwedNewsAfterTheWithdrawal(t *testing.T) {
	nest := newNest(t, nil, nil)
	part := pieceUnder(t, nest.graph, nest.parent.id, "currency")

	nest.session.deliverTaskNote(part, part.attemptNow(), part.resultTag(), "task 2 finished: currency")
	nest.parent.openRoom().speaking(nil)

	owed, _ := nest.node.taskNewsStanding()
	if owed == 0 {
		t.Fatal("a report taken an instant before the seat emptied is owed to nobody")
	}
	if nest.node.steeringHeld() {
		t.Fatal("a report is wearing the mark that belongs to a line somebody said")
	}
}

// waitBlocked gives a started goroutine time to reach the lock it is going to
// block on. It is not a synchronisation point and cannot be one — there is no
// signal for "parked on a mutex" — so it is deliberately short and the test's
// assertions hold whichever side won: what is being pinned is that no
// interleaving loses the report, and the sleep only decides which interleaving
// this run exercises.
func waitBlocked(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		t.Fatal("the delivery finished while the room's lock was held, so it never resolved a reader under it")
	case <-time.After(50 * time.Millisecond):
	}
}

// ── said once per ending ────────────────────────────────────────────────────

// THE SAME LANDING ANNOUNCED TWICE IS ONE PIECE OF NEWS. A duplicate report hook
// — a retried delivery, a settle re-entered — used to queue a second waking note
// and buy a second model turn on news the model already had.
func TestOneEndingAnnouncedTwiceIsOneNote(t *testing.T) {
	nest := newNest(t, nil, nil)
	part := pieceUnder(t, nest.graph, nest.parent.id, "currency")

	nest.session.deliverTaskNote(part, part.attemptNow(), part.resultTag(), "task 2 finished: currency")
	nest.session.deliverTaskNote(part, part.attemptNow(), part.resultTag(), "task 2 finished: currency")

	if got := queuedText(nest.node); len(got) != 1 {
		t.Fatalf("the worker holds %#v, want the one announcement of one ending", got)
	}
}

// AND A NODE THAT ENDS SOMEWHERE ELSE LATER IS NEWS AGAIN. A person deciding
// about work nobody could check is a new ending, not a repeat of the old one,
// and the identity that tells the two apart is the ending itself rather than the
// sentence — the same words about two endings are two events.
func TestANodeThatEndsSomewhereElseIsAnnouncedAgain(t *testing.T) {
	nest := newNest(t, nil, nil)
	part := pieceUnder(t, nest.graph, nest.parent.id, "currency")
	part.graph.mu.Lock()
	part.state = TaskUnverified
	part.graph.mu.Unlock()

	nest.session.deliverTaskNote(part, part.attemptNow(), part.resultTag(), "task 2 needs your look: currency")
	part.graph.mu.Lock()
	part.state = TaskDone
	part.graph.mu.Unlock()
	nest.session.deliverTaskNote(part, part.attemptNow(), part.resultTag(), "task 2 finished: currency")

	if got := queuedText(nest.node); len(got) != 2 {
		t.Fatalf("the worker holds %#v, want both endings", got)
	}
}

// ── and once per LIFE of the work ───────────────────────────────────────────

// A DELIVERY OF THE ENDING THAT JUST CLOSED CANNOT SPEAK FOR THE RUN THAT
// STARTED AFTER IT.
//
// Continuing a task re-arms the same node: it clears the announcement marks and
// its next ending is genuinely new news. A delivery of the OLD ending can still
// be in flight across that, and keyed on the state alone it did two wrong things
// — a late hand-over marked the new life announced (suppressing the ending it
// was about to reach), and a late release cleared the claim the new delivery was
// holding.
//
// The order below is written out rather than raced: each step is the instant the
// next one has to survive, so the interleaving is the test rather than the
// machine's timing.
func TestAnOldAttemptsDeliveryCannotAnnounceTheNewOne(t *testing.T) {
	nest := newNest(t, nil, nil)
	part := pieceUnder(t, nest.graph, nest.parent.id, "currency")
	endWith(t, part, TaskFailed)

	// The first attempt's delivery gets as far as its claim and is held there.
	stale, claimed := part.claimNote(part.attemptNow())
	if !claimed {
		t.Fatal("the first attempt could not claim its own ending")
	}

	// The person continues the task: same node, next life of the work.
	if err := nest.graph.reopen(part, "try the other table"); err != nil {
		t.Fatalf("continuing the task: %v", err)
	}
	if part.attemptNow() == stale.attempt {
		t.Fatal("re-arming the node left it on the same attempt")
	}

	// The stale delivery finishes now. It reached a reader — its words are not
	// lost — but it speaks for a life of the work that is over.
	if part.noteRecorded(stale) {
		t.Fatal("a delivery of the last attempt's ending was recorded as this attempt's announcement")
	}
	part.noteQueued(stale)
	if part.reported() {
		t.Fatal("the new attempt is marked as announced by the old one's hand-over")
	}

	// The new attempt ends differently, and that ending is news.
	endWith(t, part, TaskDone)
	nest.session.deliverTaskNote(part, part.attemptNow(), part.resultTag(), "task 2 finished: currency")
	if got := queuedText(nest.node); len(got) != 1 || !strings.Contains(got[0], "finished") {
		t.Fatalf("the worker holds %#v, want the new attempt's own ending", got)
	}
	if !part.reported() {
		t.Fatal("the new attempt's announcement was not marked")
	}

	// And the stale delivery's release does not take the live claim away with it.
	fresh, held := part.claimNote(part.attemptNow())
	if held {
		t.Fatal("this attempt's ending can be announced twice")
	}
	part.releaseNote(stale)
	if _, again := part.claimNote(fresh.attempt); again {
		t.Fatal("an old attempt's release cleared the claim standing now")
	}
}

// AND A REPORT COMPOSED BEFORE THE RE-ARM IS NOT ANNOUNCED AFTER IT. The payload
// and the attempt it describes are read together ([TaskNode.noticeFor]), so a
// landing that lost the race is refused rather than told as this life's news.
func TestAReportComposedForAnEarlierAttemptIsRefused(t *testing.T) {
	nest := newNest(t, nil, nil)
	part := pieceUnder(t, nest.graph, nest.parent.id, "currency")
	endWith(t, part, TaskFailed)
	was := part.attemptNow()
	// The tag a report for THAT attempt would have carried, taken before the
	// node is re-armed (wakecause.go's [TaskNode.resultOf]).
	wasTag := part.resultTag()

	if err := nest.graph.reopen(part, "try the other table"); err != nil {
		t.Fatalf("continuing the task: %v", err)
	}
	nest.session.deliverTaskNote(part, was, wasTag, "task 2 failed: currency")

	if got := queuedText(nest.node); len(got) != 0 {
		t.Fatalf("the worker was told %#v about a life of the node that is over", got)
	}
	if part.reported() {
		t.Fatal("the re-armed node is marked announced by the attempt before it")
	}
}

// endWith puts a node in one final state, the way a landing leaves it.
func endWith(t *testing.T, node *TaskNode, state TaskState) {
	t.Helper()
	node.graph.mu.Lock()
	node.state = state
	node.report = "what it came to"
	node.graph.mu.Unlock()
}

// AND NEWS NOBODY TOOK IS NOT WRITTEN DOWN AS ANNOUNCED. The mark is what stops
// a resumed session re-telling finished work, so making it for a delivery that
// reached nobody is how a report disappears for good.
func TestAReportNobodyTookIsNotMarkedAsAnnounced(t *testing.T) {
	nest := newNest(t, nil, nil)
	part := pieceUnder(t, nest.graph, nest.parent.id, "currency")
	nest.parent.openRoom().speaking(nil)
	if err := nest.session.Close(); err != nil {
		t.Fatalf("closing the conversation: %v", err)
	}

	nest.session.deliverTaskNote(part, part.attemptNow(), part.resultTag(), "task 2 finished: currency")

	if part.reported() {
		t.Fatal("a report that reached nobody is marked as handed over, so nothing will ever say it again")
	}
}

// ── queued is not delivered, across a close ─────────────────────────────────

// THE PROOF THAT A QUEUE IS NOT A RECORD.
//
// A task finishes on a session nobody is attached to: the wake declines
// ([Agent.wakeLocked] answers false with no reader), so the landing note sits on
// the conversation's queue with no step boundary coming. The reaper closes that
// session half an hour after the terminal detached, and the queue goes with the
// process. Marked announced at the enqueue — which is what the checkpoint used
// to record — the next life of the session did not re-tell it either, and the
// work was finished, merged and never mentioned to anybody.
func TestALandingNobodyReadIsStillOwedAfterAnIdleClose(t *testing.T) {
	sitting := newIdleConversation(t)

	sitting.land(t, "the currency table is written")

	// Nobody is there, so nothing was asked and nothing was read.
	if asked := sitting.completer.requests(); asked != 0 {
		t.Fatalf("an unattended session started %d turns, want the note held for a boundary that never came", asked)
	}
	if got := queuedText(sitting.agent); len(got) != 1 {
		t.Fatalf("the conversation holds %#v, want the landing note on its queue", got)
	}
	if err := sitting.agent.Close(); err != nil {
		t.Fatalf("closing the idle session: %v", err)
	}

	// The next life of this session reads the checkpoint, and the landing is
	// still owed: it is composed again, for a model that has never seen it.
	retold := sitting.reopen(t)
	if !strings.Contains(retold, "currency") {
		t.Fatalf("the resumed session is told %q, want the landing nobody ever read", retold)
	}
}

// AND A LANDING THE MODEL DID READ IS NOT SAID TWICE. The acknowledgement is the
// recipient's own record ([Agent.recordUserLocked]), and the checkpoint written
// from it is what makes the next life treat this as history.
func TestALandingTheRecordHoldsIsNotRetoldAfterAClose(t *testing.T) {
	sitting := newIdleConversation(t)

	sitting.land(t, "the currency table is written")
	// The drain is the step boundary an attended session reaches: the note goes
	// into the transcript, and the sender is told so.
	if landed := sitting.agent.drainSteering(nil); landed != 1 {
		t.Fatalf("%d notes drained, want the one landing", landed)
	}
	if err := sitting.agent.Close(); err != nil {
		t.Fatalf("closing the session: %v", err)
	}

	if retold := sitting.reopen(t); strings.Contains(retold, "finished") {
		t.Fatalf("the resumed session is told %q about work its model has already read", retold)
	}
}

// AND AN ACKNOWLEDGEMENT FROM AN EARLIER LIFE SETTLES NOTHING. A note queued
// before a continue is still owed to the attempt it was about; the attempt
// running now has its own ending to reach, and a stale settle would write it off.
func TestAStaleAcknowledgementDoesNotSettleTheNewAttempt(t *testing.T) {
	sitting := newIdleConversation(t)
	node := sitting.land(t, "the currency table is written")

	stale := sitting.agent.settlingNow(t)
	if err := sitting.graph.reopen(node, "try the other table"); err != nil {
		t.Fatalf("continuing the task: %v", err)
	}
	for _, delivery := range stale {
		delivery.settled()
	}

	if node.notedReadNow() {
		t.Fatal("an acknowledgement from the life before the continue settled this one")
	}
	// And the landing is still owed on the checkpoint, which is what a stale
	// settle would have written off.
	if retold := sitting.reopen(t); strings.Contains(retold, "announced") {
		t.Fatalf("the resumed session reads %q", retold)
	}
}

// AND IT IS STILL OWED AFTER THE SECOND CLOSE, AND THE THIRD. A resume used to
// mark its own re-telling as said the moment it composed it, so the second life
// of a session lost the landing exactly as the first one did — the same defect,
// one door along. The re-telling is a delivery like any other now: queued, and
// announced only when a reader's record holds it.
func TestALandingUnreadAcrossLivesIsToldAgainUntilItIsRead(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")

	// The first life: the work lands with nobody there, and nothing reads it.
	first := openIdleSession(t, journal)
	node := first.land(t, "the currency table is written")
	if got := queuedText(first.agent); len(got) != 1 {
		t.Fatalf("the first life holds %#v, want the landing note", got)
	}
	if err := first.agent.Close(); err != nil {
		t.Fatalf("closing the first life: %v", err)
	}

	// The second life re-tells it, and nobody reads it there either.
	second := openIdleSession(t, journal)
	if got := queuedText(second.agent); len(got) != 1 || !strings.Contains(got[0], "currency") {
		t.Fatalf("the second life holds %#v, want the landing nobody read", got)
	}
	if err := second.agent.Close(); err != nil {
		t.Fatalf("closing the second life: %v", err)
	}

	// The third life is told the same thing, and this time the model reads it.
	third := openIdleSession(t, journal)
	if got := queuedText(third.agent); len(got) != 1 || !strings.Contains(got[0], "currency") {
		t.Fatalf("the third life holds %#v, want the landing still owed", got)
	}
	if landed := third.agent.drainSteering(nil); landed != 1 {
		t.Fatalf("%d notes drained, want the one that was owed", landed)
	}
	if err := third.agent.Close(); err != nil {
		t.Fatalf("closing the third life: %v", err)
	}

	// And the fourth is told nothing: the record holds it, and the checkpoint
	// says so.
	fourth := openIdleSession(t, journal)
	// The recovery still opens with its own summary of the graph; what it must
	// not carry is the landing.
	for _, said := range queuedText(fourth.agent) {
		if strings.Contains(said, "currency") {
			t.Fatalf("the fourth life is told %q about work its own record already holds", said)
		}
	}
	// And the checkpoint says so in its own words: the node the fourth life
	// restored is announced, on the strength of a record that holds it.
	document, found := loadTaskCheckpoint(taskCheckpointPath(journal))
	if !found || len(document.Nodes) != 1 {
		t.Fatalf("checkpoint found=%v with %d nodes", found, len(document.Nodes))
	}
	if !document.Nodes[0].Noted {
		t.Fatal("the landing the model read is not written down as announced")
	}
	_ = node
}

// AND THE RECORD ITSELF IS WHAT THE REPLAY IS CHECKED AGAINST. The checkpoint is
// written after the record, so a machine that dies between them comes back with
// the landing still marked owed — and the journal line that carried its delivery
// id is the evidence that it was already told.
func TestALandingTheJournalRecordedIsNotToldAgainWithoutTheCheckpoint(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	first := openIdleSession(t, journal)
	first.land(t, "the currency table is written")
	if landed := first.agent.drainSteering(nil); landed != 1 {
		t.Fatalf("%d notes drained, want the landing", landed)
	}
	if err := first.agent.Close(); err != nil {
		t.Fatalf("closing the first life: %v", err)
	}
	// The checkpoint loses the acknowledgement, which is what a crash between the
	// record and the file looks like from the next life.
	forgetTheAcknowledgement(t, taskCheckpointPath(journal))

	second := openIdleSession(t, journal)

	for _, said := range queuedText(second.agent) {
		if strings.Contains(said, "currency") {
			t.Fatalf("the resumed session is told %q about a landing its own journal recorded", said)
		}
	}
}

// AND A JOURNAL THAT COULD NOT BE WRITTEN SETTLES NOTHING. The acknowledgement
// is the record, so a failed append leaves the landing owed — said twice at
// worst, which is the direction this has to fail in.
func TestALandingIsNotSettledWhenTheJournalRefusesTheWrite(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	sitting := openIdleSession(t, journal)
	node := sitting.land(t, "the currency table is written")

	// The journal stops taking lines under the conversation, which is what a
	// disk error or a closed file looks like to the write.
	sitting.agent.file.mu.Lock()
	sitting.agent.file.closed = true
	sitting.agent.file.mu.Unlock()

	if landed := sitting.agent.drainSteering(nil); landed != 1 {
		t.Fatalf("%d notes drained, want the landing", landed)
	}
	if node.notedReadNow() {
		t.Fatal("a landing was announced on a journal write that never happened")
	}
}

// forgetTheAcknowledgement rewrites the checkpoint with the announced mark off,
// leaving the journal as the only record that the landing was told.
func forgetTheAcknowledgement(t *testing.T, checkpoint string) {
	t.Helper()
	document, found := loadTaskCheckpoint(checkpoint)
	if !found {
		t.Fatalf("no checkpoint at %s", checkpoint)
	}
	for index := range document.Nodes {
		document.Nodes[index].Noted = false
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatalf("re-encoding the checkpoint: %v", err)
	}
	if err := os.WriteFile(checkpoint, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("rewriting the checkpoint: %v", err)
	}
}

// openIdleSession opens one life of a session on a journal that outlives it:
// nobody attached, and nothing running the work.
func openIdleSession(t *testing.T, journal string) *idleConversation {
	t.Helper()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("nobody asked for this"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = journal
	})
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()
	graph := agent.graph()
	graph.mu.Lock()
	graph.run = func(*TaskNode) {}
	graph.mu.Unlock()
	return &idleConversation{
		agent: agent, graph: graph, completer: completer,
		checkpoint: taskCheckpointPath(journal), workspace: workspace,
	}
}

// idleConversation is a session with a checkpoint and nobody attached: the shape
// a task lands into when the terminal has gone.
type idleConversation struct {
	agent      *Agent
	graph      *TaskGraph
	completer  *scriptedCompleter
	checkpoint string
	workspace  string
}

func newIdleConversation(t *testing.T) *idleConversation {
	t.Helper()
	checkpoint := filepath.Join(t.TempDir(), "session.tasks.json")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("nobody asked for this"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)
	// Nobody is attached, which is what makes the wake decline.
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()
	graph := agent.graph()
	graph.mu.Lock()
	// Nothing runs the work: this fixture is about what happens to the news after
	// it lands, and a real runner would be a second author of that landing.
	graph.run = func(*TaskNode) {}
	graph.store = newTaskStore(checkpoint)
	graph.mu.Unlock()
	return &idleConversation{agent: agent, graph: graph, completer: completer, checkpoint: checkpoint, workspace: workspace}
}

// land admits one task of this conversation's own and finishes it, which is the
// road a landing note really travels ([Agent.reportTaskNode]).
func (c *idleConversation) land(t *testing.T, report string) *TaskNode {
	t.Helper()
	id := c.graph.reserve()
	c.graph.admit(id, taskSpec{title: "the currency table", brief: "b", acceptance: "a", depth: 1})
	node := c.graph.node(id)
	if node == nil {
		t.Fatal("the task was admitted and is not in the graph")
	}
	node.finish(report, nil, "", "")
	c.graph.complete(node, TaskDone)
	return node
}

// reopen is the next life of this session reading the checkpoint: what it would
// tell its model about work that landed while the last one was away.
func (c *idleConversation) reopen(t *testing.T) string {
	t.Helper()
	document, found := loadTaskCheckpoint(c.checkpoint)
	if !found {
		t.Fatalf("no checkpoint was written to %s", c.checkpoint)
	}
	fresh := newTaskGraph()
	return fresh.rehydrate(document, c.workspace, TaskSettleAsk).note()
}

// settlingNow is the acknowledgements this conversation is holding, taken
// without sending them.
func (a *Agent) settlingNow(t *testing.T) []durableDelivery {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	held := append([]durableDelivery(nil), a.settling...)
	for _, message := range a.steering {
		held = append(held, message.delivered...)
	}
	if len(held) == 0 {
		t.Fatal("nothing is waiting to be acknowledged")
	}
	return held
}

// notedReadNow is the mark the checkpoint is written from.
func (n *TaskNode) notedReadNow() bool {
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.notedRead
}

// ── progress is not a conversation ──────────────────────────────────────────

// AMBIENT PROGRESS STARTS NOTHING. A watch's tick is telemetry with a complete
// log behind it; delivered as progress it lands where no step drain and no wake
// can reach it, whatever the note says.
func TestProgressNeitherWakesNorAsksAnything(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("nobody asked for this"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	agent.enqueueWatchNote("checks", "watch checks · 2 pending", false)

	agent.mu.Lock()
	steering, ambient := len(agent.steering), len(agent.ambient)
	agent.mu.Unlock()
	if steering != 0 {
		t.Fatalf("%d ticks reached the step queue, want none", steering)
	}
	if ambient != 1 {
		t.Fatalf("the ambient queue holds %d, want the one tick", ambient)
	}
	// A turn is the thing a tick may never buy. Nothing here is waited for,
	// because a wake would have started one synchronously in the enqueue above.
	if asked := completer.requests(); asked != 0 {
		t.Fatalf("a tick asked the model %d times", asked)
	}
}

// ── who is speaking ─────────────────────────────────────────────────────────

// spokenRoom is a running node with a worker in it that KEEPS A JOURNAL, which
// is where the difference between the person and the model is written down.
type spokenRoom struct {
	nest   *nest
	worker *Agent
	file   string
}

func newSpokenRoom(t *testing.T, worker Completer) *spokenRoom {
	t.Helper()
	nest := newNest(t, nil, nil)
	path := filepath.Join(t.TempDir(), "node.jsonl")
	if worker == nil {
		worker = &scriptedCompleter{}
	}
	agent, err := newAgent(Config{
		Workspace:   t.TempDir(),
		Model:       "test/model",
		System:      "SYSTEM",
		SessionFile: path,
		InTask:      true,
		tasker:      nest.graph,
		taskID:      nest.parent.id,
		taskDepth:   1,
	}, worker)
	if err != nil {
		t.Fatalf("newAgent for the worker: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	nest.parent.openRoom().speaking(agent)
	return &spokenRoom{nest: nest, worker: agent, file: path}
}

// drainedEntries puts whatever is queued in front of the model and hands back
// the worker's own record of it.
func (s *spokenRoom) drainedEntries(t *testing.T) []DisplayEntry {
	t.Helper()
	s.worker.mu.Lock()
	landed, _ := s.worker.drainSteeringLocked(nil)
	s.worker.mu.Unlock()
	if landed == 0 {
		t.Fatal("nothing was on the worker's queue to drain")
	}
	return s.worker.Transcript()
}

// entrySaying is the one recorded entry holding these words.
func entrySaying(t *testing.T, entries []DisplayEntry, words string) DisplayEntry {
	t.Helper()
	for _, entry := range entries {
		if strings.Contains(entry.Text, words) {
			return entry
		}
	}
	t.Fatalf("no entry says %q: %#v", words, entries)
	return DisplayEntry{}
}

// THE PERSON'S OWN LINE IS STILL THE PERSON'S. Nothing about the person's door
// moves: their words arrive undecorated, and the record marks them as the
// correction they are.
func TestThePersonsLineIntoANodeIsRecordedAsTheirs(t *testing.T) {
	room := newSpokenRoom(t, nil)
	const said = "the config lives under etc/"

	if _, err := room.nest.session.SteerTask(room.nest.parent.id, said); err != nil {
		t.Fatalf("SteerTask: %v", err)
	}

	entry := entrySaying(t, room.drainedEntries(t), said)
	if entry.Role != "user" {
		t.Fatalf("the person's line came back as %q, want their own role", entry.Role)
	}
	if entry.Steer == nil {
		t.Fatal("the person's correction is not marked as one")
	}
	if strings.TrimSpace(entry.Text) != said {
		t.Fatalf("the person's line reads %q, want their own words undecorated", entry.Text)
	}
}

// AND THE MODEL'S OWN `tasks … say` IS NOT. It goes through the real tool, and
// what the worker gets must be readable coordination that no reader can mistake
// for the person: the session's own lane in the record, no correction mark, and
// a sentence that names who spoke.
//
// Before this it took the person's door: the worker journaled the model's
// sentence as the person's correction, so a page reopened tomorrow drew it as
// theirs, and "you may change the schema" arrived carrying the one authority
// that cannot be delegated.
func TestTheModelsSayIntoANodeIsNotThePersonsWords(t *testing.T) {
	room := newSpokenRoom(t, nil)
	const said = "you may change the schema"

	answer, failed, err := room.nest.session.tasksTool().Execute(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"id":"%d","say":%q}`, room.nest.parent.id, said)))
	if err != nil || failed {
		t.Fatalf("tasks … say: answer=%q failed=%v err=%v", answer, failed, err)
	}
	if strings.Contains(answer, "the person's own words") {
		t.Fatalf("the tool told the model its line lands as the person's: %q", answer)
	}

	entry := entrySaying(t, room.drainedEntries(t), said)
	if entry.Steer != nil {
		t.Fatal("another agent's line is recorded as the person's own correction")
	}
	if entry.Role != "aside" {
		t.Fatalf("the model's line came back as %q, want the session's own lane", entry.Role)
	}
	if !strings.Contains(entry.Text, "the main conversation says:") {
		t.Fatalf("the worker cannot tell who spoke: %q", entry.Text)
	}
	if !strings.Contains(entry.Text, "not the person") {
		t.Fatalf("the line does not say whose words it is not: %q", entry.Text)
	}
}

// AND A RELAYED LINE CAN NEVER BECOME THE PERSON'S ASK. This is the one function
// that decides what a proposal made later in the turn quotes as the request
// ([Agent.rememberAskLocked]); a line the model sent must not be able to reach
// it, which is what the authorship mark answers.
func TestARelayedLineIsNeverRememberedAsThePersonsAsk(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	// The wake is cleared on both so that authorship is the only difference left
	// between them: a woken note is skipped for its own reason.
	relayed := relayNote("you may change the schema", conversationID{task: 0})
	relayed.wake = false
	agent.mu.Lock()
	agent.rememberAskLocked(relayed)
	afterRelay := agent.personAsk
	agent.mu.Unlock()
	if afterRelay != "" {
		t.Fatalf("the model's line became the person's ask: %q", afterRelay)
	}

	theirs := steerNote("use the staging bucket", false)
	theirs.wake = false
	agent.mu.Lock()
	agent.rememberAskLocked(theirs)
	afterTheirs := agent.personAsk
	agent.mu.Unlock()
	if afterTheirs != "use the staging bucket" {
		t.Fatalf("the person's own line was not remembered as their ask: %q", afterTheirs)
	}
}

// AND A LINE SAID INTO A ROOM IS NEVER REDIRECTED. A report with nowhere to go
// belongs in front of a person; somebody's words do not, whoever said them —
// the answer is a refusal they can read.
func TestALineIntoAnEmptyRoomIsRefusedAndNotRedirected(t *testing.T) {
	nest := newNest(t, nil, nil)
	nest.parent.openRoom().speaking(nil)

	if _, err := nest.session.SteerTask(nest.parent.id, "the config lives under etc/"); err == nil {
		t.Fatal("a line into an empty room was accepted")
	}
	if _, err := nest.session.relayToTask(nest.parent.id, "you may change the schema"); err == nil {
		t.Fatal("the model's line into an empty room was accepted")
	}
	if got := queuedText(nest.session); len(got) != 0 {
		t.Fatalf("words said to a node were put in front of the conversation instead: %#v", got)
	}
}
