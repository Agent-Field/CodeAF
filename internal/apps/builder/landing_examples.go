package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// completeLanding gives the Builder one actionable finding when the opening
// queries cannot be filled from the app's existing fictional story.
func completeLanding(files map[string]string, types schema.TypeSet) (*schema.Problem, error) {
	app, _ := schema.ParseApp([]byte(files[schema.AppFile]))
	if app.Kit != 2 {
		return nil, nil
	}
	count, err := fillLandingExamples(files, types, time.Now())
	if err != nil || count >= check.MinLandingRows {
		return nil, err
	}
	return &schema.Problem{File: schema.SampleFile, Code: "landing_examples",
		Message: fmt.Sprintf("The opening views have %d named examples. Give their queries distinct fictional records from the app's story, with room to show at least %d.", count, check.MinLandingRows)}, nil
}

// fillLandingExamples expands named examples already supplied for the app.
// Clones keep their story's dates, evidence, states and references. Only the
// real opening queries decide whether the additions satisfy the depth bar.
func fillLandingExamples(files map[string]string, types schema.TypeSet, now time.Time) (int, error) {
	def := definition(files)
	collections := check.LandingCollections(def)
	if len(collections) == 0 {
		return check.MinLandingRows, nil
	}
	app, err := schema.ParseApp(def.Files[schema.AppFile])
	if err != nil {
		return 0, err
	}
	rows, err := check.Sample(def)
	if err != nil || len(rows) == 0 {
		return 0, nil // Check reports invalid samples and asks for a named example.
	}
	home, err := os.MkdirTemp("", "codeaf-landing-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(home)
	place := data.NewPlace(home, data.Options{Now: func() time.Time { return now }})
	defer place.Close()
	if err := registerSampleShapes(place, app, types); err != nil {
		return 0, nil // The ordinary sample check reports unsupported shared shapes.
	}
	query := func(rows map[string][]map[string]any) (map[string][]data.Row, error) {
		const draft = "landing"
		if err := place.SeedSample(draft, def.Files[schema.AppFile], rows); err != nil {
			return nil, err
		}
		store, err := place.Store(draft)
		if err != nil {
			return nil, err
		}
		return check.LandingRows(def, store, collections), nil
	}
	visible, err := query(rows)
	if err != nil {
		return 0, nil // Let the main Check report the actionable seed error.
	}
	count := landingSize(visible)
	if count >= check.MinLandingRows {
		return count, nil
	}

	var names []string
	// Try the primary collection first, but keep filling other collections
	// when a view has a fixed window or no repeatable story rows.
	for _, bind := range collections {
		if len(visible[bind.Records]) != 0 && !slices.Contains(names, bind.Records) {
			names = append(names, bind.Records)
		}
	}
	total := 0
	usedIDs := map[string]bool{}
	for _, group := range rows {
		total += len(group)
		for _, row := range group {
			id, _ := row["id"].(string)
			usedIDs[id] = true
		}
	}
	for _, name := range names {
		record := app.Records[name]
		if record.Uses {
			continue
		}
		original := rows[name]
		byID := map[string]map[string]any{}
		titles := map[string]bool{}
		for _, row := range original {
			id, _ := row["id"].(string)
			byID[id] = row
			titles[strings.ToLower(check.ExampleTitle(record, row))] = true
		}
		limit := min(check.MinLandingRows-count, draftSampleLimit-total)
		for i, added := 0, 0; i < len(visible[name])*max(len(original), len(examplePhases)) && added < limit; i++ {
			basis := byID[visible[name][i%len(visible[name])].ID()]
			row := expandExample(name, record, basis, original, i/len(visible[name]), rows)
			if row == nil {
				continue
			}
			title := strings.ToLower(check.ExampleTitle(record, row))
			id := row["id"].(string)
			if titles[title] || usedIDs[id] {
				continue
			}
			titles[title], usedIDs[id] = true, true
			rows[name] = append(rows[name], row)
			added++
			total++
		}
		result, err := query(rows)
		if err != nil {
			total -= len(rows[name]) - len(original)
			rows[name] = original
			continue
		}
		if next := landingSize(result); next > count {
			count = next
		} else {
			total -= len(rows[name]) - len(original)
			rows[name] = original
		}
		if count >= check.MinLandingRows {
			break
		}
	}
	if count <= landingSize(visible) {
		return count, nil
	}
	var sample map[string]json.RawMessage
	if err := json.Unmarshal(def.Files[schema.SampleFile], &sample); err != nil {
		return 0, err
	}
	sample["records"], err = json.Marshal(rows)
	if err != nil {
		return 0, err
	}
	return count, saveJSON(files, schema.SampleFile, sample)
}

func landingSize(result map[string][]data.Row) int {
	count := 0
	for _, group := range result {
		count += len(group)
	}
	return count
}

func registerSampleShapes(place *data.Place, app schema.App, types schema.TypeSet) error {
	owners := map[string]map[string]schema.RecordType{}
	for name, record := range app.Records {
		if !record.Uses {
			continue
		}
		shared, ok := types.ForRecord(name, record)
		if !ok {
			return fmt.Errorf("no shared shape for %s", name)
		}
		if owners[shared.Owner] == nil {
			owners[shared.Owner] = map[string]schema.RecordType{}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(types)) {
		shared := types[name]
		if owned := owners[shared.Owner]; owned != nil {
			owned[name] = schema.RecordType{ID: shared.ID, Common: shared.Common, Fields: shared.Fields}
		}
	}
	if len(owners) == 0 {
		return nil
	}
	main, err := place.Main()
	if err != nil {
		return err
	}
	for _, owner := range slices.Sorted(maps.Keys(owners)) {
		raw, err := json.Marshal(schema.App{ID: owner, Name: owner, Version: 1, Kit: 2, Records: owners[owner]})
		if err != nil {
			return err
		}
		if err := main.RegisterApp(raw); err != nil {
			return err
		}
	}
	return nil
}

var examplePhases = []string{"Review", "Follow-up", "Handoff", "Planning", "Sign-off", "Readout", "Next steps", "Working session"}

func expandExample(typ string, record schema.RecordType, basis map[string]any, peers []map[string]any, index int, story map[string][]map[string]any) map[string]any {
	title := check.ExampleTitle(record, basis)
	if title == "" {
		return nil
	}
	field := record.Title
	if record.Present != nil && basis[record.Present.Title] == title {
		field = record.Present.Title
	}
	if field == "" {
		for _, key := range []string{"title", "name", "subject"} {
			if basis[key] == title {
				field = key
				break
			}
		}
	}
	if !showableStoryField(record.Fields[field]) || record.Fields[field].Computed != "" {
		return nil
	}
	// A numbered second copy of an entity is still the same entity. Combine
	// words from different named peers for a directory, or add a work phase
	// for a recurring item that retains its original client and evidence.
	name := ""
	if len(record.Fields) == 1 {
		first := strings.Fields(title)
		other := strings.Fields(check.ExampleTitle(record, peers[index%len(peers)]))
		if len(first) > 1 && len(other) > 1 && first[len(first)-1] != other[len(other)-1] {
			name = strings.Join(first[:len(first)-1], " ") + " " + other[len(other)-1]
		}
	} else {
		phase := examplePhases[index%len(examplePhases)]
		name = title + " · " + phase
	}
	if name == "" {
		return nil
	}
	row := maps.Clone(basis)
	for key, fieldShape := range record.Fields {
		if key != field && fieldShape.Unique && row[key] != nil {
			return nil // Reusing a unique external identifier would describe the same record.
		}
		if row[key] == nil && (fieldShape.Type == "ref" || fieldShape.Type == "refs") && fieldShape.Required {
			row[key] = arrivalValue(fieldShape, story, nil, time.Time{})
		}
	}
	row[field] = name
	hash := sha256.Sum256([]byte(typ + "\x00" + name))
	row["id"] = "00" + strings.ToUpper(hex.EncodeToString(hash[:12]))
	return row
}
