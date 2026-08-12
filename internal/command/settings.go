package command

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
func (c *Commander) Settings() *config.Settings {
	if c == nil {
		return nil
	}
	profileDir := c.profileDir()

	// A knob whose only reader is the environment — the standing watch's
	// tenure count — needs its persisted choice back in the environment before
	// the first check of this launch.
	config.InstallPersistedEnv(profileDir)

	return config.NewSettings(config.SettingsOptions{
		ProfileDir:    profileDir,
		ModelValue:    c.CurrentModel,
		SetModel:      c.SetModel,
		SplitPct:      c.SplitPct,
		SaveSplitPct:  c.SaveSplitPct,
		Applied:       c.settingApplied,
		SpentTodayUSD: c.spentTodayUSD,
		ModelCost:     c.modelCostHint,
	})
}

// spentTodayUSD is the receipt beside the day's ceiling. The bool is the whole
// point of the seam: a window with no graph behind it has not counted zero, it
// has not counted, and the row draws nothing rather than a $0.00 nobody earned.
func (c *Commander) spentTodayUSD() (float64, bool) {
	if c == nil || c.store == nil {
		return 0, false
	}
	spent, err := c.store.SpendToday()
	if err != nil {
		return 0, false
	}
	return spent, true
}

// modelCostHint hands the registry the two price tables this process already
// holds — the operator's panel and the model catalog — so a model row can carry
// a tier word without internal/config reaching for either of them itself.
func (c *Commander) modelCostHint(slug string) string {
	if c == nil {
		return ""
	}
	return config.ModelCostHint(c.settings.Panel, c.models, slug)
}

// settingApplied lands the changes this running process can honor at once.
// The dollar rail is the one the surface must not make the user restart for:
// /budget already keeps the same field current, and the next rail check reads
// it.
func (c *Commander) settingApplied(key string) {
	if key != config.KeyDailyBudget {
		return
	}
	amount, err := config.DailyBudgetUSDAt(c.profileDir())
	if err != nil {
		return
	}
	c.setDailyBudgetUSD(amount)
}

// profileDir and setDailyBudgetUSD are the two critical sections the settings
// snapshot has: everything else on it is fixed at construction. The disk read
// between them is deliberately outside both.
func (c *Commander) profileDir() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings.ProfileDir
}

func (c *Commander) setDailyBudgetUSD(amount float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings.DailyBudgetUSD = amount
}
