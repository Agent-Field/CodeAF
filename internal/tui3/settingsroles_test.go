package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// THE ROLES SECTION, from the three sides a person meets it: what it says about
// a role nobody has touched, pinning one, and unpinning it again.
//
// Every assertion here goes through the panel's own doors — the registry row,
// the cursor, the keys — because the whole claim of the section is that it is a
// VIEW OF ONE REGISTRY ROW and not a second place a pin can live.

// setRow writes one registry row the way the panel writes it, and rebuilds.
func setRow(t *testing.T, a *app, key, value string) {
	t.Helper()
	row, ok := a.sheet.registry.Row(key)
	if !ok {
		t.Fatalf("the registry has no row %q", key)
	}
	if err := row.Apply(value); err != nil {
		t.Fatalf("writing %q: %v", key, err)
	}
	a.sheet.rows = a.sheet.registry.Rows()
	a.sheet.build()
}

// roleItem is one role's row as the panel currently holds it.
func roleItem(t *testing.T, a *app, role roles.Role) *roleRow {
	t.Helper()
	for _, item := range a.sheet.items {
		if item.role != nil && item.role.role == role {
			return item.role
		}
	}
	t.Fatalf("the roles section has no row for %q", role)
	return nil
}

// cursorToRole walks the cursor onto one role's row.
func cursorToRole(t *testing.T, a *app, role roles.Role) {
	t.Helper()
	for i, item := range a.sheet.items {
		if item.role != nil && item.role.role == role {
			a.sheet.cursor = i
			return
		}
	}
	t.Fatalf("the roles section has no row for %q", role)
}

// tieredSheet is the panel with both tiers set to a model a test can recognise.
func tieredSheet(t *testing.T) *app {
	t.Helper()
	a, _ := sheetApp(t)
	a.models = func() []Model { return mixedModels }
	a.openSettings()
	setRow(t, a, config.KeyTierHighModel, "test/careful-model")
	setRow(t, a, config.KeyTierLowModel, "test/cheap-model")
	return a
}

// EVERY REGISTERED ROLE IS A ROW, and each one says which tier answers it and
// which model that comes out as. Before the section, the two tier rows were two
// model ids with no way of finding out what actually ran on them.
func TestTheRolesSectionSaysWhatAnswersEachRole(t *testing.T) {
	a := tieredSheet(t)

	for _, c := range []struct {
		role       roles.Role
		tier, want string
	}{
		{roles.RolePlanner, "careful work", "test/careful-model"},
		{roles.RoleWorker, "small work", "test/cheap-model"},
		{roles.RoleCompaction, "careful work", "test/careful-model"},
		{roles.RoleTitle, "small work", "test/cheap-model"},
	} {
		row := roleItem(t, a, c.role)
		if row.model != c.want {
			t.Errorf("%s resolves to %q, want %q", c.role, row.model, c.want)
		}
		if got := a.sheet.tierWord(row.tier); got != c.tier {
			t.Errorf("%s sits under %q, want %q", c.role, got, c.tier)
		}
		if row.pin != "" {
			t.Errorf("%s reads as pinned to %q on a profile nobody has touched", c.role, row.pin)
		}
	}

	// THE SECTION HANGS OFF THE ROW IT WRITES: it is drawn directly under
	// "pinned roles", which is where every pin it sets actually lands.
	var after int
	for i, item := range a.sheet.items {
		if item.row.Key == config.KeyModelRoles {
			after = i
			break
		}
	}
	if after == 0 {
		t.Fatal("the Session tab has no pinned roles row")
	}
	if !a.sheet.items[after+1].heading() || a.sheet.items[after+1].head != rolesHead {
		t.Fatalf("the section does not follow the row it writes: %+v", a.sheet.items[after+1])
	}
	if a.sheet.items[after+2].role == nil {
		t.Fatal("the section's heading is not followed by a role")
	}

	// And it draws as a row of this panel: the role's name, its tier, its model.
	cursorToRole(t, a, roles.RolePlanner)
	a.touch()
	screen := plain(frame(a))
	if !strings.Contains(screen, "planner") || !strings.Contains(screen, "careful work · test/careful-model") {
		t.Fatalf("the planner's row does not say what answers it:\n%s", screen)
	}
	// And the one description this panel ever shows — the selected row's — says
	// where that answer came from and how to change it.
	selected, ok := a.sheet.current()
	if !ok {
		t.Fatal("the cursor is not on the planner")
	}
	if !strings.Contains(selected.meta.about, "follows careful work above") {
		t.Fatalf("the selected role's line reads %q", selected.meta.about)
	}
}

