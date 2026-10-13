package check

import (
	"context"
	"strings"
	"testing"
)

func TestCEL(t *testing.T) {
	values := map[string]any{"record": map[string]any{"amount": int64(3), "name": "Northwind", "bad": int64(0)}, "selection": map[string]any{"deal-list": map[string]any{"count": int64(1)}}}
	cases := []struct {
		name, source string
		want         bool
		bad          bool
	}{
		{"comparison", `$record.amount > 0`, true, false},
		{"literal dollar", `'$record.missing' == '$record.missing'`, true, false},
		{"quoted escape", `'don\'t $record.missing' == 'don\'t $record.missing'`, true, false},
		{"list id", `$selection.deal-list.count == 1`, true, false},
		{"missing", `$record.missing == 1`, false, true},
		{"hidden missing", `false && $record.missing == 1`, false, true},
		{"type", `$record.name`, false, true},
		{"error", `1 / $record.bad > 0`, false, true},
		{"invalid", `true &&`, false, true},
		{"arithmetic", `$record.amount-1 == 2`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			program, err := compileWhen(tc.source, values)
			got := false
			if err == nil {
				got, err = evalWhen(context.Background(), program, values)
			}
			if (err != nil) != tc.bad || (!tc.bad && got != tc.want) {
				t.Fatalf("got %v err %v", got, err)
			}
		})
	}
}

func TestResolveValue(t *testing.T) {
	values := map[string]any{"record": map[string]any{"name": "Northwind", "amount": float64(100), "optional": nil}, "row": map[string]any{"id": "r1"}}
	cases := []struct {
		name  string
		value any
		bad   bool
	}{
		{"ref", "$record.name", false},
		{"template", "{{record.name}} · ${{record.amount}}", false},
		{"optional", "{{record.optional}}", false},
		{"missing", "{{record.missing}}", true},
		{"logic", "{{record.amount + 1}}", true},
		{"unfinished", "{{record.name", true},
		{"object", "{{record}}", true},
		{"array", []any{"$row.id", "$row.missing"}, true},
		{"nested", map[string]any{"set": map[string]any{"name": "$record.name"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveValue(tc.value, values)
			if (err != nil) != tc.bad {
				t.Fatalf("err %v", err)
			}
		})
	}
}

func TestExpressionLimit(t *testing.T) {
	_, err := compileWhen(strings.Repeat("true && ", 1000)+"true", map[string]any{})
	if err == nil {
		t.Fatal("oversized expression accepted")
	}
}

func TestCELRecordFieldsDoNotShadowBuiltins(t *testing.T) {
	values := map[string]any{"type": "deal", "string": "sample", "record": map[string]any{"type": "deal"}}
	program, err := compileWhen(`$record.type == 'deal'`, values)
	if err != nil {
		t.Fatal(err)
	}
	result, err := evalWhen(context.Background(), program, values)
	if err != nil || !result {
		t.Fatalf("result %v err %v", result, err)
	}
}
