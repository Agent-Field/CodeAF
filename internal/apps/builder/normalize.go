package builder

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

const normalizationNote = "draft_normalized"

// normalizeDraft applies mechanical fixes to generated draft files. Check
// still owns validation and consent, including any choice of a shared source.
func normalizeDraft(files map[string]string, types schema.TypeSet) (map[string]string, []schema.Problem, error) {
	app, err := schema.ParseApp([]byte(files[schema.AppFile]))
	if err != nil || app.Kit != 2 {
		return files, nil, nil
	}
	next := maps.Clone(files)
	var notes []schema.Problem
	note := func(file, path, message string) {
		notes = append(notes, schema.Problem{File: file, Path: path, Code: normalizationNote, Message: message})
	}
	changedRecords := map[string]schema.RecordType{}
	plan, planErr := schema.ParsePlan([]byte(files[schema.PlanFile]))
	planChanged := false
	for _, name := range slices.Sorted(maps.Keys(app.Records)) {
		original := app.Records[name]
		record, changes, changed := normalizeRecord(app.ID, name, original, types)
		for _, change := range changes {
			note(change.File, change.Path, change.Message)
		}
		if !changed {
			continue
		}
		changedRecords[name] = original
		app.Records[name] = record
		if record.Uses && planErr == nil {
			for i := range plan.Data {
				if plan.Data[i].ID == name && !plan.Data[i].Uses {
					plan.Data[i].Uses = true
					planChanged = true
				}
			}
		}
	}
	if len(changedRecords) != 0 {
		// Write only the sections owned by this pass. Other manifest keys
		// still go to Check even when they are foreign to this compiler.
		var manifest map[string]json.RawMessage
		if err := json.Unmarshal([]byte(files[schema.AppFile]), &manifest); err != nil {
			return files, nil, err
		}
		var records map[string]json.RawMessage
		if err := json.Unmarshal(manifest["records"], &records); err != nil {
			return files, nil, err
		}
		for name, original := range changedRecords {
			records[name], err = writeNormalizedRecord(records[name], original, app.Records[name])
			if err != nil {
				return files, nil, err
			}
		}
		manifest["records"], err = json.Marshal(records)
		if err != nil {
			return files, nil, err
		}
		if err := saveJSON(next, schema.AppFile, manifest); err != nil {
			return files, nil, err
		}
		if planChanged && plan.Ask != "" {
			var outline map[string]json.RawMessage
			if err := json.Unmarshal([]byte(files[schema.PlanFile]), &outline); err != nil {
				return files, nil, err
			}
			outline["data"], err = json.Marshal(plan.Data)
			if err != nil {
				return files, nil, err
			}
			if err := saveJSON(next, schema.PlanFile, outline); err != nil {
				return files, nil, err
			}
		}
	}
	landingNotes, err := normalizeLanding(next, app, types)
	if err != nil {
		return files, nil, err
	}
	notes = append(notes, landingNotes...)
	controlNotes, err := normalizeCommandControls(next, app)
	if err != nil {
		return files, nil, err
	}
	notes = append(notes, controlNotes...)
	sampleNotes, err := normalizeTriggerExamples(next, app, types)
	if err != nil {
		return files, nil, err
	}
	return next, append(notes, sampleNotes...), nil
}

// Patch only the values this pass owns so malformed or unknown sibling
// settings are still visible to the schema validator.
func writeNormalizedRecord(raw json.RawMessage, before, after schema.RecordType) (json.RawMessage, error) {
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	if before.Common != after.Common {
		if after.Common == "" {
			delete(record, "common")
		} else {
			record["common"], _ = json.Marshal(after.Common)
		}
	}
	if before.Uses != after.Uses {
		record["uses"], _ = json.Marshal(after.Uses)
	}
	if !reflect.DeepEqual(before.Policy, after.Policy) {
		if after.Policy == nil {
			delete(record, "policy")
		} else {
			var policy map[string]json.RawMessage
			if err := json.Unmarshal(record["policy"], &policy); err != nil {
				return nil, err
			}
			policy["fields"], _ = json.Marshal(after.Policy.Fields)
			record["policy"], _ = json.Marshal(policy)
		}
	}
	if !reflect.DeepEqual(before.Fields, after.Fields) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(record["fields"], &fields); err != nil {
			return nil, err
		}
		for name, old := range before.Fields {
			if next, exists := after.Fields[name]; !exists {
				delete(fields, name)
			} else if next.Class != old.Class {
				var field map[string]json.RawMessage
				if err := json.Unmarshal(fields[name], &field); err != nil {
					return nil, err
				}
				field["class"], _ = json.Marshal(next.Class)
				fields[name], _ = json.Marshal(field)
			}
		}
		record["fields"], _ = json.Marshal(fields)
	}
	return json.Marshal(record)
}

