package session

// livesessions.go is what this process knows about the conversations it holds
// open, and what the machine says about whether anybody is here: the registry
// a lane prober and the memory pass read, and the idle check the clock's memory
// pass is gated on. It outlived the standing orders it was first written for.

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// The cheap yes/no judgment an automation's watch makes is
// [roles.RoleSentinel] — declared there with every other role's word, which
// carries the reasoning — and registered here, beside the other session-wide
// facts this file keeps, so the role has its settings row while a judge can be
// asked of it.
func init() {
	roles.Register(roles.RoleSentinel, roles.TierLow, "is this worth telling you about")
}

// ── the live-window registry ────────────────────────────────────────────────

var (
	liveSessionsMu sync.Mutex
	liveSessions   = map[string]liveWindow{}
)

// liveWindow is one open conversation in this process.
type liveWindow struct {
	agent *Agent
	// opened is when it registered. It is half of "which window is the person
	// actually in" — see [liveSessionTouched].
	opened time.Time
}

// registerLiveSession records that this process holds a conversation open.
//
// TWO KINDS OF AGENT ARE NOT ONE. A node's own agent has a journal but no
// person in it ([Config.InTask]). An ERRAND — home's `ask here` exchange — has a
// person, but the pane it draws in closes with home and is not the room they are
// sitting in ([Config.Errand]). Neither counts as a window somebody is in.
func registerLiveSession(agent *Agent) {
	if agent == nil || agent.config.InTask || agent.config.Errand {
		return
	}
	id := strings.TrimSpace(agent.id)
	if id == "" {
		return
	}
	liveSessionsMu.Lock()
	liveSessions[id] = liveWindow{agent: agent, opened: time.Now()}
	liveSessionsMu.Unlock()
}

// forgetLiveSession erases that record. It runs from Close, so a window that
// has gone takes its entry with it.
func forgetLiveSession(agent *Agent) {
	if agent == nil {
		return
	}
	id := strings.TrimSpace(agent.id)
	if id == "" {
		return
	}
	liveSessionsMu.Lock()
	if held, found := liveSessions[id]; found && held.agent == agent {
		delete(liveSessions, id)
	}
	liveSessionsMu.Unlock()
}

// someoneIsWatching reports whether a person is in front of this process: a
// conversation they opened, or a command they typed and are waiting on.
//
// IT IS THE ONE READING OF "ATTENDED" THIS BUILD CAN HONESTLY MAKE, and it is
// this map plus the door's latch because of what the map already refuses: a
// task node's own agent and an errand's pane both decline to register
// ([registerLiveSession]), so an entry here is a room with a person in it and
// nothing else is. A headless run opens no conversation; it answers true only
// when the door said a person typed it (internal/provider's
// [provider.SetPersonAtTheDoor]), which is the difference between `codeaf do`
// at somebody's terminal and a node a spawner built with nobody there.
//
// WHAT IT IS FOR. λ — what a second of waiting is worth — is zero for work
// nobody is waiting on, and that is a true statement about a run whose owner
// has closed the window and a false one about a run they are watching land
// (internal/session's loop.go, and bench/lanelab/REPORT.md for what the false
// version costs). It is deliberately coarse: it says a person is HERE, not that
// they are looking at this particular node.
func someoneIsWatching() bool {
	if provider.PersonAtTheDoor() {
		return true
	}
	liveSessionsMu.Lock()
	defer liveSessionsMu.Unlock()
	return len(liveSessions) > 0
}

// liveSessionTouched is when a person last had anything to do with one open
// window: the later of when they last SPOKE in it ([Meta.LastUserAt], the same
// stamp the resume law and [MachineIdle] read, and deliberately never a file
// mtime) and when the window opened.
//
// THE OPENING COUNTS BECAUSE A FRESH WINDOW IS SOMEBODY ARRIVING. A
// conversation opened one minute ago with nothing typed in it yet is more
// likely to be where the person is than one they last spoke in yesterday and
// left on screen.
func liveSessionTouched(window liveWindow) time.Time {
	at := window.opened
	if dir := strings.TrimSpace(window.agent.config.Place.Dir); dir != "" {
		if meta, err := LoadMeta(dir); err == nil && meta.LastUserAt.After(at) {
			at = meta.LastUserAt
		}
	}
	return at
}

// ── whether the machine is quiet ────────────────────────────────────────────

// MachineIdle is the world reader's answer to "has nobody been here for a
// while", which the clock's memory pass is gated on ([automation.Idle]).
//
// TWO CONDITIONS, AND BOTH ARE ABOUT PEOPLE. Nothing may be working right now —
// a machine mid-build is not idle however long ago somebody typed — and the
// newest thing anybody SAID anywhere must be older than the span. The second is
// read from [Meta.LastUserAt] and deliberately not from file times, which is the
// ordering law everywhere in this codebase: a background write is not a person
// returning to a conversation.
func MachineIdle() automation.Idle {
	return func(quiet time.Duration) bool {
		world := ReadHome()
		var newest time.Time
		for _, project := range world.Projects {
			for _, row := range project.Sessions {
				if row.Live && row.Presence.State == PresenceWorking {
					return false
				}
				if row.At.After(newest) {
					newest = row.At
				}
			}
		}
		if newest.IsZero() {
			// Nobody has ever said anything on this machine. That is quiet.
			return true
		}
		return world.Read.Sub(newest) >= quiet
	}
}

// ── small words ─────────────────────────────────────────────────────────────

// oneLine folds any run of whitespace, newlines included, into one space: a
// sentence that has to sit on one row.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// homeWorkspace is the folder a conversation's work belongs to when the
// conversation has no project of its own: the person's home for an owned
// folder, the place's workspace, or the launch's.
func (a *Agent) homeWorkspace() string {
	if a.config.Place.Owned {
		if dir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(dir) != "" {
			return dir
		}
	}
	if root := strings.TrimSpace(a.config.Place.Workspace); root != "" {
		return root
	}
	return strings.TrimSpace(a.config.Workspace)
}
