package syncsetup

import (
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
	"time"
)

func TestIdleRelease_R5_1_SettingAfterIdleReleaseSurvivesOpen(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	a, _ := idleContractSession(t, h, c)
	before := idleContractReleased(t, h, c.drive)
	if !a.SetConversationEffort("high") {
		t.Fatal("valid setting refused")
	}
	a.SettleWrites()
	seen := idleContractReleased(t, h, c.drive)
	got, err := h.continuerB().OpenAvailable(t.Context(), h.cell.ID, &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence})
	mustNil(t, err)
	restored, err := session.LoadMeta(got.Taken.Cell.Root)
	mustNil(t, err)
	if restored.Effort != "high" {
		t.Errorf("setting accepted after release did not reach B: effort=%q", restored.Effort)
	}
	if seen.Cell.Lease.Fence <= before.Cell.Lease.Fence {
		t.Error("setting did not acquire a new hold before saving")
	}
}

func TestIdleRelease_R5_1_SettingBeforeReleaseDoesNotCancelHandoff(t *testing.T) {
	h := newTwoHomes(t)
	c := idleContractChat(t, h)
	a, _ := idleContractSession(t, h, c)
	c.drive.Idle()
	if !a.SetConversationEffort("high") {
		t.Fatal("effort refused")
	}
	a.SettleWrites()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v, err := h.a.Dir.Cell(t.Context(), h.cell.ID)
		if err == nil && v.Cell.Lease.Expires == 0 {
			got, err := h.continuerB().OpenAvailable(t.Context(), h.cell.ID, nil)
			mustNil(t, err)
			m, err := session.LoadMeta(got.Taken.Cell.Root)
			mustNil(t, err)
			if m.Effort != "high" {
				t.Fatalf("received effort=%q", m.Effort)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a setting before the release canceled handoff permanently")
}
