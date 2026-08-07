package main

import (
	"github.com/Agent-Field/aforge-v2/internal/config"
)

// Settings hands the TUI the one registry of user-tunable knobs, wired to this
// process's live seams: the hot-swappable model slots, the chat prefs beside
// the graph, and the profile directory every persisted setting is written to.
//
// It is built per call rather than cached because every row reads through a
// closure — the registry holds no value of its own, so a fresh one is exactly
// as current as the process it was built from.
//
// The headless entry points (aforge run, aforge wake) do not carry a settings
// surface: they read the same persisted values through config.Load and have no
// place to show a sheet.
func (c *chatCommander) Settings() *config.Settings {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	profileDir := c.settings.ProfileDir
	c.mu.Unlock()

	// A knob whose only reader is the environment — the standing watch's
	// tenure count — needs its persisted choice back in the environment before
	// the first check of this launch.
	config.InstallPersistedEnv(profileDir)

	return config.NewSettings(config.SettingsOptions{
		ProfileDir:   profileDir,
		ModelValue:   c.CurrentModel,
		SetModel:     c.SetModel,
		SplitPct:     c.SplitPct,
		SaveSplitPct: c.SaveSplitPct,
		Applied:      c.settingApplied,
	})
}

// settingApplied lands the changes this running process can honor at once.
// The dollar rail is the one the surface must not make the user restart for:
// /budget already keeps the same field current, and the next rail check reads
// it.
func (c *chatCommander) settingApplied(key string) {
	if key != config.KeyDailyBudget {
		return
	}
	c.mu.Lock()
	profileDir := c.settings.ProfileDir
	c.mu.Unlock()
	amount, err := config.DailyBudgetUSDAt(profileDir)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.settings.DailyBudgetUSD = amount
	c.mu.Unlock()
}
