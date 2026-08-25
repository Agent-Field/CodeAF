package tui3

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// placeMoneyInk is the one semantic door onto money's ink, so a palette move
// cannot leave one reading behind with the old meaning.
func placeMoneyInk(pal palette) func(string) string { return pal.add }

// foldLine keeps every collapsed count in one sentence grammar. A clause is
// already prose and therefore follows the count after one comma.
func foldLine(n int, clause string) string {
	line := tokens.GlyphCollapsed + " " + groupedInt(n) + " more"
	if clause = strings.TrimSpace(strings.TrimPrefix(clause, ",")); clause != "" {
		line += ", " + clause
	}
	return line
}

// appendPlaceSection gives consecutive blocks exactly one breath without
// growing a second blank when two callers describe the same boundary.
func appendPlaceSection(rows []string, heading string) []string {
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	if len(rows) > 0 {
		rows = append(rows, "")
	}
	return append(rows, heading)
}

// groupedInt is the one thousands spelling for reading-layer counts.
func groupedInt(n int) string {
	plain := strconv.Itoa(n)
	for at := len(plain) - 3; at > 0; at -= 3 {
		plain = plain[:at] + "," + plain[at:]
	}
	return plain
}
