package tui3

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ErrNoLiveView is what a door answers when there is simply nothing to open
// live and nothing to say about it, such as a computer that is off with no
// last-seen copy kept. Home then asks the Move card as it always did, without
// a sentence on its foot that would only repeat what the card already says.
var ErrNoLiveView = errors.New("no live view")

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

// liveMoving reports whether a Move started from the front live view is
// currently running, giving chatlist.LiveMoving the highest bar precedence.
func (a *app) liveMoving() bool {
	return a.liveFront() != nil && a.liveMoveFrom != "" && (a.homeTaking != "" || a.homeTakeOpening || a.conversationOpening)
}

// liveBar returns the dim sentence that stands in the composer's place for a
// live view when typing is not open right now, in the contract's precedence
// order, and false when the composer box itself should stand.
func (a *app) liveBar() (string, bool) {
	live := a.liveFront()
	if live == nil {
		return "", false
	}
	if a.liveMoving() {
		return chatlist.LiveMoving, true
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
	if a.liveMoving() {
		return nil
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

// liveEligible reports whether a chat row held on another computer may be
// opened as a live view in this window: the openLive seam is wired, this window
// is not itself a hosted session, the row is held elsewhere, neither this
// computer nor the holder is marked removed, and the holder is another computer
// in this person's roster.
func (a *app) liveEligible(row chatlist.Row) bool {
	if a.openLive == nil || a.hosted() || a.machineRead.computerRemoved || row.HolderRemoved || !heldElsewhere(row) {
		return false
	}
	for _, d := range a.devRow.devices {
		if (row.DeviceID != "" && d.ID == row.DeviceID) || (row.DeviceID == "" && row.Device != "" && d.Name == row.Device) {
			return !d.Self
		}
	}
	return false
}

// homeOpenLiveLater asks the door to open a held chat as a live view off the
// update loop, taking it beside the current conversation on success and falling
// back to the Move card with the door's error on Home's foot when the live open
// fails.
func (a *app) homeOpenLiveLater(line homeLine, row chatlist.Row) tea.Cmd {
	a.teamViewSet("")
	openLive := a.openLive
	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	say := func(text string) {
		a.home.say(text, "")
	}
	return a.conversationLater(func() (Conversation, error) {
		return openLive(baseCtx, row)
	}, say, func(conv Conversation) tea.Cmd {
		opened := a.takeBeside(conv)
		a.closeHome()
		return opened
	}, func(err error) tea.Cmd {
		cmd := a.homeMachineMove(line)
		if !errors.Is(err, ErrNoLiveView) {
			a.home.say(err.Error(), "")
		}
		return cmd
	})
}

// liveMoveRow returns the directory row for the front live conversation,
// filling any missing identity fields from the live seam and transcript path.
func (a *app) liveMoveRow(live *LiveSeam) chatlist.Row {
	row := live.Row
	if row.Cell == "" {
		row.Cell = live.Cell
	}
	if row.Cell == "" && a.file != "" {
		row.Cell = filepath.Base(session.DirOf(a.file))
	}
	if row.Device == "" {
		row.Device = live.Host
	}
	if row.DeviceID == "" {
		row.DeviceID = live.Device
	}
	if row.Status == "" {
		row.Status = chatlist.Running
	}
	return row
}

// liveMoveIdentity returns the file path or cell identifier that marks a Move
// as having been started from the given live view.
func (a *app) liveMoveIdentity(live *LiveSeam, row chatlist.Row) string {
	if a.file != "" {
		return a.file
	}
	if row.Cell != "" {
		return row.Cell
	}
	if live != nil {
		return live.Cell
	}
	return ""
}

// liveMatchesRow reports whether the front live conversation is viewing the
// same chat as row.
func (a *app) liveMatchesRow(live *LiveSeam, row chatlist.Row) bool {
	if live == nil || row.Cell == "" {
		return false
	}
	if live.Cell == row.Cell || live.Row.Cell == row.Cell {
		return true
	}
	return a.file != "" && filepath.Base(session.DirOf(a.file)) == row.Cell
}

// liveMoveKeyPress handles alt+m in a live front conversation before any
// chat-level use of that key, opening Home and raising the Move card for the
// live conversation's row with the cursor on move here.
func (a *app) liveMoveKeyPress(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if msg.String() != deviceKey {
		return nil, false
	}
	live := a.liveFront()
	if live == nil {
		return nil, false
	}
	if a.asking() || a.pageShowing() || a.deckShowing() || a.expandShowing() ||
		a.pick.open || a.effPick.open || a.roster.open || a.folder.open ||
		a.shelf.open || a.connPanel.open || a.addPanel.open || a.harnPanel.open ||
		a.crewUI.open || a.draftPage.open || a.permPanel.open || a.pair.open ||
		a.subPage.open || a.rewSheet.open || a.rew.on || a.copy.on ||
		a.welcome.open || a.startingChat() || a.menu.open || a.comp.open ||
		a.wall.on || a.railHold || a.roomOpen() || a.questionRoomOpen() {
		return nil, false
	}
	row := a.liveMoveRow(live)
	a.liveMoveFrom = a.liveMoveIdentity(live, row)
	cmd := a.openHome()
	asked := a.raiseContinue(row, continueYesAt)
	if _, ok := a.homeAsking(); !ok {
		a.liveMoveFrom = ""
	}
	return tea.Batch(cmd, asked), true
}

// followHolderLive replaces a displaced local front conversation with a live
// view of the new holder when remote control is available and the row is
// eligible, keeping any words already typed in the composer box and retiring
// the old local agent.
//
// THE DOOR IS ASKED OFF THE LOOP, because opening a live view dials another
// computer and a dial may take seconds; the swap itself is the fold, which runs
// on the loop for the conversation that was in front when this was asked and
// for no other. A refusal says the Superseded note the window always said, so a
// window that cannot follow is the window it was.
func (a *app) followHolderLive(row chatlist.Row) (tea.Cmd, bool) {
	if !a.liveEligible(row) {
		return nil, false
	}
	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	openLive, stale := a.openLive, a.file
	return a.offLoop(func() func(bool) tea.Cmd {
		conv, err := openLive(baseCtx, row)
		return func(here bool) tea.Cmd {
			if err != nil {
				if here {
					a.note(chatlist.Superseded(nonemptyDevice(row.Device)))
					a.touch()
				}
				return nil
			}
			if !here || a.file != stale || a.liveFront() != nil {
				if conv.Agent != nil {
					leaveOffFrame(conv.Agent)
				}
				return nil
			}
			return a.swapForLive(conv)
		}
	}), true
}

// swapForLive puts a live view in the place of the conversation in front: the
// old agent is retired, the words in the box stay in the box, and the new
// conversation is attached through the door every conversation comes through.
func (a *app) swapForLive(conv Conversation) tea.Cmd {
	oldAgent := a.agent
	oldKey := a.convKey(a.file)
	draft := a.mainComposer()
	a.detachConversation()
	if oldKey != "" {
		if held := a.behind[oldKey]; held != nil {
			if held.watch != nil {
				held.watch.stop()
			}
			delete(a.behind, oldKey)
		}
		if oldKey != a.convKey(conv.SessionFile) {
			a.forget(oldKey)
		}
	}
	if oldAgent != nil && oldAgent != conv.Agent {
		leaveAgent(oldAgent)
	}
	cmd := a.attachConversation(conv, nil)
	if key := a.convKey(conv.SessionFile); key != "" {
		a.rememberOpen(key)
	}
	if !draft.empty() {
		a.putComposer(draft)
	}
	a.touch()
	return cmd
}
