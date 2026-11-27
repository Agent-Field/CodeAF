package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

const draftSampleLimit = 200

var changedArgument = regexp.MustCompile(`\bchanged\(\s*["']([a-z][a-z0-9_]*)["']\s*\)`)
var arrivalEquality = regexp.MustCompile(`\b(?:input\.after\.|record\.)?([a-z][a-z0-9_]*)\s*==\s*("(?:[^"\\]|\\.)*"|true|false|-?[0-9]+(?:\.[0-9]+)?)`)

func normalizeTriggerExamples(files map[string]string, app schema.App, types schema.TypeSet) ([]schema.Problem, error) {
	var sample schema.Sample
	if len(app.Triggers) == 0 || json.Unmarshal([]byte(files[schema.SampleFile]), &sample) != nil || sample.Records == nil {
		return nil, nil
	}
	kit, err := readPartsCatalog()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	total := 0
	for _, rows := range sample.Records {
		total += len(rows)
	}
	var notes []schema.Problem
	for _, trigger := range app.Triggers {
		if trigger.On != "record.changed" || app.Capabilities.Records[trigger.Type] == "" {
			continue
		}
		rows := sample.Records[trigger.Type]
		if slices.ContainsFunc(rows, func(row map[string]any) bool {
			matches, _ := runs.MatchesSampleArrival(trigger.When, trigger.Type, row, now)
			return matches
		}) {
			continue
		}
		record, exists := app.Records[trigger.Type]
		if !exists {
			continue
		}
		fields := maps.Clone(record.Fields)
		if shared, ok := types.ForRecord(trigger.Type, record); record.Uses && ok {
			fields = maps.Clone(shared.Fields)
			maps.Copy(fields, record.Fields)
		}
		row, ok := exampleArrival(trigger, record, fields, sample.Records, kit.Registries.Fields, now)
		if !ok {
			continue
		}
		if total >= draftSampleLimit {
			if len(rows) == 0 {
				continue
			}
			// Keep the original ID and references when the sample is full.
			patched := maps.Clone(rows[0])
			delete(row, "id")
			maps.Copy(patched, row)
			if matches, _ := runs.MatchesSampleArrival(trigger.When, trigger.Type, patched, now); !matches {
				continue
			}
			rows[0] = patched
		} else {
			sample.Records[trigger.Type] = append([]map[string]any{row}, rows...)
			total++
		}
		notes = append(notes, schema.Problem{File: schema.SampleFile, Path: "/records/" + trigger.Type,
			Code: normalizationNote, Message: fmt.Sprintf("Added an example for the %s automation.", displayName(trigger.ID, "record change"))})
	}
	if len(notes) != 0 {
		return notes, saveJSON(files, schema.SampleFile, sample)
	}
	return nil, nil
}

func exampleArrival(trigger schema.Trigger, record schema.RecordType, fields map[string]schema.Field, rows map[string][]map[string]any, examples map[string]json.RawMessage, now time.Time) (map[string]any, bool) {
	digest := sha256.Sum256([]byte(trigger.Type + "\x00" + trigger.ID))
	row := map[string]any{"id": "00" + strings.ToUpper(hex.EncodeToString(digest[:12]))}
	values := map[string]any{}
	for _, match := range arrivalEquality.FindAllStringSubmatch(trigger.When, -1) {
		var value any
		if json.Unmarshal([]byte(match[2]), &value) == nil {
			values[match[1]] = value
		}
	}
	required := map[string]bool{record.Title: true}
	for _, match := range changedArgument.FindAllStringSubmatch(trigger.When, -1) {
		required[match[1]] = true
	}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		field := fields[name]
		if field.Masked || field.Class.Restricted() || field.Computed != "" || field.AI != "" || field.Sequence != nil ||
			record.Policy != nil && record.Policy.Fields[name] != "" {
			continue
		}
		if value, ok := values[name]; ok && validArrivalLiteral(field, value) && !field.Unique {
			row[name] = value
		} else if required[name] || field.Required {
			if value := arrivalValue(name, field, trigger.ID, rows, examples, now); value != nil {
				row[name] = value
			}
		}
	}
	matches, err := runs.MatchesSampleArrival(trigger.When, trigger.Type, row, now)
	return row, err == nil && matches
}

func validArrivalLiteral(field schema.Field, value any) bool {
	switch v := value.(type) {
	case string:
		return field.Type == "text" || field.Type == "longtext" || field.Type == "enum" && slices.Contains(field.Values, v)
	case bool:
		return field.Type == "bool"
	case float64:
		return field.Type == "number" || field.Type == "money"
	}
	return false
}

func arrivalValue(name string, field schema.Field, trigger string, rows map[string][]map[string]any, examples map[string]json.RawMessage, now time.Time) any {
	switch field.Type {
	case "text", "longtext":
		return "Example " + displayName(name, "record") + " for " + displayName(trigger, "automation")
	case "enum":
		if field.Default != nil {
			return field.Default
		}
		if len(field.Values) != 0 {
			return field.Values[0]
		}
	case "ref", "refs":
		if targets := rows[field.To]; len(targets) != 0 {
			if field.Type == "refs" {
				return []any{targets[0]["id"]}
			}
			return targets[0]["id"]
		}
	case "date":
		return now.Format(time.DateOnly)
	case "datetime":
		return now.Format(time.RFC3339)
	default:
		// The Kit owns the wire format, including synthetic file handles.
		var descriptor schema.FieldManifest
		if json.Unmarshal(examples[field.Type], &descriptor) != nil {
			return nil
		}
		var value any
		if json.Unmarshal(descriptor.Example, &value) == nil {
			return value
		}
	}
	return nil
}
