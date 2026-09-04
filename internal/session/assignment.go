package session

// The assignment: what a running task is working towards RIGHT NOW, which is
// not always what it was handed at admission.
//
// A person walks into a running task's room and says "CSV instead of JSON".
// Until this file existed that sentence reached the worker as talk and nothing
// else: `spec.brief` and `spec.acceptance` were frozen at admission, the
// auditor graded the finished work against the frozen text, and the worker's
// own prompt told it that a direction "does not replace your brief or your
// done-condition". So the harness asked the worker to obey a correction and
// then checked it for having obeyed one. That is the contradiction this file
// removes, and it removes it in the one way that keeps verification meaningful:
// the objective moves only with AUTHORITY, and it moves as a versioned,
// attributed, recorded revision that every later reader sees the same way.
//
// ── WHAT IS FROZEN AND WHAT MOVES ──
//
// The admitted spec never changes. `spec.request` is the person's own words,
// `spec.brief` is the contract the conversation groomed, `spec.acceptance` is
// what the work was originally to be judged by, and all three stay byte for
// byte what they were — so a page reopened tomorrow, a checkpoint written by an
// older build, and the record of what was actually agreed all still read true.
//
// What moves is an OVERLAY on top of them: an effective deliverable, an
// effective acceptance, and a block of revisions carrying the person's verbatim
// words. [taskAssignment.effective] folds the two into one snapshot, and the
// three readers that must never disagree — the worker's document, the check's
// harvest, the auditor's packet — all take that one snapshot.
//
// ── AUTHORITY IS NOT DELEGABLE ──
//
// A revision must cite a DIRECTION: one recorded message, with an id, from
// somebody who may revise this work. Only [directionFromPerson] may. The
// model's own coordination into a node (`tasks … say`, and after the comms
// lane's seam lands, anything carrying its `fromAgent` origin) is recorded and
// readable and is refused as a basis for revision — an agent that could revise
// the work it is being graded on would be marking its own homework, and a
// descendant phrasing a request as an instruction must not thereby acquire the
// authority nobody gave it.
//
// A worker cannot mint a direction either: the only doors that record one are
// the person's own ([Agent.SteerTask], [Agent.ContinueTask]). The worker's tool
// (assignment_tool.go) may only APPLY one that already exists, once, and the
// person's verbatim words travel with the revision it writes — so a restatement
// that has drifted from what they actually said is visible to the auditor and
// to the person, rather than being the only account left.
//
// ── DISCUSSION IS NOT DIRECTION ──
//
// Nothing here fires on its own. "Why did you do it this way?" is recorded as a
// direction receipt in `pending` and read by the worker as an ordinary message,
// and the assignment's version does not move: an answer is what that question
// wants. Moving it costs one explicit tool call by a worker that has the words
// in front of it, which is why there is no classifier model call in this file —
// the capable turn already reading the message is the reader.
//
// ── VERSION IS NOT ATTEMPT ──
//
// [taskAssignment.version] counts REVISIONS OF THE GOAL and nothing else. A
// node that is stopped, continued, repaired or run a second time after a
// provider fault is the same assignment at the same version; a node that has
// taken one accepted direction is at version 1 whether that was its first turn
// or its fortieth. The two are separate because the questions are: "what is
// this work for" and "how many times have we tried it" have different answers
// and different readers.

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// directionFrom is WHO SAID IT, and it is the whole of what decides whether a
// line may move the goal.
//
// It is deliberately a fact about the message rather than a permission on the
// caller: the same words travel from a person's room door and from another
// agent's coordination call, and by the time a worker reads them the only thing
// that can still tell them apart is what was written down when they arrived.
type directionFrom uint8

const (
	// directionFromPerson is the person at a keyboard: the room's steer, and the
	// words sent with a continue. These may be cited to revise the assignment.
	directionFromPerson directionFrom = iota + 1
	// directionFromAgent is another agent's coordination — this conversation's
	// model, or a parent node, talking to a running task. It is delivered and
	// recorded and it may never revise: see the authority note above.
	directionFromAgent
)

func (f directionFrom) String() string {
	switch f {
	case directionFromPerson:
		return "person"
	case directionFromAgent:
		return "agent"
	}
	return "unknown"
}

