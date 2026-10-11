package run

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

var tapOutcome = regexp.MustCompile(`^(?:not )?ok [0-9]+(?: |$)`)

var pytestCounts = regexp.MustCompile(`(?:^|[, ])([0-9]+) (passed|failed|skipped)(?:[, ]|$)`)

// testCounts reads explicit outcomes, never package totals or an exit code.
// Supported output is Go verbose or JSON, TAP, and pytest's final summary.
// Other commands leave counts absent, even when their exit is green.
func testCounts(output string) *factory.TestCounts {
	var counts factory.TestCounts
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "--- PASS: "):
			counts.Passed++
		case strings.HasPrefix(line, "--- FAIL: "):
			counts.Failed++
		case strings.HasPrefix(line, "--- SKIP: "):
			counts.Skipped++
		case strings.HasPrefix(line, "{"):
			var event struct{ Action, Test string }
			if json.Unmarshal([]byte(line), &event) == nil && event.Test != "" {
				switch event.Action {
				case "pass":
					counts.Passed++
				case "fail":
					counts.Failed++
				case "skip":
					counts.Skipped++
				}
			}
		case tapOutcome.MatchString(line):
			if strings.Contains(strings.ToLower(line), "# skip") {
				counts.Skipped++
			} else if strings.HasPrefix(line, "not ok ") {
				counts.Failed++
			} else {
				counts.Passed++
			}
		case strings.HasPrefix(line, "=") && strings.Contains(line, " in "):
			// Only the final pytest summary qualifies, not progress lines.
			for _, match := range pytestCounts.FindAllStringSubmatch(line, -1) {
				n, _ := strconv.Atoi(match[1])
				switch match[2] {
				case "passed":
					counts.Passed += n
				case "failed":
					counts.Failed += n
				case "skipped":
					counts.Skipped += n
				}
			}
		}
	}
	if counts.Passed+counts.Failed+counts.Skipped == 0 {
		return nil
	}
	return &counts
}
