package head

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// waitForTitles polls until every named room has a title, or gives up. The
// backfill is deliberately off the caller's goroutine — a launch may not wait
// for it — so a test that wants to see its effect waits for the effect rather
// than for the pass.
func waitForTitles(t *testing.T, graph *store.Store, rooms ...string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		named := 0
		for _, room := range rooms {
			if roomTitleOf(t, graph, room) != "" {
				named++
			}
		}
		if named == len(rooms) {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("only %d of %d rooms were named in time", named, len(rooms))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The reported bug, from the outside: rooms that were talked in before the
// clerk existed wear "untitled room" forever, because the only moment that ever
// named a room was the end of a turn in it. One launch gives them that moment.
func TestTheBackfillNamesRoomsThatWereTalkedInBeforeTheScribeExisted(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "old-1", "audit last quarter's billing code", "I'll go through the billing package.")
	seedExchange(t, graph, "old-2", "why is the deploy failing", "The VPN has to be up first.")

	// The answers are handed out in call order, so they also pin the ORDER the
	// rooms are taken in: the most recently active first, because with a cap on
	// how many can be named at all, the rooms a reader is most likely looking at
	// are the ones worth spending it on.
	scribe := &scribeClientRecorder{answers: []string{"Deploy failure", "Billing code audit"}}
	New(scribe, graph).WithRoomNaming(true).BackfillRoomNames(context.Background())

	waitForTitles(t, graph, "old-1", "old-2")
	if title := roomTitleOf(t, graph, "old-1"); title != "Billing code audit" {
		t.Fatalf("the older room is called %q", title)
	}
	if title := roomTitleOf(t, graph, "old-2"); title != "Deploy failure" {
		t.Fatalf("the newer room is called %q", title)
	}
}

// A room with words in only one direction is not a conversation yet, and a room
// somebody named themselves is theirs. Neither costs a call: naming the first
// would name the question rather than the room, and renaming the second would
// take away the one thing in the rail the person wrote.
func TestTheBackfillLeavesHalfRoomsAndNamedRoomsAlone(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "asked-only", "are you there", "")
	seedExchange(t, graph, "mine", "audit the billing code", "On it.")
	if _, err := graph.RenameSession("mine", "My own name for it"); err != nil {
		t.Fatal(err)
	}

	scribe := &scribeClientRecorder{answers: []string{"Something Else"}}
	New(scribe, graph).WithRoomNaming(true).BackfillRoomNames(context.Background())

	// Nothing to do means nothing spent. Give the goroutine a moment to prove it
	// rather than proving only that it had not started yet.
	time.Sleep(200 * time.Millisecond)
	if scribe.count() != 0 {
		t.Fatalf("the backfill spent %d calls on rooms it should not touch", scribe.count())
	}
	if title := roomTitleOf(t, graph, "mine"); title != "My own name for it" {
		t.Fatalf("a room the person named was renamed to %q", title)
	}
	if title := roomTitleOf(t, graph, "asked-only"); title != "" {
		t.Fatalf("a room with nothing answered in it was named %q", title)
	}
}

// The cap is a money bound: every row is a provider call nobody asked for, so a
// store with a year of rooms in it must not turn one launch into a hundred
// calls. What is left over keeps its turn for the next launch.
func TestTheBackfillNamesAtMostACapfulPerLaunch(t *testing.T) {
	graph := openHeadStore(t)
	for index := 0; index < roomBackfillCap+4; index++ {
		room := fmt.Sprintf("room-%02d", index)
		seedExchange(t, graph, room, "look at the "+room+" thing", "Looking at it.")
	}

	answers := make([]string, 0, roomBackfillCap+4)
	for index := 0; index < roomBackfillCap+4; index++ {
		answers = append(answers, fmt.Sprintf("Room %02d", index))
	}
	scribe := &scribeClientRecorder{answers: answers}
	New(scribe, graph).WithRoomNaming(true).BackfillRoomNames(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for scribe.count() < roomBackfillCap && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if scribe.count() != roomBackfillCap {
		t.Fatalf("the backfill made %d calls, want exactly the cap of %d", scribe.count(), roomBackfillCap)
	}
	named := 0
	for index := 0; index < roomBackfillCap+4; index++ {
		if roomTitleOf(t, graph, fmt.Sprintf("room-%02d", index)) != "" {
			named++
		}
	}
	if named != roomBackfillCap {
		t.Fatalf("%d rooms were named, want the cap of %d", named, roomBackfillCap)
	}
}

// It never blocks the launch. The call returns before the first name is bought,
// which is the whole reason it is safe to put in front of Serve.
func TestTheBackfillReturnsBeforeItHasNamedAnything(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "old", "audit the billing code", "On it.")

	scribe := &scribeClientRecorder{answers: []string{"Billing code audit"}}
	started := time.Now()
	New(scribe, graph).WithRoomNaming(true).BackfillRoomNames(context.Background())
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("the launch waited %s on the backfill", elapsed)
	}
	waitForTitles(t, graph, "old")
}

// A window that draws no list of rooms buys no names — the same switch the
// post-turn clerk obeys, for the same reason: nobody would read them.
func TestTheBackfillIsOffWhereRoomNamingIs(t *testing.T) {
	graph := openHeadStore(t)
	seedExchange(t, graph, "old", "audit the billing code", "On it.")

	scribe := &scribeClientRecorder{answers: []string{"Billing code audit"}}
	New(scribe, graph).BackfillRoomNames(context.Background())

	time.Sleep(200 * time.Millisecond)
	if scribe.count() != 0 {
		t.Fatalf("a headless window spent %d naming calls", scribe.count())
	}
}
