package config

import (
	"strings"
	"testing"
)

// M7: the background-after row distinguishes an absent answer from the zero
// that turns its clock off, and refuses non-numbers in the registry's words.
func TestBackgroundAfterKeepsZeroAndDefaultsOnlyWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if got := BashBackgroundAfterAt(dir); got != DefaultBashBackgroundAfter {
		t.Fatalf("a missing row reads as %d seconds, want %d", got, DefaultBashBackgroundAfter)
	}
	row := mustRow(t, registry(t, dir), KeyBashBackgroundAfter)
	if row.Category != CategorySpending || row.Kind != SettingCount || row.Label != "background after" || row.Hint != BashBackgroundAfterHint {
		t.Fatalf("the background-after row is not the specified row: %+v", row)
	}
	if want := "A change lands on the next session."; !strings.HasSuffix(row.Hint, want) {
		t.Fatalf("the engine-fixed row does not tell the person when it lands: %q", row.Hint)
	}
	wantRefusal := `"background after" (bash.background_after_seconds) is one of the brakes on how much work may run at once on this machine, so it is not mine to change. Open /settings and change it yourself.`
	if got := row.SelfServiceRefusal(); got != wantRefusal {
		t.Fatalf("the guarded row refuses with %q, want %q", got, wantRefusal)
	}
	if err := row.Apply("0"); err != nil {
		t.Fatalf("turn the clock off: %v", err)
	}
	if got := BashBackgroundAfterAt(dir); got != 0 {
		t.Fatalf("a persisted zero came back as %d", got)
	}
	if got := mustRow(t, registry(t, dir), KeyBashBackgroundAfter).Value(); got != "0" {
		t.Fatalf("the row rendered a persisted zero as %q", got)
	}
	if err := row.Apply("not seconds"); err == nil || err.Error() != "that's not a whole number" {
		t.Fatalf("a non-number answered %v, want the registry's exact refusal", err)
	}
}
