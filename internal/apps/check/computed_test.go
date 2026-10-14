package check

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/sample"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestCheckComputedDurations(t *testing.T) {
	for _, source := range []string{`last_touch >= now - 7d`, `last_touch >= now - days(7)`, `last_touch >= now - 7d + missing`} {
		t.Run(source, func(t *testing.T) {
			files, _ := fixture(t)
			b, err := spec.Decode(files)
			if err != nil {
				t.Fatal(err)
			}
			b.App.Records["deal"].Fields["this_week"] = &spec.Field{Type: spec.FieldBool, Computed: source}
			files[spec.AppFile], err = json.Marshal(b.App)
			if err != nil {
				t.Fatal(err)
			}
			all := services(nil)
			generator := sample.New(svc.Config{}, nil)
			generator.Now = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) }
			all.Sample = generator
			report, err := New(svc.Config{}, all).Run(context.Background(), "crm", files, 1)
			bad := strings.Contains(source, "missing")
			if err != nil || report.Pass == bad {
				t.Fatalf("report %+v error %v", report, err)
			}
			if bad {
				text := report.Issues[0].String()
				for _, needle := range []string{"this_week", source, "1:", "corrected example", "fix:"} {
					if !strings.Contains(text, needle) {
						t.Errorf("missing %q in %s", needle, text)
					}
				}
			}
		})
	}
}
