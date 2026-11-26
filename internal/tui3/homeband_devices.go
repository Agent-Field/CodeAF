package tui3

// ── THE DEVICES ROW: WHO IS ONLINE, AND A WAY TO BRING THEIR WORK HERE ──────
//
//	● This Mac  ● laptop  ○ simple (offline)
//
// The row is a home band. WHO IS ONLINE IS NEVER ASKED: it is read from the
// change feed home already follows (machinewatch.go), whose presence set the
// relay keeps current, so a device closing its lid redraws the row when the
// relay says so and not on a timer. WHO THERE IS is the roster, read with the
// other machines' chats on the same beat and the same frames.
//
// A feed that is down cannot say who is online, and the row is then absent
// rather than claiming everyone is gone.
//
// ONE CHORD OFFERS THE WORK. It picks the newest chat an online device holds
// and raises the takeover card home already has ([app.homeMachineEnter]): the
// verb is Move here, whether that device is running the chat or let it go
// ([chatlist.MoveHere]). With several devices in reach the chord
// asks which first.

import (
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Roster lists the devices a person has paired. It is an optional face of the
// [Fleet] door: one without it draws no row.
type Roster interface {
	Roster(ctx context.Context) ([]chatlist.Device, error)
}

const (
	// deviceKey is the chord that brings a device's work here.
	deviceKey      = "alt+m"
	deviceBring    = deviceKey + " move here"
	deviceArriving = "chats are still arriving from your other computer; this opens when they are ready"
	deviceNothing  = "no other device that is online has a chat to move here"
	devicePickAsk  = "Which device?"
	devicePickLeav = "leave it there"
	deviceGap      = "  "
)

func init() {
	registerHomeBand(homeBand{name: "devices", order: bandOrderDevices,
		standing: true, draw: drawDevicesBand})
}

// deviceRoster is the row's whole state: the devices last read, and whether a
// read is out.
type deviceRoster struct {
	// Version invalidates cached provenance when a computer is renamed.
	version      uint64
	devices      []chatlist.Device
	asking       bool
	bringPending bool
	// Asked counts the reads started, so an answer can say which ask it is.
	asked uint64
	// Enter is an Enter on a chat held elsewhere that is waiting for a roster
	// read begun after it, and EnterAfter is the first ask that counts. See
	// [app.homeMachineEnter] for why Enter waits.
	enter      *homeLine
	enterAfter uint64
}

// rosterMsg is one answer from the [Roster], carrying the count of the ask it
// answers.
type rosterMsg struct {
	devices []chatlist.Device
	err     error
	seq     uint64
}

// askRoster lists the devices, off the update loop. One ask is out at a time.
func (a *app) askRoster() tea.Cmd {
	if a.hosted() {
		return nil
	}
	src, ok := a.fleet.(Roster)
	if !ok || a.devRow.asking {
		return nil
	}
	a.devRow.asking = true
	a.devRow.asked++
	seq := a.devRow.asked
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fleetAskTimeout)
		defer cancel()
		devices, err := src.Roster(ctx)
		return rosterMsg{devices: devices, err: err, seq: seq}
	}
}

// tookRoster files the answer. A failed ask keeps what was known.
func (a *app) tookRoster(msg rosterMsg) tea.Cmd {
	a.devRow.asking = false
	if msg.err == nil {
		if !slices.Equal(a.devRow.devices, msg.devices) {
			a.devRow.version++
		}
		a.devRow.devices = msg.devices
		a.machineRead.rows = a.withHolderSeen(a.machineRead.rows)
		a.considerResume()
		a.rebuildMachines()
		a.touch()
	}
	return tea.Batch(a.finishBring(), a.finishEnter(msg))
}

// rosterCanAdmit says a chat's holder is unknown only because the roster has not
// said so yet: everything else that makes a held chat open live is true, a
// roster can be asked, and the holder is not in the one last read. Such a chat
// waits for a fresh read instead of being refused.
func (a *app) rosterCanAdmit(row chatlist.Row) bool {
	if _, ok := a.fleet.(Roster); !ok || !a.liveOffered() || a.hosted() {
		return false
	}
	if a.machineRead.computerRemoved || row.HolderRemoved || !heldElsewhere(row) {
		return false
	}
	for _, d := range a.devRow.devices {
		if (row.DeviceID != "" && d.ID == row.DeviceID) || (row.DeviceID == "" && row.Device != "" && d.Name == row.Device) {
			return false
		}
	}
	return true
}

// waitForRoster holds an Enter on a held chat until a roster read that began
// after the Enter has landed, and starts that read. A second Enter while one
// waits changes nothing. It answers false when no read can be asked.
func (a *app) waitForRoster(line homeLine) (tea.Cmd, bool) {
	if a.devRow.enter != nil {
		return nil, true
	}
	a.devRow.enterAfter = a.devRow.asked + 1
	if !a.devRow.asking {
		ask := a.askRoster()
		if ask == nil {
			return nil, false
		}
		a.devRow.enter = &line
		return ask, true
	}
	a.devRow.enter = &line
	return nil, true
}

// finishEnter settles the Enter that waited for the roster. An answer to a read
// begun before the Enter asks again, because it may predate the holder; the
// first answer that begun after it decides: live when the holder is a computer
// of the roster, the Move card when it is not or the roster cannot be read.
func (a *app) finishEnter(msg rosterMsg) tea.Cmd {
	line := a.devRow.enter
	if line == nil || line.remote == nil {
		a.devRow.enter = nil
		return nil
	}
	if msg.seq < a.devRow.enterAfter {
		if ask := a.askRoster(); ask != nil {
			return ask
		}
	}
	a.devRow.enter = nil
	if !a.at(pageHome) || a.hosted() {
		return nil
	}
	row := *line.remote
	for _, fresh := range a.machineRead.rows {
		if fresh.Cell == row.Cell {
			row = fresh
			break
		}
	}
	if a.liveEligible(row) {
		return a.homeOpenLiveLater(*line, row)
	}
	return a.homeMachineMove(*line)
}

