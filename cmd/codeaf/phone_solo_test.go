package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

func makeHomeSolo(t *testing.T, home string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "identity-solo"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !identity.Solo(home) {
		t.Fatalf("home %s is not solo", home)
	}
}

// assertNoOtherComputer fails when a home that only ever approved a phone has been
// told that another computer is paired: a phone is not one, and the home would start
// behaving as if it shared its chats (contract P13).
func assertNoOtherComputer(t *testing.T, home, when string) {
	t.Helper()
	s, ok, err := syncsetup.Open(home)
	if err != nil || !ok {
		t.Fatalf("%s: syncsetup.Open = %v, %v", when, ok, err)
	}
	if s.OthersPaired() {
		t.Fatalf("%s: a phone approval told the home that another computer is paired", when)
	}
}

// TestP13PhoneApprovalKeepsSoloWhileComputerEndsSoloAndRevokeForgetsPhone
// verifies contract P13 across pairlink, approvalsDoor and app_pair:
// approving a phone on a solo home leaves identity.Solo true and sets
// PhonesApproved true; revoking the phone clears PhonesApproved; approving a
// computer on a solo home ends solo; approving a phone on a non-solo home
// leaves it non-solo.
func TestP13PhoneApprovalKeepsSoloWhileComputerEndsSoloAndRevokeForgetsPhone(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	ctx := context.Background()

	// 1. CLI `codeaf pair approve` of a phone on a solo home keeps identity.Solo
	// true and sets PhonesApproved true; reopening the decided phone link keeps
	// identity.Solo true as well.
	rig := newPairRig(t)
	t.Setenv("CODEAF_HOME", rig.homeA)
	t.Setenv(syncsetup.URLVar, rig.url)
	makeHomeSolo(t, rig.homeA)

	token1, _, check1, _, phonePub1 := testPhoneRequest(t, rig, "iPhone")
	phoneID1 := clientproto.DeviceIDOf(phonePub1)
	if _, err := rig.approveWith(t, []string{"approve", token1, "--check", check1}, ""); err != nil {
		t.Fatalf("CLI phone approve: %v", err)
	}
	if !identity.Solo(rig.homeA) {
		t.Fatal("CLI phone approval ended solo on homeA")
	}
	if !syncsetup.PhonesApproved(rig.homeA) || !syncsetup.PhoneMayTalk(rig.homeA) {
		t.Fatalf("after CLI phone approve: PhonesApproved=%v PhoneMayTalk=%v, want true",
			syncsetup.PhonesApproved(rig.homeA), syncsetup.PhoneMayTalk(rig.homeA))
	}
	if syncsetup.MayTalk(rig.homeA) {
		t.Fatal("MayTalk became true on a solo home after CLI phone approval")
	}
	assertNoOtherComputer(t, rig.homeA, "after CLI phone approval")

	// Reopening the already-decided phone link in CLI after clearing phones.json
	// (lost-reply recovery path) records the phone and still leaves solo true.
	if err := syncsetup.ForgetPhone(rig.homeA, phoneID1); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.approveWith(t, []string{"approve", token1}, ""); err != nil {
		t.Fatalf("CLI reopen decided phone link: %v", err)
	}
	if !identity.Solo(rig.homeA) || !syncsetup.PhonesApproved(rig.homeA) {
		t.Fatalf("after CLI reopened phone link: Solo=%v PhonesApproved=%v, want true/true",
			identity.Solo(rig.homeA), syncsetup.PhonesApproved(rig.homeA))
	}

	// Revoking the phone through approvalsDoor clears PhonesApproved and leaves solo true.
	door := rig.approvalsAt(rig.homeA)
	if err := door.Revoke(ctx, phoneID1); err != nil {
		t.Fatalf("door.Revoke phone1: %v", err)
	}
	if syncsetup.PhonesApproved(rig.homeA) {
		t.Fatal("PhonesApproved still true after revoking the only phone")
	}
	if !identity.Solo(rig.homeA) {
		t.Fatal("revoking a phone ended solo")
	}

	// 2. approvalsDoor.Pending + Approve of a phone on a solo home leaves solo
	// true and sets PhonesApproved true; reopened decided phone link in Pending
	// also leaves solo true and sets PhonesApproved true; Revoke clears it.
	token2, _, _, _, phonePub2 := testPhoneRequest(t, rig, "Pixel")
	phoneID2 := clientproto.DeviceIDOf(phonePub2)
	pending, err := door.Pending(ctx, token2)
	if err != nil {
		t.Fatalf("door.Pending phone2: %v", err)
	}
	if !pending.Client || pending.Device != phoneID2 {
		t.Fatalf("pending = %+v, want Client=true Device=%s", pending, phoneID2)
	}
	if err := door.Approve(ctx, pending); err != nil {
		t.Fatalf("door.Approve phone2: %v", err)
	}
	if !identity.Solo(rig.homeA) || !syncsetup.PhonesApproved(rig.homeA) {
		t.Fatalf("after door.Approve phone2: Solo=%v PhonesApproved=%v, want true/true",
			identity.Solo(rig.homeA), syncsetup.PhonesApproved(rig.homeA))
	}
	assertNoOtherComputer(t, rig.homeA, "after the approvals door approved a phone")

	if err := syncsetup.ForgetPhone(rig.homeA, phoneID2); err != nil {
		t.Fatal(err)
	}
	if _, err := door.Pending(ctx, token2); !errors.Is(err, tui3.ErrAlreadyPaired) {
		t.Fatalf("reopened phone2 Pending = %v, want ErrAlreadyPaired", err)
	}
	if !identity.Solo(rig.homeA) || !syncsetup.PhonesApproved(rig.homeA) {
		t.Fatalf("after reopened phone2 Pending: Solo=%v PhonesApproved=%v, want true/true",
			identity.Solo(rig.homeA), syncsetup.PhonesApproved(rig.homeA))
	}
	if err := door.Revoke(ctx, phoneID2); err != nil {
		t.Fatalf("door.Revoke phone2: %v", err)
	}
	if syncsetup.PhonesApproved(rig.homeA) || !identity.Solo(rig.homeA) {
		t.Fatalf("after door.Revoke phone2: Solo=%v PhonesApproved=%v, want true/false",
			identity.Solo(rig.homeA), syncsetup.PhonesApproved(rig.homeA))
	}

	// 3. app_pair approval of a phone on a solo home leaves solo true and sets
	// PhonesApproved true; reopened decided phone link in app_pair does the same.
	var lose atomic.Int32
	w := rig.appApprovingAt(t, &lose)
	token3, _, _, _, phonePub3 := testPhoneRequest(t, rig, "WorkPhone")
	phoneID3 := clientproto.DeviceIDOf(phonePub3)
	appApprove(t, w, token3, "done")
	if !identity.Solo(rig.homeA) || !syncsetup.PhonesApproved(rig.homeA) {
		t.Fatalf("after appApprove phone3: Solo=%v PhonesApproved=%v, want true/true",
			identity.Solo(rig.homeA), syncsetup.PhonesApproved(rig.homeA))
	}
	assertNoOtherComputer(t, rig.homeA, "after the app approved a phone")
	if err := syncsetup.ForgetPhone(rig.homeA, phoneID3); err != nil {
		t.Fatal(err)
	}
	op := machineCall[appwire.PairResult](t, w, "pair.start", appwire.PairStartParams{Mode: "approve_link", Link: token3}).Pairing
	got := e2PairWait(t, w, op.ID, "done")
	if !slices.Contains(got.Lines, tui3.ErrAlreadyPaired.Error()) {
		t.Fatalf("app reopened phone3 = %+v, want ErrAlreadyPaired line", got)
	}
	if !identity.Solo(rig.homeA) || !syncsetup.PhonesApproved(rig.homeA) {
		t.Fatalf("after app reopened phone3: Solo=%v PhonesApproved=%v, want true/true",
			identity.Solo(rig.homeA), syncsetup.PhonesApproved(rig.homeA))
	}
	if err := door.Revoke(ctx, phoneID3); err != nil {
		t.Fatalf("door.Revoke phone3: %v", err)
	}
	if syncsetup.PhonesApproved(rig.homeA) {
		t.Fatal("PhonesApproved still true after revoking phone3")
	}

	// 4. Approving a computer on the same solo home ends solo.
	screen, done := rig.asking(t)
	compLink := screen.waitFor(t, regexp.QuoteMeta(rig.url)+`/p/\S+#\S+`)[0]
	compPending, err := door.Pending(ctx, compLink)
	if err != nil {
		t.Fatalf("door.Pending computer: %v", err)
	}
	if err := door.Approve(ctx, compPending); err != nil {
		t.Fatalf("door.Approve computer: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("computer join: %v", err)
	}
	if identity.Solo(rig.homeA) {
		t.Fatal("approving a computer left homeA solo")
	}
	if syncsetup.PhonesApproved(rig.homeA) {
		t.Fatal("approving a computer marked PhonesApproved true")
	}

	// 5. Approving a phone on a non-solo home leaves it non-solo and sets PhonesApproved true.
	token4, _, check4, _, _ := testPhoneRequest(t, rig, "Tablet")
	if _, err := rig.approveWith(t, []string{"approve", token4, "--check", check4}, ""); err != nil {
		t.Fatalf("phone approve on non-solo home: %v", err)
	}
	if identity.Solo(rig.homeA) {
		t.Fatal("approving a phone on a non-solo home made it solo")
	}
	if !syncsetup.PhonesApproved(rig.homeA) {
		t.Fatal("PhonesApproved false after approving phone on non-solo home")
	}
}

// TestP13SoloComputerWithAPhoneListsIt verifies the person-visible half of P13: a
// computer that has never shared its identity is quiet toward every other computer,
// but once a phone is approved on it the person must be able to see that phone in
// /devices and in `codeaf devices`, and remove it. With no phone approved the same
// solo computer shows only itself.
func TestP13SoloComputerWithAPhoneListsIt(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	ctx := context.Background()

	rig := newPairRig(t)
	t.Setenv("CODEAF_HOME", rig.homeA)
	t.Setenv(syncsetup.URLVar, rig.url)
	makeHomeSolo(t, rig.homeA)
	door := rig.approvalsAt(rig.homeA)

	// No phone yet: the solo computer shows only itself and has no chats kind.
	rows, err := door.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].Self {
		t.Fatalf("solo computer with no phone listed %+v, want only itself", rows)
	}
	if _, ok := openChatsKind(); ok {
		t.Fatal("a solo computer with no phone offered the devices kind")
	}

	token, _, check, _, phonePub := testPhoneRequest(t, rig, "iPhone")
	phoneID := clientproto.DeviceIDOf(phonePub)
	if _, err := rig.approveWith(t, []string{"approve", token, "--check", check}, ""); err != nil {
		t.Fatalf("phone approve: %v", err)
	}
	if !identity.Solo(rig.homeA) {
		t.Fatal("approving the phone ended solo")
	}

	rows, err = door.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices on a solo computer with a phone: %v", err)
	}
	var sawPhone bool
	for _, row := range rows {
		if row.ID == phoneID {
			sawPhone = row.Client && !row.Revoked
		}
	}
	if !sawPhone {
		t.Fatalf("/devices on a solo computer with an approved phone listed %+v, want the phone", rows)
	}
	kind, ok := openChatsKind()
	if !ok {
		t.Fatal("`codeaf devices` offered no kind on a solo computer with an approved phone")
	}
	listing, err := kind.dir.List(ctx)
	if err != nil {
		t.Fatalf("the devices kind could not read its own list: %v", err)
	}
	if _, in := listing.Devices[phoneID]; !in {
		t.Fatalf("the devices kind's list = %+v, want the phone", listing.Devices)
	}

	// Removing it makes the solo computer show only itself again.
	if err := door.Revoke(ctx, phoneID); err != nil {
		t.Fatal(err)
	}
	if _, ok := openChatsKind(); ok {
		t.Fatal("after removing the last phone a solo computer still offered the devices kind")
	}
}

