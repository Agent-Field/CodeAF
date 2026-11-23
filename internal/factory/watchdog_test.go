package factory

import (
	"testing"
	"time"
)

var watchT0 = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

// watchJob is a job on one step in state, with a budget.
func watchJob(state State, phase PhaseState) Item {
	return Item{ID: 7, Repo: "acme/api", Kind: KindIssue, Num: 7, State: state, Cap: 10,
		Stream: &Stream{Phases: []Phase{{Name: "write", Kind: StageChat, State: phase}}, Cur: 0}}
}

// watchStored is the job as the store would have written it at watchT0.
func watchStored(it Item) Item {
	SettleWait(Item{}, &it, watchT0)
	return it
}

func watchRunning() Item { return watchJob(StateRunning, PhaseRunning) }
func watchQueued() Item  { return watchJob(StateQueued, PhasePending) }

func watchAsked() Item {
	it := watchRunning()
	it.Asking = &Asked{Stage: "write", Question: "Which table?", With: AskedManager}
	return it
}

func watchPR() Item {
	it := watchJob(StateShipped, PhaseDone)
	it.PullRequest = &PullRequest{Number: 9, State: PROpen, HeadSHA: "abc", ChecksRead: true, ReviewsRead: true, Checks: PRChecks{Pending: 2}}
	it.FollowUp = &PRFollowUp{Kind: "pr", State: "watching", Reason: "Watching CI, review feedback and conflicts."}
	return it
}

func watchMergeAsked() Item {
	it := watchPR()
	it.Merging = &MergeRun{State: MergeAsked, Commit: "abc", At: watchT0, UndoUntil: watchT0.Add(10 * time.Second)}
	return it
}

func watchLanded() Item {
	it := watchJob(StateLanded, PhaseDone)
	it.Proof = []Claim{{Text: "works", OK: true}}
	return it
}

func watchHeld() Item {
	it := watchRunning()
	it.Stream.Paused = true
	return it
}

func TestStallOfTripsOnEveryStall(t *testing.T) {
	fresh := func(at time.Duration) WatchFacts { return WatchFacts{Held: true, GitHubRead: watchT0.Add(at)} }
	cases := []struct {
		name  string
		item  func() Item
		facts WatchFacts
		after time.Duration
		want  string
		wait  string
	}{
		{"1: a job with no stored wait", func() Item { return watchRunning() }, fresh(0), 0, StallWait, "working:write"},
		{"1: a stored wait the fields no longer give", func() Item {
			it := watchStored(watchRunning())
			it.State, it.Question, it.QKind = StateNeedsYou, "write asks: which table?", QKindStep
			it.Asking = &Asked{Stage: "write", Question: "Which table?", With: AskedYou}
			return it
		}, fresh(0), 0, StallWait, "you:answer"},
		{"2: a step under way that no computer runs", func() Item { return watchStored(watchRunning()) },
			WatchFacts{}, WatchWorkingFor, StallUnheld, "working:write"},
		{"2: a merge past its undo window that was never carried out", func() Item { return watchStored(watchMergeAsked()) },
			fresh(0), 10*time.Second + WatchWorkingFor, StallOverdue, "working:merging"},
		{"2: an approval past its undo window that was never carried out", func() Item {
			it := watchRunning()
			it.Approval = &Approval{State: "approving", Kind: "step", At: watchT0, UndoUntil: watchT0.Add(30 * time.Second)}
			return watchStored(it)
		}, fresh(0), 30*time.Second + WatchWorkingFor, StallOverdue, "working:approving"},
		{"3: a pull request nobody has read", func() Item { return watchStored(watchPR()) },
			WatchFacts{Held: true, GitHubRead: watchT0}, WaitGitHubFor, StallGitHub, "github:checks"},
		{"3: a pull request on a factory that never read GitHub", func() Item { return watchStored(watchPR()) },
			WatchFacts{Held: true}, WaitGitHubFor, StallGitHub, "github:checks"},
		{"4: a question the manager held too long", func() Item { return watchStored(watchAsked()) },
			fresh(0), WaitMainAgentFor, StallMainAgent, "main_agent:write"},
		{"5: a queued job no computer picked up", func() Item { return watchStored(watchQueued()) },
			fresh(0), WaitRunnerFor, StallRunner, "runner:write"},
		{"2: a publish queued for a computer that never took it", func() Item {
			it := watchJob(StateQueued, PhaseDone)
			it.Publishing = true
			return watchStored(it)
		}, WatchFacts{}, WatchWorkingFor, StallUnheld, "working:publishing"},
		{"6: a person asked for something nothing on offer does", func() Item {
			it := watchStored(watchLanded())
			it.Offers = nil
			return it
		}, fresh(0), 0, StallNoOffer, "you:approve_publish"},
		{"6: an approval Auto gives by itself", func() Item { return watchStored(watchLanded()) },
			WatchFacts{Held: true, AutoPresses: true}, WatchAutoFor, StallAuto, "you:approve_publish"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			it := c.item()
			got := StallOf(it, c.facts, watchT0.Add(c.after))
			if got == nil {
				t.Fatalf("no stall; wait %+v, offers %v", it.Wait, it.Offers)
			}
			if got.Kind != c.want || got.Wait != c.wait {
				t.Fatalf("stall = %s on %s, want %s on %s", got.Kind, got.Wait, c.want, c.wait)
			}
			if got.Words == "" || !got.At.IsZero() || !got.NextAt.IsZero() || got.GaveUp || got.Turns != 0 {
				t.Fatalf("a sighting carries words and nothing the service stamps: %+v", got)
			}
			// The clock trips fire at their moment and not a second sooner.
			if c.after > 0 {
				if early := StallOf(it, c.facts, watchT0.Add(c.after-time.Second)); early != nil {
					t.Fatalf("tripped a second early: %+v", early)
				}
			}
		})
	}
}

