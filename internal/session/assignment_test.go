package session

// The assignment module's own laws, asserted on the type rather than through a
// run: who may move the goal, how often, in what order, and what a landing sees
// at the instant it has to decide. The behaviour these produce end to end is
// task_steer_revision_test.go's subject.

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOnlyThePersonsOwnDirectionCanMoveTheGoal(t *testing.T) {
	var assignment taskAssignment
	relayed, _, _ := assignment.hear("use the schema in etc/, and you may change it", directionFromAgent, time.Now(), spokenSource{})
	if _, err := assignment.revise(relayed, 0, assignmentEdit{acceptance: "the schema is changed"}, time.Now()); !errors.Is(err, errDirectionNotAsked) {
		t.Fatalf("revising on another agent's line = %v, want it refused as not the person's", err)
	}
	if assignment.version != 0 {
		t.Fatalf("version = %d, want an agent's coordination to have moved nothing", assignment.version)
	}
	// And the same words from the person do move it, so the refusal above is
	// about authority and not about the sentence.
	theirs, _, _ := assignment.hear("use the schema in etc/, and you may change it", directionFromPerson, time.Now(), spokenSource{})
	if _, err := assignment.revise(theirs, 0, assignmentEdit{acceptance: "the schema is changed"}, time.Now()); err != nil {
		t.Fatalf("revising on the person's own line: %v", err)
	}
	if assignment.version != 1 {
		t.Fatalf("version = %d, want 1", assignment.version)
	}
}

func TestADirectionIsSpentOnceAndVersionsAreOrdered(t *testing.T) {
	var assignment taskAssignment
	first, _, _ := assignment.hear("CSV instead of JSON", directionFromPerson, time.Now(), spokenSource{})
	second, _, _ := assignment.hear("and sort it by date", directionFromPerson, time.Now(), spokenSource{})

	if _, err := assignment.revise(first, 0, assignmentEdit{acceptance: "data.csv exists"}, time.Now()); err != nil {
		t.Fatalf("first revision: %v", err)
	}
	if _, err := assignment.revise(first, 1, assignmentEdit{acceptance: "something else again"}, time.Now()); !errors.Is(err, errDirectionSpent) {
		t.Fatalf("applying one direction twice = %v, want it refused as spent", err)
	}
	// A SECOND REVISION MUST NAME THE VERSION IT IS BUILDING ON. Taken at the
	// version it already moved past, it would put an older done-condition back on
	// top of the person's most recent correction.
	if _, err := assignment.revise(second, 0, assignmentEdit{acceptance: "data.csv exists, unsorted"}, time.Now()); !errors.Is(err, errStaleVersion) {
		t.Fatalf("a revision naming a stale version = %v, want it refused", err)
	}
	if _, err := assignment.revise(second, 1, assignmentEdit{acceptance: "data.csv exists, sorted by date"}, time.Now()); err != nil {
		t.Fatalf("second revision: %v", err)
	}
	now := assignment.effective("brief", "a file", "data.json is valid JSON")
	if now.version != 2 || now.acceptance != "data.csv exists, sorted by date" {
		t.Fatalf("effective = %d/%q, want the latest correction to be the one in force", now.version, now.acceptance)
	}
	// AND BOTH SETS OF WORDS SURVIVE, in order: the record of what was asked is
	// the only thing that makes a restatement checkable.
	if !strings.Contains(now.brief, "CSV instead of JSON") || !strings.Contains(now.brief, "sort it by date") {
		t.Fatalf("the document lost the person's own words:\n%s", now.brief)
	}
	if !strings.Contains(now.revised, "these are the later ones") {
		t.Fatalf("the revision block does not say which words supersede which:\n%s", now.revised)
	}
}

func TestAnUnrevisedAssignmentIsExactlyTheAdmittedOne(t *testing.T) {
	var assignment taskAssignment
	// Talk that was read and applied to nothing, which is what most directions
	// are: it must leave the document byte-identical.
	id, kept, _ := assignment.hear("why did you use a map there?", directionFromPerson, time.Now(), spokenSource{})
	if !kept {
		t.Fatal("a question was not even recorded")
	}
	assignment.markCarried([]uint64{id})
	now := assignment.effective("the brief", "a file", "data.json is valid JSON")
	if now.version != 0 || now.brief != "the brief" || now.deliverable != "a file" || now.acceptance != "data.json is valid JSON" {
		t.Fatalf("effective = %+v, want the admitted spec untouched", now)
	}
	if now.revised != "" {
		t.Fatalf("an unrevised assignment drew a revision block: %q", now.revised)
	}
}