// presence is who the feed says is online, and whether it can say at all.
func (a *app) presence() (online map[string]bool, known bool) {
	if a.dirFeed == nil {
		return nil, false
	}
	st := a.dirFeed.State()
	online = make(map[string]bool, len(st.Online))
	for _, id := range st.Online {
		online[id] = true
	}
	return online, st.Up
}

// devicesWanted says the row is drawn: a roster of more than one device and a
// feed that can say who is online.
func (a *app) devicesWanted() bool {
	if a.hosted() {
		return false
	}
	_, known := a.presence()
	return known && len(a.devRow.devices) > 1
}

// paintPresence tells a device that is here from one that is away by colour as
// well as by the dot: online is green, away is dim.
func paintPresence(pal palette, mark string, online bool) string {
	if online {
		return pal.add(mark)
	}
	return pal.dim(mark)
}

func drawDevicesBand(a *app, ctx bandContext) []string {
	if !a.devicesWanted() {
		return nil
	}
	online, _ := a.presence()
	marks := make([]string, 0, len(a.devRow.devices))
	for _, d := range a.devRow.devices {
		marks = append(marks, paintPresence(ctx.pal, d.Mark(online[d.ID], DeviceRow{Self: d.Self, Online: online[d.ID]}.dot(ctx.pal)), online[d.ID] || d.Self))
	}
	rows := []string{fit(strings.Join(marks, deviceGap), ctx.width)}
	if line := notSharedLine(a.notShared); line != "" {
		rows = append(rows, ctx.pal.dim(fit(line, ctx.width)))
	}
	if len(a.devicePicks()) == 0 {
		return rows
	}
	keys := func(s string) string { return paintHint(s, ctx.pal, ctx.pal.dim) }
	return append(rows, bandClauses(ctx.width, 0, keys, a.chords.say(deviceBring))...)
}

// devicePicks are the chats an online device offers: for each, the newest one it
// holds that can be brought here, in the roster's order.
func (a *app) devicePicks() []chatlist.Row {
	if a.machineRead.computerRemoved {
		return nil
	}
	online, known := a.presence()
	if !known {
		return nil
	}
	var picks []chatlist.Row
	for _, d := range a.devRow.devices {
		if row, ok := a.newestBringable(d.ID); ok && online[d.ID] && !d.Self {
			picks = append(picks, row)
		}
	}
	return picks
}

// newestBringable is the newest chat of a device that offers a takeover. The
// rows are held newest first.
func (a *app) newestBringable(device string) (chatlist.Row, bool) {
	for _, row := range a.machineRead.rows {
		if row.DeviceID == device && chatlist.OfferFor(row).Kind == chatlist.ContinueHere {
			return row, true
		}
	}
	return chatlist.Row{}, false
}

// bringWork is the chord: nothing to bring says so, one device goes straight to
// the takeover card, and several ask which.
func (a *app) bringWork() tea.Cmd {
	if a.hosted() {
		return nil
	}
	picks := a.devicePicks()
	switch len(picks) {
	case 0:
		if a.machines != nil && (a.machinesAsking || a.devRow.asking || a.machineRead.at.IsZero() || a.feedAhead() || a.otherDeviceOnline()) {
			a.devRow.bringPending = true
			a.home.say(deviceArriving, "")
			return a.askMachines()
		}
		a.home.say(deviceNothing, "")
		return nil
	case 1:
		return a.homeMachineMove(machineEntry(picks[0]))
	}
	a.raiseMachineAsk(picks[0], a.devicePickShown(picks))
	return nil
}

func machineEntry(row chatlist.Row) homeLine { return homeLine{kind: homeMachineRow, remote: &row} }

// devicePickShown asks which device to bring work from, the verbs as answers.
func (a *app) devicePickShown(picks []chatlist.Row) questionShown {
	options := make([]session.AnswerOption, 0, len(picks)+1)
	for i, row := range picks {
		options = append(options, session.AnswerOption{Key: string(rune('1' + i)), Label: chatlist.VerbFrom(row)})
	}
	options = append(options, session.AnswerOption{Key: string(rune('1' + len(picks))), Label: devicePickLeav, Safe: true})
	return questionShown{
		question: session.Question{
			Kind:    homeContinueKind,
			Ask:     session.AskConfirmation,
			Form:    session.FormCard,
			Asker:   session.Asker{Kind: session.AskerSurface},
			Head:    devicePickAsk,
			Options: options,
			Stakes:  session.StakesReversible,
			Asked:   a.now(),
		},
		pick: len(picks),
		local: func(answer session.Answer) tea.Cmd {
			for i, row := range picks {
				if answer.FirstKey() == string(rune('1'+i)) {
					return a.homeMachineMove(machineEntry(row))
				}
			}
			return nil
		},
	}
}

// otherDeviceOnline distinguishes a chat still being published from a fleet
// with nobody available. Presence and catalog publication are separate frames.
func (a *app) otherDeviceOnline() bool {
	online, known := a.presence()
	if !known {
		return false
	}
	for _, d := range a.devRow.devices {
		if d.Self {
			delete(online, d.ID)
		}
	}
	return len(online) > 0
}

// finishBring keeps an early chord alive across the listing and roster reads.
// It offers the ordinary confirmation; arrival never silently takes a chat.
func (a *app) finishBring() tea.Cmd {
	if !a.devRow.bringPending || len(a.devicePicks()) == 0 {
		return nil
	}
	a.devRow.bringPending = false
	return a.bringWork()
}
