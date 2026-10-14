package prompt

import (
	"strings"
	"testing"
)

func TestParsePlanRejectsLinesOverSchemaLimit(t *testing.T) {
	limit := PlanLineLimit()
	if limit <= 0 {
		t.Fatal("plan schema has no line limit")
	}
	long := strings.Repeat("a", limit+1)
	out := `{"ask":"x","name":"X","data":[{"id":"deal","line":"` + long + `"}],"screens":[{"id":"deals","line":"Deals"}],"agent":{"line":"Helps"}}`
	_, err := ParsePlan(out)
	if err == nil || !strings.Contains(err.Error(), "at most") || !strings.Contains(err.Error(), `data "deal"`) {
		t.Fatalf("err = %v, want over-limit rejection naming the line", err)
	}
	ok := strings.Replace(out, long, strings.Repeat("a", limit), 1)
	if _, err := ParsePlan(ok); err != nil {
		t.Fatalf("line at the limit rejected: %v", err)
	}
}
