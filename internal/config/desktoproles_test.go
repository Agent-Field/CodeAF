package config

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/roles"
)

func TestADesktopRoleWithoutAChoiceRunsOnTheDefault(t *testing.T) {
	dir := t.TempDir()
	for _, role := range DesktopRoles() {
		model, effort, chosen := DesktopRoleChoice(dir, role.ID)
		if model != DesktopDefaultModel || effort != "" || chosen {
			t.Fatalf("%s: %q %q %v", role.ID, model, effort, chosen)
		}
	}
}

func TestADesktopChoiceKeepsTheRestOfTheFile(t *testing.T) {
	dir := t.TempDir()
	if err := writeProfileValue(dir, "other.row", "kept"); err != nil {
		t.Fatal(err)
	}
	if err := WriteDesktopRole(dir, "naming", "a/b", "medium"); err != nil {
		t.Fatal(err)
	}
	model, effort, chosen := DesktopRoleChoice(dir, "naming")
	if model != "a/b" || effort != "medium" || !chosen {
		t.Fatalf("%q %q %v", model, effort, chosen)
	}
	if got, ok := persistedString(dir, "other.row"); !ok || got != "kept" {
		t.Fatal("another row was lost")
	}
}

func TestOnlyTheConversationRoleWritesTheTerminalsModelRow(t *testing.T) {
	dir := t.TempDir()
	_ = WriteDesktopRole(dir, "tasks", "a/b", "")
	if ChatModelAt(dir) != "" {
		t.Fatal("a task choice moved the conversation model")
	}
	_ = WriteDesktopRole(dir, DesktopRoleConversation, "c/d", "")
	if ChatModelAt(dir) != "c/d" {
		t.Fatal("the conversation choice did not reach the terminal's row")
	}
	_ = WriteDesktopRole(dir, DesktopRoleConversation, "", "")
	if ChatModelAt(dir) != "" {
		t.Fatal("a reset left the row behind")
	}
}

func TestADesktopWriteRefusesWhatItCannotRun(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range [][3]string{{"nope", "a/b", ""}, {"tasks", "a b", ""}, {"tasks", "a/b", "loud"}, {"tasks", "a/b:low", ""}} {
		if WriteDesktopRole(dir, bad[0], bad[1], bad[2]) == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

func TestNoEngineRoleIsOwnedTwice(t *testing.T) {
	seen := map[string]string{}
	for _, role := range DesktopRoles() {
		for _, engine := range role.Engine {
			if other, dup := seen[string(engine)]; dup {
				t.Fatalf("%s owned by %s and %s", engine, other, role.ID)
			}
			seen[string(engine)] = role.ID
		}
	}
}

// Splitting one row into three must not throw away what a person chose on the
// one row: task names and summaries follow the old "naming" choice until each
// is chosen itself, and choosing one of them leaves chat titles alone.
func TestARoleSplitFromNamingKeepsTheChoiceMadeBeforeTheSplit(t *testing.T) {
	dir := t.TempDir()
	if err := writeProfileValue(dir, desktopRoleKey("naming"), "old/choice:low"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"worknames", "summaries"} {
		model, effort, chosen := DesktopRoleChoice(dir, id)
		if model != "old/choice" || effort != "low" || chosen {
			t.Fatalf("%s: %q %q %v", id, model, effort, chosen)
		}
	}
	if err := WriteDesktopRole(dir, "worknames", "new/one", ""); err != nil {
		t.Fatal(err)
	}
	if model, _, chosen := DesktopRoleChoice(dir, "worknames"); model != "new/one" || !chosen {
		t.Fatalf("own choice: %q %v", model, chosen)
	}
	if model, _, _ := DesktopRoleChoice(dir, "naming"); model != "old/choice" {
		t.Fatalf("titles moved to %q", model)
	}
	// Reset goes back to following, not to the default.
	_ = WriteDesktopRole(dir, "worknames", "", "")
	if model, _, chosen := DesktopRoleChoice(dir, "worknames"); model != "old/choice" || chosen {
		t.Fatalf("after reset: %q %v", model, chosen)
	}
}

// The engine reads an inherited choice too, so what the page shows is what runs.
func TestTheEngineRunsWhatThePageShowsForAnInheritedRole(t *testing.T) {
	dir := t.TempDir()
	_ = WriteDesktopRole(dir, "naming", "old/choice", "")
	source := DesktopRolesSource(dir)
	if got, ok := source(roles.PinKey(roles.RoleTaskName)); !ok || got != "old/choice" {
		t.Fatalf("task names run on %q", got)
	}
}

func TestEveryRoleSitsInOneOfThePagesSections(t *testing.T) {
	sections := map[string]bool{}
	for _, c := range DesktopRoleCategories() {
		sections[c.ID] = true
	}
	for _, role := range DesktopRoles() {
		if !sections[role.Category] {
			t.Errorf("%s is in no section", role.ID)
		}
		if role.Inherits != "" {
			if _, ok := DesktopRoleFor(role.Inherits); !ok {
				t.Errorf("%s inherits from %q, which is no role", role.ID, role.Inherits)
			}
		}
	}
}

// A role nothing calls must not be presented as working. The place roles are
// only registered by the engine file that makes their calls.
func TestARoleIsLiveOnlyOnceSomethingRegistersItsCall(t *testing.T) {
	filing, _ := DesktopRoleFor("placefiling")
	summaries, _ := DesktopRoleFor("summaries")
	conversation, _ := DesktopRoleFor(DesktopRoleConversation)
	if !DesktopRoleLive(conversation) {
		t.Fatal("the conversation is not live")
	}
	if DesktopRoleLive(summaries) {
		t.Fatal("a role with no engine call is live")
	}
	if _, registered := roles.TierOf(roles.RolePlaceFile); registered {
		t.Skip("this build registers the filing call; the not-live case cannot be shown")
	}
	if DesktopRoleLive(filing) {
		t.Fatal("filing is live with nothing making its call")
	}
	// Registration is what flips it; titles register from internal/roles itself.
	naming, _ := DesktopRoleFor("naming")
	if !DesktopRoleLive(naming) {
		t.Fatal("chat titles are registered but not live")
	}
}
