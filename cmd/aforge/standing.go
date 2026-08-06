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
	c.mu.Lock()
	base := c.settings.DailyBudgetUSD
	profileDir := c.settings.ProfileDir
	c.mu.Unlock()

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
		c.mu.Lock()
		c.settings.DailyBudgetUSD = amount
		c.mu.Unlock()
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
		lines = append(lines, fmt.Sprintf("%s · %s · %s", charter.ID,
			charter.Spec.Watch.Cadence, strings.Join(strings.Fields(charter.Spec.Invariant), " ")))
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
