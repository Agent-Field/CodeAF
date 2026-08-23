package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// These are the mechanically shared steps of THE SPACING LADDER. A named
// value is warranted only when two independent parts of the surface must agree;
// local geometry remains beside the thing it draws.
const (
	spacingBlockRows        = 1
	spacingRuleClearance    = 1
	spacingConversationLead = 2
)

// separated appends one blank row when rows already holds content and does not
// already end in space. Separators belong between blocks, so two callers asking
// for the same boundary still buy only one row.
func separated(rows []string) []string {
	if len(rows) == 0 || strings.TrimSpace(ansi.Strip(rows[len(rows)-1])) == "" {
		return rows
	}
	for range spacingBlockRows {
		rows = append(rows, "")
	}
	return rows
}
