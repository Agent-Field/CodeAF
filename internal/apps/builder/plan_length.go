package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// planLineLimit follows the plan schema, shared with PLAN's parse check.
var planLineLimit = prompt.PlanLineLimit

// repairBeforeCheck applies the mechanical repairs every round: plan lines
// cut to the schema limit, and human labels and AI marks in app and screens.
func (s *Service) repairBeforeCheck(ctx context.Context, b *build, files spec.Files) error {
	if err := s.shortenPlanLines(ctx, b, files); err != nil {
		return err
	}
	changed := append(polishLabels(files), fillAskInputs(files)...)
	if len(changed) == 0 {
		return nil
	}
	if err := s.all.Store.WriteFiles(ctx, b.app, files); err != nil {
		return err
	}
	s.emit(b, bridge.BuildEvent{Step: bridge.StepWrite, Status: bridge.BuildDone, Files: changed, Log: "Wrote human labels and ask inputs in " + strings.Join(changed, ", ")})
	return nil
}

// Shorten only card summaries after WRITE has used their full detail. Preserve
// all other JSON fields so Check still reports unrelated definition mistakes.
func (s *Service) shortenPlanLines(ctx context.Context, b *build, files spec.Files) error {
	var plan map[string]any
	if json.Unmarshal(files[spec.PlanFile], &plan) != nil {
		return nil
	}
	var logs []string
	limit := planLineLimit()
	if limit <= 0 {
		return nil
	}
	for _, group := range []string{"data", "automation", "screens"} {
		lines, _ := plan[group].([]any)
		for i, entry := range lines {
			line, _ := entry.(map[string]any)
			text, ok := line["line"].(string)
			length := utf8.RuneCountInString(text)
			if !ok || length <= limit {
				continue
			}
			short := shortenClause(text, limit)
			line["line"] = short
			logs = append(logs, fmt.Sprintf("Shortened plan.json:/%s/%d/line from %d to %d characters (limit %d)", group, i, length, utf8.RuneCountInString(short), limit))
		}
	}
	if len(logs) == 0 {
		return nil
	}
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	files[spec.PlanFile] = append(raw, '\n')
	if err := s.all.Store.WriteFiles(ctx, b.app, files); err != nil {
		return err
	}
	if p, err := decodePlan(string(raw)); err == nil {
		p.Brief = b.currentPlan().Brief // plan.json has no brief; keep the one in build state
		b.setPlan(p)
	}
	for _, log := range logs {
		s.emit(b, bridge.BuildEvent{Step: bridge.StepPlan, Status: bridge.BuildDone, Files: []string{spec.PlanFile}, Log: log})
	}
	return nil
}

func shortenClause(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	prefix := string(runes[:limit])
	for _, boundary := range []string{";", "—", ",", " and "} {
		if i := strings.LastIndex(prefix, boundary); i > 0 {
			if short := strings.TrimSpace(prefix[:i]); short != "" {
				return short
			}
		}
	}
	// A space exactly at the limit completes the last word in the prefix.
	if !unicode.IsSpace(runes[limit]) {
		if i := strings.LastIndexFunc(prefix, unicode.IsSpace); i > 0 {
			prefix = prefix[:i]
		}
	}
	return strings.TrimSpace(prefix)
}

var maxLengthMessage = regexp.MustCompile(`length must be <= (\d+), but got (\d+)`)

// Schema's maxLength failures already supply character counts for every file
// kind; make the repair instruction explicit without guessing from byte sizes.
func lengthHint(issue spec.Issue) string {
	if issue.Rule != "schema" {
		return ""
	}
	counts := maxLengthMessage.FindStringSubmatch(issue.Message)
	if counts == nil {
		return ""
	}
	return fmt.Sprintf("; fix: shorten %s:%s to at most %s characters (current length %s); keep one short clause and put detail in the files", issue.File, issue.Path, counts[1], counts[2])
}
