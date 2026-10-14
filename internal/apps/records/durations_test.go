package records

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestComputedDurationClock(t *testing.T) {
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for _, source := range []string{`roast_date >= now - 7d`, `roast_date >= now - 24h`, `roast_date >= now - 30m`, `roast_date >= now - 2w`, `roast_date >= now - days(7)`, `$record.roast_date >= $now - 7d`} {
		t.Run(source, func(t *testing.T) {
			schema := &spec.RecordType{Fields: map[string]*spec.Field{"roast_date": {Type: spec.FieldDatetime}, "this_week": {Type: spec.FieldBool, Computed: source}}}
			rules, err := NewRules(schema)
			if err != nil {
				t.Fatal(err)
			}
			if !rules.Timed {
				t.Fatal("clock dependency missing")
			}
			for _, tc := range []struct {
				ago  time.Duration
				want bool
			}{{time.Minute, true}, {30 * 24 * time.Hour, false}} {
				row, err := rules.Prepare(bridge.Record{Fields: map[string]any{"roast_date": at.Add(-tc.ago).Format(time.RFC3339)}}, "me", at, false)
				if err != nil || row.Fields["this_week"] != tc.want {
					t.Fatalf("row %+v error %v", row, err)
				}
			}
		})
	}
}
func TestComputedDateDuration(t *testing.T) {
	schema := &spec.RecordType{Fields: map[string]*spec.Field{"roast_date": {Type: spec.FieldDate}, "this_week": {Type: spec.FieldBool, Computed: `roast_date >= now - 7d`}}}
	rules, err := NewRules(schema)
	if err != nil {
		t.Fatal(err)
	}
	row, err := rules.Prepare(bridge.Record{Fields: map[string]any{"roast_date": "2026-10-12"}}, "me", time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), false)
	if err != nil || row.Fields["this_week"] != true {
		t.Fatalf("row %+v err %v", row, err)
	}
}
func TestComputedExpressionDiagnostic(t *testing.T) {
	_, err := NewRules(&spec.RecordType{Fields: map[string]*spec.Field{"this_week": {Type: spec.FieldBool, Computed: `now - 7d + missing`}}})
	if err == nil {
		t.Fatal("expected error")
	}
	for _, needle := range []string{"this_week", "now - 7d + missing", "1:12", "corrected example"} {
		if !strings.Contains(err.Error(), needle) {
			t.Errorf("missing %q in %v", needle, err)
		}
	}
}

func TestAComputedDeadlineInADateFieldKeepsItsDay(t *testing.T) {
	schema := &spec.RecordType{Fields: map[string]*spec.Field{"shoot_at": {Type: spec.FieldDatetime}, "deliver_by": {Type: spec.FieldDate, Computed: `shoot_at + days(28)`}}}
	rules, err := NewRules(schema)
	if err != nil {
		t.Fatal(err)
	}
	row, err := rules.Prepare(bridge.Record{Fields: map[string]any{"shoot_at": "2026-10-20T03:03:22Z"}}, "me", time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), false)
	if err != nil || row.Fields["deliver_by"] != "2026-11-17" {
		t.Fatalf("row %+v err %v", row, err)
	}
}
