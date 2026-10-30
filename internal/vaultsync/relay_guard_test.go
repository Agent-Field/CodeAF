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
	a.Carry = []Carrier{Keys("relay-guard", left, filepath.Join(a.home, "relay.ledger"))}
	b.Carry = []Carrier{Keys("relay-guard", right, filepath.Join(b.home, "relay.ledger"))}
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

func TestKeyedGuardAcceptsASecondMergeDuringCASRetry(t *testing.T) {
	r := newRig(t)
	machine := r.machine()
	source := relayGuardedKeys{heldKeys{"name": "original"}}
	carrier := Keys("retry-guard", source, filepath.Join(machine.home, "retry.ledger"))
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
