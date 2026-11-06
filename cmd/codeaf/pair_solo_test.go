package main

// A computer that shows a code stays solo, and so quiet, until a person
// approves the device that typed it and its identity is sent. A code that is
// taken back, runs out, is declined or is used up by a wrong code changes
// nothing, on every door that shows one.

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// soloRig is the terminal rig whose computer A, laptop-a, has only ever started:
// its identity is solo, as a first launch makes it.
func soloRig(t *testing.T) *pairRig {
	t.Helper()
	rig := newPairRig(t)
	rig.homeA = t.TempDir()
	if _, err := identity.EnsureSolo(rig.homeA); err != nil {
		t.Fatal(err)
	}
	if _, err := devname.Set(rig.homeA, "laptop-a"); err != nil {
		t.Fatal(err)
	}
	return rig
}

func wantSolo(t *testing.T, home string, solo bool, when string) {
	t.Helper()
	if got := identity.Solo(home); got != solo {
		t.Fatalf("%s: solo is %v, want %v", when, got, solo)
	}
}

// wantRecord is whether the rig's relay lists this computer's own device.
func wantRecord(t *testing.T, rig *pairRig, home string, listed bool, when string) {
	t.Helper()
	client, _, err := homeDirectory(home, pair.Mailbox{URL: rig.url})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	listing, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, got := listing.Devices[dev.ID()]; got != listed {
		t.Fatalf("%s: the relay lists this computer %v, want %v", when, got, listed)
	}
}

