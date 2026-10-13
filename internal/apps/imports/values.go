package imports

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func sourceValues(typ spec.FieldType, raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	switch typ {
	case spec.FieldEnums, spec.FieldPeople, spec.FieldRefs, spec.FieldFiles:
		out := []string{}
		for _, part := range strings.Split(raw, ";") {
			if value := strings.TrimSpace(part); value != "" && !slices.Contains(out, value) {
				out = append(out, value)
			}
		}
		return out
	default:
		return []string{strings.TrimSpace(raw)}
	}
}

func mapValue(f *spec.Field, raw string, m *bridge.Mapping, members []Member) (any, bool, error) {
	values := sourceValues(f.Type, raw)
	if len(values) == 0 {
		return nil, true, nil
	}
	if f.Type == spec.FieldEnums || f.Type == spec.FieldPeople || f.Type == spec.FieldRefs || f.Type == spec.FieldFiles {
		scalar := *f
		scalar.Type = map[spec.FieldType]spec.FieldType{spec.FieldEnums: spec.FieldEnum, spec.FieldPeople: spec.FieldPerson, spec.FieldRefs: spec.FieldRef, spec.FieldFiles: spec.FieldFile}[f.Type]
		out := []any{}
		chosen := true
		for _, value := range values {
			mapped, ok, err := mapValue(&scalar, value, m, members)
			if err != nil {
				return nil, false, err
			}
			chosen = chosen && ok
			if mapped != nil && !slices.Contains(out, mapped) {
				out = append(out, mapped)
			}
		}
		return out, chosen, nil
	}
	value := values[0]
	switch f.Type {
	case spec.FieldEnum:
		mapped, ok := m.Values[value]
		if !ok {
			return nil, false, nil
		}
		if mapped == "" {
			return nil, true, nil
		}
		if !slices.Contains(f.Values, mapped) {
			return nil, false, invalid("choose a valid enum value")
		}
		return mapped, true, nil
	case spec.FieldPerson:
		mapped, explicit := m.People[value]
		if explicit && mapped == "" {
			return nil, true, nil
		}
		for _, member := range members {
			if explicit && mapped == member.ID {
				return mapped, true, nil
			}
			if !explicit && member.Email != "" && strings.EqualFold(member.Email, value) {
				if m.People == nil {
					m.People = map[string]string{}
				}
				m.People[value] = member.ID
				return member.ID, true, nil
			}
		}
		return nil, false, nil
	case spec.FieldRef:
		return value, true, nil
	}
	var result any = value
	var err error
	switch f.Type {
	case spec.FieldNumber, spec.FieldMoney:
		text := strings.ReplaceAll(value, ",", "")
		if f.Type == spec.FieldMoney {
			text = strings.TrimLeft(text, "$€£")
		}
		number, e := strconv.ParseFloat(text, 64)
		if e != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, false, invalid("expected a finite number")
		}
		result = number
	case spec.FieldBool:
		result, err = strconv.ParseBool(value)
	case spec.FieldDatetime:
		if parsed, e := time.Parse("2006-01-02", value); e == nil {
			result = parsed.Format(time.RFC3339)
		}
	case spec.FieldPlace:
		err = json.Unmarshal([]byte(value), &result)
	}
	if err != nil {
		return nil, false, invalid("invalid " + string(f.Type) + " value")
	}
	optional := *f
	optional.Required = false
	schema := &spec.RecordType{Fields: map[string]*spec.Field{"value": &optional}}
	if err := records.ValidateFields(schema, map[string]any{"value": result}); err != nil {
		return nil, false, err
	}
	return result, true, nil
}