// directionState is where one direction has got to, and the three words are the
// three honest answers a surface or a record can give.
//
// pending is "the node has it, no worker has read it yet" — which is also what
// makes a completion stale (task_ledger.go). read is "a worker's request
// carried it", which a direction reaches whether or not it changed anything,
// because most directions are talk. applied is "it produced a revision", and
// the version it produced is on the receipt.
type directionState string

const (
	directionPending directionState = "pending"
	directionRead    directionState = "read"
	directionApplied directionState = "applied"
)

// taskDirection is one line said to a running task, as the node's record keeps
// it: what was said, when, by whom, and what became of it.
type taskDirection struct {
	id    uint64
	words string
	at    time.Time
	from  directionFrom
	state directionState
	// version is the assignment version this direction produced, and 0 until it
	// produces one. A direction that was read and changed nothing keeps 0
	// forever, which is the ordinary case.
	version uint64
}

// taskAssignment is the node's effective instruction and the record it came
// from. The zero value is a node nobody has said anything to, whose effective
// assignment is exactly the spec it was admitted with.
type taskAssignment struct {
	// version is 0 for an unrevised node and counts accepted revisions after
	// that. It is the number a landing revalidates and a document quotes.
	version uint64
	// deliverable and acceptance REPLACE the spec's when they are set; empty
	// means the admitted text still stands. The brief is not replaced — the
	// revision block below is added to the document instead — because the
	// assembled brief also carries what the work ahead of this node reported and
	// what a continuation found, and a replacement would silently drop them.
	deliverable string
	acceptance  string
	// revisions is the person's own words for each accepted revision, in order,
	// with whatever the worker restated about what they change. It is what makes
	// a revised assignment auditable: the document and the checker's packet
	// carry the direction and the restatement together, so a restatement that
	// has wandered from what was actually said can be seen to have wandered.
	revisions []assignmentRevision
	// directions is every line said to this node, in arrival order, including
	// the ones that were only ever talk.
	directions []taskDirection
	next       uint64
}

// assignmentRevision is one accepted move of the goal.
type assignmentRevision struct {
	version     uint64
	directionID uint64
	said        string
	work        string
	deliverable string
	acceptance  string
	at          time.Time
}

// assignmentEdit is what a revision asks for. Every field is optional and an
// empty one leaves that part of the assignment where it was; an edit that asks
// for nothing at all is refused, because a revision that changes nothing is a
// version number with no content behind it.
type assignmentEdit struct {
	work        string
	deliverable string
	acceptance  string
}

func (e assignmentEdit) empty() bool {
	return strings.TrimSpace(e.work) == "" &&
		strings.TrimSpace(e.deliverable) == "" &&
		strings.TrimSpace(e.acceptance) == ""
}

// assignmentNow is one consistent view of what this task is working towards:
// the version it is at, the document sections as they stand, and the revision
// block if there is one. Worker, check and landing all read this and never the
// fields behind it, so there is no version of events where the work was done
// against one acceptance and judged against another.
type assignmentNow struct {
	version     uint64
	brief       string
	deliverable string
	acceptance  string
	// revised is the block that says what moved and who moved it, empty on an
	// unrevised node.
	revised string
}

// The refusals a revision can meet. They are values rather than sentences built
// at the call site because the tool prints them to a model and the tests match
// them: one wording per reason, in one place.
var (
	errNoSuchDirection    = errors.New("no direction with that id was said to this task")
	errDirectionNotAsked  = errors.New("that line came from another agent and not from the person, so it cannot change what this task is judged by")
	errDirectionSpent     = errors.New("that direction has already been applied")
	errNothingToRevise    = errors.New("a revision has to change the deliverable, the done-condition or the work")
	errAssignmentSettled  = errors.New("this task has settled and its assignment is a fact now")
	errNoAssignmentToMove = errors.New("no task to revise")
	errStaleVersion       = errors.New("the assignment has moved since the version you named")
	errStaleDirection     = errors.New("that direction is older than the one already in force")
	errPublishing         = errors.New("this task's work is being published and its assignment is closed")
)

// hear records one line said to this task and answers its id. Every line is
// recorded, including the ones that will never be anything but talk: the
// receipt is what a worker cites, what a landing counts and what a person's
// page can show, and a message nobody wrote down cannot be any of the three.
func (a *taskAssignment) hear(words string, from directionFrom, at time.Time) (uint64, bool) {
	words = strings.TrimSpace(words)
	if words == "" {
		return 0, false
	}
	a.next++
	a.directions = append(a.directions, taskDirection{
		id:    a.next,
		words: words,
		at:    at,
		from:  from,
		state: directionPending,
	})
	return a.next, true
}

