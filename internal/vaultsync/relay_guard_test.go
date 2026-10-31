package vaultsync

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/keys"
)

type relayGuardedKeys struct{ heldKeys }

func (g relayGuardedKeys) ApplyIf(changes, expected map[string]string) error {
	for key, value := range changes {
		if g.heldKeys[key] != expected[key] {
			continue
		}
		if value == "" {
			delete(g.heldKeys, key)
		} else {
			g.heldKeys[key] = value
		}
	}
	return nil
}

func TestKeyedGuardKeepsAnEditMadeWhilePullIsInFlight(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	left := relayGuardedKeys{heldKeys{"name": "original"}}
	right := relayGuardedKeys{heldKeys{}}
	a.Carry = []Carrier{GuardedKeys("relay-guard", left, filepath.Join(a.home, "relay.ledger"))}
	b.Carry = []Carrier{GuardedKeys("relay-guard", right, filepath.Join(b.home, "relay.ledger"))}
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	left.heldKeys["name"] = "remote edit"
	must(t, a.Push(ctx))
	b.Dir = &raced{Client: b.Dir, hook: func() { right.heldKeys["name"] = "typed during request" }}
	must(t, b.Pull(ctx))
	if right.heldKeys["name"] != "typed during request" {
		t.Fatal("pull lost an edit made during its network request")
	}
	must(t, b.Push(ctx))
	must(t, a.Pull(ctx))
	if left.heldKeys["name"] != "typed during request" {
		t.Fatal("preserved edit did not travel next round")
	}
}

func TestGuardedPullLeavesLateEditsForTheNextIdleCapture(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	left := relayGuardedKeys{heldKeys{"name": "original"}}
	right := relayGuardedKeys{heldKeys{}}
	a.Carry = []Carrier{GuardedKeys("teams", left, filepath.Join(a.home, "teams.ledger"))}
	b.Carry = []Carrier{GuardedKeys("teams", right, filepath.Join(b.home, "teams.ledger"))}
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	must(t, a.Capture())
	left.heldKeys["name"] = "renamed while idle"
	must(t, a.Pull(ctx))
	before, _ := keys.Fingerprint(a.home)
	must(t, a.Capture())
	after, _ := keys.Fingerprint(a.home)
	if before == after {
		t.Fatal("pull hid an unpublished edit from the next idle capture")
	}
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	if right.heldKeys["name"] != "renamed while idle" {
		t.Fatal("the next idle capture did not publish the late edit")
	}
}

func TestKeyedGuardAcceptsASecondMergeDuringCASRetry(t *testing.T) {
	r := newRig(t)
	machine := r.machine()
	source := relayGuardedKeys{heldKeys{"name": "original"}}
	carrier := GuardedKeys("retry-guard", source, filepath.Join(machine.home, "retry.ledger"))
	must(t, carrier.capture(machine.vault))
	for _, value := range []string{"first merged name", "second merged name"} {
		must(t, machine.vault.Put(carrier.id("name"), keys.Entry{Name: "name", Scope: carrier.scope, Value: value}))
		must(t, carrier.restore(machine.vault))
		if source.heldKeys["name"] != value {
			t.Fatal("first merge's write was mistaken for a local edit")
		}
	}
	source.heldKeys["name"] = "typed during retry"
	must(t, machine.vault.Put(carrier.id("name"), keys.Entry{Name: "name", Scope: carrier.scope, Value: "third remote name"}))
	must(t, carrier.restore(machine.vault))
	if source.heldKeys["name"] != "typed during retry" {
		t.Fatal("retry overwrote a local edit")
	}
}

type disappearingKeys struct {
	heldKeys
	accepts bool
}

func (s *disappearingKeys) Holds(string) bool { return s.accepts }

func TestExistingCarriersTombstoneRemovedKeysEvenWithoutAPlace(t *testing.T) {
	for _, kind := range []string{"provider-keys", "preferences"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			machine := r.machine()
			source := &disappearingKeys{heldKeys: heldKeys{"key": "secret"}, accepts: true}
			carrier := Keys(kind, source, filepath.Join(machine.home, "keys.ledger"))
			must(t, carrier.capture(machine.vault))
			delete(source.heldKeys, "key")
			source.accepts = false
			must(t, carrier.capture(machine.vault))
			removed, err := machine.vault.Removed(carrier.id(""))
			must(t, err)
			if len(removed) != 1 || removed[0] != carrier.id("key") {
				t.Fatalf("removal did not tombstone: %v", removed)
			}
		})
	}
}

func TestDisabledTeamCarriersDoNoVaultWorkEvenWhenAlreadyConstructed(t *testing.T) {
	r := newRig(t)
	machine := r.machine()
	enabled := true
	carrier := BestEffort(GuardedKeys("teams", heldKeys{"name": "alpha"}, filepath.Join(machine.home, "teams.ledger")), func() bool { return enabled }, func(err error) { t.Fatal(err) })
	must(t, carrier.capture(machine.vault))
	enabled = false
	counted := &countedVault{VaultFile: machine.vault}
	must(t, carrier.capture(counted))
	must(t, carrier.captureEdits(counted))
	must(t, carrier.restore(counted))
	if counted.calls != 0 {
		t.Fatal("disabled team carrier touched vault", counted.calls)
	}
	windows := BestEffort(Windows("team-traffic", "writer", nil, "", 0), func() bool { return enabled }, func(err error) { t.Fatal(err) })
	must(t, windows.capture(counted))
	must(t, windows.captureEdits(counted))
	must(t, windows.restore(counted))
	if counted.calls != 0 {
		t.Fatal("disabled Traffic carrier touched vault", counted.calls)
	}
}
