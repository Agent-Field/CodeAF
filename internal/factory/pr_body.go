package factory

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var reportedTestTotals = regexp.MustCompile(`(?i)\b(?:all\s+)?[0-9]+\+?\s+(?:(?:named|retrieval|package)\s+)*(?:tests?|outcomes?)\s+(?:pass(?:ed)?|are\s+passing)\b`)

func publicationSentence(s string) string {
	s = reportedTestTotals.ReplaceAllString(s, "tests passed")
	s = strings.Join(strings.Fields(s), " ")
	// Keep complete sentences; never publish a mid-sentence slice of stage output.
	if len(s) > 650 {
		end := strings.LastIndexAny(s[:650], ".!?")
		if end < 0 {
			return ""
		}
		s = s[:end+1]
	}
	return s
}

// PublishBody is shared by the pre-publish preview and the actual GitHub write.
func PublishBody(it Item, trailer string) string {
	var b strings.Builder
	b.WriteString("## Changes\n\n")
	var summaries []string
	var commands []string
	var finalSummary []string
	if it.Stream != nil {
		for _, ph := range it.Stream.Phases {
			if ph.Result == nil {
				continue
			}
			if len(ph.Result.PublishSummary) > 0 {
				finalSummary = ph.Result.PublishSummary
			}
			if ph.Name == "write" || ph.Name == "review" || IsCIRepairStage(Stage{Name: ph.Name}) {
				for _, para := range strings.Split(ph.Result.Output, "\n\n") {
					text := publicationSentence(para)
					lower := strings.ToLower(text)
					if text == "" || strings.HasPrefix(lower, "#") || strings.HasPrefix(lower, "measured tests:") || (ph.Name == "review" && !strings.Contains(lower, "fixed")) {
						continue
					}
					summaries = append(summaries, text)
					break
				}
			}
			if ph.Name == "proof" || IsCIRepairStage(Stage{Name: ph.Name}) {
				for _, note := range ph.Result.Notes {
					raw, ok := strings.CutPrefix(note, "Command receipt: ")
					if !ok {
						continue
					}
					var receipt struct {
						Command string `json:"command"`
						Outcome string `json:"outcome"`
					}
					if json.Unmarshal([]byte(raw), &receipt) == nil && receipt.Outcome == "passed" {
						command := strings.Join(strings.Fields(receipt.Command), " ")
						if len(command) <= 500 {
							commands = append(commands, command)
						}
					}
				}
			}
		}
	}
	if len(finalSummary) > 0 {
		summaries = nil
		for _, line := range finalSummary {
			if text := publicationSentence(line); text != "" {
				summaries = append(summaries, text)
			}
		}
	}
	if len(summaries) == 0 {
		summaries = []string{it.Title}
	}
	if len(summaries) > 3 {
		summaries = append(summaries[:2], summaries[len(summaries)-1])
	}
	for _, s := range summaries {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	if it.Change != nil && it.Change.Stat != nil {
		fmt.Fprintf(&b, "- Final change: %d files, +%d/−%d.\n", it.Change.Stat.Files, it.Change.Stat.Added, it.Change.Stat.Removed)
	}
	if it.Kind == KindIssue && it.Num > 0 {
		fmt.Fprintf(&b, "\nCloses #%d.\n", it.Num)
	}
	b.WriteString("\n## Validation\n\n")
	seen := map[string]bool{}
	for _, cmd := range commands {
		if seen[cmd] {
			continue
		}
		seen[cmd] = true
		fmt.Fprintf(&b, "- Passed locally: `%s`.\n", strings.ReplaceAll(cmd, "`", "'"))
		if len(seen) >= 3 {
			break
		}
	}
	claims := append(append([]Claim(nil), it.Proof...), it.Policy...)
	shown := 0
	for _, cl := range claims {
		if cl.OK {
			shown++
		}
	}
	fmt.Fprintf(&b, "- Proof · %d of %d claims shown locally. GitHub CI is checked after publishing.\n", shown, len(claims))
	listed := 0
	for _, cl := range claims {
		text := publicationSentence(cl.Text)
		lower := strings.ToLower(text)
		if !cl.OK || text == "" || strings.Contains(lower, "pre-change") || strings.Contains(lower, "before the change") || strings.Contains(lower, "baseline") {
			continue
		}
		if seen[lower] {
			continue
		}
		seen[lower] = true
		fmt.Fprintf(&b, "- ✓ %s\n", text)
		listed++
		if listed >= 3 {
			break
		}
	}
	b.WriteString("\nby codeaf factory")
	if trailer != "" {
		b.WriteString("\n\n" + trailer)
	}
	return b.String()
}
