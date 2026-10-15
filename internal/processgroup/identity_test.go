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
	if Restore(other).Alive() || SameBoot(other) || !DifferentBoot(other) {
		t.Fatal("an identity from another boot read as ours")
	}
	if err := Restore(other).Kill(); err != nil {
		t.Fatal(err)
	}
	if !Restore(id).Alive() {
		t.Fatal("a refused signal reached the group")
	}
}

func TestMissingBootIdentityCannotAuthorizeRestoreOrAutomaticRebootStart(t *testing.T) {
	// All package tests are serial. Initialize once, then model a boot read that
	// failed: Linux start ticks alone can match an unrelated process after reboot.
	previous := BootID()
	bootID = ""
	t.Cleanup(func() { bootID = previous })
	g := Group{pid: 123, start: 456, known: true}
	if _, ok := g.Identity(); ok {
		t.Fatal("an unreadable boot produced a restorable identity")
	}
	for _, id := range []Identity{{PID: 123, Start: 456}, {PID: 123, Start: 456, Boot: previous}} {
		if Restore(id).known || SameBoot(id) || DifferentBoot(id) {
			t.Fatal("an unreadable boot was treated as proof of process ownership or reboot")
		}
	}
}

func TestInvalidPersistedIdentityFailsClosed(t *testing.T) {
	for _, id := range []Identity{{PID: -1, Start: 2, Boot: BootID()}, {PID: 123, Boot: BootID()}, {PID: 123, Start: 2}} {
		if Restore(id).known {
			t.Fatal("an incomplete persisted identity was trusted")
		}
	}
}
