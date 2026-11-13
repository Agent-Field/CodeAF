package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
)

// refreshProfileUI adopts presentation preferences after a panel save or a chat
// turn. Keep temporary layout choices (such as the task column) and the engine's
// approval posture out: neither is a profile-only display preference.
func (a *app) refreshProfileUI() {
	a.modelRotation = config.ChatModelRotationAt(a.profileDir)
	a.mouse = config.MouseEnabledAt(a.profileDir)
	a.timestamps = config.TimestampsAt(a.profileDir)
	a.workMode = config.WorkAt(a.profileDir)
	a.teamsSharingCmd = a.refreshTeamsSharing()
	a.adoptIcons()
	a.hopQuick = config.QuickSwitchAt(a.profileDir)
	a.askWait = a.consentWait()
	a.notices.enabled = config.HintsAt(a.profileDir)
	a.touch()
}

// adoptTeamsSharing also retires a clock armed before pairing ended.
func (a *app) adoptTeamsSharing(sharing bool) {
	sharing = sharing && (!a.machineRead.computerRemoved || a.teamsDisk.door.Sharing != nil)
	changed := a.teamsShare != sharing
	a.teamsShare = sharing
	if !a.teamsShare {
		a.teamsDisk.refreshing = false
		// Unpaired adoption stays as cheap as explicit off. Only leaving
		// effective sharing can have cached mirror rows to retire.
		if changed {
			a.traffic.mirror = nil
			for id, entries := range a.traffic.rows {
				kept := entries[:0:0]
				for _, entry := range entries {
					if entry.Origin == "" {
						kept = append(kept, entry)
					}
				}
				if len(kept) != len(entries) {
					a.traffic.rows[id] = kept
					a.traffic.version++
				}
			}
		}
	}
	if changed {
		if a.comp.open {
			a.fillMentions()
			a.comp.rank()
		}
		if a.home.comp.open {
			a.fillHomeMentions()
			a.home.comp.rank()
		}
	}
}

type teamsSharingMsg struct {
	generation uint64
	enabled    bool
}

func (a *app) takeTeamsSharing(msg teamsSharingMsg) {
	if msg.generation == a.teamsShareGeneration {
		a.adoptTeamsSharing(msg.enabled)
		a.touch()
	}
}

// Pairing events refresh the gate beside the surface loop, just like disk reads.
func (a *app) refreshTeamsSharing() tea.Cmd {
	a.teamsShareGeneration++
	return a.readTeamsSharing()
}

// readTeamsSharing captures the source and generation before leaving the loop.
func (a *app) readTeamsSharing() tea.Cmd {
	dir, paired, generation := a.profileDir, a.teamsPaired, a.teamsShareGeneration
	remote := a.teamsDisk.door.Sharing
	return func() tea.Msg {
		if remote != nil {
			enabled, err := remote()
			if err != nil {
				return nil
			}
			return teamsSharingMsg{generation, enabled}
		}
		return teamsSharingMsg{generation, config.TeamsSharingAt(dir, paired)}
	}
}

// takeTeamsSharingCmd hands a Settings save's deferred read to the event pump.
func (a *app) takeTeamsSharingCmd() tea.Cmd {
	cmd := a.teamsSharingCmd
	a.teamsSharingCmd = nil
	return cmd
}
