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
	// Enabled is the live value of the `update.auto` row. OFF means nothing is
	// downloaded on its own; the launch check still says what is out.
	Enabled bool
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
	phase    updatePhase
	tag      string
	running  string
	deadline time.Time
	// settleAt is when the ready or failed line folds away.
	settleAt time.Time
	// manual is true when a person typed the road rather than letting the offer
	// run. It changes the words, not the behaviour.
	manual bool
}

func (o updateOffer) active() bool { return o.phase != updateIdle }

// raise opens the grace window for a release the launch check found.
func (o *updateOffer) raise(tag, running string, now time.Time) {
	o.phase = updateOffering
	o.tag = strings.TrimSpace(tag)
	o.running = strings.TrimSpace(running)
	o.deadline = now.Add(codeupdate.AutoGrace)
	o.settleAt = time.Time{}
	o.manual = false
}

func (o *updateOffer) expire() {
	if o.phase == updateOffering {
		o.phase = updateDownloading
	}
}

func (o *updateOffer) downloading(manual bool) {
	o.phase = updateDownloading
	o.settleAt = time.Time{}
	o.manual = manual
}

// ready marks an install that landed, and starts the dwell that folds its line.
func (o *updateOffer) ready(now time.Time) {
	o.phase = updateReady
	o.manual = false
	o.settleAt = now.Add(updateOfferingDwell)
}

// failed marks an install that did not land, with the same dwell.
func (o *updateOffer) failed(now time.Time) {
	o.phase = updateFailed
	o.manual = false
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

// updateOfferDefer is the clause appended to a running turn's own keys while
// the grace is open, so the chance to defer survives work in the conversation.
func (a *app) updateOfferDefer() string {
	if !a.offer.offering() {
		return ""
	}
	return " · /update skip"
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
		return "codeaf " + o.tag + " installed · this session keeps running · new windows use the update; existing engines finish their work first, and an attached window keeps its engine current"
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
func (a *app) updateNowFromOffer() tea.Cmd {
	if !a.offer.offering() {
		return nil
	}
	tag := a.offer.tag
	a.offer.clear()
	a.note("installing codeaf " + tag + " now")
	return a.beginUpdate(updateChoice(tag, a.updateRunning), true)
}

// skipUpdateOffer answers this release "not now", and only while it is being
// offered. The answer is written down, so the tag is never offered again.
func (a *app) skipUpdateOffer() {
	if !a.offer.offering() {
		return
	}
	tag := a.offer.tag
	a.offer.clear()
	if a.updateAuto != nil && a.updateAuto.Dismiss != nil && tag != "" {
		_ = a.updateAuto.Dismiss(tag)
	}
	a.note("codeaf " + tag + " will not be offered again · this release only · /update installs it for the next launch whenever you want")
}

// declineUpdateOffer turns the automatic updater off for good, reached from the
// settings row and from `/update never`.
func (a *app) declineUpdateOffer() {
	a.offer.clear()
	if a.updateAuto != nil && a.updateAuto.Disable != nil {
		if err := a.updateAuto.Disable(); err != nil {
			a.note("could not turn auto update off: " + err.Error())
			return
		}
		a.updateAuto.Enabled = false
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

// tookOfferTick repaints one second of the countdown, and pauses the grace
// while a question page owns the frame: the offer cannot be seen there, so the
// clock does not run out invisibly under it.
func (a *app) tookOfferTick() tea.Cmd {
	if !a.offer.offering() {
		return nil
	}
	if a.questionRoomOpen() {
		a.offer.deadline = a.now().Add(codeupdate.AutoGrace)
		return a.offerTick()
	}
	if !a.now().Before(a.offer.deadline) {
		return a.tookUpdateGrace()
	}
	a.touch()
	return a.offerTick()
}

// tookUpdateGrace fires the automatic answer. It RE-READS the live setting and
// the persisted memory first: a person who turned auto update off during the
// window, or another window that dismissed the same tag, must not be installed
// over by a decision this session made ten seconds ago.
func (a *app) tookUpdateGrace() tea.Cmd {
	if !a.offer.offering() {
		return nil
	}
	tag := a.offer.tag
	if a.updateAuto == nil || !a.updateAuto.Enabled {
		a.offer.clear()
		a.note("auto update is off · nothing was installed · /update installs codeaf " + tag + " when you want it")
		return nil
	}
	if a.updateAuto.State != nil {
		state := a.updateAuto.State()
		if !codeupdate.ShouldOffer(state, tag, a.now()) {
			a.offer.clear()
			a.note("codeaf " + tag + " will not be installed on its own · /update installs it when you want it")
			return nil
		}
	}
	if refusal := a.updateRefusal(); refusal != "" {
		a.offer.clear()
		a.note(refusal)
		return nil
	}
	a.offer.expire()
	a.note(a.updateOfferNote())
	return a.beginUpdate(updateChoice(tag, a.updateRunning), true)
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