func TestStallOfLeavesEveryLegalWaitAlone(t *testing.T) {
	long := 48 * time.Hour
	bad := WatchFacts{AutoPresses: true}
	stopped := true
	cases := []struct {
		name  string
		item  func() Item
		facts WatchFacts
		after time.Duration
	}{
		{"a step a computer is running", func() Item { return watchStored(watchRunning()) }, WatchFacts{Held: true}, long},
		{"a step that just lost its computer", func() Item { return watchStored(watchRunning()) }, WatchFacts{}, WatchWorkingFor - time.Second},
		{"a merge still inside its undo window", func() Item { return watchStored(watchMergeAsked()) }, WatchFacts{}, 5 * time.Second},
		{"an approval being carried out, whatever the job reads meanwhile", func() Item {
			it := watchLanded()
			it.Approval = &Approval{State: "approving", Kind: "proof", At: watchT0, UndoUntil: watchT0.Add(30 * time.Second)}
			it = watchStored(it)
			it.Wait, it.Offers = nil, nil
			return it
		}, WatchFacts{}, 30*time.Second + WatchWorkingFor - time.Second},
		{"a quiet pull request the factory still reads", func() Item { return watchStored(watchPR()) },
			WatchFacts{Held: true, GitHubRead: watchT0.Add(long - time.Minute)}, long},
		{"a pull request written inside its deadline", func() Item { return watchStored(watchPR()) }, WatchFacts{}, WaitGitHubFor - time.Second},
		{"a question the manager has just been given", func() Item { return watchStored(watchAsked()) }, WatchFacts{Held: true}, WaitMainAgentFor - time.Second},
		{"a job queued a moment ago", func() Item { return watchStored(watchQueued()) }, WatchFacts{}, WaitRunnerFor - time.Second},
		{"a job in a queue that is moving, however long", func() Item { return watchStored(watchQueued()) }, WatchFacts{Busy: true}, long},
		{"a publish waiting its turn behind other work", func() Item {
			it := watchJob(StateQueued, PhaseDone)
			it.Publishing = true
			return watchStored(it)
		}, WatchFacts{Busy: true}, long},
		{"a person's approval Auto does not give", func() Item { return watchStored(watchLanded()) }, WatchFacts{}, long},
		{"an approval Auto is about to give", func() Item { return watchStored(watchLanded()) }, bad, WatchAutoFor - time.Second},
		{"a wait its person put away", func() Item { return watchStored(watchHeld()) }, bad, long},
		{"a wait put away, even stored wrong", func() Item {
			it := watchStored(watchHeld())
			it.Wait, it.Offers = nil, nil
			return it
		}, bad, long},
		{"a finished job", func() Item {
			it := watchPR()
			it.PullRequest.State = PRMerged
			return watchStored(it)
		}, bad, long},
		{"a finished job with no stored wait", func() Item {
			it := watchPR()
			it.PullRequest.State = PRMerged
			return it
		}, bad, long},
		{"a job nobody started", func() Item { return watchStored(Item{ID: 7, State: StateNew}) }, bad, long},
		{"a job nobody started, with no stored wait", func() Item { return Item{ID: 7, State: StateNew} }, bad, long},
		{"a job its person stopped", func() Item {
			it := watchRunning()
			it.PersonStopped = &stopped
			return watchStored(it)
		}, bad, long},
		{"a stall already on record", func() Item {
			it := watchStored(watchQueued())
			it.Stall = &Stall{Kind: StallRunner, Words: "No computer picked this job up in time.", Wait: "runner:write", At: watchT0}
			return it
		}, bad, long},
		{"a stall already handed to the person", func() Item {
			it := watchQueued()
			it.Stall = &Stall{Kind: StallRunner, Words: "No computer picked this job up in time.", Wait: "runner:write", At: watchT0, GaveUp: true}
			return watchStored(it)
		}, bad, long},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			it := c.item()
			if got := StallOf(it, c.facts, watchT0.Add(c.after)); got != nil {
				t.Fatalf("stall %s (%s); wait %+v", got.Kind, got.Words, it.Wait)
			}
		})
	}
}

