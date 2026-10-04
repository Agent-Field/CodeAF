package main

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// asking runs `codeaf pair` on the new computer in the background.
func (r *pairRig) asking(t *testing.T) (*pairScreenText, chan error) {
	t.Helper()
	out := &pairScreenText{}
	done := make(chan error, 1)
	go func() { done <- r.door(r.homeB, strings.NewReader(""), out).run(context.Background(), nil) }()
	return out, done
}

// approveWith runs `codeaf pair approve <token>` on the first computer, with the
// person typing answer.
func (r *pairRig) approveWith(t *testing.T, args []string, answer string) (*pairScreenText, error) {
	t.Helper()
	out := &pairScreenText{}
	return out, r.door(r.homeA, strings.NewReader(answer), out).run(context.Background(), args)
}

// The headless path end to end: the new computer prints a link and a typed form,
// the first one is shown who asks and the check number and types y, and the new
// one says it is paired.
func TestPairByLinkEndToEnd(t *testing.T) {
	rig := newPairRig(t)
	screen, done := rig.asking(t)
	typed := screen.waitFor(t, `codeaf pair approve (\S+)`)[1]
	link := screen.waitFor(t, `https://codeaf\.agentfield\.ai/p/\S+#\S+`)[0]
	check := screen.waitFor(t, `Check number: (\d{4})`)[1]
	if !strings.Contains(link, typed[:8]) {
		t.Fatalf("the link %s and the typed form %s name different requests", link, typed)
	}

	out, err := rig.approveWith(t, []string{"approve", typed}, "y\n")
	if err != nil {
		t.Fatal(err)
	}
	// The asking computer names its own operating system, so on a Mac it says Mac.
	want := pair.WantsToJoinLine("laptop", runtime.GOOS) + "\n" + pair.CheckQuestion(check) + "  y / n "
	if !strings.HasPrefix(out.String(), want) || !strings.Contains(out.String(), pair.ApprovedLine("laptop")) {
		t.Fatalf("the approving terminal printed:\n%s\nwant it to start:\n%s", out.String(), want)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	screen.waitFor(t, `Paired\.`)
	first, _ := identity.Load(rig.homeA)
	second, err := identity.Load(rig.homeB)
	if err != nil || second.ID() != first.ID() {
		t.Fatalf("the new computer holds %v (%v), want the first one's identity", second.ID(), err)
	}
}

// A link pairing puts the APPROVER on the directory too, not only the joiner:
// the joiner's devices list shows the computer that approved it, and the
// approver is on its own list as well. `pair approve` used to write only the
// joiner's record, so the approving machine's `codeaf devices` stayed empty
// until its next chat start.
func TestDevicesAfterALinkPairingShowTheApprover(t *testing.T) {
	rig := newPairRig(t)
	t.Setenv("CODEAF_SYNC_URL", rig.url)
	if _, err := devname.Set(rig.homeA, "desk"); err != nil {
		t.Fatal(err)
	}
	if _, err := devname.Set(rig.homeB, "laptop"); err != nil {
		t.Fatal(err)
	}

	screen, done := rig.asking(t)
	link := screen.waitFor(t, `https://codeaf\.agentfield\.ai/p/\S+#\S+`)[0]
	out, err := rig.approveWith(t, []string{link}, "y\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), pair.ApprovedLine("laptop")) {
		t.Fatalf("the approving terminal printed:\n%s", out.String())
	}

	// The joiner's list shows the approver, not itself alone; the approver's
	// list shows both, and names the joiner it let in.
	for _, side := range []struct{ home, want, label string }{
		{rig.homeB, "desk", "the computer that joined"},
		{rig.homeA, "laptop", "the computer that approved"},
	} {
		t.Setenv("CODEAF_HOME", side.home)
		chats, ok := openChatsKind()
		if !ok {
			t.Fatalf("%s has no chats kind", side.label)
		}
		rows, err := chats.rows()
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(rows))
		for _, row := range rows {
			names = append(names, row.Name)
		}
		if len(rows) < 2 || !slices.Contains(names, side.want) {
			t.Fatalf("%s's devices list holds %v, want the computer it paired with on it", side.label, names)
		}
	}
}

// A link pasted without the word approve means the same, because its shape says so.
func TestPairLinkShapeApproves(t *testing.T) {
	rig := newPairRig(t)
	screen, done := rig.asking(t)
	link := screen.waitFor(t, `https://codeaf\.agentfield\.ai/p/\S+#\S+`)[0]
	if _, err := rig.approveWith(t, []string{link}, "y\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Anything but y turns the new computer down, and it is told so.
func TestPairApproveNoDeclines(t *testing.T) {
	rig := newPairRig(t)
	screen, done := rig.asking(t)
	typed := screen.waitFor(t, `codeaf pair approve (\S+)`)[1]
	out, err := rig.approveWith(t, []string{"approve", typed}, "\n")
	if err != nil || !strings.Contains(out.String(), pair.DeclinedLine) {
		t.Fatalf("a no ended with %v and printed %q", err, out.String())
	}
	if err := <-done; !errors.Is(err, pair.ErrLinkDeclined) {
		t.Fatalf("the new computer ended with %v, want the declined sentence", err)
	}
}

// A computer with no identity of its own has no say, and a typo is refused
// before any request is made.
func TestPairApproveRefusals(t *testing.T) {
	rig := newPairRig(t)
	out := &pairScreenText{}
	err := rig.door(rig.homeB, strings.NewReader("y\n"), out).run(context.Background(), []string{"approve", "k7m2q9xd.AAAAAAAAAAAAAAAAAAAAAA"})
	if !errors.Is(err, pair.ErrNotJoined) {
		t.Fatalf("an unpaired computer approving ended with %v", err)
	}
	err = rig.door(rig.homeA, strings.NewReader(""), out).run(context.Background(), []string{"approve", "nope"})
	if !errors.Is(err, pair.ErrLinkShape) {
		t.Fatalf("a typo ended with %v", err)
	}
}

// `codeaf pair approve` with nothing after it is a usage error that names the
// link, not a complaint about the shape of a six-digit code.
func TestPairApproveWithoutALinkSaysUsage(t *testing.T) {
	rig := newPairRig(t)
	errs := &bytes.Buffer{}
	previous := usageErr
	usageErr = errs
	defer func() { usageErr = previous }()
	err := rig.door(rig.homeA, strings.NewReader(""), &pairScreenText{}).run(context.Background(), []string{"approve"})
	if err != exitStatus(2) || !strings.Contains(errs.String(), "usage: codeaf pair approve <link-or-code>") || strings.Contains(errs.String(), "42-715-302") {
		t.Fatalf("a bare approve ended with %v and said %q, want its own usage line and exit 2", err, errs.String())
	}
}

// ctrl+c while the link waits ends the pairing and says that nothing was paired,
// so a person is not left looking at a silent prompt.
func TestPairCancelSaysNothingWasPaired(t *testing.T) {
	rig := newPairRig(t)
	out := &pairScreenText{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rig.door(rig.homeB, strings.NewReader(""), out).run(ctx, nil) }()
	out.waitFor(t, `ctrl\+c to cancel`)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("cancelling ended with %v, want a clean exit", err)
	}
	if !strings.Contains(out.String(), pair.CancelledLine) {
		t.Fatalf("cancelling printed %q, want %q", out.String(), pair.CancelledLine)
	}
}
