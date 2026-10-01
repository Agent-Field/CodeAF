package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// doorAt is the production Approvals door for one home of the rig.
func (r *pairRig) approvalsAt(home string) *approvalsDoor {
	d := newApprovalsDoor(home)
	d.mailbox = func(string) (pair.Mailbox, error) {
		return pair.Mailbox{Box: pairbox.NewHTTP(r.url, &http.Client{Timeout: time.Minute}), URL: r.url, Host: "relay.test"}, nil
	}
	d.approver = linkApprover(home)
	return d
}

// enrol writes the first computer's own record, as its first sync does.
func (r *pairRig) enrol(t *testing.T, door *approvalsDoor, name string) {
	t.Helper()
	client, id, self, err := door.home()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := directory.SealName(directory.MetadataKey(id.CellKey()), name)
	if err != nil {
		t.Fatal(err)
	}
	dev := directory.Device{V: 1, Name: sealed, AddedBy: id.ID(), Platform: "linux"}
	if err := client.PutDevice(context.Background(), self, dev); err != nil {
		t.Fatal(err)
	}
}

// A link pasted on device A is read, approved through the door, and B joins;
// then the list names both, A is Self, and Revoke stops B.
func TestApprovalsDoorApprovesListsAndRevokes(t *testing.T) {
	rig := newPairRig(t)
	screen, done := rig.asking(t)
	link := screen.waitFor(t, `https://codeaf\.link/p/\S+#\S+`)[0]
	door := rig.approvalsAt(rig.homeA)
	ctx := context.Background()
	rig.enrol(t, door, "this computer")

	p, err := door.Pending(ctx, link)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "laptop" || p.Platform != "linux" || len(p.Check) != 4 || p.Device == "" {
		t.Fatalf("pending = %+v", p)
	}
	if err := door.Approve(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if first, _ := identity.Load(rig.homeA); first.ID() == "" {
		t.Fatal("no identity")
	}

	rows, err := door.Devices(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("devices = %+v, %v", rows, err)
	}
	var self, joined *tui3.DeviceRow
	for i := range rows {
		if rows[i].Self {
			self = &rows[i]
		} else {
			joined = &rows[i]
		}
	}
	if self == nil || joined == nil || joined.ID != p.Device || joined.Name != "laptop" || self.Name != "this computer" {
		t.Fatalf("rows = %+v, want one self and the joined %s", rows, p.Device)
	}
	if err := door.Revoke(ctx, p.Device); err != nil {
		t.Fatal(err)
	}
	rows, _ = door.Devices(ctx)
	for _, row := range rows {
		if row.ID == p.Device && !row.Revoked {
			t.Fatalf("revoked device still listed in: %+v", row)
		}
	}
}

// Deny turns the asker away, and a decided request cannot be answered again.
func TestApprovalsDoorDeny(t *testing.T) {
	rig := newPairRig(t)
	screen, done := rig.asking(t)
	link := screen.waitFor(t, `https://codeaf\.link/p/\S+#\S+`)[0]
	door := rig.approvalsAt(rig.homeA)
	p, err := door.Pending(context.Background(), link)
	if err != nil {
		t.Fatal(err)
	}
	if err := door.Deny(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("the new device was let in after a deny")
	}
	if err := door.Approve(context.Background(), p); err == nil {
		t.Fatal("answered twice")
	}
}

func TestApprovalsDoorRefusesAWrongLink(t *testing.T) {
	rig := newPairRig(t)
	if _, err := rig.approvalsAt(rig.homeA).Pending(context.Background(), "nope"); err == nil {
		t.Fatal("a bad link was read")
	}
}