func TestWatchedNeverLooksAtAJobThatWaitsOnNobody(t *testing.T) {
	if !Watched(watchRunning()) || !Watched(watchStored(watchQueued())) {
		t.Fatal("a job under way is not watched")
	}
	gaveUp := watchQueued()
	gaveUp.Stall = &Stall{Kind: StallRunner, Wait: "runner:write", GaveUp: true}
	if !Watched(watchStored(gaveUp)) {
		t.Fatal("a stall handed to the person is still the watchdog's to clear")
	}
	if Watched(watchStored(watchHeld())) || Watched(Item{State: StateNew}) {
		t.Fatal("a put-away wait or a backlog job is watched")
	}
}

func TestStallStandsUntilItsCauseIsGone(t *testing.T) {
	at := watchT0.Add(time.Hour)
	with := func(it Item, kind string) Item {
		it.Stall = &Stall{Kind: kind, Words: "stalled", Wait: StallWaitKey(it), At: at}
		return watchStored(it)
	}
	noOffer := func(gaveUp bool) Item {
		// A failed job that names no step: nothing it offers runs it again.
		it := watchJob(StateNew, PhaseFailed)
		it.Failure = &Failure{Code: "error", Detail: "the worker crashed"}
		it.Stall = &Stall{Kind: StallNoOffer, Words: "stalled", Wait: StallWaitKey(it), At: at, GaveUp: gaveUp}
		return watchStored(it)
	}
	cases := []struct {
		name  string
		item  Item
		facts WatchFacts
		want  bool
	}{
		{"unheld, still no computer", with(watchRunning(), StallUnheld), WatchFacts{}, true},
		{"unheld, a computer runs it again", with(watchRunning(), StallUnheld), WatchFacts{Held: true}, false},
		{"overdue, whatever holds it", with(watchMergeAsked(), StallOverdue), WatchFacts{Held: true}, true},
		{"github, last read before the stall", with(watchPR(), StallGitHub), WatchFacts{GitHubRead: at.Add(-time.Minute)}, true},
		{"github, read since the stall", with(watchPR(), StallGitHub), WatchFacts{GitHubRead: at.Add(time.Second)}, false},
		{"runner, until the job moves", with(watchQueued(), StallRunner), WatchFacts{Held: true}, true},
		{"runner, a computer is back and busy with other work", with(watchQueued(), StallRunner), WatchFacts{Busy: true}, false},
		{"unheld, queued behind other work", with(watchRunning(), StallUnheld), WatchFacts{Busy: true}, false},
		{"manager, until the job moves", with(watchAsked(), StallMainAgent), WatchFacts{Held: true}, true},
		{"auto, still Auto's to give", with(watchLanded(), StallAuto), WatchFacts{AutoPresses: true}, true},
		{"auto, no longer Auto's to give", with(watchLanded(), StallAuto), WatchFacts{}, false},
		{"no offer, still nothing to press", noOffer(false), WatchFacts{}, true},
		{"no offer, handed over, still nothing underneath", noOffer(true), WatchFacts{}, true},
		{"no offer, an offer appeared", with(watchLanded(), StallNoOffer), WatchFacts{}, false},
		{"a wait fault the store put right", with(watchRunning(), StallWait), WatchFacts{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.item.Stall == nil {
				t.Fatalf("the store dropped the stall: wait %+v", c.item.Wait)
			}
			if got := StallStands(c.item, c.facts); got != c.want {
				t.Fatalf("stands = %v, want %v; wait %+v offers %v", got, c.want, c.item.Wait, c.item.Offers)
			}
		})
	}
	if StallStands(watchStored(watchRunning()), WatchFacts{}) {
		t.Fatal("a job with no stall has one standing")
	}
}

