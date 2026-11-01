package tui3

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// teamMemberCell uses the same directory identity as Home for both a remote
// row and a local copy whose last holder is now another computer.
func (a *app) teamMemberCell(m teamMember) string {
	if id, ok := teamstore.RemoteCell(m.Key); ok {
		return id
	}
	if id, ok := strings.CutPrefix(m.SyncID, "cell:"); ok {
		return id
	}
	for _, t := range a.wall.teams {
		if member, ok := t.Member(m.Key); ok {
			if id, ok := strings.CutPrefix(member.SyncID, "cell:"); ok {
				return id
			}
		}
	}
	file := m.File
	if file == "" {
		file = m.Key
	}
	return filepath.Base(session.DirOf(file))
}

func (a *app) teamComputerName(id string) string {
	if a.hosted() {
		return "the other computer"
	}
	for _, d := range a.devRow.devices {
		if d.ID == id && teamComputerNamed(id, d.Name) {
			return d.Name
		}
	}
	for _, r := range a.machineRead.rows {
		if r.DeviceID == id && teamComputerNamed(id, r.Device) {
			return r.Device
		}
	}
	return "the other computer"
}

// Home's directory and roster may substitute shortened ids for absent names.
// Team words keep those identity fallbacks out of notices and provenance.
func teamComputerNamed(id, name string) bool {
	return strings.TrimSpace(name) != "" && !strings.HasPrefix(name, "dev_") &&
		!strings.HasPrefix(id, name) && !strings.HasPrefix(strings.TrimPrefix(id, "dev_"), name)
}

func (a *app) teamsElsewhere(m teamMember) string {
	if a.hosted() {
		_, remote := teamstore.RemoteCell(m.Key)
		if remote || teamstore.OnOtherComputer(m.Key) {
			return "the other computer"
		}
		return ""
	}
	if teamstore.OnOtherComputer(m.Key) {
		id := strings.TrimPrefix(m.Key, "other-computer:device:")
		id, _, _ = strings.Cut(id, ":")
		return a.teamComputerName(id)
	}
	if !a.teamsShare {
		if _, remote := teamstore.RemoteCell(m.Key); remote {
			return "the other computer"
		}
		return ""
	}
	id := a.teamMemberCell(m)
	for _, row := range a.machineRead.rows {
		if row.Cell == id && heldElsewhere(row) {
			return a.teamComputerName(row.DeviceID)
		}
	}
	if _, remote := teamstore.RemoteCell(m.Key); remote {
		return "the other computer"
	}
	return ""
}

// teamsContinue enters Home through its router before raising Home's own
// question. Arming and selecting the row keeps that question drawn and usable.
func (a *app) teamsContinue(row chatlist.Row) tea.Cmd {
	cmd := a.openHome()
	a.home.pointAt(func(line homeLine) bool { return line.remote != nil && line.remote.Cell == row.Cell })
	return tea.Batch(cmd, a.offerContinue(row, chatlist.Offer{}))
}

