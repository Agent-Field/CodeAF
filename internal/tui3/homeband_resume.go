package tui3

// ── "CONTINUE WHERE YOU LEFT OFF ON <DEVICE>?", THE RESCUE ──────────────────
//
// A person who closes a laptop mid-chat does not know that the work can be
// picked up elsewhere. When they open this window and a chat on another device
// was running or went off a short while ago AND that device is offline now, the
// home screen offers it once, as the primary action: one chord leads into the
// takeover card the chat's own row raises ([app.offerContinue]), so the yes is
// still asked there, with the reason, and nothing is taken by this card.
//
// THE OFFER IS DECIDED CONTINUOUSLY, from the newest listing and the feed's
// presence, each time either moves. A device that still looks online when the
// window opens (a lid shut without goodbye stays online until the relay's
// offline debounce, 15 s, runs out) is offered the moment the feed says it is
// gone; a device that comes back, or a chat that is let go of, withdraws the
// card. What the person answered stays answered: a device they took up or put
// off is not offered again in this opening.
//
// THE TIME BOUND. With the feed up, the card stands within one feed frame of
// the relay's offline word: the debounce plus the frame. With the feed down,
// the lapsed lease is read from the next listing, at most [machinesCap] after
// it lapsed.
//
// THE OFFER STANDS WHEN THE DEVICE IS AWAY: the change feed says it is not
// online (presence is keyed by device id, never by name), OR the chat's lease
// has lapsed ([chatlist.Off]). Without the feed (socket down, or a source that
// has none) the lapsed lease stands in for it. A device that is online and
// busy is never offered: a chat running on it is its own to finish.
//
// A card that cannot work is absent: with no [Taker] there is no takeover to
// lead into, and the offer is never made.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

const (
	resumeKey     = "alt+c"
	resumeSkipKey = "alt+x"
	// resumeRecent is how long ago a chat's last turn may be and still count as
	// where the person left off.
	resumeRecent = 24 * time.Hour

	resumeAskFormat = "Continue %s's latest chat here?"
	resumeYes       = resumeKey + " continue here"
	resumeNo        = resumeSkipKey + " not now"

	bandOrderResume = 2 // above everything: it is the first thing worth doing
)

func init() {
	registerHomeBand(homeBand{name: "resume", order: bandOrderResume,
		standing: true, draw: drawResumeBand})
}

// resumeOffer is the card's whole state. row is the chat on offer while the
// card stands; answered holds the devices the person has taken up or put off.
type resumeOffer struct {
	row      *chatlist.Row
	answered map[string]bool
}

// answer records that the person has dealt with the card for its device.
func (r *resumeOffer) answer() {
	if r.answered == nil {
		r.answered = map[string]bool{}
	}
	r.answered[r.row.Device] = true
	r.row = nil
}

// resumeState is what the offer needs to know about the fleet, so the choice of
// chat is a pure function of it.
type resumeState struct {
	self     string
	rows     []chatlist.Row
	online   []string
	feedUp   bool
	answered map[string]bool
}

// pick is the chat to offer: the newest recent one on an offline device.
func (s resumeState) pick() *chatlist.Row {
	var best *chatlist.Row
	for i := range s.rows {
		row := &s.rows[i]
		if s.offered(*row) && (best == nil || row.DurableAgo < best.DurableAgo) {
			best = row
		}
	}
	return best
}

// offered says a row is a chat to rescue.
func (s resumeState) offered(row chatlist.Row) bool {
	live := row.Status == chatlist.Running || row.Status == chatlist.Off || (row.Status == chatlist.Idle && !row.Mine)
	return live && !row.Mine && !row.HolderRemoved && (s.self == "" || row.DeviceID != s.self) && row.Device != "" && !s.answered[row.Device] && row.DurableAgo <= resumeRecent && s.away(row)
}

// away says the chat's device has let go of it or is gone: its lease lapsed,
// or the feed, when it can speak and the row names the device, does not list it
// as online.
func (s resumeState) away(row chatlist.Row) bool {
	if row.Status == chatlist.Off || (row.Status == chatlist.Idle && !row.Mine) {
		return true
	}
	return s.feedUp && row.DeviceID != "" && !slices.Contains(s.online, row.DeviceID)
}

// considerResume decides the offer afresh from the newest good listing and the
// feed. It runs whenever either has moved, and it withdraws a card whose reason
// has gone. No listing yet, or no taker, offers nothing.
func (a *app) considerResume() {
	r := &a.leftOff
	var row *chatlist.Row
	if !a.hosted() && a.taker != nil && !a.machineRead.computerRemoved {
		row = a.resumeState().pick()
	}
	if !sameOffer(r.row, row) {
		a.touch()
	}
	r.row = row
}

// resumeState is the fleet as the newest good listing and the feed show it.
func (a *app) resumeState() resumeState {
	state := resumeState{rows: a.machineRead.rows, answered: a.leftOff.answered}
	state.self = a.deviceID
	for _, device := range a.devRow.devices {
		if device.Self {
			state.self = device.ID
			break
		}
	}
	if a.dirFeed != nil {
		st := a.dirFeed.State()
		state.feedUp, state.online = st.Up, st.Online
	}
	return state
}

// sameOffer says two offers are the same chat.
func sameOffer(x, y *chatlist.Row) bool {
	if x == nil || y == nil {
		return x == y
	}
	return x.Cell == y.Cell && x.Device == y.Device
}

