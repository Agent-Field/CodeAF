package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

func (c *chatCommander) Budget(arguments []string) (string, error) {
	if c == nil || c.store == nil {
		return "", fmt.Errorf("budget store unavailable")
	}
	base, profileDir := c.dailyRailSettings()

	if len(arguments) == 0 {
		rail, err := c.store.DailyRailToday(base)
		if err != nil {
			return "", err
		}
		ceiling := "$" + formatBudgetUSD(rail.Ceiling)
		if rail.Unlimited {
			ceiling = "unlimited"
		}
		return fmt.Sprintf("spent $%.2f of %s · resets midnight", rail.Spend, ceiling), nil
	}
	if len(arguments) == 2 && arguments[0] == "default" {
		amount, err := parseBudgetUSD(arguments[1])
		if err != nil {
			return "", err
		}
		if err := config.WriteDailyBudgetUSD(profileDir, amount); err != nil {
			return "", err
		}
		c.setDailyBudgetUSD(amount)
		return "default daily budget → $" + formatBudgetUSD(amount), nil
	}
	if len(arguments) == 2 && arguments[0] == "unlimited" && arguments[1] == "today" {
		if err := c.store.RaiseDailyRailUnlimited("slash:/budget unlimited today"); err != nil {
			return "", err
		}
		return "today's budget → unlimited · resets midnight", nil
	}
	if len(arguments) == 1 {
		ceiling, err := parseBudgetUSD(arguments[0])
		if err != nil {
			return "", err
		}
		if ceiling == 0 {
			return "", fmt.Errorf("use /budget unlimited today for no ceiling")
		}
		rail, err := c.store.DailyRailToday(base)
		if err != nil {
			return "", err
		}
		if rail.Unlimited {
			return "", fmt.Errorf("today is already unlimited")
		}
		if ceiling <= rail.Ceiling {
			return "", fmt.Errorf("today's ceiling is already $%s", formatBudgetUSD(rail.Ceiling))
		}
		if err := c.store.RaiseDailyRail(ceiling-rail.Ceiling, "slash:/budget"); err != nil {
			return "", err
		}
		return "today's budget → $" + formatBudgetUSD(ceiling) + " · resets midnight", nil
	}
	return "", fmt.Errorf("usage: /budget [amount | default amount | unlimited today]")
}

// dailyRailSettings reads the ceiling and the directory it is persisted in from
// one instant, so a concurrent /budget default cannot leave them disagreeing.
func (c *chatCommander) dailyRailSettings() (base float64, profileDir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings.DailyBudgetUSD, c.settings.ProfileDir
}

func (c *chatCommander) Standing() (string, error) {
	if c == nil || c.store == nil {
		return "", fmt.Errorf("standing store unavailable")
	}
	charters, err := c.store.ActiveCharters()
	if err != nil {
		return "", err
	}
	if len(charters) == 0 {
		return "no active charters", nil
	}
	lines := make([]string, 0, len(charters))
	for _, charter := range charters {
		cadence := strings.TrimSpace(charter.Watch.Cadence)
		if cadence == "" {
			cadence = charter.Watch.String()
		}
		lines = append(lines, fmt.Sprintf("%s · %s · %s", charter.ID,
			cadence, strings.Join(strings.Fields(charter.Invariant), " ")))
	}
	return strings.Join(lines, "\n"), nil
}

func parseBudgetUSD(raw string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("budget must be a non-negative dollar amount")
	}
	return value, nil
}

func formatBudgetUSD(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
