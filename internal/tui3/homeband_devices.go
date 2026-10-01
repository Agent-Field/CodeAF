package tui3

// ── THE DEVICES ROW: WHO IS ONLINE, AND A WAY TO BRING THEIR WORK HERE ──────
//
//	● This Mac  ● spark  ○ dumb (offline)
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
// verb is Move here for a chat that device is running and Continue here for one
// it let go of ([chatlist.VerbFor]). With several devices in reach the chord
// asks which first.

import (
	"context"
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
	deviceBring    = deviceKey + " bring work here"
	deviceNothing  = "no other device that is online has a chat to bring here"
	devicePickAsk  = "Which device?"
	devicePickLeav = "leave it there"
	deviceGap      = "  "
)

func init() {
	registerHomeBand(homeBand{name: "devices", order: bandOrderDevices,
		kinds: []bandKind{bandKindSession, bandKindProject}, draw: drawDevicesBand})
}

// deviceRoster is the row's whole state: the devices last read, and whether a
// read is out.
type deviceRoster struct {
	devices []chatlist.Device
	asking  bool
}

// rosterMsg is one answer from the [Roster].
type rosterMsg struct {
	devices []chatlist.Device
	err     error
}

// askRoster lists the devices, off the update loop. One ask is out at a time.
func (a *app) askRoster() tea.Cmd {
	src, ok := a.fleet.(Roster)
	if !ok || a.devRow.asking {
		return nil
	}
	a.devRow.asking = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fleetAskTimeout)
		defer cancel()
		devices, err := src.Roster(ctx)
		return rosterMsg{devices: devices, err: err}
	}
}

// tookRoster files the answer. A failed ask keeps what was known.
func (a *app) tookRoster(msg rosterMsg) tea.Cmd {
	a.devRow.asking = false
	if msg.err == nil {
		a.devRow.devices = msg.devices
		a.touch()
	}
	return nil
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
	_, known := a.presence()
	return known && len(a.devRow.devices) > 1
}

func drawDevicesBand(a *app, ctx bandContext) []string {
	if !a.devicesWanted() {
		return nil
	}
	online, _ := a.presence()
	marks := make([]string, 0, len(a.devRow.devices))
	for _, d := range a.devRow.devices {
		marks = append(marks, d.Mark(online[d.ID]))
	}
	rows := []string{ctx.pal.ink(fit(strings.Join(marks, deviceGap), ctx.width))}
	if len(a.devicePicks()) == 0 {
		return rows
	}
	keys := func(s string) string { return paintHint(s, ctx.pal, ctx.pal.dim) }
	return append(rows, bandClauses(ctx.width, 0, keys, a.chords.say(deviceBring))...)
}

// devicePicks are the chats an online device offers: for each, the newest one it
// holds that can be brought here, in the roster's order.
func (a *app) devicePicks() []chatlist.Row {
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
	picks := a.devicePicks()
	switch len(picks) {
	case 0:
		a.home.say(deviceNothing, "")
		return nil
	case 1:
		return a.homeMachineEnter(machineEntry(picks[0]))
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
					return a.homeMachineEnter(machineEntry(row))
				}
			}
			return nil
		},
	}
}