func normalizeRecord(appID, name string, record schema.RecordType, types schema.TypeSet) (schema.RecordType, []schema.Problem, bool) {
	path := "/records/" + name
	var notes []schema.Problem
	var changed bool
	note := func(suffix, message string) {
		notes = append(notes, schema.Problem{File: schema.AppFile, Path: path + suffix, Code: normalizationNote, Message: message})
		changed = true
	}
	record.Fields = maps.Clone(record.Fields)
	for field := range record.Fields {
		if data.IsRecordMetadata(field) {
			delete(record.Fields, field)
			changed = true
		}
	}
	if record.Policy != nil {
		policy := *record.Policy
		policy.Fields = maps.Clone(policy.Fields)
		for _, field := range slices.Sorted(maps.Keys(policy.Fields)) {
			mode := policy.Fields[field]
			if mode == "hide" || mode == "mask" {
				continue
			}
			detail := fmt.Sprintf("Removed the invalid visibility policy for %s.", displayName(field, "this field"))
			class := schema.DataClass(mode)
			if shape, exists := record.Fields[field]; exists && mode != "" && class.Rank() <= schema.ClassRestricted.Rank() {
				if class.Rank() > shape.Class.Rank() {
					shape.Class = class
					record.Fields[field] = shape
				}
				detail = fmt.Sprintf("Kept %s as a data classification for %s.", mode, displayName(field, "this field"))
			}
			delete(policy.Fields, field)
			note("/policy/fields/"+field, detail)
		}
		if len(policy.Fields) != len(record.Policy.Fields) && len(policy.Fields) == 0 && policy.Row == nil {
			record.Policy = nil
		} else {
			record.Policy = &policy
		}
	}
	if record.PackagePrivate {
		return record, notes, changed
	}
	if source, exists := types[name]; exists && source.Owner != "" && source.Owner != appID &&
		(record.Common == "" || source.Common == record.Common || source.Common == "") {
		if !record.Uses || record.Common != source.Common {
			record.Uses, record.Common = true, source.Common
			note("", fmt.Sprintf("Used the existing %s records and kept this app's new fields.", displayName(record.Label, name)))
		}
	}
	if record.Common == "" && !record.Uses {
		if common := inferredRecordIdentity(name, record); common != "" {
			record.Common = common
			note("/common", fmt.Sprintf("Recognized %s as common %s records.", displayName(record.Label, name), common))
		}
	}
	if source, exists := types.ForRecord(name, record); exists && source.Owner != "" && source.Owner != appID {
		// A common binding may have a different storage name. Retain an
		// explicit conflicting identity for Check instead of overwriting it.
		if record.Common == source.Common {
			if !record.Uses {
				record.Uses = true
				note("", fmt.Sprintf("Used the existing %s records and kept this app's new fields.", displayName(record.Label, name)))
			}
			for _, field := range slices.Sorted(maps.Keys(record.Fields)) {
				if _, inherited := source.Fields[field]; inherited {
					delete(record.Fields, field)
					note("/fields/"+field, fmt.Sprintf("Kept the existing %s field.", displayName(field, "record")))
				}
			}
		}
	}
	return record, notes, changed
}

func inferredRecordIdentity(name string, record schema.RecordType) string {
	name = strings.ToLower(strings.TrimSpace(name))
	offer, ok := (schema.TypeSet(nil)).OfferCommonType(name, record)
	if !ok || record.Fields["name"].Type != "text" {
		return ""
	}
	switch name {
	case "contact", "contacts", "person", "people", "organization", "organizations", "company", "companies":
		return offer.Key
	}
	// A role such as customer may carry its own lifecycle. Keep it
	// distinct unless its record shape is explicitly bound to an identity.
	return ""
}
