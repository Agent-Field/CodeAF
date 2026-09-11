package session

import "testing"

// ── one account for the process: another conversation's work is not this one's ──
//
// Two conversations share one process — tabs share the binary — and /proc
// cannot say which of them started which compiler: a reading that carries a
// build carries it for the whole tree. Both tests below state one machine and
// two graphs over it and ask the oldest question in accounting: whose work is
// this? A second conversation's build must not become this one's per-node
// weight, and its visible memory must not stand in for this one's reservation
// (#907).

// ANOTHER CONVERSATION'S BUILD IS NOT THIS ONE'S PER-NODE WEIGHT. This
// conversation has two parts running and neither is holding anything yet; a
// second conversation in the same process starts one node whose build shows
// six core-shares in the tree every reading reads. Three lanes are out in the
// process and twelve gibibytes are visible above rest, so what a lane has been
// seen to weigh is four — and that is what this conversation's next fan is
// judged at. Divided over this conversation's own two lanes alone, the same
// reading says six core-shares a lane, and the measured footprint only rises,
// so that one reading would have narrowed every later fan here for the rest of
// the session.
func TestAnotherConversationsBuildIsNotThisOnesPerNodeWeight(t *testing.T) {
	// What the reading says a lane weighs, over the whole process's lanes and
	// over this conversation's own — the wrong answer is the defect's.
	const (
		build       = 6 * testFootprintMB
		overProcess = build / 3
		overMine    = build / 2
	)
	process := newLaneAccount()
	machine := &fakeMachine{}
	machine.set(roomFor(12), true)
	mine, _ := fanGraphIn(t, machine, process)

	// A reading with nothing running anywhere in the process, which is what
	// fixes the tree's own weight for the visible half to count above.
	mine.runFrontier()

	// This conversation starts two parts, and they hold nothing visible yet.
	parts := admitChildren(mine, 0, 2)
	mine.runFrontier()
	if running, _ := partition(mine, parts); len(running) != 2 {
		t.Fatalf("%d of this conversation's parts started, want both", len(running))
	}

	// A second conversation in the same process starts one node of its own.
	theirs, _ := fanGraphIn(t, machine, process)
	theirNode := admitChildren(theirs, 0, 1)
	theirs.runFrontier()
	if running, _ := partition(theirs, theirNode); len(running) != 1 {
		t.Fatal("the second conversation's node did not start")
	}
	if lanes := process.running(); lanes != 3 {
		t.Fatalf("the process counts %d lanes, want the 3 its two conversations have out", lanes)
	}

	// And its build shows: six core-shares above rest, visible to every reading
	// and attributable to nobody in particular.
	busy := roomFor(12)
	busy.availableMB -= build
	busy.workMB += build
	machine.set(busy, true)

	// This conversation reads the machine while that build is under way.
	mine.runFrontier()
	mine.governor.mu.Lock()
	footprint := mine.governor.footprintLocked()
	mine.governor.mu.Unlock()
	if footprint == overMine {
		t.Fatalf("a lane is measured at %d MiB, which is the other conversation's build divided by this conversation's own lanes", footprint)
	}
	if footprint != overProcess {
		t.Fatalf("a lane is measured at %d MiB, want the %d the process's three lanes hold between them", footprint, overProcess)
	}
}

// ANOTHER CONVERSATION'S MEMORY DOES NOT STAND IN FOR THIS ONE'S RESERVATION.
// The machine has three core-shares of room above the floor, one lane of this
// conversation is running and the neighbour's node is visibly holding a share.
// Three more parts are asked for here, and the machine has room for two of
// them: the fourth lane of the process is the one it cannot carry. Read per
// conversation, the neighbour's visible share was counted as covering one of
// this conversation's own reservations and all three started, which is a part
// running in memory the machine does not have.
func TestAnotherConversationsMemoryDoesNotCoverThisOnesReservation(t *testing.T) {
	process := newLaneAccount()
	machine := &fakeMachine{}
	machine.set(roomFor(3), true)
	mine, log := fanGraphIn(t, machine, process)
	mine.runFrontier()

	// One part of this conversation is already running before the neighbour's
	// memory shows, so the reading that carries it is not mistaken for what
	// this process holds at rest.
	first := admitChildren(mine, 0, 1)
	mine.runFrontier()
	if running, _ := partition(mine, first); len(running) != 1 {
		t.Fatal("this conversation's first part did not start")
	}

	theirs, _ := fanGraphIn(t, machine, process)
	theirNode := admitChildren(theirs, 0, 1)
	theirs.runFrontier()
	if running, _ := partition(theirs, theirNode); len(running) != 1 {
		t.Fatal("the second conversation's node did not start")
	}

	// The second conversation's node is carrying one core's share, visibly.
	busy := roomFor(3)
	busy.availableMB -= testFootprintMB
	busy.workMB += testFootprintMB
	machine.set(busy, true)

	// Three more parts are asked for here: two start and one is held, which is
	// the room left once the neighbour's lane is reserved for as well.
	more := admitChildren(mine, 0, 3)
	running, held := partition(mine, append(first, more...))
	if len(running) != 3 {
		t.Fatalf("%d of this conversation's parts run beside the neighbour's node, want 3: the fourth lane of the process is one the machine cannot carry", len(running))
	}
	if len(held) != 1 {
		t.Fatalf("%d parts are held, want the one the machine has no room for", len(held))
	}
	for _, id := range held {
		if notice, said := log.last(id); !said || notice.Waiting != waitingMachineBusy {
			t.Fatalf("held part %d says %q, want %q", id, notice.Waiting, waitingMachineBusy)
		}
	}
}
