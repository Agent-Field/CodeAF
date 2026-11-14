package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestIdleRelease_R5_1_AppSettingsRetakeOrKeepRequestedChoice(t *testing.T) {
	f := newIdleAppContract(t)
	first := f.released(t)
	if _, err := f.b.SetEffort(f.agent.ID, "high"); err != nil {
		t.Fatal(err)
	}
	second := f.released(t)
	if second.Cell.Lease.Fence <= first.Cell.Lease.Fence {
		t.Fatal("app setting did not retake ownership")
	}
	peer := appDeviceSync(t, f.peerHome)
	engine := cellstore.EngineFor("")
	engine.DataRoot = filepath.Join(peer.Home, "v3", "stores")
	got, err := peer.Continuer(engine, syncsetup.TakeOptions{RootFor: func(id string) string { return takeRootForAt(peer.Home, id) }}).OpenAvailable(t.Context(), f.agent.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := session.LoadMeta(got.Taken.Cell.Root)
	if err != nil || m.Effort != "high" {
		t.Fatalf("receiver effort=%q err=%v", m.Effort, err)
	}
	for _, change := range []func() error{
		func() error { _, err := f.b.SetEffort(f.agent.ID, "low"); return err },
		func() error { _, err := f.b.SetModel(f.agent.ID, "stub/other"); return err },
	} {
		var wire *appwire.Error
		if err := change(); !errors.As(err, &wire) {
			t.Fatalf("taken setting error=%v", err)
		}
		raw, _ := json.Marshal(wire.Detail)
		var moved appwire.HeldElsewhere
		if err := json.Unmarshal(raw, &moved); err != nil || moved.Kind != "held_elsewhere" || moved.Holder == nil || moved.Holder.ID != peer.Device.ID() {
			t.Fatalf("held-elsewhere detail=%s err=%v", raw, err)
		}
	}
	m, err = session.LoadMeta(session.DirOf(appTranscriptOf(f.b, f.agent.ID)))
	if err != nil || m.Effort != "high" {
		t.Fatalf("refused app setting saved effort=%q err=%v", m.Effort, err)
	}
}