// A ROLE WITH NOTHING SET ANYWHERE FOLLOWS THE CONVERSATION, which is
// roles.Resolve's floor and not a failure: an install that configured no tiers
// has every role answering on the model the person is already talking to.
func TestARoleWithNoTiersFollowsTheConversation(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()

	for _, role := range []roles.Role{roles.RolePlanner, roles.RoleWorker} {
		if got := roleItem(t, a, role).model; got != a.model {
			t.Errorf("%s resolves to %q, want the conversation's own %q", role, got, a.model)
		}
	}
}

// PINNING IS THE PICKER AND IT WRITES THE ROW THE PINS LIVE IN. Enter opens the
// same component /model opens, and what it chooses lands as one pair inside
// "pinned roles" — not as a knob of its own.
func TestPinningARoleWritesThePinnedRolesRow(t *testing.T) {
	a := tieredSheet(t)
	cursorToRole(t, a, roles.RolePlanner)
	drive(t, a, key("enter"))

	if a.sheet.sel == nil {
		t.Fatal("a role row did not open a picker")
	}
	if a.sheet.sel.role != roles.RolePlanner {
		t.Fatalf("the picker is answering for %q", a.sheet.sel.role)
	}
	if a.sheet.sel.key != config.KeyModelRoles {
		t.Fatalf("the picker writes %q, want the row every pin lives in", a.sheet.sel.key)
	}
	chosen, ok := a.sheet.sel.choice()
	if !ok {
		t.Fatal("the picker offered nothing")
	}
	drive(t, a, key("enter"))

	row, _ := a.sheet.registry.Row(config.KeyModelRoles)
	if row.Value() != "planner:"+chosen {
		t.Fatalf("the pinned roles row reads %q, want planner:%s", row.Value(), chosen)
	}
	pinned := roleItem(t, a, roles.RolePlanner)
	if pinned.pin != chosen || pinned.model != chosen {
		t.Fatalf("the row reads pin %q model %q, want %q for both", pinned.pin, pinned.model, chosen)
	}
	// The pin is on the row, and only that row: its tier is untouched, so
	// everything else under "careful work" still answers there.
	if got := roleItem(t, a, roles.RoleCompaction).model; got != "test/careful-model" {
		t.Fatalf("pinning the planner moved the compaction to %q", got)
	}

	cursorToRole(t, a, roles.RolePlanner)
	a.touch()
	screen := plain(frame(a))
	if !strings.Contains(screen, "pinned") {
		t.Fatalf("a pinned role does not say so:\n%s", screen)
	}
	if !strings.Contains(screen, "del unpin") {
		t.Fatalf("the keys line does not offer the one key that clears it:\n%s", screen)
	}

	// AND DEL PUTS IT BACK. It is the one thing enter cannot do — no row in a
	// catalog means "no model".
	drive(t, a, key("delete"))
	row, _ = a.sheet.registry.Row(config.KeyModelRoles)
	if rowText(row) != "" {
		t.Fatalf("del left the row reading %q", row.Value())
	}
	if unpinned := roleItem(t, a, roles.RolePlanner); unpinned.pin != "" || unpinned.model != "test/careful-model" {
		t.Fatalf("the unpinned planner reads pin %q model %q", unpinned.pin, unpinned.model)
	}
}

