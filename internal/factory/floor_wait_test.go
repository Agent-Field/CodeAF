package factory

import (
	"strings"
	"testing"
	"time"
)

// THE FACTORY MANAGER READS A JOB'S WAIT FROM WHAT THE STORE WROTE, in words
// for somebody looking at the job from outside: who it waits on, whether the
// watchdog found it stalled, and what it accepts from its person. A job no
// write has touched since waits were stored is read from its fields.
func TestTheFloorSaysAJobsStoredWaitAndOffers(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	stored := func(it Item) Item {
		SettleWait(Item{}, &it, now)
		return it
	}
	running := Item{ID: 1, State: StateRunning, Cap: 5, Stream: &Stream{Cur: 0, Phases: []Phase{{Name: "followpr", Kind: StageChat, State: PhaseRunning}}}}
	asked := running
	asked.Asking = &Asked{Stage: "followpr", Question: "rebase or merge?", With: AskedManager}
	held := running
	held.Stream = &Stream{Cur: 0, Paused: true, Phases: running.Stream.Phases}
	queued := Item{ID: 2, State: StateQueued, Cap: 5, Stream: &Stream{Phases: []Phase{{Name: "write", Kind: StageChat, State: PhasePending}}}}
	stalled := queued
	stalled.Stall = &Stall{Kind: StallRunner, Words: "No computer picked this job up in time.\nmore", Wait: "runner:write"}
	gaveUp := stalled
	gaveUp.Stall = &Stall{Kind: StallRunner, Words: "No computer picked this job up in time.", Wait: "runner:write", GaveUp: true}
	checks := Item{ID: 3, State: StateShipped, Publish: "pr", Cap: 5, PullRequest: &PullRequest{Number: 7, State: PROpen, HeadSHA: "abc", Checks: PRChecks{Pending: 1}}}
	merge := Item{ID: 4, State: StateShipped, Publish: "pr", Cap: 5, HeadCommit: "abc", PullRequest: &PullRequest{Number: 8, State: PROpen, HeadSHA: "abc", Mergeable: MergeableYes, ChecksRead: true, ReviewsRead: true}}
	for name, c := range map[string]struct {
		it          Item
		words, line string
	}{
		"working":      {stored(running), "waits on nobody (work under way)", "It waits on nobody: work is under way."},
		"main agent":   {stored(asked), "handed to its main agent, which is deciding what to do", "It is with its main agent, which is deciding what to do."},
		"held":         {stored(held), "waits on the person to resume it, put away by them", "It waits on the person to resume it. They put it away. It accepts from the person now: resume"},
		"queued":       {stored(queued), "waits on a computer to run it", "It waits on a computer to run it."},
		"stalled":      {stored(stalled), "waits on a computer to run it · stalled: No computer picked this job up in time. (reported to its main agent)", "It waits on a computer to run it."},
		"given up":     {stored(gaveUp), "waits on the person to bring a computer online · stalled: No computer picked this job up in time.", "It waits on the person to bring a computer online."},
		"checks":       {stored(checks), "waits on GitHub (its checks)", "It waits on GitHub (its checks)."},
		"merge":        {stored(merge), "waits on the person to merge the pull request", "It waits on the person to merge the pull request. It accepts from the person now: merge"},
		"not started":  {stored(Item{ID: 5, State: StateNew}), "", "It has not started. It accepts from the person now: start."},
		"finished":     {stored(Item{ID: 6, State: StateShipped, DoneHow: "merged"}), "", "It is finished."},
		"never stored": {merge, "waits on the person to merge the pull request", "It waits on the person to merge the pull request. It accepts from the person now: merge"},
	} {
		if got := FloorWaitWords(c.it); got != c.words {
			t.Errorf("%s: the row says %q, want %q", name, got, c.words)
		}
		if got := FloorWaitLine(c.it); !strings.HasPrefix(got, c.line) {
			t.Errorf("%s: the line says %q, want it to begin %q", name, got, c.line)
		}
		// A step is named by the row, never by its key here, and nothing is
		// addressed to the job's own manager.
		for _, got := range []string{FloorWaitWords(c.it), FloorWaitLine(c.it)} {
			if strings.Contains(got, "followpr") || strings.Contains(got, "you, the manager") || strings.Contains(got, "\n") {
				t.Errorf("%s: %q", name, got)
			}
		}
	}
	// WHO IT WAITS ON AND WHAT IT ACCEPTS, as the tool asks them.
	if w, yours := WaitsOnPerson(stored(held)); !yours || !w.PutAway {
		t.Errorf("a held job reads %+v, %v", w, yours)
	}
	if _, yours := WaitsOnPerson(stored(asked)); yours || !WaitsOnMainAgent(stored(asked)) || !WaitsOnRunner(stored(queued)) {
		t.Error("a job with its main agent, or queued, reads as the person's")
	}
	if it := stored(merge); !OffersPerson(it, DoMerge) || OffersPerson(it, DoApprove) || OffersPerson(merge, DoApprove) || !OffersPerson(merge, DoMerge) {
		t.Errorf("a mergeable job offers its person %v", PersonOffers(it))
	}
	if got := MainAgentOffers(stored(asked)); len(got) == 0 || got[0] != DoAnswer {
		t.Errorf("a job whose question its main agent holds offers it %v", got)
	}
}
