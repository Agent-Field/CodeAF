package tui3

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/history"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE AGENT-BUILDING SEAM (tui3.go's [Conversation]).
//
// The door hands this surface two closures that build a conversation and
// everything that answers for it in the same call. These are the surface's half:
// that /new and a resume go through them when they are wired, that they are
// asked about THIS conversation's own workspace, and — the reason the seam
// exists at all — that the closures which came with a conversation are the ones
// a keystroke in that conversation reaches.

// seamHistory is a recall list that records nothing. These tests care only about
// WHICH conversation's list the surface is holding, never about its contents.
type seamHistory struct{}

func (*seamHistory) Append(string, string)                 {}
func (*seamHistory) RecentFor(string, int) []history.Entry { return nil }
func (*seamHistory) Recent(int) []history.Entry            { return nil }

// TestAnAlwaysAfterNewReachesTheConversationInFront is this lane's proof.
//
// THE BUG IT REPRODUCES. The consent card's "always" does two things: it writes
// the rule into the person's profile, and it hands the gate the running session
// is behind a rebuilt policy (cmd/aforge's chatv3_approval.go). The second half
// was wired once, at boot, around the agent the door opened before this surface
// existed — so after a /new the write still landed and the push went into the
// session that /new had just closed. The card said `saved`, it was saved, and
// the very next call asked again, with nothing on screen to connect the two.
//
// It cannot be written against the code this lane replaces, and that is the
// shape of the fault rather than a gap in the test: before [Options.Start] there
// was exactly ONE save seam on the whole surface and no way for a second
// conversation to carry its own, so there was nothing for an assertion to
// compare. What is asserted here is that the boot conversation's seam is never
// reached again once a new conversation is in front.
func TestAnAlwaysAfterNewReachesTheConversationInFront(t *testing.T) {
	var banked []string
	// The conversation /new opens, with the turn that raises the card in it.
	front := &wiredAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("read", "read internal/session/consent.go"),
		consentEvent(7, "read", "read internal/session/consent.go", `tool "read"`),
	}}}}
	a := newTestApp(&fakeAgent{model: "m"})
	// The boot conversation's seam, wired the way the door has always wired it:
	// around the agent that was open before the surface was.
	a.saveApproval = func(tool string) error {
		banked = append(banked, "the closed conversation: "+tool)
		return nil
	}
	a.start = func(string) (Conversation, error) {
		return Conversation{
			Agent:       front,
			SessionFile: "/tmp/lab/next/transcript.jsonl",
			Workspace:   "/tmp/lab",
			SaveApproval: func(tool string) error {
				banked = append(banked, "the conversation in front: "+tool)
				return nil
			},
		}, nil
	}

	typeLine(t, a, "/new")
	typeLine(t, a, "look at the gate")
	settleAsk(a)
	drive(t, a, key("2"))

	if len(front.answers) != 1 || !front.answers[0].allow {
		t.Fatalf("the new conversation was answered %+v", front.answers)
	}
	if len(banked) != 1 {
		t.Fatalf("the always was banked %d times: %v", len(banked), banked)
	}
	if banked[0] != "the conversation in front: read" {
		t.Fatalf("the always went to %q — the rule was pushed into a session that is closed", banked[0])
	}
}

// The same law on the other door: a conversation resumed from the picker brings
// its own seams with it.
func TestAnAlwaysAfterAResumeReachesTheConversationInFront(t *testing.T) {
	var banked []string
	front := &wiredAgent{fakeAgent: &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("read", "read internal/session/consent.go"),
		consentEvent(7, "read", "read internal/session/consent.go", `tool "read"`),
	}}}}
	a := newTestApp(&fakeAgent{model: "m"})
	a.saveApproval = func(tool string) error {
		banked = append(banked, "the closed conversation: "+tool)
		return nil
	}
	a.open = func(_, transcript string) (Conversation, error) {
		return Conversation{
			Agent: front, SessionFile: transcript, Workspace: "/tmp/lab", Resumed: true,
			SaveApproval: func(tool string) error {
				banked = append(banked, "the conversation in front: "+tool)
				return nil
			},
		}, nil
	}

	if _, note := a.openSession(Session{File: "/tmp/lab/earlier/transcript.jsonl"}); note != "" {
		t.Fatalf("the resume refused: %s", note)
	}
	typeLine(t, a, "look at the gate")
	settleAsk(a)
	drive(t, a, key("2"))

	if len(banked) != 1 || banked[0] != "the conversation in front: read" {
		t.Fatalf("the always went to %v", banked)
	}
}

