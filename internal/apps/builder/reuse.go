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
	for _, local := range slices.Sorted(maps.Keys(app.Records)) {
		record := app.Records[local]
		if typ.Common != "" && record.Common == typ.Common {
			aliases = append(aliases, local)
		}
		for _, field := range record.Fields {
			if (field.Type == "ref" || field.Type == "refs") && field.To == source {
				return source
			}
		}
	}
	if len(aliases) == 1 {
		return aliases[0]
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
	var rawRecords map[string]json.RawMessage
	_ = json.Unmarshal(app["records"], &rawRecords)
	if rawRecords == nil {
		rawRecords = map[string]json.RawMessage{}
	}
	var caps schema.Capabilities
	_ = json.Unmarshal(app["capabilities"], &caps)
	if caps.Records == nil {
		caps.Records = map[string]string{}
	}
	plan, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
	for _, local := range slices.Sorted(maps.Keys(pins)) {
		pin := pins[local]
		typ := types[pin.Type]
		var record schema.RecordType
		_ = json.Unmarshal(rawRecords[local], &record)
		record.Uses, record.Common = true, pin.Common
		if record.Fields == nil {
			record.Fields = map[string]schema.Field{}
		}
		for field := range typ.Fields {
			delete(record.Fields, field)
		}
		if _, ok := typ.Fields[record.Title]; !ok {
			if _, ok := record.Fields[record.Title]; !ok {
				record.Title = ""
			}
		}
		rawRecords[local], _ = json.Marshal(record)
		if caps.Records[local] != "rw" {
			caps.Records[local] = pin.Access
		}
		index := slices.IndexFunc(plan.Data, func(line schema.PlanLine) bool { return line.ID == local })
		if index == -1 {
			plan.Data = append(plan.Data, pin.Line)
		} else {
			plan.Data[index].Uses = true
			plan.Data[index].Create = plan.Data[index].Create && caps.Records[local] == "rw" || pin.Line.Create
		}
	}
	app["records"], _ = json.Marshal(rawRecords)
	app["capabilities"], _ = json.Marshal(caps)
	raw, _ := json.MarshalIndent(app, "", "  ")
	files[schema.AppFile] = string(raw)
	if plan.Ask != "" {
		raw, _ := json.MarshalIndent(plan, "", "  ")
		files[schema.PlanFile] = string(raw)
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
