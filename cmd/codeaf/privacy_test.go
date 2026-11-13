package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// TestC8DeviceListsOnASoloHomeSendNothing proves C8 for both CLI and slash
// command doors; reading either device list must not start a fleet.
func TestC8DeviceListsOnASoloHomeSendNothing(t *testing.T) {
	celltest.Available(t)
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(syncsetup.URLVar, srv.URL)
	if _, ok, err := syncsetup.OpenFirst(home.Dir()); !ok || err != nil {
		t.Fatalf("first launch: %v, %v", ok, err)
	}
	if err := runDevices(nil); err != nil {
		t.Fatal(err)
	}
	if rows, err := newApprovalsDoor(home.Dir()).Devices(context.Background()); err != nil || len(rows) != 1 || !rows[0].Self || rows[0].Name != devname.Name(home.Dir()) {
		t.Fatalf("slash device list: %v, %v", rows, err)
	}
	if hits.Load() != 0 || !syncsetup.Quiet(home.Dir()) {
		t.Fatalf("reading devices sent %d requests or ended solo", hits.Load())
	}
}

// TestC8IdentityShowAndExportStaySolo proves C8 for identities made outside the
// home screen: inspecting or exporting a local identity does not offer pairing.
func TestC8IdentityShowAndExportStaySolo(t *testing.T) {
	for _, verb := range []string{"show", "export"} {
		t.Run(verb, func(t *testing.T) {
			d, _ := door(t, t.TempDir(), "fixture passphrase")
			args := []string{verb}
			if verb == "export" {
				args = append(args, filepath.Join(t.TempDir(), "identity.blob"))
			}
			if err := runIdentityAt(args, d); err != nil {
				t.Fatal(err)
			}
			if !identity.Solo(d.home) || syncsetup.MayTalk(d.home) {
				t.Fatal("a local identity operation enabled relay traffic")
			}
		})
	}
}

// TestC8FailedApprovalAndDeclineStaySolo proves C8: an attempted approval that
// fails before completion, and a subsequent decline, do not enable chat sync.
func TestC8FailedApprovalAndDeclineStaySolo(t *testing.T) {
	rig := newPairRig(t)
	rig.homeA = t.TempDir()
	if _, err := identity.EnsureSolo(rig.homeA); err != nil {
		t.Fatal(err)
	}
	screen, done := rig.asking(t)
	link := screen.waitFor(t, `codeaf pair approve (\S+)`)[1]
	door := rig.approvalsAt(rig.homeA)
	pending, err := door.Pending(context.Background(), link)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := door.Approve(ctx, pending); err == nil {
		t.Fatal("a cancelled approval succeeded")
	}
	if !identity.Solo(rig.homeA) {
		t.Fatal("a failed approval enabled chat sync")
	}
	if err := door.Deny(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("the declined computer joined")
	}
	if !identity.Solo(rig.homeA) {
		t.Fatal("declining a device enabled chat sync")
	}
}
