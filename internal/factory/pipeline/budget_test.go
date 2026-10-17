package pipeline

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestPipelineDefaultBudgetReadsWritesAndTightens(t *testing.T) {
	if Defaults().DefaultBudget != factory.DefaultJobBudget {
		t.Fatal("default drift")
	}
	f, err := Parse("default_budget: 10\n")
	if err != nil || len(f.Problems) != 0 || f.DefaultBudget != 10 {
		t.Fatalf("read %+v %v", f, err)
	}
	data, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(string(data))
	if err != nil || back.DefaultBudget != 10 {
		t.Fatalf("round trip %s %v", data, err)
	}
	personal, err := ParsePersonal("default_budget: 5\n")
	if err != nil {
		t.Fatal(err)
	}
	merged, _ := Merge(f, personal)
	if merged.DefaultBudget != 5 {
		t.Fatal("personal budget did not tighten")
	}
	personal, _ = ParsePersonal("default_budget: 20\n")
	merged, _ = Merge(f, personal)
	if merged.DefaultBudget != 10 {
		t.Fatal("personal budget widened")
	}
	for _, value := range []string{"0", "-1", "no"} {
		bad, _ := Parse("default_budget: " + value + "\n")
		if len(bad.Problems) != 1 || bad.DefaultBudget != factory.DefaultJobBudget {
			t.Fatalf("bad budget %s %+v", value, bad)
		}
	}
}