// forget removes a receipt that was minted for words nobody took. It exists
// because the id has to be known BEFORE the line is delivered — it rides under
// the words, so the worker can cite it — and a delivery can still be refused
// after that. A receipt left behind for words no reader ever got would be a
// direction the landing waits for forever.
func (a *taskAssignment) forget(id uint64) {
	kept := a.directions[:0]
	for _, said := range a.directions {
		if said.id == id && said.state == directionPending {
			continue
		}
		kept = append(kept, said)
	}
	a.directions = kept
}

// direction answers one receipt by id.
func (a *taskAssignment) direction(id uint64) (taskDirection, bool) {
	for _, said := range a.directions {
		if said.id == id {
			return said, true
		}
	}
	return taskDirection{}, false
}

// markCarried moves exactly the named directions to read. Anything not named is
// left pending, whatever else has happened to the work.
func (a *taskAssignment) markCarried(ids []uint64) {
	for _, id := range ids {
		for index := range a.directions {
			if a.directions[index].id == id && a.directions[index].state == directionPending {
				a.directions[index].state = directionRead
			}
		}
	}
}

// pendingFrom is every direction from one speaker that no worker has read. The
// landing asks it about the person: their words arriving while the checker ran
// are the whole reason a finished-looking node may not publish (task_ledger.go).
func (a *taskAssignment) pendingFrom(from directionFrom) []taskDirection {
	var waiting []taskDirection
	for _, said := range a.directions {
		if said.state == directionPending && said.from == from {
			waiting = append(waiting, said)
		}
	}
	return waiting
}

// revise applies one direction to the assignment and answers the new version.
//
// The four refusals are the whole of the authority law, and they are checked
// here rather than at the tool so that every door — the worker's call today,
// anything that reaches for this seam later — meets the same rules.
// expected is the version the caller believes the assignment is at, and a
// mismatch is refused rather than merged: two directions applied out of order
// would leave the later one's done-condition underneath the earlier one's, which
// is the person's most recent correction being quietly discarded.
func (a *taskAssignment) revise(id uint64, expected uint64, edit assignmentEdit, at time.Time) (uint64, error) {
	said, found := a.direction(id)
	if !found {
		return 0, errNoSuchDirection
	}
	if said.from != directionFromPerson {
		return 0, errDirectionNotAsked
	}
	if said.state == directionApplied {
		return 0, errDirectionSpent
	}
	// AND NEVER OUT OF ORDER. A direction older than the last one applied cannot
	// move the goal, whatever version it names: the person's later correction is
	// already in force, and folding an earlier one in on top of it would put back
	// a condition they have moved on from. The version check below is about
	// concurrent readings; this is about which sentence came last.
	if latest := a.latestApplied(); said.id < latest {
		return 0, fmt.Errorf("%w: direction %d came before %d, which is already in force", errStaleDirection, said.id, latest)
	}
	if expected != a.version {
		return 0, fmt.Errorf("%w: it is at revision %d and you named %d", errStaleVersion, a.version, expected)
	}
	if edit.empty() {
		return 0, errNothingToRevise
	}
	a.version++
	revision := assignmentRevision{
		version:     a.version,
		directionID: id,
		said:        said.words,
		work:        strings.TrimSpace(edit.work),
		deliverable: strings.TrimSpace(edit.deliverable),
		acceptance:  strings.TrimSpace(edit.acceptance),
		at:          at,
	}
	if revision.deliverable != "" {
		a.deliverable = revision.deliverable
	}
	if revision.acceptance != "" {
		a.acceptance = revision.acceptance
	}
	a.revisions = append(a.revisions, revision)
	for index := range a.directions {
		if a.directions[index].id != id {
			continue
		}
		a.directions[index].state = directionApplied
		a.directions[index].version = a.version
	}
	return a.version, nil
}

// latestApplied is the newest direction that has moved this assignment, and 0
// when none has. Ids are minted in arrival order, so the largest is the last
// thing the person said that counted.
func (a *taskAssignment) latestApplied() uint64 {
	var latest uint64
	for _, revision := range a.revisions {
		if revision.directionID > latest {
			latest = revision.directionID
		}
	}
	return latest
}

