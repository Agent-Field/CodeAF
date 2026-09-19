package config

import "testing"

func TestOrganizeEnabledAtDefaultsOnAndHonoursAWrittenOff(t *testing.T) {
	dir := t.TempDir()
	if !OrganizeEnabledAt(dir) {
		t.Fatal("unset workspace.organize was off; automatic filing is the default")
	}
	if err := writeBool(dir, KeyWorkspaceOrganize, formatBool(false)); err != nil {
		t.Fatal(err)
	}
	if OrganizeEnabledAt(dir) {
		t.Fatal("workspace.organize written off still read as on")
	}
	if err := writeBool(dir, KeyWorkspaceOrganize, formatBool(true)); err != nil {
		t.Fatal(err)
	}
	if !OrganizeEnabledAt(dir) {
		t.Fatal("workspace.organize written on still read as off")
	}
}
