package factory

import (
	"encoding/json"
	"testing"
)

// AN ITEM WRITTEN BEFORE THE ONE-WORD LAW IS READ WITH ONE-WORD NAMES: the
// 2026-10-09 item's `do through` stage and its phase become `through`, a
// sent-back `prove 2` phase becomes `prove2`, and one-word names stay.
func TestAnOldItemsStageNamesAreReadAsOneWord(t *testing.T) {
	doc := `{"ID":1,"Kind":"pr","Stages":[` +
		`{"Name":"read","Ask":"the diff","On":true},` +
		`{"Name":"do through","Ask":"do a through review from security and code","On":true},` +
		`{"Name":"deep review","Ask":"look again","On":true}],` +
		`"Stream":{"Phases":[{"Name":"read"},{"Name":"do through"},{"Name":"deep review"},{"Name":"prove 2","Note":"and again"}]}}`
	var it Item
	if err := json.Unmarshal([]byte(doc), &it); err != nil {
		t.Fatal(err)
	}
	want := []string{"read", "through", "deep"}
	for i, w := range want {
		if it.Stages[i].Name != w {
			t.Errorf("stage %d is %q, want %q", i, it.Stages[i].Name, w)
		}
	}
	phases := []string{"read", "through", "deep", "prove2"}
	for i, w := range phases {
		if it.Stream.Phases[i].Name != w {
			t.Errorf("phase %d is %q, want %q", i, it.Stream.Phases[i].Name, w)
		}
	}
}

func TestOneStageWordNeverRepeatsAName(t *testing.T) {
	taken := map[string]bool{"review": true}
	if got := OneStageWord("review again", "read it as a stranger", taken); got != "again" {
		t.Errorf("got %q", got)
	}
	if got := OneStageWord("do it", "audit the change", nil); got != "audit" {
		t.Errorf("a name of fillers takes its ask's word, got %q", got)
	}
	if got := OneStageWord("arch", "", nil); got != "arch" {
		t.Errorf("a one-word name changed to %q", got)
	}
}
