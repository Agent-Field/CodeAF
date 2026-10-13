package records

import (
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

var duration = regexp.MustCompile(`^P(?:[0-9]+Y)?(?:[0-9]+M)?(?:[0-9]+W)?(?:[0-9]+D)?(?:T(?:[0-9]+H)?(?:[0-9]+M)?(?:[0-9]+(?:\.[0-9]+)?S)?)?$`)
var phone = regexp.MustCompile(`^\+?[0-9][0-9 ()\-.]{4,30}$`)

// ValidateFields is shared with the sample generator so it cannot emit values
// that the store would reject. Relations are checked in the write transaction.
func ValidateFields(typ *spec.RecordType, fields map[string]any) error {
	for name := range fields {
		if typ.Fields[name] == nil {
			return invalid("unknown field " + name)
		}
	}
	for _, name := range sorted(typ.Fields) {
		f, value := typ.Fields[name], fields[name]
		if f == nil {
			return invalid("invalid schema field " + name)
		}
		if value == nil {
			if f.Required {
				return invalid(name + " is required")
			}
			continue
		}
		if err := validateValue(f, value); err != nil {
			return invalid(name + ": " + err.Error())
		}
	}
	return nil
}
func validateValue(f *spec.Field, value any) error {
	switch f.Type {
	case spec.FieldNumber, spec.FieldMoney:
		n, ok := value.(float64)
		if !ok || math.IsInf(n, 0) || math.IsNaN(n) {
			return fmt.Errorf("expected a finite number")
		}
		return nil
	case spec.FieldBool:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected a bool")
		}
		return nil
	case spec.FieldEnums, spec.FieldPeople, spec.FieldRefs, spec.FieldFiles:
		list, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected a list")
		}
		seen := map[string]bool{}
		scalar := *f
		scalar.Type = map[spec.FieldType]spec.FieldType{spec.FieldEnums: spec.FieldEnum, spec.FieldPeople: spec.FieldPerson, spec.FieldRefs: spec.FieldRef, spec.FieldFiles: spec.FieldFile}[f.Type]
		for _, item := range list {
			if err := validateValue(&scalar, item); err != nil {
				return err
			}
			text := item.(string)
			if seen[text] {
				return fmt.Errorf("duplicate list value")
			}
			seen[text] = true
		}
		if f.Required && len(list) == 0 {
			return fmt.Errorf("is required")
		}
		return nil
	case spec.FieldPlace:
		return validatePlace(value)
	}
	text, ok := value.(string)
	if !ok {
		return fmt.Errorf("expected text")
	}
	if f.Required && strings.TrimSpace(text) == "" {
		return fmt.Errorf("is required")
	}
	switch f.Type {
	case spec.FieldText, spec.FieldLongtext:
		return nil
	case spec.FieldPerson, spec.FieldRef, spec.FieldFile:
		if text == "" {
			return fmt.Errorf("expected an id")
		}
		return nil
	case spec.FieldEnum:
		if !slices.Contains(f.Values, text) {
			return fmt.Errorf("unknown enum value %q", text)
		}
		return nil
	case spec.FieldDate:
		_, err := time.Parse("2006-01-02", text)
		return err
	case spec.FieldDatetime:
		_, err := time.Parse(time.RFC3339Nano, text)
		return err
	case spec.FieldDuration:
		if !duration.MatchString(text) || text == "P" || text == "PT" || strings.HasSuffix(text, "T") {
			return fmt.Errorf("expected an ISO 8601 duration")
		}
		return nil
	case spec.FieldEmail:
		addr, err := mail.ParseAddress(text)
		if err != nil || addr.Address != text || !strings.Contains(text, "@") {
			return fmt.Errorf("expected an email address")
		}
		return nil
	case spec.FieldPhone:
		if !phone.MatchString(text) {
			return fmt.Errorf("expected a phone number")
		}
		return nil
	case spec.FieldURL:
		u, err := url.Parse(text)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			return fmt.Errorf("expected an http or https URL")
		}
		return nil
	default:
		return fmt.Errorf("unknown field type %q", f.Type)
	}
}
func validatePlace(value any) error {
	place, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("expected {lat, lng, label}")
	}
	lat, a := place["lat"].(float64)
	lng, b := place["lng"].(float64)
	_, c := place["label"].(string)
	if !a || !b || !c || math.IsNaN(lat) || math.IsNaN(lng) || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return fmt.Errorf("invalid place coordinates or label")
	}
	return nil
}
func copyFields(fields map[string]any) (map[string]any, error) {
	data, err := json.Marshal(fields)
	if err != nil {
		return nil, invalid("fields must be JSON values")
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = map[string]any{}
	}
	return result, nil
}
