package factory

import (
	"reflect"
	"strings"
	"testing"
)

// A HOUSE RULE IS ONE LINE: trimmed, said once whatever its case, and never a
// paragraph, a line break or an overlong list, because every step's brief
// quotes it.
func TestHouseRulesAreCleanedAndBounded(t *testing.T) {
	got, err := HouseRules{
		Rules:    []string{"  don't touch auth without checking with me ", "Don't touch auth without checking with me", ""},
		AskFirst: nil,
		Never:    []string{"infra/"},
	}.Clean()
	if err != nil {
		t.Fatal(err)
	}
	want := HouseRules{Rules: []string{"don't touch auth without checking with me"}, AskFirst: []string{}, Never: []string{"infra/"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleaned to %#v, want %#v", got, want)
	}
	if !(HouseRules{}).Empty() || got.Empty() {
		t.Fatal("Empty misread the lists")
	}
	for name, bad := range map[string]HouseRules{
		"long line":  {Rules: []string{strings.Repeat("x", HouseRuleLongest+1)}},
		"line break": {AskFirst: []string{"touch auth/\nand everything else"}},
		"too many":   {Never: make([]string, 0)},
	} {
		if name == "too many" {
			for i := 0; i <= HouseRulesMost; i++ {
				bad.Never = append(bad.Never, "path"+strings.Repeat("x", i))
			}
		}
		if _, err := bad.Clean(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
