package sample

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func fieldValue(rows map[string][]bridge.Record, typ, name string, schema *spec.RecordType, f *spec.Field, i int, rng *rand.Rand, now time.Time) (any, error) {
	if useDefault(f) {
		return defaultValue(f)
	}
	switch f.Type {
	case spec.FieldText:
		return textValue(typ, name, schema, f, i), nil
	case spec.FieldLongtext:
		return fmt.Sprintf("%s for %s %d.", label(f.Label, name), label(schema.Label, typ), i+1), nil
	case spec.FieldDate, spec.FieldDatetime:
		return dateValue(name, f, i, rng, now), nil
	case spec.FieldEnum, spec.FieldEnums:
		return choiceValue(f, i)
	case spec.FieldRef, spec.FieldRefs:
		return referenceValue(rows[f.To], f, i)
	default:
		return scalarValue(typ, f.Type, i, rng)
	}
}

func useDefault(f *spec.Field) bool {
	if len(f.Default) == 0 {
		return false
	}
	// Defaults are stored values, not sample hints: relations need generated
	// targets, dates need today's clock, and enums need all states on display.
	switch f.Type {
	case spec.FieldRef, spec.FieldRefs, spec.FieldDate, spec.FieldDatetime, spec.FieldEnum:
		return false
	default:
		return !f.Unique
	}
}

func defaultValue(f *spec.Field) (any, error) {
	var value any
	if err := json.Unmarshal(f.Default, &value); err != nil {
		return nil, err
	}
	if text, ok := value.(string); ok && text == "$me" {
		value = svc.Owner
	}
	return value, nil
}

func choiceValue(f *spec.Field, i int) (any, error) {
	if len(f.Values) == 0 || (f.Unique && i >= len(f.Values)) {
		return nil, fmt.Errorf("not enough enum values")
	}
	value := f.Values[i%len(f.Values)]
	if f.Type == spec.FieldEnums {
		return []string{value}, nil
	}
	return value, nil
}

func referenceValue(targets []bridge.Record, f *spec.Field, i int) (any, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("no target records for %s", f.To)
	}
	id := targets[i%len(targets)].ID
	if f.Type == spec.FieldRefs {
		return []string{id}, nil
	}
	return id, nil
}

func scalarValue(typ string, kind spec.FieldType, i int, rng *rand.Rand) (any, error) {
	switch kind {
	case spec.FieldNumber:
		return float64(1 + i*20 + rng.Intn(20)), nil
	case spec.FieldMoney:
		return float64(3600 + i*1200 + rng.Intn(12)*100), nil
	case spec.FieldDuration:
		return fmt.Sprintf("PT%dH", i+1), nil
	case spec.FieldBool:
		return i%2 == 0, nil
	case spec.FieldPerson:
		return svc.Owner, nil
	case spec.FieldPeople:
		return []string{svc.Owner}, nil
	default:
		return addressValue(typ, kind, i)
	}
}

func addressValue(typ string, kind spec.FieldType, i int) (any, error) {
	switch kind {
	case spec.FieldEmail:
		return fmt.Sprintf("person.%d@%s.example", i+1, typ), nil
	case spec.FieldPhone:
		return fmt.Sprintf("+1 416 555 %04d", i+100), nil
	case spec.FieldURL:
		return fmt.Sprintf("https://%s.example/records/%d", typ, i+1), nil
	case spec.FieldFile:
		return fmt.Sprintf("sample-file-%s-%04d", typ, i+1), nil
	case spec.FieldFiles:
		return []string{fmt.Sprintf("sample-file-%s-%04d", typ, i+1)}, nil
	case spec.FieldPlace:
		return map[string]any{"lat": 43.65 + float64(i)*0.001, "lng": -79.38 + float64(i)*0.001, "label": fmt.Sprintf("%s location %d", label("", typ), i+1)}, nil
	default:
		return nil, fmt.Errorf("unknown field type %q", kind)
	}
}
