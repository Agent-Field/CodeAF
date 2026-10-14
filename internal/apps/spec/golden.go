package spec

import (
	"embed"
	"encoding/json"
	"io/fs"
	"time"
)

// The golden CRM is the contract's worked example and the walking skeleton's
// demo app until the store lane replaces the fixture-backed stub.
//
//go:embed testdata/crm
var goldenFS embed.FS

// GoldenCRMAsOf is the instant sample.json was written against: a deal whose
// last_touch is two hours before it was touched two hours ago.
var GoldenCRMAsOf = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

// GoldenCRM returns the golden CRM's definition files and its sample.json.
func GoldenCRM() (Files, []byte) {
	root, _ := fs.Sub(goldenFS, "testdata/crm")
	files := Files{}
	for _, pattern := range []string{AppFile, PlanFile, AgentFile, ScreensDir + "/*.json", RunsDir + "/*.yaml"} {
		matches, _ := fs.Glob(root, pattern)
		for _, name := range matches {
			files[name], _ = fs.ReadFile(root, name)
		}
	}
	sample, _ := fs.ReadFile(root, "sample.json")
	return files, sample
}

type goldenRow struct {
	ID     string         `json:"id"`
	Fields map[string]any `json:"fields"`
}

// GoldenCRMSampleAt returns sample.json with every date and datetime moved by
// the time elapsed since GoldenCRMAsOf, so the demo's ages ("2h", "7d") stay
// true on whatever day it is seeded. Dates move by whole days.
func GoldenCRMSampleAt(now time.Time) ([]byte, error) {
	files, sample := GoldenCRM()
	var app App
	if err := json.Unmarshal(files[AppFile], &app); err != nil {
		return nil, err
	}
	var rows map[string][]goldenRow
	if err := json.Unmarshal(sample, &rows); err != nil {
		return nil, err
	}
	elapsed := now.UTC().Sub(GoldenCRMAsOf)
	for typ, list := range rows {
		for _, row := range list {
			if err := shiftDates(app.Records[typ], row.Fields, elapsed); err != nil {
				return nil, err
			}
		}
	}
	return json.Marshal(rows)
}

func shiftDates(schema *RecordType, fields map[string]any, by time.Duration) error {
	if schema == nil {
		return nil
	}
	for name, field := range schema.Fields {
		layout := map[FieldType]string{FieldDate: time.DateOnly, FieldDatetime: time.RFC3339}[field.Type]
		text, ok := fields[name].(string)
		if layout == "" || !ok {
			continue
		}
		at, err := time.Parse(layout, text)
		if err != nil {
			return err
		}
		shift := by
		if field.Type == FieldDate {
			shift = by.Truncate(24 * time.Hour)
		}
		fields[name] = at.Add(shift).Format(layout)
	}
	return nil
}
