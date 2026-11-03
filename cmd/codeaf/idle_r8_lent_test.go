package main

import "testing"

func TestIdleRelease_R8_LentModelRPCPreservesFactoryChoice(t *testing.T) {
	r := newLendRig(t, "")
	r.askTitled(t, "read the ledger")
	before := r.live.Model()
	conv := r.backend.liveOf(mustFind(t, r.backend, r.agent)).conn.Conv
	conv.SetModel("other/model")
	t.Logf("factory model before=%q after direct lent RPC=%q", before, r.live.Model())
	if got := r.live.Model(); got != before {
		t.Errorf("lent model RPC bypassed the factory model fence: %q -> %q", before, got)
	}
}