func (a *app) teamsRemoteGo(m teamMember, local ...func() tea.Cmd) (tea.Cmd, bool) {
	_, sentinel := teamstore.RemoteCell(m.Key)
	if !a.teamsShare {
		if !sentinel && !teamstore.OnOtherComputer(m.Key) {
			return nil, false
		}
		a.tp.msg = "This chat is on " + a.teamsElsewhere(m) + ". Open it there."
		a.note(a.tp.msg)
		a.touch()
		return nil, true
	}
	if m.Key == "" {
		return nil, false
	}
	if a.hosted() {
		_, remote := teamstore.RemoteCell(m.Key)
		if remote || teamstore.OnOtherComputer(m.Key) {
			a.tp.msg = "This chat is on the other computer. Open it there."
			a.note(a.tp.msg)
			a.touch()
			return nil, true
		}
		return nil, false
	}
	if teamstore.OnOtherComputer(m.Key) {
		say := func() tea.Cmd {
			name := m.Handle
			if name != "" {
				name = "@" + name
			} else {
				name = m.Word
			}
			a.tp.msg = fmt.Sprintf("%s's chat is on %s and is not shared. Open it there.", name, a.teamsElsewhere(m))
			a.note(a.tp.msg)
			a.touch()
			return nil
		}
		device := strings.TrimPrefix(m.Key, "other-computer:device:")
		device, _, _ = strings.Cut(device, ":")
		if roster, ok := a.machines.(Roster); ok && !a.teamComputerKnown(device) {
			return a.besideLine(func() func(bool) tea.Cmd {
				ctx, cancel := context.WithTimeout(context.Background(), machinesAskTimeout)
				defer cancel()
				devices, err := roster.Roster(ctx)
				return func(bool) tea.Cmd {
					if err == nil {
						a.devRow.version++
						a.devRow.devices = devices
					}
					return say()
				}
			}), true
		}
		return say(), true
	}
	id := a.teamMemberCell(m)
	_, remote := teamstore.RemoteCell(m.Key)
	for _, row := range a.machineRead.rows {
		if row.Cell == id {
			if !remote && !heldElsewhere(row) {
				return nil, false
			}
			if a.machineRead.down {
				a.note(a.machineRead.unavailableWord())
				return nil, true
			}
			return a.teamsContinue(row), true
		}
	}
	// A stale local transcript can be the first thing opened after launch,
	// before Home has read ownership. Wait for that reading as remote rows do.
	if !remote && (a.machines == nil || !a.machineRead.at.IsZero()) {
		return nil, false
	}
	if a.machines != nil {
		src := a.machines
		return a.besideLine(func() func(bool) tea.Cmd {
			ctx, cancel := context.WithTimeout(context.Background(), machinesAskTimeout)
			defer cancel()
			rows, version, err := chatlist.RowsForSend(ctx, src)
			return func(bool) tea.Cmd {
				a.fileMachines(homeMachinesMsg{rows: rows, version: version, err: err})
				if err != nil {
					a.note(a.machineRead.unavailableWord())
					return nil
				}
				for _, row := range rows {
					if row.Cell == id {
						if remote || heldElsewhere(row) {
							return a.teamsContinue(row)
						}
						break
					}
				}
				if !remote {
					if len(local) > 0 {
						return local[0]()
					}
					return a.trafficGo(m.Key)
				}
				a.tp.msg = "This chat is on " + a.teamsElsewhere(m) + " and is not available here. Open it there."
				a.note(a.tp.msg)
				return nil
			}
		}), true
	}
	a.tp.msg = "This chat is on " + a.teamsElsewhere(m) + ". Open it there."
	a.note(a.tp.msg)
	a.touch()
	return nil, true
}

type teamsRefreshTickMsg struct{}

// teamsRefreshArm keeps the cached dropdown and completion model current even
// with no managed member open. Its command stats through ReadSince off paint.
func (a *app) teamsRefreshArm() tea.Cmd {
	if !a.teamsShare || a.teamsDisk.refreshing || !a.wall.loaded || a.teamsOff() {
		return nil
	}
	a.teamsDisk.refreshing = true
	return teamsRefreshNext()
}
func teamsRefreshNext() tea.Cmd {
	return surfaceTick(homeEvery, func(time.Time) tea.Msg { return teamsRefreshTickMsg{} })
}
func (a *app) teamsRefreshTick() tea.Cmd {
	if !a.teamsShare || !a.wall.loaded || a.teamsOff() {
		a.teamsDisk.refreshing = false
		return nil
	}
	return tea.Batch(a.trafficReadOf(nil, false), teamsRefreshNext())
}

func (a *app) teamComputerKnown(id string) bool {
	return a.teamComputerName(id) != "the other computer"
}

// Same-chat gestures consult only Home's cached ownership. A key press cannot
// create a new directory request merely to rediscover the front chat.
func (a *app) teamsRemoteFront(key string) (tea.Cmd, bool) {
	if !a.teamsShare || key == "" {
		return nil, false
	}
	member := teamMember{Key: key}
	for _, t := range a.wall.teams {
		if m, ok := t.Member(key); ok {
			member = m
			break
		}
	}
	if a.hosted() {
		return a.teamsRemoteGo(member)
	}
	if a.teamsElsewhere(member) == "" {
		return nil, false
	}
	return a.teamsRemoteGo(member)
}
