package data

import (
	"database/sql"
	"errors"
	"fmt"

	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
)

type bookingRule struct {
	id, label                 string
	resource, start, end, day *Field
	maxPerDay                 int
}

func (s *schema) compileBooking(t *Type, id string, def appschema.Resource) (bookingRule, error) {
	b := def.Booking
	r := bookingRule{id: id, label: def.Label, resource: ruleField(t, b.Resource), start: ruleField(t, b.Start),
		end: ruleField(t, b.End), day: ruleField(t, b.Day), maxPerDay: b.MaxPerDay}
	if r.resource == nil {
		return r, ruleError(id, "a booking needs a visible resource field")
	}
	if def.Record != "" {
		target := s.appType(t.DefinedBy, def.Record)
		if r.resource.Type != TypeRef || target == nil || s.typeNamed(r.resource.To) != target {
			return r, ruleError(id, "the booking field must reference the declared resource type")
		}
	} else if r.resource.Type != TypePerson {
		return r, ruleError(id, "a booking without a resource record needs a person field")
	}
	switch {
	case b.Overlap == "reject" && b.Day == "" && b.MaxPerDay == 0:
		if r.start == nil || r.end == nil || r.start == r.end || r.start.Type != r.end.Type || !bookingClock(r.start) {
			return r, ruleError(id, "a booking needs two distinct clock fields of the same type")
		}
	case b.MaxPerDay > 0 && b.Overlap == "" && b.Start == "" && b.End == "":
		if !bookingClock(r.day) {
			return r, ruleError(id, "daily capacity needs a date or datetime field")
		}
	default:
		return r, ruleError(id, "choose overlap rejection or a positive daily capacity")
	}
	return r, nil
}

func bookingClock(f *Field) bool {
	return f != nil && (f.Type == TypeDate || f.Type == TypeDatetime)
}

// bookingSlot has the same fixed-width date and UTC timestamp spellings as the store.
type bookingSlot struct {
	resource, start, end, day string
}

func (b bookingRule) slot(r *record) bookingSlot {
	if r == nil || r.DeletedAt != "" {
		return bookingSlot{}
	}
	value := func(f *Field) string {
		s, _ := r.Data[f.Name].(string)
		return s
	}
	slot := bookingSlot{resource: value(b.resource)}
	if b.day != nil {
		slot.day = value(b.day)
	} else {
		slot.start, slot.end = value(b.start), value(b.end)
	}
	return slot
}

func (w *writeTx) checkBookings(t *Type, r *record, sc Scope) error {
	if len(t.rules.bookings) == 0 {
		return nil
	}
	before, err := loadRecord(w.tx, w.schema, r.ID)
	if err != nil {
		return err
	}
	for _, rule := range t.rules.bookings {
		slot := rule.slot(r)
		if slot == rule.slot(before) {
			continue
		}
		reason, err := rule.check(w.tx, t, r.ID, slot)
		if err != nil {
			return err
		}
		if err := w.ruleActivity(sc, t, r.ID, fmt.Sprintf("%s (%s): %s", rule.label, rule.id, reason)); err != nil {
			return err
		}
	}
	return nil
}

func (b bookingRule) check(q querier, t *Type, id string, slot bookingSlot) (string, error) {
	if slot == (bookingSlot{}) {
		return "booking released", nil
	}
	stmt := `SELECT id FROM records WHERE type_id = '` + mustID(t.ID) + `' AND deleted_at IS NULL AND id != ? AND ` + jsonPath("data_json", b.resource.ID) + ` = ? AND `
	args := []any{id, slot.resource}
	var reason string
	var offset int
	if b.day != nil {
		if slot.resource == "" || len(slot.day) < len(dateLayout) {
			return "", fieldErr(b.resource.Name, "%s needs a resource and its booking dates", b.label)
		}
		stmt += `substr(` + jsonPath("data_json", b.day.ID) + `, 1, 10) = ?`
		args = append(args, slot.day[:len(dateLayout)])
		offset = b.maxPerDay - 1
		reason = "daily capacity available"
	} else {
		if slot.resource == "" || slot.start == "" || slot.end == "" {
			return "", fieldErr(b.resource.Name, "%s needs a resource and its booking dates", b.label)
		}
		if slot.start >= slot.end {
			return "", fieldErr(b.end.Name, "%s needs an end after its start", b.label)
		}
		stmt += jsonPath("data_json", b.start.ID) + ` < ? AND ` + jsonPath("data_json", b.end.ID) + ` > ?`
		args = append(args, slot.end, slot.start)
		reason = "time slot available"
	}
	var conflict string
	err := q.QueryRow(stmt+` ORDER BY id LIMIT 1 OFFSET ?`, append(args, offset)...).Scan(&conflict)
	if err == nil {
		return "", &Error{Code: CodeBookingConflict, Field: b.resource.Name,
			Message: fmt.Sprintf("%s (%s): booking conflicts with %s record %s", b.label, b.id, t.Name, conflict)}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return reason, nil
}

// checkNewBookings also protects existing rows when an app introduces or
// tightens a booking rule. A conflicting definition leaves the old one live.
func checkNewBookings(tx *sql.Tx, next, before *schema, t *Type) error {
	previous := before.byID[t.ID]
	for _, rule := range t.rules.bookings {
		if previous != nil && t.rules.declarations.Bookings[rule.id] == previous.rules.declarations.Bookings[rule.id] {
			continue
		}
		err := eachRow(tx, `SELECT id, type_id, rev, data_json, meta_json, created_at, updated_at, deleted_at
			FROM records WHERE type_id = ? AND deleted_at IS NULL ORDER BY id`, []any{t.ID}, func(scan func(...any) error) error {
			record, err := scanRecord(next, scan)
			if err != nil {
				return err
			}
			_, err = rule.check(tx, t, record.ID, rule.slot(record))
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
