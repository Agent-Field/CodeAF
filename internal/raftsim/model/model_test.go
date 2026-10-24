package model

import "testing"

func TestR1ObservedTakeHasOneWinnerInEitherOrder(t *testing.T) {
	s, created := Step(State{}, Call{Kind: "create", Actor: "a", Cell: "c", Head: "h", At: 100})
	for _, order := range [][2]string{{"b", "c"}, {"c", "b"}} {
		first, won := Step(s, Call{Kind: "acquire", Actor: order[0], Cell: "c", Expected: created.Cell, Force: true})
		last, lost := Step(first, Call{Kind: "acquire", Actor: order[1], Cell: "c", Expected: created.Cell, Force: true})
		if won.Code != "ok" || lost.Code != "lease_held" || last.Cells["c"].Holder != order[0] || last.Cells["c"].Fence != 2 {
			t.Fatalf("order %v: won=%+v lost=%+v state=%+v", order, won, lost, last)
		}
		if s.Cells["c"].Holder != "a" {
			t.Fatal("a candidate history mutated the input state")
		}
	}
}

func TestR2FencePrecedesHeadAndDisplacedWritesChangeNothing(t *testing.T) {
	s, created := Step(State{}, Call{Kind: "create", Actor: "a", Cell: "c", Head: "h"})
	s, _ = Step(s, Call{Kind: "acquire", Actor: "b", Cell: "c", Force: true, Expected: created.Cell})
	for _, kind := range []string{"publish", "beat", "release"} {
		next, out := Step(s, Call{Kind: kind, Actor: "a", Cell: "c", Fence: 1, OldHead: "wrong", Head: "wrong"})
		if out.Code != "fence_stale" || next.Cells["c"] != s.Cells["c"] {
			t.Fatalf("%s: %+v, %+v", kind, out, next)
		}
	}
}

func TestR16RevokeAppliesBeforeAlreadySubmittedMutation(t *testing.T) {
	s, _ := Step(State{}, Call{Kind: "revoke", Actor: "a", Target: "b", At: 100})
	next, out := Step(s, Call{Kind: "create", Actor: "b", Cell: "c", At: 50})
	if out.Code != "revoked" || len(next.Cells) != 0 || next.Now != 100 {
		t.Fatalf("revoked mutation: %+v, %+v", next, out)
	}
}

func TestR4VaultCASAndReadAreOneSequentialHistory(t *testing.T) {
	s, ok := Step(State{}, Call{Kind: "set_vault", NewVault: "v1"})
	s, lost := Step(s, Call{Kind: "set_vault", OldVault: "", NewVault: "v2"})
	_, read := Step(s, Call{Kind: "read_vault"})
	if ok.Code != "ok" || lost.Code != "cas" || read.Vault != "v1" {
		t.Fatalf("CAS history: %+v %+v %+v", ok, lost, read)
	}
}