func TestStallBackoffGivesTheManagerFourTurns(t *testing.T) {
	st := Stall{Kind: StallRunner, Words: "No computer picked this job up in time.", Wait: "runner:write", At: watchT0, NextAt: watchT0}
	now := watchT0
	for i, want := range []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour} {
		if !st.TurnsLeft() {
			t.Fatalf("no turn left after %d", i)
		}
		st = st.Turned(now, false)
		if st.Turns != i+1 || !st.NextAt.Equal(now.Add(want)) || st.GaveUp {
			t.Fatalf("after turn %d: %+v, want the next look %s on", i+1, st, want)
		}
		now = st.NextAt
	}
	if !st.TurnsLeft() {
		t.Fatal("the turn after the last back-off is not given")
	}
	st = st.Turned(now, false)
	if st.Turns != 4 || !st.NextAt.IsZero() || st.TurnsLeft() {
		t.Fatalf("after the last turn: %+v, want no further look", st)
	}
}

func TestAFailedTurnHandsTheStallToThePerson(t *testing.T) {
	st := Stall{Kind: StallRunner, Words: "No computer picked this job up in time.", Wait: "runner:write", At: watchT0}
	st = st.Turned(watchT0, true)
	if !st.GaveUp || st.Words != "No computer picked this job up in time. "+StallTurnFailed {
		t.Fatalf("stall = %+v", st)
	}
	if again := st.GivenUp(StallUnmoved); again.Words != st.Words {
		t.Fatalf("a stall handed over twice changed its words: %q", again.Words)
	}
	it := watchQueued()
	it.Stall = &st
	it = watchStored(it)
	// No computer picked it up, so what it asks its person for is a computer,
	// and that one wait stands with nothing to press.
	if it.Wait.Who != WaitYou || it.Wait.Action != ActComputer || it.Wait.Words != st.Words {
		t.Fatalf("a stall handed over reads %+v, want you: computer with its words", it.Wait)
	}
	if err := WaitFault(it); err != nil {
		t.Fatalf("a stall only a computer ends is a fault: %v", err)
	}
	other := watchAsked()
	gone := Stall{Kind: StallMainAgent, Words: "stalled", Wait: StallWaitKey(other), At: watchT0, GaveUp: true}
	other.Stall = &gone
	other = watchStored(other)
	if other.Wait.Who != WaitYou || other.Wait.Action != ActDecide {
		t.Fatalf("any other stall handed over reads %+v, want you: decide", other.Wait)
	}
}