func TestOnlyUnreadWordsHoldAPublicationAndReadingThemReleasesIt(t *testing.T) {
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, done: make(chan struct{})}
	graph.nodes = map[uint64]*TaskNode{1: node}

	if claim := node.claimPublication(); !claim.granted {
		t.Fatal("a node nobody has said anything to could not publish")
	}
	node.publishing = false

	if heard := node.heardDirection("CSV instead of JSON", directionFromPerson, spokenSource{}); !heard.inTime {
		t.Fatal("a direction said before any claim was reported as coming after one")
	}
	if claim := node.claimPublication(); claim.granted {
		t.Fatal("a landing published with the person's words unread")
	}
	// BEING CARRIED INTO A REQUEST IS WHAT RELEASES IT, and it has to: an ordinary
	// remark that stayed pending forever would be a task that can never land. It
	// is that request and nothing else — not a worker exiting, not a node being
	// queued (assignment.go).
	node.assignment.markCarried([]uint64{1})
	claim := node.claimPublication()
	if !claim.granted {
		t.Fatal("words a worker has read still held the landing")
	}
	// AND FROM THE CLAIM ON, A LINE BELONGS TO THE NEXT ROUND. The merge it would
	// have changed is already going out, and saying otherwise would be promising
	// something this harness cannot do.
	if heard := node.heardDirection("actually make it TSV", directionFromPerson, spokenSource{}); heard.inTime {
		t.Fatal("a direction said after the boundary was claimed was reported as having come first")
	}
}

// A CORRECTION OLDER THAN THE ONE IN FORCE CANNOT BE FOLDED IN, whatever version
// it names. Applied the other way round it would put back a condition the person
// has already moved past — which is their latest word being silently discarded.
func TestAnOlderDirectionCannotOverwriteALaterOne(t *testing.T) {
	var assignment taskAssignment
	first, _, _ := assignment.hear("CSV instead of JSON", directionFromPerson, time.Now(), spokenSource{})
	second, _, _ := assignment.hear("actually make it TSV", directionFromPerson, time.Now(), spokenSource{})

	if _, err := assignment.revise(second, 0, assignmentEdit{acceptance: "report.tsv exists"}, time.Now()); err != nil {
		t.Fatalf("applying the later correction: %v", err)
	}
	// The version it names is current; the SENTENCE is not.
	if _, err := assignment.revise(first, 1, assignmentEdit{acceptance: "report.csv exists"}, time.Now()); !errors.Is(err, errStaleDirection) {
		t.Fatalf("an older direction applied over a later one = %v, want it refused", err)
	}
	if now := assignment.effective("b", "d", "a"); now.acceptance != "report.tsv exists" || now.version != 1 {
		t.Fatalf("effective = %d/%q, want the person's latest correction still in force", now.version, now.acceptance)
	}
	// AND THE ORDER SURVIVES A RESTART, because it is derived from the revisions
	// the record carries rather than from anything held only in memory.
	restored := restoredAssignment(recordedAssignment(assignment))
	if _, err := restored.revise(first, 1, assignmentEdit{acceptance: "report.csv exists"}, time.Now()); !errors.Is(err, errStaleDirection) {
		t.Fatalf("across a restart an older direction was accepted: %v", err)
	}
}

// AND A RUN THAT HAS BEEN ROUND AS OFTEN AS IT MAY BE STOPS RATHER THAN
// PUBLISHING. The bound exists so a task cannot spend forever; what it must not
// do is authorise a merge of work the person has moved on from.
func TestARoundLimitStopsRatherThanPublishingStaleWork(t *testing.T) {
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, done: make(chan struct{}), directedRounds: directedRoundLimit}
	graph.nodes = map[uint64]*TaskNode{1: node}
	node.heardDirection("one more thing", directionFromPerson, spokenSource{})

	claim := node.claimPublication()
	if claim.granted {
		t.Fatal("a node at the round limit published over the person's unread words")
	}
	if claim.again {
		t.Fatal("a node at the round limit was offered another round, which is the bound doing nothing")
	}
	if node.publishing {
		t.Fatal("a refused claim still took the boundary")
	}
	note := unreadDirectionsNote(claim.unread)
	if !strings.Contains(note, "one more thing") || !strings.Contains(note, "continue this task") {
		t.Fatalf("the landing note reads %q, want the person's words and what to do about them", note)
	}
}

