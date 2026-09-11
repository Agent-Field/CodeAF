package tui3

// /spend ARGS — doors onto lenses, windows, and a JSON dump of the machine bill.
//
// Bare `/spend` opens the place on Rhythm, as it always has. Words after it are
// either a lens (`models`, `days`, `year`), a window hint (`7d`, `month`), or
// `export` — never a second money UI. The conversation's own bill stays on
// `/cost`.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// runSpendCommand is `/spend` with an optional rest. It always ends on the
// spend place except for `export`, which writes a note with the path.
func (a *app) runSpendCommand(rest string) tea.Cmd {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		a.spend.lens = spendLensRhythm
		return a.showPage(pageSpend)
	}
	fields := strings.Fields(strings.ToLower(rest))
	switch fields[0] {
	case "export", "--json", "json":
		path, err := a.exportSpendJSON()
		if err != nil {
			a.note("could not export spend · " + err.Error())
			return nil
		}
		a.note("spend · " + path)
		return nil
	case "7d", "14d", "30d":
		days := 14
		_, _ = fmt.Sscanf(fields[0], "%dd", &days)
		if days < 1 {
			days = 14
		}
		a.spend.win = session.LastDays(a.now(), days)
		a.spend.lens = spendLensRhythm
		a.spend.woke = false
		cmd := a.showPage(pageSpend)
		a.rebuildSpend()
		return cmd
	case "month", "monthly":
		win := session.LastDays(a.now(), 30)
		win.Grain = session.GrainMonth
		a.spend.win = win.Normalized()
		a.spend.lens = spendLensRhythm
		a.spend.woke = false
		cmd := a.showPage(pageSpend)
		a.rebuildSpend()
		return cmd
	}
	if lens, ok := parseSpendLens(fields[0]); ok {
		cmd := a.showPage(pageSpend)
		a.setSpendLens(lens)
		return cmd
	}
	a.note("usage: /spend [models|days|year|export|7d|month]")
	return nil
}

// exportSpendJSON writes the current window's priced lines as JSON beside the
// ledger, named for the instant, and answers the path.
func (a *app) exportSpendJSON() (string, error) {
	now := a.now()
	if len(a.spend.lines) == 0 {
		// Open the place's read so export works without visiting first.
		a.spend.cache.Path = a.usageLedger
		if a.spend.win.From.IsZero() {
			a.spend.win = session.LastDays(now, spendWindowDays)
		}
		lines, _ := a.usageSince(a.spend.win.From)
		a.spend.lines = lines
	}
	win := a.spend.win.Normalized()
	type row struct {
		At     time.Time `json:"at"`
		Day    string    `json:"day,omitempty"`
		Model  string    `json:"model,omitempty"`
		Role   string    `json:"role,omitempty"`
		Calls  int       `json:"calls,omitempty"`
		Input  int       `json:"in,omitempty"`
		Output int       `json:"out,omitempty"`
		USD    float64   `json:"usd"`
		Task   string    `json:"task,omitempty"`
		Root   string    `json:"root,omitempty"`
	}
	out := make([]row, 0, len(a.spend.lines))
	for _, line := range a.spend.lines {
		if !(line.USD > 0) || !win.Holds(session.UsageLineDay(line)) {
			continue
		}
		out = append(out, row{
			At: line.At, Day: line.Day, Model: line.Model, Role: line.Role,
			Calls: line.Calls, Input: line.Input, Output: line.Output, USD: line.USD,
			Task: line.Task, Root: line.Root,
		})
	}
	dir := filepath.Dir(a.usageLedger)
	if dir == "" || dir == "." {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, fmt.Sprintf("spend-%s.json", now.Format("20060102-150405")))
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
