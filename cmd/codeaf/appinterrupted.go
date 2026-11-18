package main

// ── a chat the app stopped mid-turn waits for the person ────────────────────
//
// A CHAT THAT WAS MID-TURN WHEN THE APP QUIT OR CLOSED DOES NOT CARRY ON BY
// ITSELF WHEN THE APP OPENS AGAIN. It reads "Interrupted · Continue" where the
// turn stopped, and nothing it was doing — the reply, the tasks it had running,
// the frontier those tasks would have turned — spends a cent until the person
// presses Continue (OWNER-DECISIONS 2026-11-17 §12). The resume it replaced
// cost a demo machine about $1.60 on Opus, unasked, on every relaunch.
//
// The mark is the app's state file (Interrupted), written before the chat can
// be opened, and an open of a marked chat holds its recovered work back
// ([session.Config.HoldInterruptedWork]), so even looking at the chat starts
// nothing. Continue consumes the mark ONCE and carries the turn on through the
// one resume door ([appBackend.resumeOneWith]); a person's own message, answer
// or retry in that chat consumes it too, because the person is then there.
//
// This is the app's chats only. An organization's task runs resume once on the
// server (internal/factory/serve's workstream_task_lease.go), a Personal task
// once in the desk; neither reads this file.

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	// appInterruptedWord is the footer's own words; the window adds the
	// Continue action beside it while the chat is still marked.
	appInterruptedWord = "Interrupted"
	// appInterruptedQuit and appInterruptedClose name the road that stopped
	// the turn, which decides what the model is told when it carries on.
	appInterruptedQuit  = "quit"
	appInterruptedClose = "close"
)

// appInterrupted is one chat the app stopped mid-turn and left for the person.
type appInterrupted struct {
	At time.Time `json:"at"`
	// Key is the footer's id, and the stem of the one Continue's own footer.
	Key string `json:"key"`
	// Road is appInterruptedQuit or appInterruptedClose.
	Road string `json:"road"`
	// Reply says a reply was in the middle of its work, so Continue carries it
	// on as a turn; Tasks is the tasks it had running.
	Reply bool     `json:"reply,omitempty"`
	Tasks []string `json:"tasks,omitempty"`
}

// markInterrupted writes the mark for one chat inside the caller's state write.
func (f *appStateFile) markInterrupted(id string, entry appInterrupted) {
	if f.Interrupted == nil {
		f.Interrupted = map[string]appInterrupted{}
	}
	if old, ok := f.Interrupted[id]; ok {
		// A second stop before anybody pressed Continue keeps the first line,
		// and owes the union of what both stopped.
		entry.Key, entry.At = old.Key, old.At
		entry.Reply = entry.Reply || old.Reply
		entry.Tasks = appUnion(old.Tasks, entry.Tasks)
		if old.Road == appInterruptedClose {
			entry.Road = old.Road
		}
	}
	f.Interrupted[id] = entry
}

func appUnion(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b} {
		for _, s := range list {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

// holdOpen answers whether an open of this chat must hold its recovered work:
// it is marked, or an unexpected close's claim on it has not been settled yet,
// so the mark may be a moment away.
func (b *appBackend) holdOpen(id, transcript string) bool {
	if b.interrupted(id) {
		return true
	}
	if transcript == "" {
		return false
	}
	dir := session.DirOf(transcript)
	r := &b.closeRecovery
	claimed := false
	r.locked(func() { _, claimed = r.claims[dir] })
	return claimed
}

// interrupted answers whether a chat is waiting for the person's Continue.
func (b *appBackend) interrupted(id string) bool {
	marked := false
	_ = b.state.read(func(f *appStateFile) { _, marked = f.Interrupted[id] })
	return marked
}

// sayInterrupted keeps the footer where the turn stopped and says the chat as
// interrupted. It never opens the chat.
func (b *appBackend) sayInterrupted(id string, entry appInterrupted) {
	a, err := b.find(id)
	if err != nil {
		return
	}
	if err := b.keepAppFooter(a, entry.Key, appwire.FooterInterrupted, appInterruptedWord); err == nil {
		b.emitAppFooter(a, entry.Key)
	}
	b.restateInterrupted(a, true)
}

// restateInterrupted lays the mark over the held view and tells the window.
func (b *appBackend) restateInterrupted(a *appAgent, marked bool) {
	var view appwire.Agent
	changed := false
	b.locked(func() {
		if a.view.Interrupted != marked {
			a.view.Interrupted = marked
			view, changed = a.view, true
		}
	})
	if changed {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
}

// takeInterrupted consumes a chat's mark, once.
func (b *appBackend) takeInterrupted(id string) (appInterrupted, bool) {
	var entry appInterrupted
	found := false
	_ = b.state.write(func(f *appStateFile) error {
		entry, found = f.Interrupted[id]
		delete(f.Interrupted, id)
		return nil
	})
	return entry, found
}

// ContinueInterrupted is the person's Continue on a chat the app stopped
// mid-turn: its held tasks carry on and, when a reply was cut, the turn carries
// on once. A second press, or a press on a chat with no mark, does nothing.
func (b *appBackend) ContinueInterrupted(id string) (appwire.Agent, error) {
	a, err := b.find(id)
	if err != nil {
		return appwire.Agent{}, err
	}
	defer b.allowLazyOpen(id)
	entry, found := b.takeInterrupted(id)
	b.restateInterrupted(a, false)
	if found {
		kind, words := appwire.FooterResumedAfterQuit, appResumedWord
		if entry.Road == appInterruptedClose {
			kind, words = appwire.FooterResumedAfterClose, appCloseResumedWord
		}
		b.resumeOneWith(id, entry.Reply, entry.Tasks, kind, words, entry.Key+"-continued")
	}
	var view appwire.Agent
	b.locked(func() { view = a.view })
	return view, nil
}

// releaseInterrupted is a person acting in a marked chat by any other door:
// they are there, so the mark goes and what the open held back carries on.
func (b *appBackend) releaseInterrupted(id string) {
	a, err := b.find(id)
	if err != nil {
		return
	}
	if b.interrupted(id) {
		if _, found := b.takeInterrupted(id); found {
			defer b.allowLazyOpen(id)
			b.restateInterrupted(a, false)
		}
	}
	// An open held for a close claim that was never marked is released here
	// too; on a conversation holding nothing the door does nothing.
	if live := b.liveOf(a); live != nil {
		if door, ok := live.conn.Conv.(appInterruptedDoor); ok {
			_, _ = door.ContinueInterruptedWork()
		}
	}
}

// appInterruptedDoor is a conversation holding recovered work for Continue.
type appInterruptedDoor interface {
	ContinueInterruptedWork() (bool, error)
}
