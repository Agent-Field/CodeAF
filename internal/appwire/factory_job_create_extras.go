package appwire

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// FactoryReasonTooOld identifies a Factory that cannot carry a field or route.
const FactoryReasonTooOld = "factory_too_old"

// The shared bounds keep attachments separate from the description limit.
const (
	MaxFactoryJobContextItems  = factory.MaxJobContextItems
	MaxFactoryJobContext       = factory.MaxJobContext
	MaxFactoryJobContextTitle  = factory.MaxJobContextTitle
	MaxFactoryJobContextSource = factory.MaxJobContextSource
	MaxFactoryJobLabel         = factory.MaxJobLabel
	MaxFactoryJobKey           = 128
)

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
	return nil
}
