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
//   - The link a new machine opens: [PairLinker], an OPTIONAL face of the
//     [Pairing] door (pair.go). A door that has no link yet is a card that
//     shows the instructions alone.
//
// ONE CHORD OPENS IT. The words under it name both ways in — the app and the
// terminal — because the person at this machine does not yet know which one
// the new machine will use. Every word is the vocabulary law's: devices, and
// never the names of the machinery behind them.

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

// PairLinker is the optional face of a [Pairing] door that can hand out the
// link a new machine opens to ask to join. The link is short-lived, so it is
// asked for each time the card is opened and never kept across closes.
type PairLinker interface {
	Link(ctx context.Context) (string, error)
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
	addMachineApp     = "In the app: on the new machine choose Add this machine."
	addMachineCLI     = "In a terminal: run codeaf pair on the new machine."
	addMachineThen    = "Then approve it here."
	addMachineShow    = addMachineKey + " how"
	addMachineHide    = addMachineKey + " hide"
	addMachineMaking  = "making a link…"
	addMachineCheck   = "Check number: "

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
	// open says the instructions are showing, and link is the link made for
	// them ("" until it arrives or when the door has none).
	open bool
	link string
	// The live link's own state (homeband_addmachine_live.go): the wait that is
	// out, the check number beside the link, and how the wait ended.
	run    *linkCardRun
	check  string
	ended  string
	paired bool
}

// wanted says the card is on: a door that can pair, and a fleet known to be one.
func (a *app) addMachineWanted() bool {
	m := a.addMachine
	return a.pairing != nil && m.known && (m.size < fleetEnough || m.paired)
}

// fleetMsg is one answer from the [Fleet].
type fleetMsg struct {
	size int
	err  error
}

// pairLinkMsg is one answer from the [PairLinker].
type pairLinkMsg struct {
	link string
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

// askLink makes the link for the open card, if the door can.
func (a *app) askLink() tea.Cmd {
	if live, ok := a.pairing.(LivePairLinker); ok {
		return a.startLive(live)
	}
	door, ok := a.pairing.(PairLinker)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fleetAskTimeout)
		defer cancel()
		link, err := door.Link(ctx)
		return pairLinkMsg{link: link, err: err}
	}
}

// tookLink files the link; a closed card has no use for one.
func (a *app) tookLink(msg pairLinkMsg) tea.Cmd {
	if a.addMachine.open && msg.err == nil {
		a.addMachine.link = msg.link
	}
	return nil
}

// toggleAddMachine is the chord. It does nothing where the card is not drawn.
func (a *app) toggleAddMachine() tea.Cmd {
	m := &a.addMachine
	if !a.addMachineWanted() {
		return nil
	}
	m.stopLive()
	m.open = !m.open
	a.touch()
	if !m.open {
		return nil
	}
	return a.askLink()
}

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

// addMachineHow is the opened card: the link when there is one, and the two
// ways the new machine can ask, then the one step left on this machine.
func (a *app) addMachineHow(ctx bandContext) []string {
	var rows []string
	if _, live := a.pairing.(LivePairLinker); live {
		rows = a.addMachine.liveRows(ctx.pal.ink, ctx.pal.dim, ctx.width)
	} else if a.addMachine.link != "" {
		rows = append(rows, ctx.pal.ink(fit(a.addMachine.link, ctx.width)))
	} else if _, ok := a.pairing.(PairLinker); ok {
		rows = append(rows, ctx.pal.dim(addMachineMaking))
	}
	if a.addMachine.ended != "" {
		return rows
	}
	for _, line := range []string{addMachineApp, addMachineCLI, addMachineThen} {
		for _, wrapped := range wrap(line, ctx.width) {
			rows = append(rows, ctx.pal.ink(fit(wrapped, ctx.width)))
		}
	}
	return rows
}
