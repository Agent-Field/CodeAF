package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Read the embedded limit so the safety net follows the schema when it changes.
var planLineLimit = sync.OnceValue(func() int {
	raw, err := spec.SchemaJSON(spec.PlanSchemaURL)
	if err != nil {
		panic(err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]struct {
				MaxLength int `json:"maxLength"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		panic(err)
	}
	return schema.Defs["line"].Properties["line"].MaxLength
})

// Shorten only card summaries after WRITE has used their full detail. Preserve
// all other JSON fields so Check still reports unrelated definition mistakes.
func (s *Service) shortenPlanLines(ctx context.Context, b *build, files spec.Files) error {
	var plan map[string]any
	if json.Unmarshal(files[spec.PlanFile], &plan) != nil {
		return nil
	}
	shortened := 0
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
			shortened++
			s.detail(b, "shortened plan line", "file", spec.PlanFile, "path", fmt.Sprintf("/%s/%d/line", group, i),
				"from", length, "to", utf8.RuneCountInString(short), "limit", limit)
		}
	}
	if shortened == 0 {
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
		b.setPlan(p)
	}
	s.emit(b, bridge.BuildEvent{Step: bridge.StepPlan, Status: bridge.BuildDone, Files: []string{spec.PlanFile}, Log: shortenedWords(shortened)})
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
