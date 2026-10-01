package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/roles"
)

// A person's search and the picker it opens must use the same seat word as
// /crew, while the role stored in the profile keeps its original identifier.
func TestSettingsCallsTheCheckingRoleChecker(t *testing.T) {
	a := tieredSheet(t)
	cursorToRole(t, a, roles.RoleAuditor)
	item, ok := a.sheet.current()
	if !ok {
		t.Fatal("the checking role has no settings row")
	}
	if item.meta.label != "checker" {
		t.Errorf("the checking role label is %q, want checker", item.meta.label)
	}
	for _, row := range a.sheet.items {
		if row.role == nil {
			continue
		}
		for _, banned := range []string{"auditor", "verdict", "verified", "refuted"} {
			if strings.Contains(row.meta.label, banned) {
				t.Errorf("the settings role label exposes %q: %q", banned, row.meta.label)
			}
		}
	}
	a.touch()
	screen := plain(frame(a))
	if strings.Contains(screen, "auditor") || !strings.Contains(screen, "checker") {
		t.Errorf("the role row does not use the crew's seat word:\n%s", screen)
	}
	drive(t, a, key("enter"))
	if a.sheet.sel == nil || a.sheet.sel.label != "checker" || a.sheet.sel.role != roles.RoleAuditor {
		t.Fatalf("the checking role's model picker lost its display word or stored role: %+v", a.sheet.sel)
	}
}

func TestSettingsFindsTheCheckingRoleByItsSeatWord(t *testing.T) {
	a := tieredSheet(t)
	for _, r := range "checker" {
		drive(t, a, key(string(r)))
	}
	roleItem(t, a, roles.RoleAuditor)
	cursorToRole(t, a, roles.RoleAuditor)
	a.touch()
	if screen := plain(frame(a)); strings.Contains(screen, "auditor") || !strings.Contains(screen, "checker") {
		t.Fatalf("searching checker did not find its role under that word:\n%s", screen)
	}
}
