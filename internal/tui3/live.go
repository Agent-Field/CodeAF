package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// LiveKind is the state of a live view. The zero value is a healthy live view.
type LiveKind int

const (
	LiveOn LiveKind = iota
	LiveReconnecting
	LiveWentOffline
	LiveIdle
	LiveRevoked
	LiveBudget
	LiveCopy
)

// LiveRead is what the door says about a live view right now. It is answered
// from fields already held because it is asked on the draw path.
type LiveRead struct {
	Kind   LiveKind
	Reason string
	SeenAt time.Time
}

// LiveSeam travels on Conversation.Live. A nil seam means the conversation is
// not a live view and every behaviour in this file is absent.
type LiveSeam struct {
	Host      string
	Device    string
	Cell      string
	Road      string
	Row       chatlist.Row
	Read      func() LiveRead
	Touch     func()
	Reconnect func()
	Changed   func() <-chan struct{}
}

// liveFront returns the live seam for the conversation currently in front, or
// nil when the front conversation is not a live view.
func (a *app) liveFront() *LiveSeam {
	return a.liveView
}

// liveRead asks the front live seam for its held state, defaulting to LiveOn
// when no reader closure is wired.
func (a *app) liveRead() LiveRead {
	live := a.liveFront()
	if live == nil || live.Read == nil {
		return LiveRead{}
	}
	return live.Read()
}

// seamHost returns the host label for the seam line above the composer. A live
// conversation names the computer it runs on and adds the live word while the
// connection is healthy; every other conversation uses the window-wide host.
func (a *app) seamHost() string {
	live := a.liveFront()
	if live == nil || live.Host == "" {
		return a.host
	}
	if a.liveRead().Kind == LiveOn {
		return "on " + live.Host + " · live"
	}
	return "on " + live.Host
}

// liveBar returns the dim sentence that stands in the composer's place for a
// live view when typing is not open right now, in the contract's precedence
// order, and false when the composer box itself should stand.
func (a *app) liveBar() (string, bool) {
	live := a.liveFront()
	if live == nil {
		return "", false
	}
	read := a.liveRead()
	switch read.Kind {
	case LiveWentOffline:
		return chatlist.LiveWentOffline(live.Host), true
	case LiveCopy:
		return chatlist.LiveOfflineSeen(live.Host, chatlist.LiveSeenAt(read.SeenAt, a.now())), true
	case LiveIdle:
		return chatlist.LiveIdle, true
	case LiveRevoked:
		return chatlist.LiveSaid(read.Reason, live.Host), true
	case LiveBudget:
		return chatlist.LiveBudget, true
	}
	if a.link.Driving != nil {
		driving := a.link.Driving()
		if driving.ReadOnly != "" {
			return driving.ReadOnly, true
		}
		if !driving.Yours && driving.Holder != "" {
			return chatlist.LiveTypingFrom(driving.Holder), true
		}
	}
	return "", false
}

// liveBarEnter handles the Enter key while a composer-position bar stands: on a
// live view only LiveIdle reconnects and only a LiveTypingFrom bar takes the
// keyboard, while every other bar leaves the draft untouched and sends nothing.
func (a *app) liveBarEnter() tea.Cmd {
	live := a.liveFront()
	if live == nil {
		return a.takeKeyboard()
	}
	read := a.liveRead()
	switch read.Kind {
	case LiveIdle:
		return a.liveReconnect()
	case LiveWentOffline, LiveCopy, LiveRevoked, LiveBudget:
		return nil
	}
	if a.link.Driving != nil {
		driving := a.link.Driving()
		if driving.ReadOnly != "" {
			return nil
		}
		if !driving.Yours && driving.Holder != "" {
			return a.takeKeyboard()
		}
	}
	return nil
}

// liveReconnect asks the door to reattach an idle live view off the update loop
// so the UI never blocks on a wire call.
func (a *app) liveReconnect() tea.Cmd {
	live := a.liveFront()
	if live == nil || live.Reconnect == nil {
		return nil
	}
	reconnect := live.Reconnect
	return func() tea.Msg {
		reconnect()
		return nil
	}
}

// liveRoadWord formats the road value for /status when the front conversation
// is a live view other than an offline copy and the seam names its road.
func (a *app) liveRoadWord() string {
	live := a.liveFront()
	if live == nil || live.Road == "" || a.liveRead().Kind == LiveCopy {
		return ""
	}
	return "live · " + live.Road
}

// liveTurnTouchEvery bounds how often a running turn reports activity on a live
// view so streaming deltas do not flood the activity tracker.
const liveTurnTouchEvery = time.Second

// touchLive reports a person's action on a live front conversation at most once
// per routed message, keeping the idle timer informed without duplicate calls
// when a keystroke also scrolls or sends.
func (a *app) touchLive() {
	live := a.liveFront()
	if live == nil || live.Touch == nil {
		return
	}
	if a.inRoute && a.liveTouchedInRoute {
		return
	}
	if a.inRoute {
		a.liveTouchedInRoute = true
	}
	a.liveTurnTouchAt = a.now()
	live.Touch()
}

// touchLiveTurn reports activity while a turn is running on a live front
// conversation at most once a second on the surface clock, and never when the
// view is idle.
func (a *app) touchLiveTurn() {
	live := a.liveFront()
	if live == nil || live.Touch == nil {
		return
	}
	if a.state != stateWorking && a.stream == nil {
		return
	}
	now := a.now()
	if !a.liveTurnTouchAt.IsZero() && now.Sub(a.liveTurnTouchAt) < liveTurnTouchEvery {
		return
	}
	a.liveTurnTouchAt = now
	live.Touch()
}

// liveChangedMsg wakes the update loop when a live view's state changes, tagged
// with the conversation generation so an arrival from a conversation no longer
// in front is dropped.
type liveChangedMsg struct {
	gen int
}

// armLive waits for the next change on the front live seam through watchDriving
// so the test harness classifies the channel wait beside DrivingChanged.
func (a *app) armLive() tea.Cmd {
	live := a.liveFront()
	if live == nil || live.Changed == nil {
		return nil
	}
	a.armingLive = true
	cmd := a.watchDriving()
	a.armingLive = false
	return cmd
}

// liveMoved repaints the surface and re-arms the wait when the front live
// conversation's state changes, ignoring arrivals from an older generation.
func (a *app) liveMoved(msg liveChangedMsg) tea.Cmd {
	if msg.gen != a.convGen {
		return nil
	}
	a.touch()
	return tea.Batch(a.wake(), a.armLive())
}
