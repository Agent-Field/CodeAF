package prompt

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// planLimits are the plan schema's own maxLength values. The prompt renders
// them instead of copying numbers, so a schema change reaches the model.
type planLimits struct{ Line, Name int }

var loadPlanLimits = sync.OnceValue(func() planLimits {
	raw, err := spec.SchemaJSON(spec.PlanSchemaURL)
	if err != nil {
		panic("plan schema is embedded: " + err.Error())
	}
	type maxLen struct {
		MaxLength int `json:"maxLength"`
	}
	var schema struct {
		Properties map[string]maxLen `json:"properties"`
		Defs       map[string]struct {
			Properties map[string]maxLen `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		panic("plan schema is embedded: " + err.Error())
	}
	return planLimits{
		Line: schema.Defs["line"].Properties["line"].MaxLength,
		Name: schema.Properties["name"].MaxLength,
	}
})

// limit renders "aim ≤ N, hard limit M characters": the aim leaves room for
// the owner's later edits, the hard limit is what Check rejects.
func limit(hard int) string {
	return fmt.Sprintf("aim ≤ %d, hard limit %d characters", hard*3/5, hard)
}

func lineLimit() string { return limit(loadPlanLimits().Line) }
func nameLimit() string { return fmt.Sprintf("hard limit %d characters", loadPlanLimits().Name) }

// partHints says when to reach for a kit part. kit.json says what a part
// takes, not what it is for, and a cheap model picks parts by name alone.
// Only parts the catalog actually lists are shown (see kitHints).
var partHints = []struct{ Part, Hint string }{
	{"SectionTabs", "screen nav; items = every screen title in plan order"},
	{"ScreenSubtitle", "one line under the list title: noun + phrase (\"open deals · biggest first\")"},
	{"StatStrip", "counts per status stage above a list; the daily view's first glance"},
	{"FilterTokens", "quick filters the user flips daily (stage, owner:me)"},
	{"RecordList", "the main list/board of a record type"},
	{"BulkBar", "actions on several selected rows"},
	{"Panel", "the selected record beside its list"},
	{"Chart", "a trend or total the owner asked to watch"},
	{"Empty", "what to add first when a list is empty"},
}

// kitHints is partHints cut to the parts catalog names, one line each.
func kitHints(catalog string) string {
	var lines []string
	for _, h := range partHints {
		if strings.Contains(catalog, `"`+h.Part+`"`) || strings.Contains(catalog, h.Part+"(") {
			lines = append(lines, "- "+h.Part+": "+h.Hint)
		}
	}
	return strings.Join(lines, "\n")
}
