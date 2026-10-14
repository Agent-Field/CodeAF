package expr

import (
	"strings"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
)

func TestDurations(t *testing.T) {
	for _, tc := range []struct{ source, rewritten string }{
		{`7d`, `duration("168h")`}, {`24h`, `duration("24h")`}, {`30m`, `duration("30m")`}, {`2w`, `duration("336h")`},
		{`'7d'`, `'7d'`}, {`"24h"`, `"24h"`}, {`r'30m'`, `r'30m'`}, {`'''2w'''`, `'''2w'''`},
		{`'don\'t 7d'`, `'don\'t 7d'`}, {"// 7d\n2w", "// 7d\nduration(\"336h\")"},
		{`field7d`, `field7d`}, {`7days`, `7days`}, {`0x7d`, `0x7d`}, {`1.7d`, `1.7d`}, {`7d.field`, `7d.field`},
		{`now-7d`, `now-duration("168h")`}, {`-2w`, `-duration("336h")`},
	} {
		t.Run(tc.source, func(t *testing.T) {
			got, _ := durations(tc.source)
			if got != tc.rewritten {
				t.Fatalf("got %q want %q", got, tc.rewritten)
			}
		})
	}
}
func TestDurationEvaluation(t *testing.T) {
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for _, source := range []string{`7d == days(7)`, `24h == days(1)`, `30m == days(0.020833333333333332)`, `2w == days(14)`, `now - 7d == now() - duration("168h")`, `days(-1) == duration("-24h")`, `'7d' == '7d'`} {
		t.Run(source, func(t *testing.T) {
			ast, err := Compile(env, source)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Program(env, ast, at, "me")
			if err != nil {
				t.Fatal(err)
			}
			value, _, err := p.Eval(map[string]any{"now": at})
			if err != nil || value.Value() != true {
				t.Fatalf("value %v error %v", value, err)
			}
		})
	}
}
func TestDiagnosticOriginalPosition(t *testing.T) {
	env, _ := NewEnv(cel.Variable("roast_date", cel.DynType))
	for _, tc := range []struct{ source, position string }{
		{`roast_date >= now - 7d + missing`, "1:26"},
		{"now - 7d\n+ missing", "2:3"},
		{`'é' == 'é' && now - 7d == missing`, "1:25"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			_, err := Compile(env, tc.source)
			if err == nil {
				t.Fatal("expected error")
			}
			for _, needle := range []string{tc.position, "expression", "corrected example", "days(7)"} {
				if !strings.Contains(err.Error(), needle) {
					t.Errorf("missing %q in %v", needle, err)
				}
			}
		})
	}
}
func TestDurationLimits(t *testing.T) {
	env, _ := NewEnv()
	for _, source := range []string{`18446744073709551615w`, `999999999999999999999d`, `days(1e30)`, `days(-1e30)`} {
		t.Run(source, func(t *testing.T) {
			ast, err := Compile(env, source)
			if err == nil {
				p, e := Program(env, ast, time.Time{}, "")
				if e != nil {
					t.Fatal(e)
				}
				_, _, err = p.Eval(map[string]any{})
			}
			if err == nil {
				t.Fatal("unbounded duration accepted")
			}
		})
	}
}

func TestActivationPreservesDateLookingText(t *testing.T) {
	values := map[string]any{"name": "2026-10-13", "roast_date": "2026-10-12", "now": "2026-10-13T12:00:00Z"}
	got := Activation(values, "roast_date")
	if got["name"] != values["name"] {
		t.Fatal("text converted to timestamp")
	}
	if _, ok := got["roast_date"].(time.Time); !ok {
		t.Fatal("date not converted")
	}
	if _, ok := got["now"].(time.Time); !ok {
		t.Fatal("clock not converted")
	}
	if _, ok := values["roast_date"].(string); !ok {
		t.Fatal("stored values changed")
	}
}