func TestWatchAtIsTheJobsNextClock(t *testing.T) {
	now := watchT0.Add(time.Minute)
	next := watchT0.Add(7 * time.Minute)
	stalled := watchStored(watchQueued())
	stalled.Stall = &Stall{Kind: StallRunner, Wait: "runner:write", At: watchT0, NextAt: next}
	done := watchStored(watchQueued())
	done.Stall = &Stall{Kind: StallRunner, Wait: "runner:write", At: watchT0, GaveUp: true}
	noOffer := watchStored(watchLanded())
	noOffer.Offers = nil
	approving := watchLanded()
	approving.Approval = &Approval{State: "approving", Kind: "proof", At: watchT0, UndoUntil: watchT0.Add(30 * time.Second)}
	approving = watchStored(approving)
	hour := watchT0.Add(time.Hour)
	cases := []struct {
		name  string
		item  Item
		facts WatchFacts
		now   time.Time
		want  time.Time
	}{
		{"a queued job's deadline", watchStored(watchQueued()), WatchFacts{}, now, watchT0.Add(WaitRunnerFor)},
		{"a late job in a moving queue, on the watchdog's period", watchStored(watchQueued()), WatchFacts{Busy: true}, hour, hour.Add(WatchWorkingFor)},
		{"a held question's deadline", watchStored(watchAsked()), WatchFacts{}, now, watchT0.Add(WaitMainAgentFor)},
		{"a pull request's deadline", watchStored(watchPR()), WatchFacts{}, now, watchT0.Add(WaitGitHubFor)},
		{"a quiet pull request, twenty minutes on from the last read", watchStored(watchPR()), WatchFacts{GitHubRead: hour.Add(-time.Minute)}, hour, hour.Add(WaitGitHubFor - time.Minute)},
		{"a merge's undo window and its grace", watchStored(watchMergeAsked()), WatchFacts{}, now, watchT0.Add(10*time.Second + WatchWorkingFor)},
		{"an approval's undo window and its grace, whatever the job reads", approving, WatchFacts{}, now, watchT0.Add(30*time.Second + WatchWorkingFor)},
		{"a step that just began", watchStored(watchRunning()), WatchFacts{}, now, watchT0.Add(WatchWorkingFor)},
		{"a held step, on the watchdog's period", watchStored(watchRunning()), WatchFacts{Held: true}, hour, hour.Add(WatchWorkingFor)},
		{"a step nothing holds, at once", watchStored(watchRunning()), WatchFacts{}, hour, hour},
		{"a job with no stored wait, at once", watchRunning(), WatchFacts{Held: true}, now, now},
		{"a person with nothing to press, at once", noOffer, WatchFacts{}, now, now},
		{"a person's wait has none", watchStored(watchLanded()), WatchFacts{}, now, time.Time{}},
		{"an approval Auto gives, when it is late", watchStored(watchLanded()), WatchFacts{AutoPresses: true}, now, watchT0.Add(WatchAutoFor)},
		{"a put-away wait has none", watchStored(watchHeld()), WatchFacts{AutoPresses: true}, now, time.Time{}},
		{"a stall's next look", stalled, WatchFacts{}, now, next},
		{"a stall the watchdog is done with", done, WatchFacts{}, now, time.Time{}},
	}
	for _, c := range cases {
		if got := WatchAt(c.item, c.facts, c.now); !got.Equal(c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// WatchAt is StallOf read as a clock. On any facts and at any moment: a job
// StallOf reports is due by now, so the timer's pass finds it; and a job it
// does not report is not due by now, so the timer never comes round for a
// job its pass can do nothing about.
func TestWatchAtAndStallOfAgreeAtEveryMoment(t *testing.T) {
	noOffer := watchStored(watchLanded())
	noOffer.Offers = nil
	approving := watchLanded()
	approving.Approval = &Approval{State: "approving", Kind: "proof", At: watchT0, UndoUntil: watchT0.Add(30 * time.Second)}
	queuedPublish := watchJob(StateQueued, PhaseDone)
	queuedPublish.Publishing = true
	items := map[string]Item{
		"queued": watchStored(watchQueued()), "asked": watchStored(watchAsked()), "pull request": watchStored(watchPR()),
		"merge asked": watchStored(watchMergeAsked()), "approving": watchStored(approving), "running": watchStored(watchRunning()),
		"publish queued": watchStored(queuedPublish), "landed": watchStored(watchLanded()), "no stored wait": watchRunning(),
		"no offer": noOffer, "held": watchStored(watchHeld()), "backlog": watchStored(Item{ID: 7, State: StateNew}),
	}
	var facts []WatchFacts
	for _, held := range []bool{false, true} {
		for _, busy := range []bool{false, true} {
			for _, auto := range []bool{false, true} {
				for _, read := range []time.Time{{}, watchT0, watchT0.Add(30 * time.Minute), watchT0.Add(3 * time.Hour)} {
					facts = append(facts, WatchFacts{Held: held, Busy: busy, AutoPresses: auto, GitHubRead: read})
				}
			}
		}
	}
	var moments []time.Time
	for _, d := range []time.Duration{0, time.Second, 40 * time.Second, WatchWorkingFor, WatchWorkingFor + 30*time.Second, WaitMainAgentFor, WaitRunnerFor, WaitGitHubFor, WaitGitHubFor + time.Second, 45 * time.Minute, 4 * time.Hour} {
		moments = append(moments, watchT0.Add(d-time.Nanosecond), watchT0.Add(d))
	}
	for name, it := range items {
		for _, f := range facts {
			for _, now := range moments {
				stall, at := StallOf(it, f, now), WatchAt(it, f, now)
				switch {
				case stall != nil && (at.IsZero() || at.After(now)):
					t.Fatalf("%s on %+v at +%v: a %s stall the clock does not come round for until %v", name, f, now.Sub(watchT0), stall.Kind, at)
				case stall == nil && !at.IsZero() && !at.After(now):
					t.Fatalf("%s on %+v at +%v: the clock is due (%v) for a job that is no stall", name, f, now.Sub(watchT0), at)
				}
			}
		}
	}
}

func TestStallBudgetSpent(t *testing.T) {
	it := watchRunning()
	if StallBudgetSpent(it) {
		t.Fatal("an unspent budget reads spent")
	}
	it.Stream.Spent = it.Cap
	if !StallBudgetSpent(it) {
		t.Fatal("a spent budget reads unspent")
	}
	it.Cap = 0
	if StallBudgetSpent(it) {
		t.Fatal("a job with no budget set reads spent")
	}
}