// TestP13WatchPhoneGateTellsTheHostWhenAPhoneIsApprovedElsewhere verifies that a host
// is told once each time the phone gate goes from closed to open, so a phone approved
// by another process (a terminal's `codeaf pair approve`) wakes a runner that had gone
// quiet, and that a gate that stays open or closes again tells it nothing.
func TestP13WatchPhoneGateTellsTheHostWhenAPhoneIsApprovedElsewhere(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	rig := newPairRig(t)
	t.Setenv("CODEAF_HOME", rig.homeA)
	t.Setenv(syncsetup.URLVar, rig.url)
	makeHomeSolo(t, rig.homeA)

	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time) // unbuffered: sending the next tick proves the last one was handled
	told := make(chan struct{}, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchPhoneGate(ctx, rig.homeA, ticks, func() { told <- struct{}{} })
	}()
	tick := func() { ticks <- time.Time{} }
	expect := func(want int, when string) {
		t.Helper()
		tick() // handled before this returns the next time one is sent; flush with a no-op tick
		tick()
		if got := len(told); got != want {
			t.Fatalf("%s: host was told %d times, want %d", when, got, want)
		}
	}

	expect(0, "no phone approved")
	if err := syncsetup.NotePhoneApproved(rig.homeA, "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	expect(1, "a phone was approved")
	expect(1, "the gate stayed open")
	if err := syncsetup.ForgetPhone(rig.homeA, "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	expect(1, "the last phone was removed")
	if err := syncsetup.NotePhoneApproved(rig.homeA, "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatal(err)
	}
	expect(2, "a new phone was approved after the gate had closed")
	cancel()
	<-done
}
