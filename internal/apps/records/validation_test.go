package records

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestFieldValidation(t *testing.T) {
	tests := []struct {
		typ       spec.FieldType
		good, bad any
	}{
		{spec.FieldText, "hello", 42}, {spec.FieldLongtext, "notes", false},
		{spec.FieldNumber, 1.25, "1.25"}, {spec.FieldMoney, 1200.0, "$1200"},
		{spec.FieldDate, "2026-10-13", "2026-02-30"}, {spec.FieldDatetime, "2026-10-13T00:00:00Z", "yesterday"},
		{spec.FieldDuration, "P2DT3H", "3h"}, {spec.FieldEnum, "Open", "missing"},
		{spec.FieldEnums, []string{"Open", "Closed"}, []string{"Open", "Open"}},
		{spec.FieldBool, true, "true"}, {spec.FieldPerson, "me", ""}, {spec.FieldPeople, []string{"me"}, []any{1}},
		{spec.FieldRef, "record_id", ""}, {spec.FieldRefs, []string{"record_id"}, "record_id"},
		{spec.FieldEmail, "priya@example.com", "Priya <priya@example.com>"}, {spec.FieldPhone, "+1 416 555 0101", "phone"},
		{spec.FieldURL, "https://example.com", "https://"}, {spec.FieldFile, "file_id", ""}, {spec.FieldFiles, []string{"file_id"}, []any{nil}},
		{spec.FieldPlace, map[string]any{"lat": 43.0, "lng": -79.0, "label": "Toronto"}, map[string]any{"lat": 91.0, "lng": 0.0, "label": "Bad"}},
	}
	for _, test := range tests {
		t.Run(string(test.typ), func(t *testing.T) {
			schema := &spec.RecordType{Fields: map[string]*spec.Field{"field": {Type: test.typ, Required: true, Values: []string{"Open", "Closed"}}}}
			good, err := copyFields(map[string]any{"field": test.good})
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateFields(schema, good); err != nil {
				t.Fatal(err)
			}
			bad, _ := copyFields(map[string]any{"field": test.bad})
			if err := ValidateFields(schema, bad); err == nil {
				t.Fatal("accepted invalid value")
			}
			if err := ValidateFields(schema, map[string]any{}); err == nil {
				t.Fatal("accepted absent required field")
			}
		})
	}
}
