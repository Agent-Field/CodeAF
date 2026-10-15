//go:build !windows

package processgroup

import (
	"os/exec"
	"testing"
)

// TestARestoredIdentityIsTheGroupItWasRecordedAs proves the round trip a later
// process depends on: the identity read back from a file gates exactly as the
// captured group did, and an identity from another boot gates nothing.
func TestARestoredIdentityIsTheGroupItWasRecordedAs(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	ConfigureDetached(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	id, ok := CaptureGroup(cmd.Process.Pid).Identity()
	if !ok {
		t.Fatal("a live leader recorded no identity")
	}
	if !Restore(id).Alive() {
		t.Fatal("the restored group is not alive")
	}
	moved := id
	moved.Start++
	if Restore(moved).Alive() {
		t.Fatal("a recycled pid with another start identity read as ours")
	}
	other := id
	other.Boot = "another boot"
	if Restore(other).Alive() || SameBoot(other) {
		t.Fatal("an identity from another boot read as ours")
	}
	if err := Restore(other).Kill(); err != nil {
		t.Fatal(err)
	}
	if !Restore(id).Alive() {
		t.Fatal("a refused signal reached the group")
	}
}