// resumeKeyHandler claims the card's two chords where the card stands.
func (a *app) resumeKeyHandler(key string) (tea.Cmd, bool) {
	if a.hosted() || a.machineRead.computerRemoved {
		return nil, false
	}
	row := a.leftOff.row
	if row == nil {
		return nil, false
	}
	switch key {
	case resumeKey:
		a.leftOff.answer()
		a.touch()
		if a.homeTaking != "" && !a.homeTakeAutomatic {
			a.raiseMachineAsk(*row, a.continueBusyShown(*row))
			a.home.say(continueAlreadyRunning, "")
			return nil, true
		}
		if a.homeTakeAutomatic && row.Cell == a.homeTaking && (!a.homeTakeAbandoned || a.homeTakeResult == nil) {
			return a.takeRemote(*row), true
		}
		a.abandonAutomaticTake()
		if safe, ok := a.taker.(continuationSafety); ok {
			return a.continueIfSafe(*row, safe), true
		}
		return a.offerContinue(*row, chatlist.OfferFor(*row)), true
	case resumeSkipKey:
		a.leftOff.answer()
		a.touch()
		return nil, true
	}
	return nil, false
}

func drawResumeBand(a *app, ctx bandContext) []string {
	if a.machineRead.computerRemoved {
		return nil
	}
	row := a.leftOff.row
	if row == nil {
		return nil
	}
	head := ctx.pal.ink(fit(fmt.Sprintf(resumeAskFormat, row.Device), ctx.width))
	keys := func(s string) string { return paintHint(s, ctx.pal, ctx.pal.dim) }
	return append([]string{head}, bandClauses(ctx.width, 0, keys, a.chords.say(resumeYes), a.chords.say(resumeNo))...)
}

// continuationSafety checks local work off the surface loop. A real choice
// keeps the existing card; a clean move needs no second keystroke (C31.2).
type continuationSafety interface {
	SafeToContinue(context.Context, string, string, bool) (bool, error)
}

// continueIfSafe checks immediately before taking, retaining the displayed
// lease expectation so a later holder can never be displaced by this gesture.
func (a *app) continueIfSafe(row chatlist.Row, safe continuationSafety) tea.Cmd {
	ctx, file := a.ctx, a.file
	choice := a.nextContinueAskID()
	a.continueChecking = &continuationChecked{row: row, generation: choice}
	if a.homeTakeAutomatic && a.homeTaking != "" {
		a.home.say(fmt.Sprintf("continuing %s next; %s is still on its way here", row.Title, a.homeTakeRow.Title), "")
	}
	return func() tea.Msg {
		clean, err := safe.SafeToContinue(ctx, row.Cell, file, false)
		return continuationChecked{row: row, clean: clean, generation: choice, err: err}
	}
}

type continuationChecked struct {
	row        chatlist.Row
	clean      bool
	automatic  bool
	generation uint64
	err        error
}

// continueAfterQuit is considered once, on bare startup's first directory
// answer. User input cancels the intent before an asynchronous answer arrives.
func (a *app) continueAfterQuit(err error) tea.Cmd {
	if !a.autoContinue {
		return nil
	}
	a.autoContinue = false
	if err != nil && a.taker != nil {
		// The listing already supplies its unreachable sentence. Reopening
		// Home here would erase account news that arrived ahead of this reply.
		if !a.at(pageHome) {
			return a.openHome()
		}
		return nil
	}
	if a.taker == nil || a.pickSession || a.hosted() {
		return nil
	}
	var best *chatlist.Row
	self := a.resumeState().self
	for i := range a.machineRead.rows {
		r := &a.machineRead.rows[i]
		if r.Status == chatlist.Idle && r.Released && !r.Mine && r.DeviceID != "" && r.DeviceID != self && r.ReleasedAgo >= 0 && r.ReleasedAgo <= resumeRecent && (best == nil || r.ReleasedAgo < best.ReleasedAgo) {
			best = r
		}
	}
	safe, ok := a.taker.(continuationSafety)
	if best == nil || !ok {
		return nil
	}
	row, ctx, gen, file := *best, a.ctx, a.continueGeneration, a.file
	return func() tea.Msg {
		clean, err := safe.SafeToContinue(ctx, row.Cell, file, true)
		return continuationChecked{row: row, clean: clean, automatic: true, generation: gen, err: err}
	}
}

func (a *app) continuationReady(msg continuationChecked) tea.Cmd {
	if !msg.automatic && msg.generation != 0 {
		if a.continueChecking == nil || a.continueChecking.generation != msg.generation {
			return nil
		}
		a.continueChecking = nil
	}
	if a.hosted() || a.machineRead.computerRemoved {
		return nil
	}
	if msg.automatic && msg.generation != a.continueGeneration {
		return nil
	}
	if msg.automatic {
		world, _ := a.readWorldKnown()
		for _, row := range world.Sessions() {
			if row.Open && row.Transcript != a.file {
				return nil
			}
		}
	}

	var opened tea.Cmd
	if msg.automatic && !a.at(pageHome) {
		// Home starts reads and a clock even when the chat opens immediately.
		opened = a.openHome()
	}
	if msg.err != nil {
		if !a.at(pageHome) {
			opened = a.openHome()
		}
		a.abandonAutomaticTake()
		failed := homeTakenMsg{row: msg.row, err: msg.err}
		if errors.Is(msg.err, directory.ErrLeaseHeld) {
			return tea.Batch(opened, a.lostRace(failed))
		}
		failure := a.continueFailure(failed)
		a.note(failure)
		a.showTakeFailure(failed, failure)
		return opened
	}
	if !msg.clean {
		if msg.automatic {
			return opened
		}
		return a.raiseContinue(msg.row, continueYesAt)
	}
	return tea.Batch(opened, a.takeRemoteState(homeTakenMsg{row: msg.row, automatic: msg.automatic}))
}
