package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// TestC4SeatConstructionDoesNotWaitForTheRelay proves C4: the surface can draw
// its first frame while the first relay response is still held by the network.
func TestC4SeatConstructionDoesNotWaitForTheRelay(t *testing.T) {
	cfg, _ := doorChat(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	defer close(release)
	t.Setenv(syncsetup.URLVar, srv.URL)
	if _, err := identity.Ensure(home.Dir()); err != nil {
		t.Fatal(err)
	}
	built := make(chan session.Config, 1)
	go func() { built <- v3Seated(cfg) }()
	var seated session.Config
	select {
	case seated = <-built:
	case <-entered:
		select {
		case seated = <-built:
		case <-time.After(10 * time.Second):
			t.Fatal("first paint is blocked by an unfinished relay response during seat construction")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("seat construction did not reach the relay or return")
	}
	seated.Model = "test/model"
	seated.BaseURL, seated.APIKey = "http://127.0.0.1:1", "fixture-key"
	seated.Workspace = cfg.Place.Workspace
	agent, err := session.New(seated)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	sink := &firstFrameSink{painted: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ended := make(chan error, 1)
	go func() {
		ended <- tui3.Run(ctx, tui3.Options{Agent: agent, Workspace: cfg.Place.Workspace, Notice: "frame arrived before sync", Input: input, Output: sink, Width: 100, Height: 30})
	}()
	select {
	case <-sink.painted:
	case err := <-ended:
		t.Fatalf("surface ended before its first frame: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("no first frame while the relay response was held")
	}
	cancel()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("surface did not close")
	}
}

// TestC4AStartupFaultReleasesWaitingTools proves C4: background sync failing
// cannot leave the visible conversation waiting forever for its ownership read.
func TestC4AStartupFaultReleasesWaitingTools(t *testing.T) {
	d := newLiveDrive(func() (*syncsetup.Drive, error) { panic("startup fixture") })
	done := make(chan struct{})
	go func() { _ = d.Gate(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a sync startup fault left tool work waiting forever")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := d.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestC1ChatApprovalPublishesTheApproverBeforePaired proves C1: no prior chat
// or sync tick is needed for the joining device to list its approver.
func TestC1ChatApprovalPublishesTheApproverBeforePaired(t *testing.T) {
	rig := newPairRig(t)
	if _, err := devname.Set(rig.homeA, "host-a"); err != nil {
		t.Fatal(err)
	}
	screen, done := rig.asking(t)
	token := screen.waitFor(t, `codeaf pair approve (\S+)`)[1]
	door := rig.approvalsAt(rig.homeA)
	pending, err := door.Pending(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if err := door.Approve(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	rows, err := rig.approvalsAt(rig.homeB).Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if !row.Self && row.Name == "host-a" {
			return
		}
	}
	t.Fatalf("Paired. without the approving computer in the new device's list: %+v", rows)
}

// TestC3RemovedDeviceHasOneExplanation proves C3: a refused list must not also
// claim that this computer never paired.
func TestC3RemovedDeviceHasOneExplanation(t *testing.T) {
	rig := newPairRig(t)
	screen, done := rig.asking(t)
	token := screen.waitFor(t, `codeaf pair approve (\S+)`)[1]
	if _, err := rig.approveWith(t, []string{"approve", token}, "a\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	door := rig.approvalsAt(rig.homeA)
	rows, err := door.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if !row.Self {
			if err := door.Revoke(context.Background(), row.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	route, err := rig.door(rig.homeB, strings.NewReader(""), &pairScreenText{}).mailbox("")
	if err != nil {
		t.Fatal(err)
	}
	client, id, err := homeDirectory(rig.homeB, route)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(rig.homeB)
	if err != nil {
		t.Fatal(err)
	}
	kind := chatsKindOf(client, id.CellKey(), dev.ID(), rig.url)
	// The command returns the removal explanation instead of swallowing it and
	// allowing listDevices to print pair.NoDevices.
	if _, err := kind.table(time.Now()); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("removed device's list ended with %v; would print %q", err, pair.NoDevices)
	}
}

// firstFrameSink recognizes text drawn in the frame, rather than terminal
// setup escape sequences that can be emitted before any screen exists.
type firstFrameSink struct {
	mu      sync.Mutex
	text    strings.Builder
	painted chan struct{}
}

func (s *firstFrameSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.text.Write(p)
	if strings.Contains(s.text.String(), "frame arrived before sync") {
		select {
		case s.painted <- struct{}{}:
		default:
		}
	}
	return len(p), nil
}
