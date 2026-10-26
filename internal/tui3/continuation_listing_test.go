package tui3

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

type continuationListing struct {
	mu    sync.Mutex
	row   chatlist.Row
	err   error
	reads int
}

func (s *continuationListing) Rows(context.Context) ([]chatlist.Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	return []chatlist.Row{s.row}, s.err
}

func (s *continuationListing) change(edit func(*chatlist.Row)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	edit(&s.row)
}

// TestStartupContinueKeepsOwnershipAndLaterListings proves that a startup take is
// reflected at once, that later listings land, and that the first send needs no card.
func TestStartupContinueKeepsOwnershipAndLaterListings(t *testing.T) {
	lab := newHomeLab(t)
	file := lab.session("project", heldCell, "Mine", lab.workspace("project"), time.Now())
	a := lab.app(file)
	a.width, a.height = 200, 70
	now := time.Now()
	a.clock = func() time.Time { return now }
	a.lastQuestionKey = now
	startupClock := a.homeGen
	src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Idle, time.Minute)}
	src.row.Released = true
	taker := &safeContinueTaker{clean: true}
	taker.on = func(string) {
		src.change(func(row *chatlist.Row) { row.Mine, row.Status, row.Released = true, chatlist.Here, false })
	}
	a.machines, a.taker, a.autoContinue = src, taker, true
	drain(t, a, a.askMachines())
	if len(taker.cells) != 1 || a.at(pageHome) {
		t.Fatalf("startup did not open the taken chat: takes=%v home=%v", taker.cells, a.at(pageHome))
	}
	if a.machinesAsking || len(a.machineRead.rows) != 1 || !a.machineRead.rows[0].Mine {
		t.Errorf("take left the pre-take reading: asking=%v reads=%d rows=%+v", a.machinesAsking, src.reads, a.machineRead.rows)
	}
	t.Logf("listing after arrival: reads=%d asking=%v mine=%v", src.reads, a.machinesAsking, a.machineRead.rows[0].Mine)
	src.change(func(row *chatlist.Row) { row.Device = "renamed tower" })
	now = now.Add(homeEvery)
	drive(t, a, homeTickMsg{gen: startupClock})
	if a.machineRead.rows[0].Device != "renamed tower" {
		t.Errorf("later listing did not land: asking=%v reads=%d rows=%+v", a.machinesAsking, src.reads, a.machineRead.rows)
	}
	agent := a.agent.(*fakeAgent)
	typeText(t, a, "first message after moving")
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || a.at(pageHome) {
		t.Fatalf("first send raised a continuation door: sent=%v home=%v", agent.sent, a.at(pageHome))
	}
	src.change(func(row *chatlist.Row) { row.Mine, row.Status = false, chatlist.Running })
	if a.machinePoll.due.Sub(now) > machinesCap {
		t.Fatal("the next listing exceeded the normal poll cap")
	}
	now = a.machinePoll.due
	drive(t, a, homeTickMsg{gen: a.homeGen})
	if _, elsewhere := a.heldElsewhere(); !elsewhere {
		t.Fatal("later takeover did not reach the open window")
	}
}

// TestContinueSendUsesFreshOwnership proves that a send decides the continuation
// card from a fresh reading, including when the service is unreachable.
func TestContinueSendUsesFreshOwnership(t *testing.T) {
	for _, unreachable := range []bool{false, true} {
		t.Run(map[bool]string{false: "held here", true: "unreachable"}[unreachable], func(t *testing.T) {
			src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute)}
			a, agent := heldLab(t, src, &fakeTaker{})
			a.closeHome()
			src.change(func(row *chatlist.Row) { row.Mine, row.Status = true, chatlist.Here })
			if unreachable {
				src.err = directory.ErrUnreachable
			}
			before := src.reads
			typeText(t, a, "keep these words")
			drive(t, a, key("enter"))
			if src.reads != before+1 {
				t.Errorf("send made %d ownership reads, want one", src.reads-before)
			}
			if _, card := a.homeAsking(); card {
				t.Error("cached ownership raised the continuation card")
			}
			if unreachable {
				if len(agent.sent) != 0 || a.input.String() != "keep these words" {
					t.Fatalf("unreachable send=%v draft=%q", agent.sent, a.input.String())
				}
			} else if len(agent.sent) != 1 {
				t.Fatalf("locally held chat did not send: %v", agent.sent)
			}
		})
	}
}