// Both closures are asked about THIS conversation's own workspace, which the
// seam spells as the empty string. A surface that named a directory here would
// be answering the door's own question for it.
func TestTheSeamIsAskedAboutThisConversationsOwnWorkspace(t *testing.T) {
	var started, opened []string
	a := newTestApp(&fakeAgent{model: "m"})
	a.start = func(workspace string) (Conversation, error) {
		started = append(started, workspace)
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/next/transcript.jsonl"}, nil
	}
	a.open = func(workspace, transcript string) (Conversation, error) {
		opened = append(opened, workspace+"|"+transcript)
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: transcript}, nil
	}

	a.renew()
	if len(started) != 1 || started[0] != "" {
		t.Fatalf("/new asked for %v, want this conversation's own workspace", started)
	}
	if _, note := a.openSession(Session{File: "/tmp/lab/earlier/transcript.jsonl"}); note != "" {
		t.Fatalf("the resume refused: %s", note)
	}
	if len(opened) != 1 || opened[0] != "|/tmp/lab/earlier/transcript.jsonl" {
		t.Fatalf("the picker asked for %v", opened)
	}
}

// The whole bundle is taken up, and the four answers that belong to a workspace
// are taken up even when they are EMPTY: a project that keeps no draft and a
// workspace that records no history are answering about their own directory.
func TestTakingUpAConversationRebindsEverythingThatCameWithIt(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.draftFile = "/tmp/lab/draft-old.txt"
	a.history = &seamHistory{}
	a.applyApprovals = func() error { return nil }

	next := &fakeAgent{model: "m2"}
	a.start = func(string) (Conversation, error) {
		return Conversation{
			Agent:          next,
			SessionFile:    "/elsewhere/next/transcript.jsonl",
			Workspace:      "/elsewhere/repo",
			ContextWindow:  128000,
			RecentSessions: func() []Session { return []Session{{File: "/elsewhere/one.jsonl"}} },
		}, nil
	}
	a.renew()

	if a.workspace != "/elsewhere/repo" || a.place != "repo" {
		t.Fatalf("the place is %q at %q", a.place, a.workspace)
	}
	if a.ctxWindow != 128000 {
		t.Fatalf("the context window is %d", a.ctxWindow)
	}
	if a.draftFile != "" {
		t.Fatalf("the draft is still kept at %q — the new workspace said nothing is", a.draftFile)
	}
	if a.history != nil {
		t.Fatal("the recall list survived into a workspace that records none")
	}
	if a.applyApprovals != nil {
		t.Fatal("the permissions panel is still wired to the conversation that closed")
	}
	if len(a.recentSessions()) != 1 {
		t.Fatal("the recent list is still the old project's")
	}
}

// A door that answers only the OLDER seam is unchanged by all of this: what
// comes back carries an agent and nothing else, and everything the surface was
// holding stays exactly where it was. The hosted door is that door.
func TestTheOlderSeamLeavesEveryOtherClosureStanding(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	kept := &seamHistory{}
	a.history = kept
	a.draftFile = "/tmp/lab/draft.txt"
	saved := 0
	a.saveApproval = func(string) error { saved++; return nil }
	a.fresh = func() (Agent, string, error) {
		return &fakeAgent{model: "m2"}, "/tmp/lab/next/transcript.jsonl", nil
	}

	a.renew()

	if a.history != History(kept) || a.draftFile != "/tmp/lab/draft.txt" {
		t.Fatal("the older seam cleared answers it never gave")
	}
	if a.saveApproval == nil {
		t.Fatal("the older seam took the consent card's write with it")
	}
	if err := a.saveApproval("read"); err != nil || saved != 1 {
		t.Fatalf("the write seam was replaced: %v %d", err, saved)
	}
}

// And the door's fields reach the surface at all, which is the plumbing every
// test above stands on.
func TestTheDoorsSeamReachesTheSurface(t *testing.T) {
	a := newApp(context.Background(), Options{
		Agent:     &fakeAgent{model: "m"},
		Workspace: "/tmp/lab",
		Start:     func(string) (Conversation, error) { return Conversation{}, nil },
		Open:      func(string, string) (Conversation, error) { return Conversation{}, nil },
	})
	a.width, a.height = 60, 20
	a.pal = newPalette(tokens.ANSI256, false)
	if a.start == nil || a.open == nil {
		t.Fatal("the seam did not reach the surface")
	}
	if !a.canStart() || !a.canOpen() {
		t.Fatal("a surface with the seam wired says it cannot open a conversation")
	}
	// And a surface with neither seam nor the older pair refuses, which is what
	// a headless frame is.
	bare := newApp(context.Background(), Options{Agent: &fakeAgent{model: "m"}})
	if bare.canStart() || bare.canOpen() {
		t.Fatal("a surface with no door onto a conversation claims to have one")
	}
}
