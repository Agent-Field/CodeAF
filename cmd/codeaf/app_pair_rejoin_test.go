package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// B1: a computer removed from your devices pairs back from the app, by code and
// by link, with no --replace to know about. It keeps the identity and gets a new
// device key; one that was never removed is told nothing changed, not "Paired".
func TestB1AppRePairsARemovedComputer(t *testing.T) {
	for _, tc := range []struct {
		name    string
		link    bool
		removed bool
	}{{"code", false, true}, {"link", true, true}, {"code, not removed", false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			celltest.Available(t)
			relay := e2Relay(t)
			t.Setenv(syncsetup.URLVar, relay.URL)
			dirA, dirB := t.TempDir(), t.TempDir()
			id, err := identity.Ensure(dirA)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := identity.Adopt(dirB, id, false); err != nil {
				t.Fatal(err)
			}
			a, old := appDeviceSync(t, dirA), appDeviceSync(t, dirB)
			for side, s := range map[string]*syncsetup.Sync{"A": a, "B": old} {
				if err := s.Rename(context.Background(), side); err != nil {
					t.Fatal(err)
				}
			}
			if tc.removed {
				if err := a.Dir.Revoke(context.Background(), old.Device.ID()); err != nil {
					t.Fatal(err)
				}
				if _, err := old.Dir.List(context.Background()); !errors.Is(err, directory.ErrRevoked) {
					t.Fatalf("the removed computer must be refused before re-pairing: %v", err)
				}
			}
			kept := filepath.Join(dirB, "v3", "projects", "local", "saved", "meta.json")
			if err := os.MkdirAll(filepath.Dir(kept), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(kept, []byte(`{"id":"saved","title":"keep me"}`), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, wa := appDeviceBackend(t, dirA)
			_, _, wb := appDeviceBackend(t, dirB)

			var joinID string
			if tc.link {
				request := machineCall[appwire.PairResult](t, wb, "pair.start", appwire.PairStartParams{Mode: "request_link", Name: "B"}).Pairing
				waiting := e2PairWait(t, wb, request.ID, "waiting")
				approve := machineCall[appwire.PairResult](t, wa, "pair.start", appwire.PairStartParams{Mode: "approve_link", Link: waiting.Link}).Pairing
				q := e2PairWait(t, wa, approve.ID, "question")
				machineCall[appwire.PairResult](t, wa, "pair.answer", appwire.PairAnswerParams{ID: approve.ID, Question: q.Question.ID, Option: "y"})
				e2PairWait(t, wa, approve.ID, "done")
				joinID = request.ID
			} else {
				offer := machineCall[appwire.PairResult](t, wa, "pair.start", appwire.PairStartParams{Mode: "offer_code", Name: "A"}).Pairing
				waiting := e2PairWait(t, wa, offer.ID, "waiting")
				joinID = machineCall[appwire.PairResult](t, wb, "pair.start", appwire.PairStartParams{Mode: "use_code", Code: waiting.Code, Name: "B"}).Pairing.ID
				q := e2PairWait(t, wa, offer.ID, "question")
				machineCall[appwire.PairResult](t, wa, "pair.answer", appwire.PairAnswerParams{ID: offer.ID, Question: q.Question.ID, Option: "y"})
				e2PairWait(t, wa, offer.ID, "done")
			}
			done := e2PairWait(t, wb, joinID, "done")
			if raw, err := os.ReadFile(kept); err != nil || !strings.Contains(string(raw), "keep me") {
				t.Fatalf("re-pairing lost a local chat: %s %v", raw, err)
			}
			fresh := appDeviceSync(t, dirB)
			if fresh.Identity.ID() != id.ID() {
				t.Fatalf("re-pairing changed the identity: %s, want %s", fresh.Identity.ID(), id.ID())
			}

			if !tc.removed {
				if fresh.Device.ID() != old.Device.ID() || done.Restart || !slices.Contains(done.Lines, appPairAlready) {
					t.Fatalf("an unremoved computer must be told nothing changed: %+v", done)
				}
				return
			}
			if fresh.Device.ID() == old.Device.ID() {
				t.Fatal("the removed computer kept its refused device key")
			}
			if slices.Contains(done.Lines, appPairAlready) || !done.Restart {
				t.Fatalf("the removed computer was not paired back: %+v", done)
			}
			listing, err := fresh.Dir.List(context.Background())
			if err != nil {
				t.Fatalf("the re-paired computer is still refused: %v", err)
			}
			if dev, ok := listing.Devices[fresh.Device.ID()]; !ok || dev.Revoked {
				t.Fatalf("the new device is not in your devices: %+v", listing.Devices)
			}
			if !listing.Devices[old.Device.ID()].Revoked {
				t.Fatal("the old device key must stay removed")
			}
		})
	}
}
