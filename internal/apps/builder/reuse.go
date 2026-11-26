package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type reuseBinding struct {
	Type   string
	Owner  string
	Common string
	Access string
	Line   schema.PlanLine
}

// Pin a choice before running the writer so a restart or repair sees the same source.
func (s *Jobs) pinReuse(j *job, t turn, types schema.TypeSet, using []string) (map[string]reuseBinding, error) {
	app, err := schema.ParseApp([]byte(j.Working.Files[schema.AppFile]))
	if err != nil {
		return j.Reuse, nil
	}
	plan, _ := schema.ParsePlan([]byte(j.Working.Files[schema.PlanFile]))
	next := maps.Clone(j.Reuse)
	if next == nil {
		next = map[string]reuseBinding{}
	}
	for _, source := range using {
		if common, ok := strings.CutPrefix(source, "common:"); ok {
			for _, id := range slices.Sorted(maps.Keys(types)) {
				if types[id].Common == common {
					source = id
					break
				}
			}
		}
		existing, ok := types[source]
		if !ok || existing.Owner == "" {
			continue
		}
		local := reuseLocal(source, existing, app, plan)
		if _, pinned := next[local]; pinned || local == "" {
			continue
		}
		line := schema.PlanLine{ID: local, Line: "Use your existing " + displayName(local, "records"), Uses: true}
		for _, planned := range plan.Data {
			if planned.ID == local {
				line = planned
				line.Uses = true
				break
			}
		}
		access := app.Capabilities.Records[local]
		if line.Create {
			access = "rw"
		} else if access == "" {
			access = "r"
		}
		next[local] = reuseBinding{Type: source, Owner: existing.Owner, Common: existing.Common, Access: access, Line: line}
	}
	if maps.Equal(j.Reuse, next) {
		return next, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.load(j.Draft)
	if err != nil {
		return nil, err
	}
	if len(current.Queue) == 0 || current.Queue[0].ID != t.ID {
		return nil, context.Canceled
	}
	current.Reuse = next
	return next, s.save(current, Event{})
}

func reuseLocal(source string, typ schema.ExistingType, app schema.App, plan schema.Plan) string {
	if _, ok := app.Records[source]; ok || slices.ContainsFunc(plan.Data, func(line schema.PlanLine) bool { return line.ID == source }) {
		return source
	}
	aliases := []string{}
	referenced := false
	for _, local := range slices.Sorted(maps.Keys(app.Records)) {
		record := app.Records[local]
		if typ.Common != "" && record.Common == typ.Common {
			aliases = append(aliases, local)
		}
		for _, field := range record.Fields {
			if (field.Type == "ref" || field.Type == "refs") && field.To == source {
				referenced = true
			}
		}
	}
	if len(aliases) == 1 {
		return aliases[0]
	}
	if referenced {
		return source
	}
	return ""
}

func restoreReuse(files map[string]string, pins map[string]reuseBinding, types schema.TypeSet) error {
	if len(pins) == 0 {
		return nil
	}
	if err := validateReuse(pins, types); err != nil {
		return err
	}
	var app map[string]json.RawMessage
	if err := json.Unmarshal([]byte(files[schema.AppFile]), &app); err != nil {
		return err
	}
	if app == nil {
		return fmt.Errorf("app.json must contain an app object")
	}
	var rawRecords map[string]json.RawMessage
	if raw := app["records"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &rawRecords); err != nil {
			return fmt.Errorf("read records for accepted reuse: %w", err)
		}
	}
	if rawRecords == nil {
		rawRecords = map[string]json.RawMessage{}
	}
	original, err := schema.ParseApp([]byte(files[schema.AppFile]))
	if err != nil {
		return fmt.Errorf("read capabilities and records for accepted reuse: %w", err)
	}
	plan, err := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if err != nil && files[schema.PlanFile] != "" {
		return fmt.Errorf("read plan for accepted reuse: %w", err)
	}
	manifestChanged, planChanged := false, false
	for _, local := range slices.Sorted(maps.Keys(pins)) {
		pin := pins[local]
		typ := types[pin.Type]
		record := original.Records[local]
		changed := !record.Uses || record.Common != pin.Common || record.Fields == nil
		record.Uses, record.Common = true, pin.Common
		if record.Fields == nil {
			record.Fields = map[string]schema.Field{}
		}
		// The source owns inherited fields. A local definition would shadow them.
		for field := range typ.Fields {
			if _, exists := record.Fields[field]; exists {
				delete(record.Fields, field)
				changed = true
			}
		}
		if _, ok := typ.Fields[record.Title]; !ok {
			if _, ok := record.Fields[record.Title]; !ok {
				changed = changed || record.Title != ""
				record.Title = ""
			}
		}
		if changed {
			rawRecords[local], err = json.Marshal(record)
			if err != nil {
				return err
			}
			manifestChanged = true
		}
		access := original.Capabilities.Records[local]
		if access != "rw" && access != pin.Access {
			access = pin.Access
			if err := declareRecordAccess(app, local, access); err != nil {
				return err
			}
			manifestChanged = true
		}
		index := slices.IndexFunc(plan.Data, func(line schema.PlanLine) bool { return line.ID == local })
		if index == -1 {
			plan.Data = append(plan.Data, pin.Line)
			planChanged = true
		} else {
			line := &plan.Data[index]
			create := line.Create && access == "rw" || pin.Line.Create
			planChanged = planChanged || !line.Uses || line.Create != create
			line.Uses, line.Create = true, create
		}
	}
	if manifestChanged {
		app["records"], err = json.Marshal(rawRecords)
		if err != nil {
			return err
		}
		if err := saveJSON(files, schema.AppFile, app); err != nil {
			return err
		}
	}
	if planChanged && plan.Ask != "" {
		return saveJSON(files, schema.PlanFile, plan)
	}
	return nil
}

func validateReuse(pins map[string]reuseBinding, types schema.TypeSet) error {
	for _, local := range slices.Sorted(maps.Keys(pins)) {
		pin := pins[local]
		typ, ok := types[pin.Type]
		if !ok || typ.Owner != pin.Owner || typ.Common != pin.Common {
			return fmt.Errorf("the accepted source for %s is no longer available", displayName(local, "records"))
		}
	}
	return nil
}

// validateReuseDefinition checks the binding in the exact files being kept.
func validateReuseDefinition(files map[string]string, pins map[string]reuseBinding) error {
	app, err := schema.ParseApp([]byte(files[schema.AppFile]))
	if err != nil {
		return err
	}
	plan, err := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if err != nil {
		return err
	}
	for _, local := range slices.Sorted(maps.Keys(pins)) {
		pin := pins[local]
		record := app.Records[local]
		access := app.Capabilities.Records[local]
		line := slices.ContainsFunc(plan.Data, func(line schema.PlanLine) bool { return line.ID == local && line.Uses })
		if !record.Uses || record.Common != pin.Common || !line || (access != "rw" && access != pin.Access) {
			return fmt.Errorf("the accepted source for %s is missing from the draft", displayName(local, "records"))
		}
	}
	return nil
}
