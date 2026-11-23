package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
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
	if _, err := devname.Set(door.dir, name); err != nil {
		t.Fatal(err)
	}
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
	if _, err := devname.Set(rig.homeB, "laptop"); err != nil {
		t.Fatal(err)
	}
	screen, done := rig.asking(t)
	link := screen.waitFor(t, regexp.QuoteMeta(rig.url)+`/p/\S+#\S+`)[0]
	door := rig.approvalsAt(rig.homeA)
	ctx := context.Background()
	rig.enrol(t, door, "this computer")

	p, err := door.Pending(ctx, link)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "laptop" || p.Platform != runtime.GOOS || len(p.Check) != 4 || p.Device == "" {
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
	link := screen.waitFor(t, regexp.QuoteMeta(rig.url)+`/p/\S+#\S+`)[0]
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

// A computer that another of the person's computers stopped is told so, in the
// one sentence every sync surface uses, by the screens that list devices.
func TestApprovalsDoorTellsAStoppedComputerItWasStopped(t *testing.T) {
	rig := newPairRig(t)
	screen, done := rig.asking(t)
	link := screen.waitFor(t, regexp.QuoteMeta(rig.url)+`/p/\S+#\S+`)[0]
	door := rig.approvalsAt(rig.homeA)
	ctx := context.Background()
	rig.enrol(t, door, "this computer")
	p, err := door.Pending(ctx, link)
	if err != nil {
		t.Fatal(err)
	}
	if err := door.Approve(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := door.Revoke(ctx, p.Device); err != nil {
		t.Fatal(err)
	}

	stopped := rig.approvalsAt(rig.homeB)
	if _, err := stopped.Devices(ctx); err == nil || err.Error() != chatlist.RemovedChat {
		t.Fatalf("the stopped computer's device list said %v, want %q", err, chatlist.RemovedChat)
	}
	if err := stopped.Revoke(ctx, p.Device); err == nil || err.Error() != chatlist.RemovedChat {
		t.Fatalf("the stopped computer's revoke said %v, want %q", err, chatlist.RemovedChat)
	}
}

// lostReply is a slow relay seen from the approving computer: with decide it
// lets the device in but the reply never comes back; without, the approval
// never reaches it at all.
type lostReply struct {
	directory.Client
	decide bool
}

func (l lostReply) ApproveRequest(ctx context.Context, code string, a directory.Approval) error {
	if l.decide {
		if err := l.Client.ApproveRequest(ctx, code, a); err != nil {
			return err
		}
	}
	return context.DeadlineExceeded
}

// withLostReply points the door's approver at a relay that loses replies.
func withLostReply(door *approvalsDoor, home string, decide bool) {
	door.approver = func(route pair.Mailbox) (pair.Approver, error) {
		who, err := linkApprover(home)(route)
		who.Dir = lostReply{who.Dir, decide}
		return who, err
	}
}

// soloAsking starts the new device asking and returns its link, with the first
// computer enrolled and solo.
func (r *pairRig) soloAsking(t *testing.T) (string, chan error) {
	t.Helper()
	screen, done := r.asking(t)
	link := screen.waitFor(t, regexp.QuoteMeta(r.url)+`/p/\S+#\S+`)[0]
	r.enrol(t, r.approvalsAt(r.homeA), "this computer")
	// A first computer that has never shared its identity is solo.
	if err := os.WriteFile(filepath.Join(r.homeA, "identity-solo"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return link, done
}

// approvedEarlier approves link from the first computer without ending solo, as
// an earlier attempt that never heard back did.
func (r *pairRig) approvedEarlier(t *testing.T, link string, done chan error) {
	t.Helper()
	earlier := r.approvalsAt(r.homeA)
	p, err := earlier.Pending(context.Background(), link)
	if err != nil {
		t.Fatal(err)
	}
	as, _ := earlier.recall(p)
	who, err := earlier.who()
	if err != nil {
		t.Fatal(err)
	}
	if err := who.Approve(context.Background(), as); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the new device did not join: %v", err)
	}
	if !identity.Solo(r.homeA) {
		t.Fatal("setup: the earlier attempt should have left the marker")
	}
}

// pendingAt opens the new device's link on the first computer.
func (r *pairRig) pendingAt(t *testing.T, door *approvalsDoor) (string, tui3.PendingDevice, chan error) {
	t.Helper()
	link, done := r.soloAsking(t)
	p, err := door.Pending(context.Background(), link)
	if err != nil {
		t.Fatal(err)
	}
	return link, p, done
}

// T1: the relay lets the device in but the reply is lost. The door looks the
// request up, so the approval counts, the first computer stops being solo and
// both computers end up paired.
func TestApprovalsDoorApprovalWhoseReplyWasLostStillPairs(t *testing.T) {
	rig := newPairRig(t)
	door := rig.approvalsAt(rig.homeA)
	_, p, done := rig.pendingAt(t, door)
	if !identity.Solo(rig.homeA) {
		t.Fatal("the first computer did not start solo")
	}
	withLostReply(door, rig.homeA, true)
	if err := door.Approve(context.Background(), p); err != nil {
		t.Fatalf("approve = %v, want the lost reply settled as approved", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the new device did not join: %v", err)
	}
	if identity.Solo(rig.homeA) {
		t.Fatal("the first computer stays solo, so it never shares")
	}
	rows, err := door.Devices(context.Background())
	if err != nil || len(rows) != 2 {
		t.Fatalf("devices = %+v, %v; want both computers", rows, err)
	}
}

// An approval that never reached the relay is said as a plain retry, and the
// retry lets the device in.
func TestApprovalsDoorApprovalThatNeverArrivedCanBeRetried(t *testing.T) {
	rig := newPairRig(t)
	door := rig.approvalsAt(rig.homeA)
	_, p, done := rig.pendingAt(t, door)
	withLostReply(door, rig.homeA, false)
	if err := door.Approve(context.Background(), p); !errors.Is(err, tui3.ErrAnswerNotArrived) {
		t.Fatalf("approve = %v, want ErrAnswerNotArrived", err)
	}
	if !identity.Solo(rig.homeA) {
		t.Fatal("nothing was approved, yet the first computer stopped being solo")
	}
	door.approver = linkApprover(rig.homeA)
	if err := door.Approve(context.Background(), p); err != nil {
		t.Fatalf("retry = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the new device did not join: %v", err)
	}
	if identity.Solo(rig.homeA) {
		t.Fatal("the first computer stays solo after the retry")
	}
}

// T3: an approval landed in an earlier attempt that never heard back (here, a
// screen that closed). Opening the same link again says the device is already
// paired and ends solo, instead of asking to approve again.
func TestApprovalsDoorReopenedLinkOfALostApprovalEndsSolo(t *testing.T) {
	rig := newPairRig(t)
	door := rig.approvalsAt(rig.homeA)
	link, p, done := rig.pendingAt(t, door)
	rig.approvedEarlier(t, link, done)
	if _, err := door.Pending(context.Background(), link); !errors.Is(err, tui3.ErrAlreadyPaired) {
		t.Fatalf("reopened link = %v, want ErrAlreadyPaired", err)
	}
	if identity.Solo(rig.homeA) {
		t.Fatal("the first computer stays solo after the reopened link")
	}
	// A retried answer on the old card settles the same way.
	if err := door.Approve(context.Background(), p); err != nil {
		t.Fatalf("retried approve = %v, want it settled as approved", err)
	}
}

// approveLosingReply runs `codeaf pair approve <link>` on the first computer,
// typing a, through a relay that loses its reply.
func (r *pairRig) approveLosingReply(link string, decide bool) (*pairScreenText, error) {
	out := &pairScreenText{}
	d := r.door(r.homeA, strings.NewReader("a\n"), out)
	d.approver = func(route pair.Mailbox) (pair.Approver, error) {
		who, err := linkApprover(r.homeA)(route)
		who.Dir = lostReply{who.Dir, decide}
		return who, err
	}
	return out, d.run(context.Background(), []string{link})
}

// T1 for `codeaf pair approve`: the lost reply is looked up, so the approval
// counts and the first computer stops being solo.
func TestPairApproveWhoseReplyWasLostStillPairs(t *testing.T) {
	rig := newPairRig(t)
	link, done := rig.soloAsking(t)
	out, err := rig.approveLosingReply(link, true)
	if err != nil {
		t.Fatalf("approve = %v, want the lost reply settled as approved", err)
	}
	if !strings.Contains(out.String(), pair.ApprovedLine("laptop")) {
		t.Fatalf("the approving terminal printed:\n%s", out.String())
	}
	if err := <-done; err != nil {
		t.Fatalf("the new device did not join: %v", err)
	}
	if identity.Solo(rig.homeA) {
		t.Fatal("the first computer stays solo, so it never shares")
	}
}

// `codeaf pair approve` whose approval never reached the relay says to run it
// again, and running it again lets the device in.
func TestPairApproveThatNeverArrivedCanBeRetried(t *testing.T) {
	rig := newPairRig(t)
	link, done := rig.soloAsking(t)
	if _, err := rig.approveLosingReply(link, false); err == nil || err.Error() != pairAnswerNotArrived {
		t.Fatalf("approve = %v, want %q", err, pairAnswerNotArrived)
	}
	if !identity.Solo(rig.homeA) {
		t.Fatal("nothing was approved, yet the first computer stopped being solo")
	}
	if _, err := rig.approveWith(t, []string{link}, "a\n"); err != nil {
		t.Fatalf("retry = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the new device did not join: %v", err)
	}
	if identity.Solo(rig.homeA) {
		t.Fatal("the first computer stays solo after the retry")
	}
}

// T3 for `codeaf pair approve`: the same link again, after an approval that
// never heard back, says the device is already paired and ends solo.
func TestPairApproveReopenedLinkOfALostApprovalEndsSolo(t *testing.T) {
	rig := newPairRig(t)
	link, done := rig.soloAsking(t)
	rig.approvedEarlier(t, link, done)
	out, err := rig.approveWith(t, []string{link}, "")
	if err != nil || !strings.Contains(out.String(), tui3.ErrAlreadyPaired.Error()) {
		t.Fatalf("reopened link = %v, printed:\n%s\nwant it to say already paired", err, out.String())
	}
	if identity.Solo(rig.homeA) {
		t.Fatal("the first computer stays solo after the reopened link")
	}
}

// appApprovingAt is the app on the first computer, with its link approver
// pointed at a relay that loses replies while lose is set.
func (r *pairRig) appApprovingAt(t *testing.T, lose *atomic.Int32) *actionWire {
	t.Helper()
	t.Setenv(syncsetup.URLVar, r.url)
	old := appLinkApprover
	appLinkApprover = func(dir string) func(pair.Mailbox) (pair.Approver, error) {
		return func(route pair.Mailbox) (pair.Approver, error) {
			who, err := old(dir)(route)
			if n := lose.Load(); n != 0 {
				who.Dir = lostReply{who.Dir, n > 0}
			}
			return who, err
		}
	}
	t.Cleanup(func() { appLinkApprover = old })
	_, _, w := appDeviceBackend(t, r.homeA)
	return w
}

// appApprove opens link in the app's Pair a computer and answers yes.
func appApprove(t *testing.T, w *actionWire, link, state string) appwire.Pairing {
	t.Helper()
	op := machineCall[appwire.PairResult](t, w, "pair.start", appwire.PairStartParams{Mode: "approve_link", Link: link}).Pairing
	q := e2PairWait(t, w, op.ID, "question")
	machineCall[appwire.PairResult](t, w, "pair.answer", appwire.PairAnswerParams{ID: op.ID, Question: q.Question.ID, Option: "y"})
	return e2PairWait(t, w, op.ID, state)
}

const (
	appLoseNothing = 0
	appLoseReply   = 1  // the relay decides but the reply is lost
	appLoseAnswer  = -1 // the approval never reaches the relay
)

// T1 for the app: the lost reply is looked up, so the pairing is done, not
// failed, and the first computer stops being solo.
func TestAppApprovalWhoseReplyWasLostStillPairs(t *testing.T) {
	rig := newPairRig(t)
	link, done := rig.soloAsking(t)
	var lose atomic.Int32
	lose.Store(appLoseReply)
	w := rig.appApprovingAt(t, &lose)
	appApprove(t, w, link, "done")
	if err := <-done; err != nil {
		t.Fatalf("the new device did not join: %v", err)
	}
	if identity.Solo(rig.homeA) {
		t.Fatal("the first computer stays solo, so it never shares")
	}
}

// An app approval that never reached the relay fails as a plain retry, and the
// retry lets the device in.
func TestAppApprovalThatNeverArrivedCanBeRetried(t *testing.T) {
	rig := newPairRig(t)
	link, done := rig.soloAsking(t)
	var lose atomic.Int32
	lose.Store(appLoseAnswer)
	w := rig.appApprovingAt(t, &lose)
	if got := appApprove(t, w, link, "failed"); got.Error == nil || got.Error.Message != appPairFailed {
		t.Fatalf("approve = %+v, want %q", got.Error, appPairFailed)
	}
	if !identity.Solo(rig.homeA) {
		t.Fatal("nothing was approved, yet the first computer stopped being solo")
	}
	lose.Store(appLoseNothing)
	appApprove(t, w, link, "done")
	if err := <-done; err != nil {
		t.Fatalf("the new device did not join: %v", err)
	}
	if identity.Solo(rig.homeA) {
		t.Fatal("the first computer stays solo after the retry")
	}
}

// T3 for the app: opening the link again, after an approval that never heard
// back, is done with already paired and ends solo, instead of failing as
// already answered.
func TestAppReopenedLinkOfALostApprovalEndsSolo(t *testing.T) {
	rig := newPairRig(t)
	link, done := rig.soloAsking(t)
	rig.approvedEarlier(t, link, done)
	var lose atomic.Int32
	w := rig.appApprovingAt(t, &lose)
	op := machineCall[appwire.PairResult](t, w, "pair.start", appwire.PairStartParams{Mode: "approve_link", Link: link}).Pairing
	got := e2PairWait(t, w, op.ID, "done")
	if !slices.Contains(got.Lines, tui3.ErrAlreadyPaired.Error()) {
		t.Fatalf("reopened link = %+v, want it to say already paired", got)
	}
	if identity.Solo(rig.homeA) {
		t.Fatal("the first computer stays solo after the reopened link")
	}
}
