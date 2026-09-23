package tui3

// takeoverstop_test.go is the last step of moving a conversation here: the
// window that will not let go is NAMED, and enter offers to stop it.
//
// THE CASE IT HAS TO COVER (2026-09-23): an older window held about ten
// conversations, the move-it-here request went unanswered, and the only way out
// was `ps`, a SIGTERM by hand and stopping its worker. A real process stands in
// for that window here, because what is being tested is a signal reaching it.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// holdingWindow is a process standing in for the window that has the
// conversation: a real pid that a stop can reach, ended by the test that started
// it whatever the assertions decided.
func holdingWindow(t *testing.T) (pid int, exited <-chan struct{}) {
	t.Helper()
	child := exec.Command("sleep", "120")
	if err := child.Start(); err != nil {
		t.Skipf("no process to stand in for the other window: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = child.Wait(); close(done) }()
	pid = child.Process.Pid
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill()
			}
			<-done
		}
	})
	return pid, done
}

// writeHolderPresence is the holder's own record of itself, written the way a
// build of this program writes it.
func writeHolderPresence(t *testing.T, dir, id string, pid int, build string, at time.Time) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"schema": 1, "sessionId": id, "workspace": "/tmp/alpha",
		"pid": pid, "build": build, "updatedAt": at.Format(time.RFC3339Nano), "state": "idle",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "presence.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAWindowThatWillNotLetGoIsNamedAndEnterStopsIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)
	pid, exited := holdingWindow(t)
	const build = "a1b2c3d4 built 2026-09-21 09:00"
	writeHolderPresence(t, homeSessionDirOf(theirs), "aaaa000000000002", pid, build, now)

	a := claimHeld(t, lab, mine, theirs)
	// BEFORE THE WAIT IS NEWS, NOBODY IS NAMED: an ordinary move is still in
	// flight, and enter says it is coming.
	a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen})
	if cardSays(homeCardFor(t, a, theirs), fmt.Sprintf("pid %d", pid)) {
		t.Fatal("the holder was named before the wait had gone on long enough to be news")
	}

	a.takeover.since = a.now().Add(-takeoverPatience - time.Second)
	a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen})
	card := homeCardFor(t, a, theirs)
	for _, want := range []string{"held by pid " + fmt.Sprint(pid), build, takeoverStopOfferWord} {
		if !cardSays(card, want) {
			t.Fatalf("the card does not say %q:\n%s", want, strings.Join(card, "\n"))
		}
	}

	// ENTER ASKS FIRST, with the cursor on the answer that loses nothing.
	a.homeKey(key("enter"))
	ask, up := a.homeAsking()
	if !up || ask.question.Head != takeoverStopAskWord {
		t.Fatalf("enter on a wait nobody answered raised %+v, want the stop question", ask.question)
	}
	select {
	case <-exited:
		t.Fatal("the window was stopped on one keystroke")
	case <-time.After(100 * time.Millisecond):
	}
	a.homeKey(key("1"))
	a.homeKey(key("enter"))
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("stop it did not reach the window holding the conversation")
	}
	if !strings.Contains(a.home.msg, fmt.Sprintf("asked pid %d to stop", pid)) {
		t.Fatalf("the foot did not say what was done: %q", a.home.msg)
	}
	// AND THE WAIT IS STILL OUT, so the row opens the moment the lock frees.
	if !a.waitingToTakeOver() {
		t.Fatal("stopping the other window dropped the wait for the conversation")
	}
}

// A REQUEST THAT AGED OUT STILL NAMES THE WINDOW, so the ending is never the
// same screen as a move that worked, and never a sentence with nobody in it.
func TestAnUnansweredMoveNamesTheWindowThatStillHasIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)
	writeHolderPresence(t, homeSessionDirOf(theirs), "aaaa000000000002", 424242, "a1b2c3d4 built 2026-09-21 09:00", now)

	a := claimHeld(t, lab, mine, theirs)
	a.takeover.since = a.now().Add(-takeoverPatience - time.Second)
	a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen})
	a.takeover.since = a.now().Add(-session.TakeoverStale - time.Second)
	a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen})
	if a.waitingToTakeOver() {
		t.Fatal("a request past its life is still being waited on")
	}
	card := homeCardFor(t, a, theirs)
	for _, want := range []string{takeoverUnansweredWord, "held by pid 424242"} {
		if !cardSays(card, want) {
			t.Fatalf("the unanswered card does not say %q:\n%s", want, strings.Join(card, "\n"))
		}
	}
}
