package tui3

// ── "ADD ANOTHER MACHINE", THE CARD THAT STAYS UNTIL THERE ARE TWO ──────────
//
// A person with one machine has no way to learn that their work can follow
// them, so the home card says it, in one sentence, for as long as the fleet is
// one. The card draws NOTHING when the fleet is two or more, when the size of
// the fleet is not known, and when this connection cannot pair at all: a
// capability that cannot work is absent, not broken.
//
// THE CARD KNOWS TWO THINGS AND ASKS FOR BOTH THROUGH SMALL DOORS.
//
//   - How many devices there are: [Fleet]. Asked off the frame, and no longer
//     asked once the answer has reached two.
//   - Who asks to join: the [Approvals] door (approve.go), the same one
//     `/pair <link>` uses.
//
// THIS MACHINE MAKES NO LINK. The NEW machine makes it (`codeaf pair`), so a
// machine that is already paired has none to hand out. The opened card is two
// numbered steps and a paste target: the link pasted here goes to the approve
// screen, and its toast ends the card's job.
//
// ONE CHORD OPENS IT. Every word is the vocabulary law's: devices, and never
// the names of the machinery behind them.

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Fleet counts the devices this person has paired. Nil in [Options] is a
// connection with no directory, and the card is then absent.
type Fleet interface {
	FleetSize(ctx context.Context) (int, error)
}

const (
	// addMachineKey is the chord that opens and closes the instructions.
	addMachineKey = "alt+d"
	// fleetEnough is how many devices end the card's job.
	fleetEnough = 2
	// fleetAskTimeout bounds one ask, so a silent directory is an unknown fleet.
	fleetAskTimeout = 5 * time.Second

	addMachineHeading = "+ Add another machine"
	addMachinePitch   = "Add another machine - pick up your work anywhere, exactly where you left it."
	addMachineStepRun = "1. On the new machine, install and run: codeaf pair"
	addMachineStepPut = "2. It shows a link. Paste it here:"
	addMachineField   = "> "
	addMachineShow    = addMachineKey + " how"
	addMachineHide    = addMachineKey + " hide"

	bandOrderAddMachine = 20 // a one-machine fleet is told it can have two
)

func init() {
	registerHomeBand(homeBand{name: "addmachine", order: bandOrderAddMachine,
		kinds: []bandKind{bandKindSession, bandKindProject}, draw: drawAddMachineBand})
}

// addMachine is the card's whole state.
type addMachine struct {
	// size is the fleet's size once known, and asking says an ask is out.
	size   int
	known  bool
	asking bool
	// open says the steps are showing.
	open bool
}

// wanted says the card is on: a door that can approve, and a fleet known to be one.
func (a *app) addMachineWanted() bool {
	m := a.addMachine
	return a.approvals != nil && m.known && m.size < fleetEnough
}

// fleetMsg is one answer from the [Fleet].
type fleetMsg struct {
	size int
	err  error
}

// askFleet counts the devices, off the update loop. Once the fleet is two the
// card has no more job and nothing is asked again.
func (a *app) askFleet() tea.Cmd {
	m := &a.addMachine
	if a.fleet == nil || m.asking || (m.known && m.size >= fleetEnough) {
		return nil
	}
	m.asking = true
	fleet := a.fleet
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fleetAskTimeout)
		defer cancel()
		size, err := fleet.FleetSize(ctx)
		return fleetMsg{size: size, err: err}
	}
}

// tookFleet files the answer. A failed ask keeps what was known.
func (a *app) tookFleet(msg fleetMsg) tea.Cmd {
	m := &a.addMachine
	m.asking = false
	if msg.err == nil {
		m.size, m.known = msg.size, true
	}
	return nil
}

// toggleAddMachine is the chord. It does nothing where the card is not drawn.
func (a *app) toggleAddMachine() tea.Cmd {
	m := &a.addMachine
	if !a.addMachineWanted() {
		return nil
	}
	m.open = !m.open
	a.touch()
	return nil
}

// pasteLink is a paste that lands on the open card: a link goes to the same
// approve screen `/pair <link>` opens, and anything else is not the card's.
func (a *app) pasteLink(text string) (tea.Cmd, bool) {
	if !a.addMachine.open || !a.addMachineWanted() || !isLinkShape(text) {
		return nil, false
	}
	a.addMachine.open = false
	return a.openApprove(strings.TrimSpace(text)), true
}

// grew counts the device an approval just let in, so the card ends its job at two.
func (m *addMachine) grew() { m.size++ }

func drawAddMachineBand(a *app, ctx bandContext) []string {
	if !a.addMachineWanted() {
		return nil
	}
	m := a.addMachine
	rows := []string{ctx.pal.muted(fit(addMachineHeading, ctx.width))}
	for _, line := range wrap(addMachinePitch, ctx.width) {
		rows = append(rows, ctx.pal.dim(fit(line, ctx.width)))
	}
	if m.open {
		rows = append(rows, a.addMachineHow(ctx)...)
	}
	key := addMachineShow
	if m.open {
		key = addMachineHide
	}
	keys := func(s string) string { return paintHint(s, ctx.pal, ctx.pal.dim) }
	return append(rows, bandClauses(ctx.width, 0, keys, a.chords.say(key))...)
}

// addMachineStrip is the card standing at the foot of the resting home, drawn
// whatever the cursor is on and whether or not home has any row at all: the
// pitch is for the person with one machine, and an empty home is theirs most
// of all. It is a blank row and the card, indented as the panels are, and it
// is absent where it would take more than half of the room.
func (a *app) addMachineStrip(width, room int) []placeRow {
	inner := width - homeGridMargin
	if inner < 1 || !a.addMachineWanted() {
		return nil
	}
	card := drawAddMachineBand(a, bandContext{width: inner, pal: a.pal, now: a.now()})
	if len(card)+1 > room/2 {
		return nil
	}
	rows := []placeRow{{hit: homeMark{line: -1, pane: -1}}}
	for _, text := range card {
		rows = append(rows, placeRow{text: strings.Repeat(" ", homeGridMargin) + text, hit: homeMark{line: -1, pane: -1}})
	}
	return rows
}

// addMachineHow is the opened card: two steps, and the field the link goes in.
func (a *app) addMachineHow(ctx bandContext) []string {
	var rows []string
	for _, line := range []string{addMachineStepRun, addMachineStepPut} {
		for _, wrapped := range wrap(line, ctx.width) {
			rows = append(rows, ctx.pal.ink(fit(wrapped, ctx.width)))
		}
	}
	return append(rows, ctx.pal.accent(fit(addMachineField, ctx.width)))
}