// THE SECTION AND THE TEXT ROW CANNOT DISAGREE. A pin typed into "pinned roles"
// by hand shows here as a pin, and pinning a second role from the section keeps
// the first — the row is parsed and re-serialized, never appended to.
func TestTheSectionAndTheTextRowAreOneAnswer(t *testing.T) {
	a := tieredSheet(t)
	setRow(t, a, config.KeyModelRoles, "worker:openai/gpt-4.1-mini")

	byHand := roleItem(t, a, roles.RoleWorker)
	if byHand.pin != "openai/gpt-4.1-mini" || byHand.model != "openai/gpt-4.1-mini" {
		t.Fatalf("a pin typed into the row reads as pin %q model %q", byHand.pin, byHand.model)
	}

	row, _ := a.sheet.registry.Row(config.KeyModelRoles)
	a.applyRolePin(row, roles.RolePlanner, "test/pinned-model")

	row, _ = a.sheet.registry.Row(config.KeyModelRoles)
	if row.Value() != "planner:test/pinned-model, worker:openai/gpt-4.1-mini" {
		t.Fatalf("the row reads %q, want both pins sorted", row.Value())
	}
	if a.sheet.msg != "" {
		t.Fatalf("the panel refused a legal pin: %q", a.sheet.msg)
	}

	// Pinning a role that is already pinned REPLACES its model rather than
	// naming it twice, which the registry refuses.
	row, _ = a.sheet.registry.Row(config.KeyModelRoles)
	a.applyRolePin(row, roles.RoleWorker, "test/cheap-model")
	row, _ = a.sheet.registry.Row(config.KeyModelRoles)
	if row.Value() != "planner:test/pinned-model, worker:test/cheap-model" {
		t.Fatalf("re-pinning wrote %q", row.Value())
	}
}

// A ROLE IS FOUND BY ITS OWN NAME. The word is in no registry key, so the
// search would answer nothing at all if the section did not answer for itself.
func TestSearchingFindsARoleByName(t *testing.T) {
	a := tieredSheet(t)
	for _, r := range "planner" {
		drive(t, a, key(string(r)))
	}

	found := roleItem(t, a, roles.RolePlanner)
	if found.model != "test/careful-model" {
		t.Fatalf("the found row resolves to %q", found.model)
	}
	if _, ok := a.sheet.current(); !ok {
		t.Fatal("the search left the cursor on a heading")
	}
	for _, item := range a.sheet.items {
		if item.role != nil && item.role.role != roles.RolePlanner {
			t.Fatalf("the search kept %q as well", item.role.role)
		}
	}
}

// DEL DOES NOTHING ANYWHERE ELSE. It is one row's key, not a delete key the
// panel grew, so it must not touch whatever the cursor happens to be on.
func TestDelOnAnOrdinaryRowChangesNothing(t *testing.T) {
	a := tieredSheet(t)
	cursorTo(t, a, config.KeyTierHighModel)
	drive(t, a, key("delete"))

	row, _ := a.sheet.registry.Row(config.KeyTierHighModel)
	if row.Value() != "test/careful-model" {
		t.Fatalf("del changed a tier row to %q", row.Value())
	}
	if got := roleItem(t, a, roles.RoleWorker); got.pin != "" {
		t.Fatalf("del pinned something: %q", got.pin)
	}
}

// EVERY REGISTERED ROLE HAS A ROW — no exceptions and no allow-list.
//
// The section is built from [roles.Registered], which is filled by init
// functions in whichever packages make the calls (internal/roles states the
// open-registry law). So the failure this holds shut is a role added one file
// away and silently unreachable from the settings sheet: a call spending
// somebody's money that they cannot see, cannot price and cannot pin.
func TestTheRolesSectionListsEveryRegisteredRole(t *testing.T) {
	a := tieredSheet(t)
	drawn := map[roles.Role]bool{}
	for _, item := range a.sheet.items {
		if item.role != nil {
			drawn[item.role.role] = true
		}
	}
	for _, role := range roles.Registered() {
		if !drawn[role] {
			t.Errorf("the roles section has no row for %q", role)
		}
	}
}

// THE PER-TURN PAIR IS ONE OF THEM, and it is the row that would be silently
// wrong: the router and the extractor run twice every turn (internal/reflex), so
// a reflex resolving anywhere but its own tier reads as thrift and bills as a
// habit.
func TestTheReflexRoleFollowsItsOwnTier(t *testing.T) {
	a := tieredSheet(t)
	row := roleItem(t, a, roles.RoleReflex)
	if row.tier != roles.TierReflex {
		t.Fatalf("reflex resolves on %q, want its own tier", row.tier)
	}
}