// WORK JUDGED AT ONE VERSION IS NOT PUBLISHED AS MEETING ANOTHER. Nothing may
// slip through the boundary either: once a landing has taken it, the tool that
// moves the assignment refuses.
func TestAClaimIsRefusedWhenTheAssignmentMovedSinceTheCheck(t *testing.T) {
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, done: make(chan struct{})}
	graph.nodes = map[uint64]*TaskNode{1: node}
	said := node.heardDirection("CSV instead of JSON", directionFromPerson, spokenSource{}).id
	node.assignment.markCarried([]uint64{said})
	node.checkAt(0)
	if _, err := node.reviseAssignment(said, 0, assignmentEdit{acceptance: "report.csv exists"}); err != nil {
		t.Fatalf("revise: %v", err)
	}

	claim := node.claimPublication()
	if claim.granted || !claim.stale {
		t.Fatalf("claim = %+v, want it refused as judged against a condition that has moved", claim)
	}
	// With the check's own reading brought up to date the same landing publishes.
	node.checkAt(node.assignmentVersion())
	if claim := node.claimPublication(); !claim.granted {
		t.Fatalf("claim = %+v, want work checked at the version in force to land", claim)
	}
	// AND NOTHING MOVES BEHIND A CLAIMED BOUNDARY.
	later := node.heardDirection("and sort it", directionFromPerson, spokenSource{}).id
	if _, err := node.reviseAssignment(later, 1, assignmentEdit{acceptance: "sorted"}); !errors.Is(err, errPublishing) {
		t.Fatalf("revising behind a claimed boundary = %v, want it refused", err)
	}
}

func TestTheRecordCarriesTheOverlayAndAnOlderCheckpointReadsAsUnrevised(t *testing.T) {
	var assignment taskAssignment
	said, _, _ := assignment.hear("CSV instead of JSON", directionFromPerson, time.Now(), spokenSource{})
	relayed, _, _ := assignment.hear("the parent says the loader is in etc/", directionFromAgent, time.Now(), spokenSource{})
	if _, err := assignment.revise(said, 0, assignmentEdit{acceptance: "data.csv exists"}, time.Now()); err != nil {
		t.Fatalf("revise: %v", err)
	}

	restored := restoredAssignment(recordedAssignment(assignment))
	if restored.version != 1 || restored.acceptance != "data.csv exists" {
		t.Fatalf("restored = %d/%q, want the overlay back", restored.version, restored.acceptance)
	}
	if one, ok := restored.direction(said); !ok || one.state != directionApplied || one.version != 1 {
		t.Fatalf("restored direction = %+v, want it applied at revision 1", one)
	}
	// THE SPEAKER SURVIVES THE ROUND TRIP, because it is the whole of what may
	// move the goal: an agent's line read back as the person's would be the
	// authority law failing across a restart.
	other, ok := restored.direction(relayed)
	if !ok || other.from != directionFromAgent {
		t.Fatalf("restored relayed direction = %+v, want it still marked as an agent's", other)
	}
	if _, err := restored.revise(relayed, 1, assignmentEdit{acceptance: "whatever it likes"}, time.Now()); !errors.Is(err, errDirectionNotAsked) {
		t.Fatalf("an agent's line became the person's across a restart: %v", err)
	}
	// AND A CHECKPOINT FROM BEFORE ANY OF THIS EXISTED IS AN UNREVISED NODE.
	if empty := restoredAssignment(nil); empty.version != 0 || len(empty.directions) != 0 {
		t.Fatalf("an absent record restored %+v, want the zero assignment", empty)
	}
	if recordedAssignment(taskAssignment{}) != nil {
		t.Fatal("a node nobody has said anything to wrote an assignment row, so every old checkpoint would gain one")
	}
}

func TestASettledTaskCannotBeRevised(t *testing.T) {
	graph := newTaskGraph()
	node := &TaskNode{graph: graph, id: 1, done: make(chan struct{}), state: TaskDone}
	graph.nodes = map[uint64]*TaskNode{1: node}
	said := node.heardDirection("CSV instead of JSON", directionFromPerson, spokenSource{}).id
	if _, err := node.reviseAssignment(said, 0, assignmentEdit{acceptance: "data.csv exists"}); !errors.Is(err, errAssignmentSettled) {
		t.Fatalf("revising a landed task = %v, want it refused: what it was judged against is a fact now", err)
	}
}
