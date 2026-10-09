package config

import "testing"

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
