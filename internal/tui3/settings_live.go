package tui3

import "github.com/Agent-Field/codeaf/internal/config"

// refreshProfileUI adopts presentation preferences after a panel save or a chat
// turn. Keep temporary layout choices (such as the task column) and the engine's
// approval posture out: neither is a profile-only display preference.
func (a *app) refreshProfileUI() {
	a.mouse = config.MouseEnabledAt(a.profileDir)
	a.timestamps = config.TimestampsAt(a.profileDir)
	a.workMode = config.WorkAt(a.profileDir)
	a.adoptIcons()
	a.hopQuick = config.QuickSwitchAt(a.profileDir)
	a.askWait = a.consentWait()
	a.notices.enabled = config.HintsAt(a.profileDir)
	a.touch()
}
