package main

// ── THE KEYBOARD OF A CHAT THIS DESKTOP HOLDS FOR OTHERS ────────────────────
//
// With remote control on, another of the person's computers can open a chat the
// desktop is running and, if its permit allows, take the keyboard. The desktop's
// own window is then refused with the engine's sentence (it always was), but
// until now it had no way to SAY who had the keyboard or to take it back:
// the fact lived only in the engine's per-window driver answer.
//
// THE FACT IS THE ENGINE'S, NEVER THE DESKTOP'S. The desktop's own connection
// to its chat is a [remote.Client] like any other window's, so its driver answer
// is read exactly as a live view's is (app_live_open.go) and said on the agent's
// location as [appwire.Location.Keyboard]. It is present only while another
// window holds the keyboard of a chat held here, and absent for every other
// chat, which is why a desktop with remote control off says nothing new.

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// heldKeyboardOf is the keyboard fact of a driver answer: nothing while the
// desktop's own window holds the keyboard (or nobody does yet), and the other
// window's name otherwise.
func heldKeyboardOf(d remote.Driver) *appwire.LiveDriver {
	if d.Yours || d == (remote.Driver{}) {
		return nil
	}
	return &appwire.LiveDriver{Machine: d.Machine, Here: d.Here}
}

// layKeyboardLocked lays the agent's keyboard fact onto its location and says
// whether the view changed. A chat with no location of its own gets the plainest
// one, "held here", so the fact has somewhere to ride, and loses it again when
// the fact goes. A chat the list says is held elsewhere carries no fact: this
// desktop is not running it. The caller holds the backend's lock.
func (a *appAgent) layKeyboardLocked() bool {
	kb, loc := a.heldKeyboard, a.view.Location
	switch {
	case kb != nil && loc == nil:
		a.view.Location = &appwire.Location{Here: true, Keyboard: kb}
		return true
	case kb != nil && loc.Here:
		if loc.Keyboard != nil && *loc.Keyboard == *kb {
			return false
		}
		next := *loc
		next.Keyboard = kb
		a.view.Location = &next
		return true
	case loc != nil && loc.Keyboard != nil:
		next := *loc
		next.Keyboard = nil
		if next == (appwire.Location{Here: true}) {
			a.view.Location = nil
		} else {
			a.view.Location = &next
		}
		return true
	}
	return false
}

// holdKeyboardFact reads the desktop's own driver answer into the agent's
// location and says agent.changed when that moved the view. It does nothing
// for a connection that is no longer the agent's (a later open owns the fact).
func (b *appBackend) holdKeyboardFact(a *appAgent, conn *appConn, client *remote.Client) {
	kb := heldKeyboardOf(client.Driver())
	var view appwire.Agent
	changed := false
	b.locked(func() {
		if a.live == nil || a.live.conn != conn {
			return
		}
		a.heldKeyboard = kb
		changed = a.layKeyboardLocked()
		view = a.view
	})
	if changed {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
}

// watchHeldDriver says who has the keyboard of a chat this desktop holds
// whenever the engine's answer moves, and stops with the connection.
func (b *appBackend) watchHeldDriver(a *appAgent, conn *appConn) {
	agent, ok := conn.Conv.(*remote.Agent)
	if !ok || conn.Gone == nil {
		return
	}
	client := agent.Client()
	b.holdKeyboardFact(a, conn, client)
	guard.Go("app/held-driver", func() {
		for {
			changed := client.DriverChanged()
			select {
			case <-conn.Gone:
				// THE CHAT IS LET GO: the fact goes with it, unless a later open
				// of the same agent has already taken it over.
				var view appwire.Agent
				cleared := false
				b.locked(func() {
					if a.live == nil || a.live.conn == conn {
						a.heldKeyboard = nil
						cleared = a.layKeyboardLocked()
						view = a.view
					}
				})
				if cleared {
					b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
				}
				return
			case <-changed:
			}
			b.holdKeyboardFact(a, conn, client)
		}
	})
}

// heldKeyboardCall is chats.live_keyboard for a chat this desktop holds: who
// has the keyboard and, when asked, the desktop's own window takes it. It is
// answered only for a chat the desktop opened behind its door with remote
// control on; for every other chat it says what it always said.
func (b *appBackend) heldKeyboardCall(a *appAgent, p appwire.ChatLiveKeyboardParams) (appwire.ChatLiveKeyboardResult, error) {
	notLive := appwire.Errorf(appwire.CodeConflict, "this chat is not open live")
	live := b.liveOf(a)
	if live == nil || !live.conn.Held || !config.RemoteControlOn() {
		return appwire.ChatLiveKeyboardResult{}, notLive
	}
	agent, ok := live.conn.Conv.(*remote.Agent)
	if !ok {
		return appwire.ChatLiveKeyboardResult{}, notLive
	}
	client := agent.Client()
	if p.Take {
		if err := client.Take(); err != nil {
			return appwire.ChatLiveKeyboardResult{}, appwire.Errorf(appwire.CodeConflict, "%s", err.Error())
		}
	}
	b.holdKeyboardFact(a, live.conn, client)
	return appwire.ChatLiveKeyboardResult{Driver: appLiveDriverOf(client.Driver())}, nil
}