// effective folds the overlay onto the admitted spec. The brief keeps whatever
// the node assembled — the groomed contract, its prerequisites' reports, a
// continuation's finding — and the revision block is added under it.
func (a *taskAssignment) effective(brief, deliverable, acceptance string) assignmentNow {
	now := assignmentNow{
		version:     a.version,
		brief:       brief,
		deliverable: deliverable,
		acceptance:  acceptance,
		revised:     a.revisionBlock(),
	}
	if a.deliverable != "" {
		now.deliverable = a.deliverable
	}
	if a.acceptance != "" {
		now.acceptance = a.acceptance
	}
	if now.revised != "" {
		now.brief = strings.TrimSpace(strings.TrimRight(now.brief, "\n") + "\n\n" + now.revised)
	}
	return now
}

// assignmentRevisedLead opens the block that says the goal moved. It leads with
// the person's own words because those are the authority; the restatement under
// them is the worker's reading of what they change, and it is labelled as one.
const assignmentRevisedLead = "REVISED WHILE THIS WORK RAN, BY THE PERSON. These words came AFTER the message quoted at the top of this document: where the two differ, these are the later ones and these are what this work is now for. What they did not change stands exactly as it was, and DONE WHEN below is the revised condition — the earlier one is history and nothing is owed for satisfying it."

// revisionBlock renders every accepted revision in order. An unrevised
// assignment renders nothing at all, which is the emptiness law this package
// applies to every other section of a task's document.
func (a *taskAssignment) revisionBlock() string {
	if len(a.revisions) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(assignmentRevisedLead + "\n")
	for _, revision := range a.revisions {
		fmt.Fprintf(&out, "\nRevision %d — they said: %s\n", revision.version, revision.said)
		if revision.work != "" {
			out.WriteString("What it changes about the work: " + revision.work + "\n")
		}
		if revision.deliverable != "" {
			out.WriteString("What must exist now: " + revision.deliverable + "\n")
		}
		if revision.acceptance != "" {
			out.WriteString("Done now means: " + revision.acceptance + "\n")
		}
	}
	return strings.TrimRight(out.String(), "\n")
}

// revisedSaid is every word of the person's that has actually moved this
// assignment, in order, and empty on an unrevised node. It is what the check
// harvest reads as "the person's message" once there is a later one than the one
// the work was admitted from (task_run.go's [TaskNode.checkTexts]).
func (a *taskAssignment) revisedSaid() string {
	if len(a.revisions) == 0 {
		return ""
	}
	said := make([]string, 0, len(a.revisions))
	for _, revision := range a.revisions {
		said = append(said, revision.said)
	}
	return strings.Join(said, "\n")
}

// directionBlock renders directions for a document that has to hand them over
// without pretending they revised anything: the reopen's finding, which carries
// words the last attempt never got to read.
func directionBlock(said []taskDirection) string {
	if len(said) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(directionCarriedLead + "\n")
	for _, one := range said {
		// WHO SAID IT TRAVELS WITH IT. The person's words are given plainly,
		// because that is what they are; another agent's coordination is named, so
		// that a worker reading both cannot take the second for the first when it
		// decides which of them may move the done-condition.
		if one.from == directionFromPerson {
			fmt.Fprintf(&out, "\nThe person said (direction %d): %s\n", one.id, one.words)
			continue
		}
		fmt.Fprintf(&out, "\nAnother agent said (direction %d, coordination and not the person's authority): %s\n",
			one.id, one.words)
	}
	return strings.TrimRight(out.String(), "\n")
}

// directionCarriedLead opens the unread words a continuation hands to the next
// attempt. It says plainly that they have not been applied, because the next
// worker is the one that decides whether they are a correction to the goal or a
// remark about the work.
const directionCarriedLead = "Said while the last attempt was finishing, and read by no worker — this has NOT been applied to the assignment"

// ── the node's own doors ────────────────────────────────────────────────────

