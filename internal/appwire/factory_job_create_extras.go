package appwire

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// FactoryReasonTooOld identifies a Factory that cannot carry a create field.
const FactoryReasonTooOld = "factory_too_old"

// The creation bounds keep attachments separate from the description limit.
const (
	MaxFactoryJobContextItems  = factory.MaxJobContextItems
	MaxFactoryJobContext       = factory.MaxJobContext
	MaxFactoryJobContextTitle  = factory.MaxJobContextTitle
	MaxFactoryJobContextSource = factory.MaxJobContextSource
	MaxFactoryJobLabel         = factory.MaxJobLabel
	MaxFactoryJobKey           = 128
)

// FactoryJobContext is reference material the plan reads, outside the job text.
type FactoryJobContext struct {
	// Title names the attachment, trimmed to 1–200 characters.
	Title string `json:"title"`
	// Markdown is the complete reference text, separate from the description.
	Markdown string `json:"markdown"`
	// Source is an optional label, path or URL of at most 500 characters.
	Source string `json:"source,omitempty"`
}

var factoryJobKey = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

func checkCreateExtras(p *FactoryJobCreateParams) error {
	for field, value := range map[string]*string{"group": &p.Group, "milestone": &p.Milestone} {
		if strings.ContainsFunc(*value, unicode.IsControl) || utf8.RuneCountInString(strings.TrimSpace(*value)) > MaxFactoryJobLabel {
			return Errorf(CodeBadParams, "%s must be at most %d characters with no control characters", field, MaxFactoryJobLabel)
		}
		*value = strings.TrimSpace(*value)
	}
	if p.Budget != nil && !slices.Contains(FactoryBudgetSteps, *p.Budget) {
		return Errorf(CodeBadParams, "budget must be one of %v US dollars", FactoryBudgetSteps)
	}
	if p.Key != "" && (len(p.Key) > MaxFactoryJobKey || !factoryJobKey.MatchString(p.Key)) {
		return Errorf(CodeBadParams, "key must be 1–%d characters from [A-Za-z0-9._:-]", MaxFactoryJobKey)
	}
	if len(p.Context) > MaxFactoryJobContextItems {
		return Errorf(CodeBadParams, "context takes at most %d items", MaxFactoryJobContextItems)
	}
	total := 0
	for i := range p.Context {
		c := &p.Context[i]
		c.Title = strings.TrimSpace(c.Title)
		if n := utf8.RuneCountInString(c.Title); n < 1 || n > MaxFactoryJobContextTitle {
			return Errorf(CodeBadParams, "context title must be 1–%d characters after trimming", MaxFactoryJobContextTitle)
		}
		if utf8.RuneCountInString(c.Source) > MaxFactoryJobContextSource {
			return Errorf(CodeBadParams, "context source must be at most %d characters", MaxFactoryJobContextSource)
		}
		if strings.TrimSpace(c.Markdown) == "" {
			return Errorf(CodeBadParams, "context markdown must be non-empty")
		}
		total += utf8.RuneCountInString(c.Markdown)
	}
	if total > MaxFactoryJobContext {
		return Errorf(CodeBadParams, "context markdown must total at most %d characters", MaxFactoryJobContext)
	}
	return nil
}
