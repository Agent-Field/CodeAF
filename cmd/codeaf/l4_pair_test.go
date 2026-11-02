package main

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/devname"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// L4-H2c: already paired is the first explanation, even when --replace was supplied.
func TestL4AlreadyPairedNamesDevices(t *testing.T) {
	r := newPairRig(t)
	id, err := identity.Load(r.homeA)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"laptop-a", "box-c"} {
		peerHome := t.TempDir()
		if _, err := identity.Adopt(peerHome, id, true); err != nil {
			t.Fatal(err)
		}
		if _, err := devname.Set(peerHome, name); err != nil {
			t.Fatal(err)
		}
		if err := ownDeviceRecord(context.Background(), peerHome, pair.Mailbox{URL: r.url}); err != nil {
			t.Fatal(err)
		}
	}
	d := r.door(r.homeA, strings.NewReader(""), &pairScreenText{})
	for _, replace := range []bool{false, true} {
		err := d.pairingStatus(context.Background(), pair.Mailbox{URL: r.url}, replace)
		if err == nil || err.Error() != "this computer is already paired with box-c and laptop-a — codeaf devices lists them" {
			t.Fatalf("%v", err)
		}
	}
}

// L4-H1b, L4-H1c: neither approval road reads a mismatched link from any service.
func TestL4ApprovalMismatchMakesNoRequest(t *testing.T) {
	for _, tc := range []struct{ own, link string }{
		{"http://127.0.0.1:8901", "https://codeaf.agentfield.ai/p/k7m2q9xd"},
		{"https://codeaf.agentfield.ai/fabric", "http://127.0.0.1:8901/p/k7m2q9xd"},
	} {
		mailbox := func(string) (pair.Mailbox, error) { return pair.Mailbox{URL: tc.own}, nil }
		called := false
		approver := func(pair.Mailbox) (pair.Approver, error) { called = true; return pair.Approver{}, nil }
		d := pairDoor{mailbox: mailbox, approver: approver}
		if err := d.approve(context.Background(), "", tc.link); err == nil || strings.Contains(err.Error(), "not waiting") {
			t.Fatal(err)
		}
		ui := &approvalsDoor{mailbox: mailbox, approver: approver}
		if _, err := ui.Pending(context.Background(), tc.link); err == nil || strings.Contains(err.Error(), "not waiting") {
			t.Fatal(err)
		}
		if called {
			t.Fatal("a mismatched approval reached the approver")
		}
	}
}

// L4-H2a, L4-M12a, L4-M12b: a looked-around computer pairs through both real doors.
func TestL4PairEmptyChatThroughBothDoors(t *testing.T) {
	for _, code := range []bool{false, true} {
		t.Run(map[bool]string{false: "link", true: "code"}[code], func(t *testing.T) {
			r := newPairRig(t)
			t.Setenv("CODEAF_HOME", r.homeB)
			if _, err := devname.Set(r.homeA, "laptop-a"); err != nil {
				t.Fatal(err)
			}
			if _, err := identity.EnsureSolo(r.homeB); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(r.homeB, "v3/projects/project/chat/.cell")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte("{\"type\":\"session\",\"id\":\"chat\"}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if !code {
				screen, done := r.asking(t)
				token := screen.waitFor(t, `codeaf pair approve (\S+)`)[1]
				if _, err := r.approveWith(t, []string{"approve", token}, "a\n"); err != nil {
					t.Fatal(err)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(screen.String(), pair.PairedWithLine("laptop-a")) {
					t.Fatal(screen.String())
				}
				return
			}
			offer := r.show(t)
			typed := offer.code(t)
			out := &pairScreenText{}
			done := make(chan error, 1)
			go func() { done <- r.door(r.homeB, strings.NewReader(""), out).run(context.Background(), []string{typed}) }()
			offer.out.waitFor(t, `y / n`)
			if _, err := io.WriteString(offer.typed, "y\n"); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), pair.PairedWithLine("laptop-a")) {
				t.Fatal(out.String())
			}
		})
	}
}