// heardDirection records one line said to this node and answers its receipt id
// and WHICH SIDE OF THE PUBLICATION BOUNDARY it landed on.
//
// The graph's lock is the one every other field of the node is written under,
// and it is also the linearization point: a landing claims the boundary under
// this same lock ([TaskNode.claimPublication]), so a direction is either
// admitted before the claim — and then the claim fails and the work does not
// publish — or after it, and then it belongs to the round after this one. There
// is no third ordering and no window between them.
func (n *TaskNode) heardDirection(words string, from directionFrom) (uint64, bool) {
	if n == nil || n.graph == nil {
		return 0, false
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	id, kept := n.assignment.hear(words, from, time.Now())
	return id, kept && !n.publishing
}

// forgetDirection drops a receipt whose words were never delivered.
func (n *TaskNode) forgetDirection(id uint64) {
	if n == nil || n.graph == nil || id == 0 {
		return
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	n.assignment.forget(id)
}

// directionReceiptLine is the HARNESS'S OWN LINE under the person's words, and
// it is the one thing this file adds to a message that is otherwise delivered
// exactly as it was typed (task_room.go's no-decoration law).
//
// It is here because a direction cannot become a revision without an id to cite,
// and the id is a fact about the delivery rather than about what was said. It
// goes UNDER the words and says who it is from, so the person's sentence still
// leads and still reads as somebody talking; a frame around them would teach the
// worker to read a person as a system event, which is the thing that law is for.
func directionReceiptLine(id uint64) string {
	return fmt.Sprintf("[the harness: that was the person, direction %d. If it changes what this work is FOR — a different output, a different target, a requirement added or dropped — fold it in with revise_assignment citing %d. If it is a fact or a question, just use it or answer it.]", id, id)
}

// directionsNow copies the receipts out for a reader outside the lock.
func (n *TaskNode) directionsNow() []taskDirection {
	if n == nil || n.graph == nil {
		return nil
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	said := make([]taskDirection, len(n.assignment.directions))
	copy(said, n.assignment.directions)
	return said
}

// unreadPersonDirections is what the landing revalidates against: the person's
// words that arrived while nobody was reading.
func (n *TaskNode) unreadPersonDirections() []taskDirection {
	if n == nil || n.graph == nil {
		return nil
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.assignment.pendingFrom(directionFromPerson)
}

// markDirectionsCarried is called by a WORKER agent at the drain immediately
// before its next provider request, with the receipt ids that request carries
// (agent.go's [Agent.drainSteering]). That is the only moment a direction stops
// being pending, and the distinction matters: a line accepted after the worker's
// last drain was never in front of the model, and marking it read there would
// let the landing publish over words nobody has read.
func (a *Agent) markDirectionsCarried(ids []uint64) {
	if len(ids) == 0 || a.config.tasker == nil || a.config.taskID == 0 {
		return
	}
	node := a.config.tasker.node(a.config.taskID)
	if node == nil {
		return
	}
	node.graph.mu.Lock()
	node.assignment.markCarried(ids)
	node.graph.mu.Unlock()
}

// reviseAssignment applies one of the person's directions and answers the new
// version. A settled node refuses: what a landed task was judged against is a
// fact about what happened, and a revision after the fact would rewrite it.
func (n *TaskNode) reviseAssignment(id, expected uint64, edit assignmentEdit) (uint64, error) {
	if n == nil || n.graph == nil {
		return 0, errNoAssignmentToMove
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.state.settled() {
		return 0, errAssignmentSettled
	}
	// AND NOTHING MOVES ONCE A LANDING HAS TAKEN THE BOUNDARY. From that instant
	// the work being published is fixed, and a revision squeezed in behind it
	// would leave a merge already going out labelled with a condition it was
	// never checked against.
	if n.publishing {
		return 0, errPublishing
	}
	return n.assignment.revise(id, expected, edit, time.Now())
}

// assignmentNow is the snapshot every reader of "what is this work for" takes.
func (n *TaskNode) assignmentNow() assignmentNow {
	if n == nil || n.graph == nil {
		return assignmentNow{}
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.assignmentLocked()
}

// assignmentLocked is the same fold for callers already holding the graph.
func (n *TaskNode) assignmentLocked() assignmentNow {
	return n.assignment.effective(n.brief, n.spec.deliverable, n.spec.acceptance)
}

// revisionNote is what a reader who is JUDGING the work is told about a goal
// that moved: which revision governs, the person's own words for each one, and
// that the earlier done-condition no longer counts. It is empty on an unrevised
// node, which is nearly all of them.
func (n *TaskNode) revisionNote() string {
	if n == nil || n.graph == nil {
		return ""
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.assignment.version == 0 {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "THE PERSON CHANGED THIS WORK WHILE IT RAN. The acceptance above is revision %d and it is the ONE you judge against; what this task was first given no longer governs, and work that still satisfies the earlier condition is not owed for it.\n",
		n.assignment.version)
	out.WriteString("Their own words, in order:\n")
	for _, revision := range n.assignment.revisions {
		fmt.Fprintf(&out, "  %d. %s\n", revision.version, revision.said)
	}
	out.WriteString("If the acceptance above does not say what those words say, judge that as the work failing to carry out the person's direction.")
	return out.String()
}

// checkAt records the assignment version the work about to be checked was done
// against, which is what the publication boundary compares.
func (n *TaskNode) checkAt(version uint64) {
	if n == nil || n.graph == nil {
		return
	}
	n.graph.mu.Lock()
	n.checkedAt = version
	n.graph.mu.Unlock()
}

// assignmentVersion is the number a landing compares and a record keeps.
func (n *TaskNode) assignmentVersion() uint64 {
	if n == nil || n.graph == nil {
		return 0
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.assignment.version
}

// ── the record ──────────────────────────────────────────────────────────────

// recordedAssignment copies the overlay onto the checkpoint, and answers nil
// for a node nobody has said anything to — which keeps the row a session writes
// byte-identical to what it wrote before this existed.
func recordedAssignment(a taskAssignment) *assignmentRecord {
	if a.version == 0 && len(a.directions) == 0 {
		return nil
	}
	record := &assignmentRecord{
		Version:     a.version,
		Deliverable: a.deliverable,
		Acceptance:  a.acceptance,
		Next:        a.next,
	}
	for _, revision := range a.revisions {
		record.Revisions = append(record.Revisions, revisionRecord{
			Version:     revision.version,
			Direction:   revision.directionID,
			Said:        revision.said,
			Work:        revision.work,
			Deliverable: revision.deliverable,
			Acceptance:  revision.acceptance,
			At:          revision.at,
		})
	}
	for _, said := range a.directions {
		record.Directions = append(record.Directions, directionRecord{
			ID:      said.id,
			Words:   said.words,
			At:      said.at,
			From:    said.from.String(),
			State:   string(said.state),
			Version: said.version,
		})
	}
	return record
}

// restoredAssignment reads the overlay back. A record from a build that did not
// write one restores the zero value, and the node's effective assignment is the
// admitted spec — which is what it was.
func restoredAssignment(record *assignmentRecord) taskAssignment {
	if record == nil {
		return taskAssignment{}
	}
	restored := taskAssignment{
		version:     record.Version,
		deliverable: record.Deliverable,
		acceptance:  record.Acceptance,
		next:        record.Next,
	}
	for _, revision := range record.Revisions {
		restored.revisions = append(restored.revisions, assignmentRevision{
			version:     revision.Version,
			directionID: revision.Direction,
			said:        revision.Said,
			work:        revision.Work,
			deliverable: revision.Deliverable,
			acceptance:  revision.Acceptance,
			at:          revision.At,
		})
	}
	for _, said := range record.Directions {
		restored.directions = append(restored.directions, taskDirection{
			id:      said.ID,
			words:   said.Words,
			at:      said.At,
			from:    directionFromWord(said.From),
			state:   directionStateWord(said.State),
			version: said.Version,
		})
		// A record written by a build with a different idea of the counter still
		// leaves this one able to mint ids nothing else holds.
		if said.ID > restored.next {
			restored.next = said.ID
		}
	}
	return restored
}

// directionFromWord reads a speaker back. Anything a reader cannot recognise —
// a word from a newer build, a corrupted row — is taken as another agent's,
// which is the reading that grants no authority.
func directionFromWord(word string) directionFrom {
	if word == directionFromPerson.String() {
		return directionFromPerson
	}
	return directionFromAgent
}

// directionStateWord reads a state back, defaulting to read: an unrecognisable
// state must not be able to hold a landing forever.
func directionStateWord(word string) directionState {
	switch directionState(word) {
	case directionPending:
		return directionPending
	case directionApplied:
		return directionApplied
	}
	return directionRead
}

// ── the publication boundary ────────────────────────────────────────────────
//
// PUBLISHING IS THIS HARNESS'S ONE UNDOABLE STEP — merging the work onto the
// person's branch, closing `done`, telling the parent. It is not the only thing
// a task does that cannot be taken back: a task runs commands, and what those
// did to the world is beyond anything here. What this boundary governs is the
// publication, and the question "has the assignment moved?" has to be asked
// once, at an instant, and answered for good. Asking it "just before the merge"
// is not that: a direction can arrive between the question and the git call, and
// the work then lands as done against an assignment the person has changed.
//
// So there is a boundary rather than a check. [TaskNode.claimPublication] takes
// the graph's lock, reads the unread directions and — if there are none — marks
// the node as publishing in the same locked step. From that instant a direction
// arriving through any door is admitted to the record and is told it belongs to
// the next round ([TaskNode.heardDirection] answers false). Nothing is held
// across the git work: the claim is made, the lock is released, the merge runs,
// and the boundary is released when the node has settled.
//
// THIS MAKES NOTHING ATOMIC. What it makes deterministic is which side of the
// line a direction fell on, and what the person is told about it.

// publicationClaim is the boundary's whole answer, taken in one locked step so
// that no reader has to ask a second question and get a second instant's answer.
type publicationClaim struct {
	// version is the assignment this landing is publishing.
	version uint64
	// granted says the landing may go ahead. A refused claim marks nothing: the
	// node is untouched and the caller takes the work round again.
	granted bool
	// unread is the person's words this landing could not read. A claim is never
	// granted while there are any: the caller either takes the work round again
	// or, once a run has been round as often as it may be, settles the node as
	// unfinished with its branch kept and nothing merged ([directedRoundLimit],
	// task_ledger.go).
	unread []taskDirection
	// again says the node may take another attempt with those words. False with
	// unread words is the end of the line for this run.
	again bool
	// stale says the assignment moved between the check and this landing — the
	// work in hand was judged against a condition that is no longer the one in
	// force, so it must not be published as meeting it.
	stale bool
}

// claimPublication is the linearization point. Either the person's words are in
// before the claim, and then nothing publishes, or they are after it, and then
// they belong to the round after this one.
func (n *TaskNode) claimPublication() publicationClaim {
	if n == nil || n.graph == nil {
		return publicationClaim{granted: true}
	}
	n.graph.mu.Lock()
	claim := publicationClaim{version: n.assignment.version}
	claim.unread = n.assignment.pendingFrom(directionFromPerson)
	// THE CHECK'S OWN READING IS PART OF THE BOUNDARY. What is about to be
	// published was worked and judged at one version of the assignment; if the
	// assignment has moved since, this work meets a condition nobody is asking
	// for any more, whoever moved it.
	claim.stale = n.checkedAt != n.assignment.version
	if len(claim.unread) > 0 || claim.stale {
		claim.again = n.directedRounds < directedRoundLimit && !n.graph.quitting
		n.graph.mu.Unlock()
		return claim
	}
	claim.granted = true
	n.publishing = true
	barrier := n.graph.publishBarrier
	n.graph.mu.Unlock()
	// The barrier is nil in the product. It exists so a test can put a direction
	// on the far side of a boundary that is otherwise an instant wide, which is
	// the one ordering no amount of timing can reproduce.
	if barrier != nil {
		barrier(n)
	}
	return claim
}

// unreadDirectionsNote is what a landing that STOPPED says about the words it
// could not take up: the node has been round as often as one run may be, so the
// work stays on its branch, nothing merges, and the person is told what is
// waiting and how to take it further.
func unreadDirectionsNote(unread []taskDirection) string {
	said := make([]string, 0, len(unread))
	for _, one := range unread {
		if one.from == directionFromPerson {
			said = append(said, one.words)
		}
	}
	if len(said) == 0 {
		return ""
	}
	return "this did not land: you have said something this work has not taken up (" +
		strings.Join(said, " / ") + "). Its work is kept on its branch — continue this task to have it taken up"
}

// taskRunAgain is NOT A STATE A NODE IS EVER IN. It is the answer a landing
// gives when the person's words arrived before it could claim the boundary, and
// [Agent.runTaskNode] is the only reader: it turns this into one transition from
// running to queued, carrying the directions, without the node ever passing
// through a final state.
//
// IT MUST NOT REACH A RECORD, and the reason it is written as a state at all is
// that the landing roads already answer in this type — three functions deep,
// each of them `return a.landFinished(…)` — and a second return value on all of
// them would be a wider change to say the same thing.
const taskRunAgain TaskState = "run-again"

// directedRoundLimit bounds how many times one node may be sent round again by
// the person's own corrections inside a single run. It is not a suspicion of the
// person: it is the same bound every other loop in this package has, so that a
// pathological sequence — a direction admitted at every landing, forever —
// cannot spend without end. Past it the work lands and the words wait for a
// continue, which is the ordinary door for a settled task.
const directedRoundLimit = 3

// runAgainForDirections is the ONE TRANSITION a held direction can force, and it
// is deliberately not a settle-and-reopen.
//
// A node that settled first would have published a terminal state that is not
// true — `done` closed, the parent woken with an answer that is about to be
// re-done, this node's own children cut — and then contradicted it a moment
// later. So the node never leaves the graph: it goes from running straight back
// to queued, with what the last attempt produced as its finding and the person's
// unread words carried into it, and the frontier starts the next attempt.
//
// ONE WRITER AT A TIME. This is called from [Agent.runTaskNode] after the run
// goroutine has finished with the node — its worker is closed, its parts are
// stopped, its working copy is released to the next attempt — so the frontier
// pass at the end is starting work nothing else is doing.
func (g *TaskGraph) runAgainForDirections(node *TaskNode) bool {
	if g == nil || node == nil {
		return false
	}
	g.mu.Lock()
	if node.graph != g || g.nodes[node.id] != node || g.quitting {
		g.mu.Unlock()
		return false
	}
	if node.directedRounds >= directedRoundLimit {
		g.mu.Unlock()
		return false
	}
	node.directedRounds++
	// THE WORDS STAY PENDING UNTIL A REQUEST CARRIES THEM. They are written into
	// the finding the next attempt opens with and listed on the node, and it is
	// that attempt's opening request that marks them read (task_child_run.go's
	// [childRun.open]). Marked here they would be read by a node that is merely
	// QUEUED — and a process that died before the attempt started would come back
	// with the person's correction already counted as answered.
	waiting := node.assignment.pendingFrom(directionFromPerson)
	waiting = append(waiting, node.assignment.pendingFrom(directionFromAgent)...)
	node.carried = directionIDs(waiting)
	node.finding = composeDirectedFinding(node.deliveredLocked(), waiting)
	node.publishing = false
	node.continuing = true
	node.state = TaskQueued
	node.claimed = false
	node.stopped = false
	node.ending = ""
	node.noted = false
	node.queuedSaid = false
	node.held = ""
	node.checked = ""
	node.repairs = 0
	node.blockedBy = ""
	node.ctx = nil
	node.cancel = nil
	// THE LANE GOES BACK because the goroutine that held it is on its way out,
	// exactly as it does on the settling road ([TaskGraph.handBackSlotLocked]).
	// `done` is left alone and untouched: it is closed when this node reaches a
	// final state, and this is the transition that says it has not.
	g.handBackSlotLocked(node)
	g.mu.Unlock()

	g.checkpoint()
	g.announce(node)
	g.runFrontier()
	return true
}

// directionIDs is the receipts of a set of directions.
func directionIDs(said []taskDirection) []uint64 {
	ids := make([]uint64, 0, len(said))
	for _, one := range said {
		ids = append(ids, one.id)
	}
	return ids
}

// carriedDirections is what this attempt's opening request will carry: the words
// written into its finding that no request has put in front of a model yet.
func (n *TaskNode) carriedDirections() []uint64 {
	if n == nil || n.graph == nil {
		return nil
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	ids := make([]uint64, len(n.carried))
	copy(ids, n.carried)
	return ids
}

// openingCarried marks the finding's own directions read, and is called by the
// attempt that has just made its opening request with them in it. The list is
// cleared in the same step so a second request cannot claim to have carried them
// again.
func (n *TaskNode) openingCarried() {
	if n == nil || n.graph == nil {
		return
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	n.assignment.markCarried(n.carried)
	n.carried = nil
}

// composeDirectedFinding is what the next attempt is handed: what the last one
// produced, and under it the person's own unread words, labelled as theirs and
// as unapplied. The next worker decides whether they are a correction to the
// goal — in which case it revises, citing them — or a remark about the work.
func composeDirectedFinding(delivered string, carried []taskDirection) string {
	return withReport(composeContinueFinding(delivered, "", 0), directionBlock(carried))
}
