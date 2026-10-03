package tui3

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// MENTION LIVE STATE — THE REFRESH HALF. The mention in the prose and the mark
// on the tab both read [app.tabSignalFor], which opens no file and crosses no
// wire; what this file proves is that every flip of that reading in a HELD
// conversation reaches [app.touch], so the next frame re-lays out and the mark
// appears or clears — and that a quiet frame pays nothing.

// mentionRefreshLab holds one idle conversation in the keeper with an
// unrelated one in front, the shape parkedKeeperLab uses, minus the queue.
func mentionRefreshLab(t *testing.T) (*app, string, *kept) {
	t.Helper()
	heldAgent := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(heldAgent)
	a.file = "/tmp/lab/held.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)

	conv, side := a.front(), a.detachConversation()
	drain(t, a, a.stow(conv, side))
	drain(t, a, a.attachConversation(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/lab/front.jsonl"}, nil))
	key := convKey("/tmp/lab/held.jsonl")
	held := a.behind[key]
	if held == nil {
		t.Fatal("the conversation was not kept")
	}
	t.Cleanup(held.watch.stop)
	return a, key, held
}

// awaitStir takes the next contentless nudge off the shared lane, which no
// waitStir pump is reading in these tests.
func awaitStir(t *testing.T, a *app, what string) behindStirMsg {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case note := <-a.stirs:
			return note
		default:
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatal(what)
	return behindStirMsg{}
}

// A turn STARTING in a held conversation is the one signal edge with no close
// behind it: the watcher's working flag flips in [behindWatch.adopt], and the
// stir it raises must set dirty — or a mention of that conversation holds "at
// rest" for the whole turn.
func TestMentionRefreshHeldTurnStartDirtiesTheFrame(t *testing.T) {
	a, key, held := mentionRefreshLab(t)

	a.dirty = false
	turn := make(chan session.Event)
	held.watch.adopt(turn, nil)

	note := awaitStir(t, a, "a turn starting in a held conversation raised no stir")
	a.behindStir(note)

	if !a.dirty {
		t.Fatal("a held turn starting did not dirty the frame — the working mark could never appear")
	}
	if sig := a.tabSignalFor(key, false); sig != tabWorking {
		t.Fatalf("a held conversation with a turn in flight reads %v, want working", sig)
	}

	// AND THE CLOSE OF THAT STREAM IS THE EDGE THAT CLEARS IT.
	a.dirty = false
	close(turn)
	note = awaitStir(t, a, "a held turn landing raised no stir")
	a.behindStir(note)

	if !a.dirty {
		t.Fatal("a held turn landing did not dirty the frame — the working mark could never clear")
	}
	if sig := a.tabSignalFor(key, false); sig != tabIdle {
		t.Fatalf("a held conversation whose turn landed reads %v, want idle", sig)
	}
}

// A stir folded for a turn that LANDED sets dirty on its own: the count, the
// banner and the mark all repaint off the same touch.
func TestMentionRefreshStirOnTurnLandedSetsDirty(t *testing.T) {
	a, key, held := mentionRefreshLab(t)

	held.watch.landed.Store(true)
	a.dirty = false
	a.behindStir(behindStirMsg{key: key})

	if !a.dirty {
		t.Fatal("a stir about a landed turn left the frame clean")
	}
}

// A conversation this window does not hold — foreign, or only a name on the
// recency stack — reads idle. The honesty law: idle means "at rest OR not
// known", and a surface never claims state it cannot refresh.
func TestMentionRefreshAKeyThisWindowDoesNotHoldReadsIdle(t *testing.T) {
	a, _, _ := mentionRefreshLab(t)

	if sig := a.tabSignalFor(convKey("/tmp/lab/foreign.jsonl"), false); sig != tabIdle {
		t.Fatalf("a conversation this window does not hold reads %v, want idle", sig)
	}
}

// Over a shared engine handle the keeper holds nothing (stow's own guard), so
// a conversation that is not in front has no watcher and must read idle —
// never a guess at what the engine's other end is doing.
func TestMentionRefreshSharedHandleNonLiveConversationReadsIdle(t *testing.T) {
	heldAgent := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(heldAgent)
	a.file = "/tmp/lab/held.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.shared = true

	conv, side := a.front(), a.detachConversation()
	drain(t, a, a.stow(conv, side))

	key := convKey("/tmp/lab/held.jsonl")
	if a.behind[key] != nil {
		t.Fatal("a shared-handle door kept a conversation it cannot refresh")
	}
	if sig := a.tabSignalFor(key, false); sig != tabIdle {
		t.Fatalf("a non-live conversation over a shared handle reads %v, want idle", sig)
	}
}

// The repo law a live mark could break: a quiet frame redraws nothing. A held
// conversation at rest raises no stir, so dirty stays down until something
// happens.
func TestMentionRefreshQuietFramesDoNotRelayout(t *testing.T) {
	a, _, _ := mentionRefreshLab(t)

	a.dirty = false
	time.Sleep(50 * time.Millisecond)
	if a.dirty {
		t.Fatal("a quiet frame with a held conversation at rest dirtied itself")
	}
}
