package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
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
	lines, err := standingCharterLines(c.store, 0)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "no active charters", nil
	}
	return strings.Join(lines, "\n"), nil
}

// standingCharterLines is what every active charter watches for and how often,
// one line each. /standing is one caller; the head's standing read is the other,
// and it is the reason this is a free function rather than a method.
//
// The head used to receive this as an integer. doctor counted the charters and
// kept only len(charters), so "what are you watching for me?" could be answered
// with "three standing items" and nothing about what any of the three watched
// for — while the rail beside the conversation listed all three in full. The
// count was never the interesting part of a watch.
//
// limit <= 0 renders them all; a bounded caller passes what it can afford.
func standingCharterLines(graph *store.Store, limit int) ([]string, error) {
	if graph == nil {
		return nil, nil
	}
	charters, err := graph.ActiveCharters()
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(charters) > limit {
		charters = charters[:limit]
	}
	lines := make([]string, 0, len(charters))
	for _, charter := range charters {
		cadence := strings.TrimSpace(charter.Watch.Cadence)
		if cadence == "" {
			// Spoken, never String: this list is read by a person, and it is
			// also handed to the conversational standing read.
			cadence = charter.Watch.Spoken()
		}
		lines = append(lines, fmt.Sprintf("%s · %s · %s", charter.ID,
			cadence, strings.Join(strings.Fields(charter.Invariant), " ")))
	}
	return lines, nil
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
