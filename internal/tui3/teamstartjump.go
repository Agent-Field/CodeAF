package tui3

import (
	"encoding/json"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"strings"
)

// A start's new receipt carries its traffic number. Older receipts can match
// only one successful call and one root in the current team's retained history.
// Repeated identical starts are ambiguous, so they never guess another message.
func (a *app) teamStartEntryAt(id string) int {
	team, ok := a.teamOfFront()
	if !ok || team.Manager != a.frontTabKey() {
		return -1
	}
	var root teamstore.Entry
	for _, row := range a.traffic.rows[team.ID] {
		if row.ID == id && row.Kind == teamstore.KindStart && row.From == teamstore.FromManager {
			root = row
			break
		}
	}
	if root.ID == "" {
		return -1
	}
	number := teamstore.ThreadNumber(id)
	legacy, count := -1, 0
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		if !teamStartMatches(e, root, team.ID, team.Name) {
			continue
		}
		if strings.Contains(e.detail.Output, "("+number+")") {
			return i
		}
		if strings.Contains(e.detail.Output, "(#") {
			continue
		}
		legacy, count = i, count+1
	}
	if count != 1 {
		return -1
	}
	for _, row := range a.traffic.rows[team.ID] {
		if row.ID != id && row.Kind == teamstore.KindStart && row.From == teamstore.FromManager && row.To == root.To && strings.TrimSpace(row.Text) == strings.TrimSpace(root.Text) {
			return -1
		}
	}
	return legacy
}

func teamStartMatches(e *entry, root teamstore.Entry, teamID, teamName string) bool {
	if e.kind != entryTool || e.tool != "team_start" || e.status != toolOK {
		return false
	}
	var args struct{ Handle, Brief, Team string }
	if json.Unmarshal([]byte(e.detail.Args), &args) != nil {
		return false
	}
	handle := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(args.Handle)), "@")
	target := strings.TrimSpace(args.Team)
	return handle == root.To && strings.TrimSpace(args.Brief) == strings.TrimSpace(root.Text) && (target == "" || target == teamID || target == teamName)
}
