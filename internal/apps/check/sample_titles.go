package check

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

var fillerTitle = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])(?:example|sample|test|placeholder|lorem)\b.*[0-9]+[[:space:]]*$`)

// PlaceholderTitle identifies a numbered filler label in fictional record data.
func PlaceholderTitle(title string) bool { return fillerTitle.MatchString(strings.TrimSpace(title)) }

func sampleTitleFindings(def schema.Definition) []schema.Problem {
	rows, err := Sample(def)
	if err != nil {
		return nil
	}
	app, err := schema.ParseApp(def.Files[schema.AppFile])
	if err != nil {
		return nil
	}
	var problems []schema.Problem
	for _, name := range slices.Sorted(maps.Keys(rows)) {
		record := app.Records[name]
		keys := []string{record.Title}
		if record.Title == "" {
			keys = []string{"title", "name", "subject"}
		}
		for i, row := range rows[name] {
			for _, field := range keys {
				if title, ok := row[field].(string); ok && PlaceholderTitle(title) {
					problems = append(problems, schema.Problem{File: schema.SampleFile,
						Path: fmt.Sprintf("/records/%s/%d/%s", name, i, field), Code: "placeholder_title",
						Message: fmt.Sprintf("Give %s row %d a believable fictional title from the sample story.", name, i+1)})
					break
				}
			}
		}
	}
	return problems
}