// TestResumeChordAcquiresTakeableChat proves through the key router that alt+c
// acquires a takeable chat rather than only reading it.
func TestResumeChordAcquiresTakeableChat(t *testing.T) {
	for _, status := range []chatlist.Status{chatlist.Idle, chatlist.Off, chatlist.Running} {
		t.Run(string(status), func(t *testing.T) {
			src := &continuationListing{row: leftOffRow(heldCell, "tower", status, time.Minute)}
			taker := &safeContinueTaker{clean: true}
			a, _ := heldLab(t, src, taker)
			if status == chatlist.Running {
				_, feed := devicesRig()
				a.dirFeed = feed
			}
			taker.on = func(string) { src.change(func(row *chatlist.Row) { row.Mine, row.Status = true, chatlist.Here }) }
			drain(t, a, a.askMachines())
			drive(t, a, key(resumeKey))
			if len(taker.cells) != 1 {
				t.Fatalf("chord did not acquire: %v", taker.cells)
			}
		})
	}
}

// TestContinueTakeReplacesAnOutstandingListing proves that a listing asked before
// a take cannot put the previous holder back on screen.
func TestContinueTakeReplacesAnOutstandingListing(t *testing.T) {
	src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute)}
	taker := &fakeTaker{}
	a, _ := heldLab(t, src, taker)
	old := a.askChats()()
	taker.on = func(string) { src.change(func(row *chatlist.Row) { row.Mine, row.Status = true, chatlist.Here }) }
	drain(t, a, a.takeRemote(src.row))
	if !a.machineRead.rows[0].Mine {
		t.Fatal("successful take waited for the old listing before showing local ownership")
	}
	drive(t, a, old)
	if a.machinesAsking || !a.machineRead.rows[0].Mine {
		t.Fatalf("old answer restored the previous holder: asking=%v rows=%+v", a.machinesAsking, a.machineRead.rows)
	}
}

// TestContinueAlreadyHeldHereKeepsTheSessionAndDraft proves at the surface that
// continuing a chat this computer holds keeps its session and the typed words.
func TestContinueAlreadyHeldHereKeepsTheSessionAndDraft(t *testing.T) {
	src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute)}
	taker := &fakeTaker{}
	a, agent := heldLab(t, src, taker)
	a.closeHome()
	taker.taken = Taken{AlreadyHere: true, Transcript: a.file}
	taker.on = func(string) { src.change(func(row *chatlist.Row) { row.Mine, row.Status = true, chatlist.Here }) }
	a.open = func(string, string) (Conversation, error) {
		t.Fatal("already held chat reopened its active session")
		return Conversation{}, nil
	}
	typeText(t, a, "words kept through the stale card")
	drive(t, a, key("enter"))
	confirm(t, a)
	if a.at(pageHome) || a.agent != agent || a.input.String() != "words kept through the stale card" {
		t.Fatalf("no-op lost the active session or draft: home=%v draft=%q", a.at(pageHome), a.input.String())
	}
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 {
		t.Fatalf("send after the no-op did not proceed: %v", agent.sent)
	}
}

// TestOwnershipReadDoesNotCompleteAnOutstandingListing proves that a send's own
// reading leaves the window's outstanding listing request to finish by itself.
func TestOwnershipReadDoesNotCompleteAnOutstandingListing(t *testing.T) {
	src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Here, time.Minute)}
	src.row.Mine = true
	a, _ := heldLab(t, src, &fakeTaker{})
	a.closeHome()
	listing := a.askChats()
	typeText(t, a, "send during the listing")
	drive(t, a, key("enter"))
	if !a.machinesAsking {
		t.Fatal("the send completed a listing that has not answered")
	}
	drain(t, a, listing)
	if a.machinesAsking {
		t.Fatal("the listing's own answer did not clear its guard")
	}
}
