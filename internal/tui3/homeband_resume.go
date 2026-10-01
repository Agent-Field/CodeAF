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
// THE OFFER IS DECIDED ONCE PER OPENING, from the first listing that arrives.
// Whatever it decides, it is not decided again: a device that goes offline
// later in the session is not a rescue, it is a thing the person watched.
//
// OFFLINE IS WHAT THE CHANGE FEED SAYS WHEN IT HAS SAID. Without the feed
// (socket down, or a source that has none) a chat whose lease has lapsed
// ([chatlist.Off]) stands in for it. A device that is online is never offered:
// a chat running on it is busy and a chat it let go of is the person's to open.
//
// A card that cannot work is absent: with no [Taker] there is no takeover to
// lead into, and the offer is never made.

import (
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

const (
	resumeKey     = "alt+c"
	resumeSkipKey = "alt+x"
	// resumeRecent is how long ago a chat's last turn may be and still count as
	// where the person left off.
	resumeRecent = 24 * time.Hour

	resumeAskFormat = "Continue where you left off on %s?"
	resumeYes       = resumeKey + " continue"
	resumeNo        = resumeSkipKey + " not now"

	bandOrderResume = 2 // above everything: it is the first thing worth doing
)

func init() {
	registerHomeBand(homeBand{name: "resume", order: bandOrderResume,
		standing: true, draw: drawResumeBand})
}

// resumeOffer is the card's whole state. decided says the opening's one look
// at the listing is over; row is the chat on offer while the card stands.
type resumeOffer struct {
	decided bool
	row     *chatlist.Row
}

// resumeState is what the offer needs to know about the fleet, so the choice of
// chat is a pure function of it.
type resumeState struct {
	rows   []chatlist.Row
	online []string
	feedUp bool
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
	live := row.Status == chatlist.Running || row.Status == chatlist.Off
	return live && row.Device != "" && row.DurableAgo <= resumeRecent && s.offline(row)
}

// offline reads the feed's presence when it is up, and the lapsed lease when
// it is not.
func (s resumeState) offline(row chatlist.Row) bool {
	if s.feedUp {
		return !slices.Contains(s.online, row.Device)
	}
	return row.Status == chatlist.Off
}

// considerResume makes the opening's one decision, from the first listing that
// was read. A listing that failed decides nothing.
func (a *app) considerResume(msg homeMachinesMsg) {
	r := &a.leftOff
	if r.decided || msg.err != nil {
		return
	}
	r.decided = true
	if a.taker == nil {
		return
	}
	state := resumeState{rows: msg.rows}
	if a.dirFeed != nil {
		st := a.dirFeed.State()
		state.feedUp, state.online = st.Up, st.Online
	}
	r.row = state.pick()
	a.touch()
}

// resumeKeyHandler claims the card's two chords where the card stands.
func (a *app) resumeKeyHandler(key string) (tea.Cmd, bool) {
	row := a.leftOff.row
	if row == nil {
		return nil, false
	}
	switch key {
	case resumeKey:
		a.leftOff.row = nil
		a.touch()
		return a.offerContinue(*row, chatlist.OfferFor(*row)), true
	case resumeSkipKey:
		a.leftOff.row = nil
		a.touch()
		return nil, true
	}
	return nil, false
}

func drawResumeBand(a *app, ctx bandContext) []string {
	row := a.leftOff.row
	if row == nil {
		return nil
	}
	head := ctx.pal.ink(fit(fmt.Sprintf(resumeAskFormat, row.Device), ctx.width))
	keys := func(s string) string { return paintHint(s, ctx.pal, ctx.pal.dim) }
	return append([]string{head}, bandClauses(ctx.width, 0, keys, a.chords.say(resumeYes), a.chords.say(resumeNo))...)
}
