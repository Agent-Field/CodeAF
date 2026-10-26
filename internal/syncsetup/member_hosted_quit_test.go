package syncsetup

import (
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// A clean quit requires the hosted relay to commit closing releases and
// accept another home's immediate take through an anchor without election ticks.
func TestHostedLeaderCleanQuitReleasesAndHandsBack(t *testing.T) {
	r := newHostedLeadRig(t)
	p, ok, err := Open(machine(t, r.server.URL))
	mustNil(t, err)
	if !ok {
		t.Fatal("open")
	}
	stop := hostedOwner(t, p)
	receipt := hostedPersonalLeader(t, p)
	id := p.Identity
	b := r.home(&id)
	results := []<-chan error{}
	for _, cell := range []string{"01J00000000000000000QUIT1", "01J00000000000000000QUIT2"} {
		client, result := quitChatView(t, p, cell)
		mustNil(t, client.Agent().DetachAndWait())
		mustNil(t, client.Close())
		results = append(results, result)
	}
	mustNil(t, p.HandBack(t.Context()))
	stop()
	for _, result := range results {
		select {
		case err := <-result:
			mustNil(t, err)
		case <-time.After(requestTimeout):
			t.Fatal("hosted closing release never completed")
		}
	}
	for _, cell := range []string{"01J00000000000000000QUIT1", "01J00000000000000000QUIT2"} {
		v, err := b.Dir.Cell(t.Context(), cell)
		mustNil(t, err)
		if v.Cell.Lease.ReleasedAt == 0 || v.Cell.Lease.Expires != 0 {
			t.Fatal("hosted release did not commit", v)
		}
		_, err = b.Dir.Acquire(t.Context(), cell, directory.AcquireOpts{})
		mustNil(t, err)
	}
	status, err := b.clusterClient().Status(t.Context())
	mustNil(t, err)
	if !slices.Contains([]uint64{1, 2, 3}, status.Leader) {
		t.Fatal("hosted anchor did not take over", status)
	}
	state := hostedFallbackTicks(t, p, 8, "")
	if !slices.Equal(state.Configuration.Voters, []uint64{1, 2, 3}) || state.Configuration.Joint() || !slices.Contains(state.Configuration.Learners, receipt.Node) || state.Membership.Leadership != nil {
		t.Fatal("hosted topology did not restore", state)
	}
	hostedOwner(t, p)
	hostedReady(t, p)
	status, err = p.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Leader == receipt.Node || !slices.Contains(status.Configuration.Learners, receipt.Node) {
		t.Fatal("hosted return regained leadership", status)
	}
}

// A clean quit requires a hosted partition to bound quit without changing the
// ordinary fallback, and require a non-leading member to send no hand-back.
func TestHostedUnreachableAndNonLeaderQuit(t *testing.T) {
	r := newHostedLeadRig(t)
	down := partitionQuitPeers(r.server.Config)
	p, ok, err := Open(machine(t, r.server.URL))
	mustNil(t, err)
	if !ok {
		t.Fatal("open")
	}
	stop := hostedOwner(t, p)
	hostedReady(t, p)
	waitMember(t, p, func(m *personalMember) bool { return m.idle })
	before := p.relayAttempts.Load() + p.clusterFrames.Load()
	mustNil(t, p.HandBack(t.Context()))
	if p.relayAttempts.Load()+p.clusterFrames.Load() != before {
		t.Fatal("non-leading quit sent requests")
	}
	receipt := hostedPersonalLeader(t, p)
	down.Store(true)
	start := time.Now()
	if err := p.HandBack(t.Context()); err == nil {
		t.Fatal("hosted partition claimed a hand-back")
	}
	stop()
	if time.Since(start) > LeaderQuitWithin+time.Second {
		t.Fatal("hosted quit exceeded its bound")
	}
	down.Store(false)
	hostedLeaderDeparted(t, p)
	state := hostedFallbackTicks(t, p, 21, "")
	if state.Leader == receipt.Node || !slices.Equal(state.Configuration.Voters, []uint64{1, 2, 3}) {
		t.Fatal("hosted fallback failed", state)
	}
}
