package session

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// A REPLY TO A MANAGER NOBODY CAN REACH IS A WAKE KEPT, never one dropped: the
// first failure is said once, with the promise, and the wake is tried again
// until the door can open the manager.
func TestTeamRouseAWakeThatCannotBeDeliveredIsKeptUntilItCanBe(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	SetTeamResume(nil)
	t.Cleanup(func() { SetTeamResume(nil) })
	web := teamAgent(t, fixture, fixture.web, oneAnswer(1), nil)

	post := func(text string) {
		args, _ := json.Marshal(map[string]string{"to": "manager", "text": text})
		if said, failed, err := web.teamPostTool(context.Background(), args); err != nil || failed {
			t.Fatalf("team_post refused: %s %v", said, err)
		}
	}
	post("the header is fixed")
	said := waitEvent(t, fixture, "could not wake ◆")
	if !strings.Contains(said.Text, teamRouseKept) {
		t.Fatalf("the failure does not say the wake is kept: %q", said.Text)
	}
	if !rouseOwed(fixture.manager) {
		t.Fatal("the manager's wake was dropped")
	}
	post("and the footer")
	quietFor()
	if got := trafficEvents(t, fixture, "could not wake ◆"); len(got) != 1 {
		t.Fatalf("an owed wake was said %d times, want once", len(got))
	}

	// The door arrives (the app reopening its moved manager, or a host): the
	// next retry opens the manager, and the queue lets go.
	recorder := &recordedResume{}
	withTeamResume(t, recorder.open)
	deadline := time.Now().Add(5 * time.Second)
	for len(recorder.seen()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if opened := recorder.seen(); len(opened) == 0 || !strings.HasPrefix(opened[0], fixture.manager+"|") {
		t.Fatalf("the owed wake did not open the manager: %v", opened)
	}
	for rouseOwed(fixture.manager) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if rouseOwed(fixture.manager) {
		t.Fatal("a delivered wake stayed in the queue")
	}
}

// A MANAGER SOMETHING OPENS MEANWHILE IS LEFT TO WAKE ITSELF: the queue lets go
// without opening it a second time, and the opened manager is handed the
// replies that waited.
func TestTeamRouseAManagerOpenedElsewhereTakesTheWaitingReplies(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	SetTeamResume(nil)
	t.Cleanup(func() { SetTeamResume(nil) })
	// The manager was open once (a crew manager always has been), so its
	// cursor is kept in its folder, and then its window went away.
	before := teamAgent(t, fixture, fixture.manager, oneAnswer(1), nil)
	before.teamBoundary()
	before.Close()
	web := teamAgent(t, fixture, fixture.web, oneAnswer(1), nil)
	args, _ := json.Marshal(map[string]string{"to": "manager", "text": "the header is fixed"})
	if said, failed, err := web.teamPostTool(context.Background(), args); err != nil || failed {
		t.Fatalf("team_post refused: %s %v", said, err)
	}
	waitEvent(t, fixture, "could not wake ◆")

	completer := oneAnswer(1)
	teamAgent(t, fixture, fixture.manager, completer, nil)
	waitRequests(t, completer, 1)
	if said := userTextIn(completer.request(0)); !strings.Contains(said, "the header is fixed") {
		t.Fatalf("the reopened manager was not handed the reply that waited:\n%s", said)
	}
	deadline := time.Now().Add(5 * time.Second)
	for rouseOwed(fixture.manager) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if rouseOwed(fixture.manager) {
		t.Fatal("the queue kept a wake for a manager that is open")
	}
}

// AND A WAKE FOR SOMETHING THAT NO LONGER EXISTS IS GIVEN UP, said once.
func TestTeamRouseAWakeForAGoneTranscriptIsGivenUp(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	SetTeamResume(nil)
	t.Cleanup(func() { SetTeamResume(nil) })
	web := teamAgent(t, fixture, fixture.web, oneAnswer(1), nil)
	args, _ := json.Marshal(map[string]string{"to": "manager", "text": "done"})
	if _, failed, err := web.teamPostTool(context.Background(), args); err != nil || failed {
		t.Fatal("team_post refused")
	}
	waitEvent(t, fixture, "could not wake ◆")
	if err := os.Remove(fixture.manager); err != nil {
		t.Fatal(err)
	}
	said := waitEvent(t, fixture, "stopped trying to wake ◆")
	if !strings.Contains(said.Text, "no longer on this machine") {
		t.Errorf("the reason is not named: %q", said.Text)
	}
	if rouseOwed(fixture.manager) {
		t.Fatal("a wake for a gone transcript stayed queued")
	}
}
