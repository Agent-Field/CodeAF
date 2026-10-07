package tui3

import (
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

// The launch offer. Three properties hold it together:
//
//   - It is one sentence in the line that already carries the idle keys, and its
//     only key is a chord. Nothing is modal, no plain letter is bound, and it
//     never takes the message box.
//   - Auto update is on by default, so doing nothing answers it: the install
//     starts after [codeupdate.AutoGrace], counted down in the line.
//   - It never restarts anything. The file is replaced in the background; this
//     process, its turns and its engines keep running. `/update` typed by hand
//     installs the same way.
//
// [updateOffer] is a pure state machine; updatedemo.go raises it deterministically
// for captures.
type UpdateCoordinator struct {
	// Enabled is the value of the `update.auto` row read when this window wired
	// its coordinator. OFF means nothing is downloaded on its own; the launch
	// check still says what is out.
	Enabled bool
	// EnabledLive re-reads the row from the profile, and it is the answer the
	// AUTOMATIC road acts on: a row changed in /settings updates this window's
	// Enabled, and a row changed in ANOTHER window is only visible here. Nil
	// falls back to Enabled.
	EnabledLive func() bool
	// State reads the remembered dismissal, failure count and backoff.
	State func() codeupdate.AutoState
	// Dismiss records "not now" for one exact release.
	Dismiss func(tag string) error
	// Failure records a failed automatic install and its words.
	Failure func(tag string, err error)
	// Success clears the failure count belonging to a release now installed.
	Success func()
	// Disable writes the `update.auto` row off.
	Disable func() error
	// Refusal names why this executable cannot be replaced in place, or "".
	Refusal func() string
}

// updateSkipKey is the offer's one action: "not now", said about one exact
// release. Install-now is `/update`, discoverable in the command list and in
// the offer's own sentence, and turning the updater off is a settings row.
const updateSkipKey = chordAltWord + "n"

// updateOfferingDwell is how long a finished or failed install keeps its line
// before the keys row settles back to the ordinary idle keys. The transcript
// note is the durable record; the footer is a moment, not a billboard.
const updateOfferingDwell = 8 * time.Second

type updatePhase uint8

const (
	updateIdle updatePhase = iota
	// updateOffering is the grace window: the release is known, the automatic
	// answer has not fired, and either the person or the clock will answer.
	updateOffering
	updateDownloading
	// updateReady is an install that finished. A later safe launch opens it.
	updateReady
	// updateFailed is an automatic install that did not finish; the failure is
	// remembered so the same release is not retried every launch.
	updateFailed
)

// updateOffer is the launch offer's whole state.
type updateOffer struct {
	phase   updatePhase
	tag     string
	running string
	// published is the moment the launch check said this exact release was
	// published, kept so the pinned resolve can hand it to the installer
	// ([codeupdate.Choice.Published]): an install record written without it
	// cannot order two same-day dev or staging builds, and a stale one would be
	// written over a newer file on disk. A raise with no check behind it leaves
	// it zero, which the installer treats as "unknown" rather than as old.
	published time.Time
	deadline  time.Time
	// settleAt is when the ready or failed line folds away.
	settleAt time.Time
}

func (o updateOffer) active() bool { return o.phase != updateIdle }

// updateAutoEnabledNow is the LIVE answer to whether the automatic road may
// still spend, read from the profile where production supplied a reader and
// from the captured flag otherwise.
func (a *app) updateAutoEnabledNow() bool {
	if a.updateAuto == nil {
		return false
	}
	if a.updateAuto.EnabledLive != nil {
		return a.updateAuto.EnabledLive()
	}
	return a.updateAuto.Enabled
}

// updateAutoMemory is the remembered answer — the dismissal, the failure
// count and its backoff — read fresh, because another window writes those too.
func (a *app) updateAutoMemory() codeupdate.AutoState {
	if a.updateAuto == nil || a.updateAuto.State == nil {
		return codeupdate.AutoState{}
	}
	return a.updateAuto.State()
}

// updateAutoWithdrawn is the ONE question the automatic road asks before it
// spends anything, and the sentence to say when the answer is no. It is asked
// at the deadline, on every countdown beat, and AGAIN after the resolver
// answers: a person turning the row off or another window dismissing the tag
// can land in any of those gaps, and the decision is not this window's to keep
// once it is stale.
func (a *app) updateAutoWithdrawn(tag string) string {
	tag = strings.TrimSpace(tag)
	if !a.updateAutoEnabledNow() {
		return "auto update is off · nothing was installed · /update installs codeaf " + tag + " when you want it"
	}
	if !codeupdate.ShouldOffer(a.updateAutoMemory(), tag, a.now()) {
		return "codeaf " + tag + " will not be installed on its own · /update installs it when you want it"
	}
	return ""
}

// raise opens the grace window for a release the launch check found.
func (o *updateOffer) raise(tag, running string, now time.Time) {
	o.phase = updateOffering
	o.tag = strings.TrimSpace(tag)
	o.running = strings.TrimSpace(running)
	o.deadline = now.Add(codeupdate.AutoGrace)
	o.settleAt = time.Time{}
}

func (o *updateOffer) expire() {
	if o.phase == updateOffering {
		o.phase = updateDownloading
	}
}

// downloading marks the phase: the file is being fetched. WHICH ROAD ASKED is
// not part of this state — the completion's words come from the message that
// carried the install, and a second flag here would be a spelling of `auto`
// that could only drift from it.
func (o *updateOffer) downloading() {
	o.phase = updateDownloading
	o.settleAt = time.Time{}
}

// ready marks an install that landed, and starts the dwell that folds its line.
func (o *updateOffer) ready(now time.Time) {
	o.phase = updateReady
	o.settleAt = now.Add(updateOfferingDwell)
}

// failed marks an install that did not land, with the same dwell.
func (o *updateOffer) failed(now time.Time) {
	o.phase = updateFailed
	o.settleAt = now.Add(updateOfferingDwell)
}

func (o *updateOffer) clear() { *o = updateOffer{} }

func (o updateOffer) offering() bool { return o.phase == updateOffering }

// remaining is the countdown shown while the grace is open.
func (o updateOffer) remaining(now time.Time) time.Duration {
	if !o.offering() {
		return 0
	}
	left := o.deadline.Sub(now)
	if left < 0 {
		return 0
	}
	return left.Round(time.Second)
}

// ── WHAT THE SURFACE SAYS ───────────────────────────────────────────────────

// updateOfferHint is the one sentence the offer puts in the keys line: plain
// text at the hint slot's own weight, no colour, no icon.
func (a *app) updateOfferHint(width int) string {
	o := a.offer
	switch o.phase {
	case updateOffering:
		lead := "codeaf " + o.tag + " is out"
		count := "installs in " + spellGrace(o.remaining(a.now()))
		skip := a.chords.say(updateSkipKey) + " skip"
		now := "/update"
		// THE ACTIONS SURVIVE A NARROW WINDOW: the countdown clause is dropped
		// before either action is.
		for _, clauses := range [][]string{
			{lead, count, skip, now},
			{lead, skip, now},
			{lead, count, skip},
			{lead, skip},
			{lead, now},
			{lead},
		} {
			line := strings.Join(clauses, " · ")
			if width > 0 && ansi.StringWidth(line) > width {
				continue
			}
			return line
		}
		return lead
	case updateDownloading:
		return "Updating codeaf in the background · keep working"
	case updateReady:
		return "codeaf " + o.tag + " installed · this session keeps running"
	case updateFailed:
		return "codeaf " + o.tag + " could not be installed · /update retries"
	}
	return ""
}

// spellGrace prints a short count for the keys line.
func spellGrace(left time.Duration) string {
	seconds := int(left / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	if seconds <= 1 {
		return "1s"
	}
	return itoa(seconds) + "s"
}

// updateOfferDeferWords is the defer clause's own words, spelled once so the
// shortener recognises exactly what this appended (steer.go's [app.hintShorter]).
const updateOfferDeferWords = "/update skip"

// updateOfferDefer is the clause appended to a running turn's own keys while
// the grace is open, so the chance to defer survives work in the conversation.
func (a *app) updateOfferDefer() string {
	if !a.offer.offering() {
		return ""
	}
	return hintSegment + updateOfferDeferWords
}

// withUpdateOffer folds the defer clause into whatever the keys line was going
// to say. It adds nothing unless the grace is open.
func (a *app) withUpdateOffer(hint string) string {
	return hint + a.updateOfferDefer()
}

// updateOfferNote is the transcript record: ONE quiet line per meaningful state,
// said once. The footer may fold away; this is what remains.
func (a *app) updateOfferNote() string {
	o := a.offer
	switch o.phase {
	case updateOffering:
		shown := o.running
		if shown == "" {
			shown = "an unstamped build"
		}
		return "codeaf " + o.tag + " is out · you have " + shown + " · it installs on its own shortly · " +
			a.chords.say(updateSkipKey) + " skips · /update installs it"
	case updateDownloading:
		return "updating codeaf in the background · this session keeps running"
	case updateReady:
		return "codeaf " + o.tag + " installed · this session keeps running · new windows open the updated app; work already running keeps its current engine until all its windows and work close"
	case updateFailed:
		return "codeaf " + o.tag + " could not be installed · this version keeps running · /update retries"
	}
	return ""
}

// quietUpdateNotice is what a launch says when a release is out but the
// automatic updater is off: knowing is still worth one dim line, acting is not
// offered until it is asked for.
func quietUpdateNotice(available codeupdate.Available) string {
	running := strings.TrimSpace(available.Running)
	if running == "" {
		running = "an unstamped build"
	}
	return "codeaf " + available.Latest + " is out · you have " + running + " · /update installs it for the next launch"
}

// ── THE ANSWERS ─────────────────────────────────────────────────────────────

// updateOfferKey reads the offer's one chord and lets every other key past. It
// answers only while the grace is open: alt+n cannot cancel a download already
// running, and pretending otherwise would be the same lie as a dead /update
// skip.
func (a *app) updateOfferKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.offer.offering() || msg.String() != updateSkipKey {
		return nil, false
	}
	a.skipUpdateOffer()
	return nil, true
}

// updateNowFromOffer is `/update` while the grace is open: install now rather
// than waiting the countdown out.
//
// IT IS THE MANUAL ROAD. A person typed it, so it is not re-checked against a
// row another window may have just turned off, and its completion carries the
// same checksum line every hand-run install does — the offer simply answered
// early.
func (a *app) updateNowFromOffer() tea.Cmd {
	if !a.offer.offering() {
		return nil
	}
	tag := a.offer.tag
	// THE MOMENT IS TAKEN BEFORE THE OFFER IS CLEARED, because the pinned
	// resolve still has to name it (see [app.pinnedChoice]).
	choice := a.pinnedChoice(a.offer.tag)
	a.offer.clear()
	a.note("installing codeaf " + tag + " now")
	// THE TAG IS OURS, NOT THEIRS: the person typed bare `/update`, so this
	// install may not roll the file back to the candidate this window pinned.
	return a.beginUpdate(choice, false, false)
}

// skipUpdateOffer answers this release "not now", and only while it is being
// offered. The answer is written down for that EXACT tag, so this release is
// never offered again — and a note that claimed more than the write proved
// would be the surface lying about a file.
func (a *app) skipUpdateOffer() {
	if !a.offer.offering() {
		return
	}
	tag := a.offer.tag
	a.offer.clear()
	if a.updateAuto != nil && a.updateAuto.Dismiss != nil && tag != "" {
		if err := a.updateAuto.Dismiss(tag); err != nil {
			// THIS WINDOW KEEPS ITS ANSWER either way: the offer and its
			// countdown are gone. Only the note changes, because only the
			// durable half is in doubt.
			a.note("skipped codeaf " + tag + " for this window · could not save the skip: " + err.Error() + " · it may be offered again at the next launch")
			return
		}
	}
	a.note("skipped codeaf " + tag + " · newer releases will still be offered · /update installs it anytime")
}

// declineUpdateOffer turns the automatic updater off for good, reached from
// `/update never`.
//
// IT DOES NOT STOP AN INSTALL ALREADY RUNNING, and it does not say it did: the
// road is about the next release, so a download in flight finishes and is
// reported by the completion that owns its line.
func (a *app) declineUpdateOffer() {
	finishing := a.updateInFlight
	a.offer.clear()
	if a.updateAuto != nil && a.updateAuto.Disable != nil {
		if err := a.updateAuto.Disable(); err != nil {
			a.note("could not turn auto update off: " + err.Error())
			return
		}
		a.updateAuto.Enabled = false
	}
	if finishing {
		a.note("auto update is off · the install already running finishes · /settings turns it back on · /update still installs a release when you ask")
		return
	}
	a.note("auto update is off · /settings turns it back on · /update still installs a release for the next launch when you ask")
}

// updateRefusal is why this executable may not be replaced in place, or "".
func (a *app) updateRefusal() string {
	if a.updateAuto == nil || a.updateAuto.Refusal == nil {
		return ""
	}
	return strings.TrimSpace(a.updateAuto.Refusal())
}

// ── THE CLOCK ───────────────────────────────────────────────────────────────

// updateOfferTickMsg is the countdown's own beat, one second apart.
type updateOfferTickMsg struct{}

// updateGraceMsg is the deadline reached.
type updateGraceMsg struct{}

// offerSettleMsg folds a ready or failed line away.
type offerSettleMsg struct{}

func (a *app) offerTick() tea.Cmd {
	if !a.offer.offering() {
		return nil
	}
	wait := time.Until(a.offer.deadline)
	if wait <= 0 {
		return surfaceTick(0, func(time.Time) tea.Msg { return updateGraceMsg{} })
	}
	if wait > time.Second {
		wait = time.Second
	}
	return surfaceTick(wait, func(time.Time) tea.Msg { return updateOfferTickMsg{} })
}

// updateOfferHidden reports whether something other than the keys row owns the
// keyboard right now, so the countdown cannot be read and its chord cannot be
// answered. The clock WAITS there rather than spending the grace invisibly.
//
// THE LIST IS [app.key]'s RUNG ORDER AND NOT A GUESS. Every layer that takes the
// whole keyboard above [app.updateOfferKey] — a team's card, the move picker,
// the switcher's menu, the map's fold menu, the wall, the first-run sheet, a
// rail plan still typing itself — a question page, every place but home and the
// conversation (whose foot is the offer's own: /settings, memory, standing,
// spend, a team's page, the chats switcher), and the two dialogs inside a place
// that own every key: the settings sheet's value box and model picker, and the
// provider panel's key box.
//
// AND THE SHEETS THAT TAKE THE KEYS SLOT OR THE WHOLE FRAME ARE ON IT TOO. The
// three typed lists put their own sentence in the slot the countdown is READ
// from — [app.hintWord] outranks [app.updateOfferHint], so the release and the
// seconds are not drawn and only the defer clause survives beside them — the
// folder chooser shades the frame it keeps underneath (view.go's
// [app.contextModalShowing]), and the phone tier's status sheet and tool detail
// replace the frame outright. In all of them the countdown cannot be watched,
// and the clock WAITS rather than running down out of sight. A door added beside
// these should ask its own question here.
//
// A RUNNING TURN IS NOT ON THIS LIST. There the offer's defer clause is drawn
// under the turn's own keys and its chord still works, which is exactly where
// the countdown must keep running (steer.go's [app.hintShorter] keeps that
// clause while it does): keeping work moving and staying current at the same
// time is the whole reason the grace exists. The list is the states that TAKE
// the slot away from the running conversation, not the conversation's own work.
func (a *app) updateOfferHidden() bool {
	if a.questionRoomOpen() || a.setup.open || a.railPlanPending.id != "" {
		return true
	}
	if a.teamMenu.on || a.navMore.on || a.tsheet.on || a.tmove.on || a.wall.on {
		return true
	}
	// A PLACE WITH A FOOT OF ITS OWN IS NOT SHOWING THE OFFER. The question is
	// the registry's and not this file's: a place says whether its keys row is
	// the offer's ([place.showsOffer]), so a room added later answers it on the
	// day it is registered rather than on the day somebody remembers this list.
	if pl := a.showing(); pl != nil && !pl.showsOffer(a) {
		return true
	}
	// THE INPUT LINE'S OWN OVERLAYS take the keys slot with their own sentence:
	// the model picker, the thinking ladder and the session picker all answer
	// through [app.hintWord], which outranks [app.updateOfferHint] — so the
	// release and the seconds are replaced, and only the defer clause rides
	// beside their keys.
	if a.pick.open || a.effPick.open || a.roster.open {
		return true
	}
	// AND THE FRAME-TAKING SHEETS: the context chooser draws over what stays
	// underneath, and the phone tier's status sheet and tool detail are pages of
	// their own ([app.contextModalShowing], [app.deckShowing],
	// [app.expandShowing]).
	if a.contextModalShowing() || a.deckShowing() || a.expandShowing() {
		return true
	}
	return a.sheetLayerOwnsKeys() || a.addPanel.open
}

// tookOfferTick repaints one second of the countdown. It pauses where the offer
// cannot be read, and it ENDS the grace the moment the automatic road has been
// answered elsewhere, rather than counting ten seconds out for an answer that
// is no longer wanted.
func (a *app) tookOfferTick() tea.Cmd {
	if !a.offer.offering() {
		return nil
	}
	if a.updateOfferHidden() {
		a.offer.deadline = a.now().Add(codeupdate.AutoGrace)
		return a.offerTick()
	}
	if withdrawn := a.updateAutoWithdrawn(a.offer.tag); withdrawn != "" {
		a.offer.clear()
		a.note(withdrawn)
		return nil
	}
	if !a.now().Before(a.offer.deadline) {
		return a.tookUpdateGrace()
	}
	a.touch()
	return a.offerTick()
}

// tookUpdateGrace fires the automatic answer at the deadline. It asks the same
// live question the countdown asks on every beat ([app.updateAutoWithdrawn]):
// a person who turned auto update off during the window, or another window
// that dismissed the same tag, must not be installed over by a decision this
// session made ten seconds ago.
func (a *app) tookUpdateGrace() tea.Cmd {
	if !a.offer.offering() {
		return nil
	}
	tag := a.offer.tag
	if withdrawn := a.updateAutoWithdrawn(tag); withdrawn != "" {
		a.offer.clear()
		a.note(withdrawn)
		return nil
	}
	if refusal := a.updateRefusal(); refusal != "" {
		a.offer.clear()
		a.note(refusal)
		return nil
	}
	a.offer.expire()
	a.note(a.updateOfferNote())
	return a.beginUpdate(a.pinnedChoice(tag), true, false)
}

// offerSettle schedules the fold of a finished line and clears it when due.
func (a *app) offerSettleTick() tea.Cmd {
	if a.offer.settleAt.IsZero() {
		return nil
	}
	wait := time.Until(a.offer.settleAt)
	if wait < 0 {
		wait = 0
	}
	return surfaceTick(wait, func(time.Time) tea.Msg { return offerSettleMsg{} })
}

func (a *app) tookOfferSettle() tea.Cmd {
	if a.offer.phase != updateReady && a.offer.phase != updateFailed {
		return nil
	}
	a.offer.clear()
	a.touch()
	return nil
}

// updateRefusalFrom is the message a typed or automatic install failure
// produces, and whether it is a refusal (which is never retried) rather than a
// failed download.
func refusalReason(err error) (string, bool) {
	var refusal *codeupdate.RefusalError
	if errors.As(err, &refusal) {
		return refusal.Reason, true
	}
	return "", false
}