// The production grant keeps a fresh computer solo and carries the step that
// ends it; on a computer that already syncs the step changes nothing.
func TestPairGrantStaysSoloUntilItIsSent(t *testing.T) {
	t.Setenv(syncsetup.URLVar, "http://127.0.0.1:1")
	fresh := t.TempDir()
	g, err := pairGrantAt(fresh, pair.Mailbox{URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	wantSolo(t, fresh, true, "after the grant was made")
	if !syncsetup.Quiet(fresh) {
		t.Fatal("making a grant let this computer talk to sync")
	}
	if g.BeforeSend == nil {
		t.Fatal("the production grant carries no BeforeSend, so sending it would leave this computer solo")
	}
	if err := g.BeforeSend(); err != nil {
		t.Fatal(err)
	}
	wantSolo(t, fresh, false, "once the grant is sent")

	syncing := t.TempDir()
	if _, err := identity.Ensure(syncing); err != nil {
		t.Fatal(err)
	}
	g, err = pairGrantAt(syncing, pair.Mailbox{URL: "http://127.0.0.1:1"})
	if err != nil || g.BeforeSend() != nil {
		t.Fatalf("a syncing computer's grant: %v", err)
	}
	wantSolo(t, syncing, false, "on a computer that already synced")
}

// The terminal: Enter is no, a wrong code burns, ctrl+c takes the code back,
// and each leaves A solo, with no record of it at the relay; y ends it, and the
// record goes out before the answer, so the joining computer names A.
func TestPairShowingACodeStaysSoloUntilYes(t *testing.T) {
	t.Run("a no", func(t *testing.T) {
		rig := soloRig(t)
		shown := rig.show(t)
		code := shown.code(t)
		joined := make(chan error, 1)
		go func() { _, err := rig.joinWith(t, code, false); joined <- err }()
		shown.out.waitFor(t, `y / n `)
		if _, err := io.WriteString(shown.typed, "\n"); err != nil {
			t.Fatal(err)
		}
		if err := <-joined; !errors.Is(err, pair.ErrRefused) {
			t.Fatalf("the joining computer ended with %v, want a refusal", err)
		}
		if err := shown.end(t); !errors.Is(err, pair.ErrRefused) {
			t.Fatalf("the sharing computer ended with %v, want a refusal", err)
		}
		wantSolo(t, rig.homeA, true, "after a no")
		wantRecord(t, rig, rig.homeA, false, "after a no")
	})
	t.Run("a wrong code, then ctrl+c", func(t *testing.T) {
		rig := soloRig(t)
		shown := rig.show(t)
		code := shown.code(t)
		wrong := code[:len(code)-1] + string(rune('0'+(code[len(code)-1]-'0'+1)%10))
		if _, err := rig.joinWith(t, wrong, false); !errors.Is(err, pair.ErrCodeDidNotWork) {
			t.Fatalf("a wrong code ended with %v", err)
		}
		shown.out.waitFor(t, `(?s)`+pair.BurnLine+`.*codeaf pair \S+`)
		wantSolo(t, rig.homeA, true, "after a wrong code")
		shown.stop()
		if err := shown.end(t); err != nil {
			t.Fatalf("ctrl+c ended the sharing door with %v", err)
		}
		wantSolo(t, rig.homeA, true, "after the code was taken back")
		wantRecord(t, rig, rig.homeA, false, "after the code was taken back")
	})
	t.Run("a yes", func(t *testing.T) {
		rig := soloRig(t)
		shown := rig.show(t)
		code := shown.code(t)
		type joinEnd struct {
			out *pairScreenText
			err error
		}
		joined := make(chan joinEnd, 1)
		go func() { out, err := rig.joinWith(t, code, false); joined <- joinEnd{out, err} }()
		shown.out.waitFor(t, `y / n `)
		wantSolo(t, rig.homeA, true, "while a person decides")
		if _, err := io.WriteString(shown.typed, "y\n"); err != nil {
			t.Fatal(err)
		}
		end := <-joined
		if end.err != nil {
			t.Fatalf("the joining computer ended with %v", end.err)
		}
		if !strings.Contains(end.out.String(), "Paired with laptop-a") {
			t.Fatalf("the joining computer could not name A:\n%s", end.out.String())
		}
		if err := shown.end(t); err != nil {
			t.Fatalf("the sharing computer ended with %v", err)
		}
		wantSolo(t, rig.homeA, false, "after the yes")
		wantRecord(t, rig, rig.homeA, true, "after the yes")
	})
}

// chatShowing is the chat door's screen in a test: it hands over each code and
// answers the question as told.
type chatShowing struct {
	codes chan *pair.Code
	yes   bool
}

func (s chatShowing) Show(code *pair.Code, _ string)           { s.codes <- code }
func (chatShowing) Burned()                                    {}
func (s chatShowing) Ask(context.Context, string, string) bool { return s.yes }

// The chat's /pair: taking the code back leaves this computer solo, and a yes
// ends it.
func TestChatPairShowingACodeStaysSoloUntilYes(t *testing.T) {
	for _, yes := range []bool{false, true} {
		t.Run(map[bool]string{false: "taken back", true: "a yes"}[yes], func(t *testing.T) {
			rig := soloRig(t)
			t.Setenv("CODEAF_HOME", rig.homeA)
			t.Setenv("CODEAF_PROFILE_DIR", rig.homeA)
			t.Setenv(syncsetup.URLVar, rig.url)
			ctx, stop := context.WithTimeout(context.Background(), time.Minute)
			defer stop()
			screen := chatShowing{codes: make(chan *pair.Code, 4), yes: yes}
			ended := make(chan error, 1)
			go func() { _, err := (chatPairDoor{}).Offer(ctx, screen); ended <- err }()
			var code *pair.Code
			select {
			case code = <-screen.codes:
			case <-time.After(15 * time.Second):
				t.Fatal("/pair showed no code")
			}
			wantSolo(t, rig.homeA, true, "while the code is shown")
			if !yes {
				stop()
				if err := <-ended; err == nil {
					t.Fatal("a code taken back said it paired")
				}
				wantSolo(t, rig.homeA, true, "after the code was taken back")
				wantRecord(t, rig, rig.homeA, false, "after the code was taken back")
				return
			}
			route, err := pairMailboxAt(rig.homeA, "")
			if err != nil {
				t.Fatal(err)
			}
			joining := pair.Joining{Home: t.TempDir(), Label: "laptop"}
			if _, err := pair.Join(ctx, route, joining, code.Shown(), joinScreen{newTerminal(strings.NewReader(""), io.Discard)}); err != nil {
				t.Fatalf("the joining computer ended with %v", err)
			}
			if err := <-ended; err != nil {
				t.Fatalf("/pair ended with %v", err)
			}
			wantSolo(t, rig.homeA, false, "after the yes")
			wantRecord(t, rig, rig.homeA, true, "after the yes")
		})
	}
}

// ctrl+c after the answer went out keeps the not-confirmed sentence: the other
// computer may hold the chats, so the terminal never says nothing was paired.
func TestPairCtrlCAfterTheAnswerIsNotConfirmed(t *testing.T) {
	out := &pairScreenText{}
	d := pairDoor{term: newTerminal(strings.NewReader(""), out)}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	err := d.endedByPerson(ctx, pair.NotConfirmedBy("laptop"))
	if !errors.Is(err, pair.ErrNotConfirmed) || strings.Contains(out.String(), pair.CancelledLine) {
		t.Fatalf("ctrl+c after the answer ended with %v and printed %q", err, out.String())
	}
	if err := d.endedByPerson(ctx, context.Canceled); err != nil || !strings.Contains(out.String(), pair.CancelledLine) {
		t.Fatalf("ctrl+c before any answer ended with %v and printed %q", err, out.String())
	}
}
