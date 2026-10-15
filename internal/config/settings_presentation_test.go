package config

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestChatPresentationLeavesExistingProfileUntouched(t *testing.T) {
	dir := t.TempDir()
	existing := []byte(`{"ui.hints":false,"memory.enabled":"off","task.parallel":4,"teams.wake":false,"practice_idle":"31m","future.setting":{"nested":[1,"keep"]}}`)
	if err := os.WriteFile(BudgetConfigPath(dir), existing, 0600); err != nil {
		t.Fatal(err)
	}
	rows := registry(t, dir).Rows()
	for _, row := range rows {
		_ = row.ChatPresentation()
	}
	got, err := os.ReadFile(BudgetConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, existing) {
		t.Fatalf("reading the new categories rewrote the existing profile: %s", got)
	}
	if TaskParallelAt(dir) != 4 || TeamDefaultsAt(dir).Wake || HintsAt(dir) || MemoryEnabledAt(dir) {
		t.Fatal("existing preferences were reset")
	}
	row, _ := registry(t, dir).Row(KeyTaskParallel)
	if err := row.Apply("6"); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(BudgetConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]json.RawMessage
	if err := json.Unmarshal(existing, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &after); err != nil {
		t.Fatal(err)
	}
	for key, value := range before {
		if key == KeyTaskParallel {
			continue
		}
		var a, b bytes.Buffer
		if err := json.Compact(&a, value); err != nil {
			t.Fatal(err)
		}
		if err := json.Compact(&b, after[key]); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a.Bytes(), b.Bytes()) {
			t.Errorf("editing concurrency changed %s: %s -> %s", key, value, after[key])
		}
	}
	if TaskParallelAt(dir) != 6 {
		t.Fatal("newly saved concurrency not used by resolver")
	}
}

// The v1 resident's scheduler rows — the practice budget and its quiet period,
// the arrival brief's absence, and tenure — used to stay registered and hidden
// from chat. They went with the scheduler, so they are not rows at all: a
// profile still holding one is a retired key ([retiredProfileKeys]).
func TestChatPresentationHasNoRowsForTheRemovedScheduler(t *testing.T) {
	r := registry(t, t.TempDir())
	for _, key := range []string{"practice_budget_usd", "practice_idle", "brief_after", "tenure_after"} {
		if _, ok := r.Row(key); ok {
			t.Fatalf("%s is still a settings row; nothing reads it", key)
		}
		if !retiredProfileKeys[key] {
			t.Fatalf("%s is not retired, so a profile holding it would be told it is ignored", key)
		}
	}
	for _, slot := range ModelSlots() {
		row, ok := r.Row(ModelSettingKey(slot.Slot))
		if !ok {
			t.Fatal(slot.Slot)
		}
		wantHidden := slot.Role != "" && slot.Slot != "talk"
		if row.ChatPresentation().Hidden != wantHidden {
			t.Errorf("slot %s visibility incompatible with live picker", slot.Slot)
		}
	}
}

func TestChatPresentationKeepsAliasesAndEveryVisibleRowCategorized(t *testing.T) {
	categories := []string{"General", "Models", "Memory", "Tasks", "AI teams", "Permissions", "Spending", "Connections", "Privacy"}
	for _, row := range registry(t, t.TempDir()).Rows() {
		p := row.ChatPresentation()
		if !p.Hidden && !slices.Contains(categories, p.Category) {
			t.Errorf("%s has unknown category %q", row.Key, p.Category)
		}
		if !slices.Contains(p.Aliases, row.Label) || !slices.Contains(p.Aliases, row.Key) {
			t.Errorf("%s lost legacy search terms", row.Key)
		}
	}
}

func TestChatPresentationDoesNotInvertPersistedHints(t *testing.T) {
	dir := t.TempDir()
	row, _ := registry(t, dir).Row(KeyHints)
	if row.ChatPresentation().Label != "show hints" {
		t.Fatal("missing positive display label")
	}
	// The shared registry still answers the resident's negative question. The
	// chat surface alone reverses its display; storage remains positive.
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	if HintsAt(dir) {
		t.Fatal("disable-hints on must preserve ui.hints=false")
	}
	if err := row.Apply("off"); err != nil {
		t.Fatal(err)
	}
	if !HintsAt(dir) {
		t.Fatal("disable-hints off must preserve ui.hints=true")
	}
}
