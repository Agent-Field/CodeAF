package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

func framePictureBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// frameBackend records a real-looking mp4 under the agent's workspace.
func frameBackend(t *testing.T) (*appBackend, appwire.Agent, string, string, string) {
	t.Helper()
	b, _, agent := workBackend(t, &fakeConversation{})
	a := b.agent(agent.ID)
	b.locked(func() { a.transcript = filepath.Join(t.TempDir(), "0123456789abcdef", "transcript.jsonl") })
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	id, _, err := b.mediaScope(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agent.Workspace, "hero.mp4")
	clip := append([]byte{0, 0, 0, 0x18}, []byte("ftypisom\x00\x00\x02\x00isomiso2")...)
	if err := os.WriteFile(path, clip, 0o600); err != nil {
		t.Fatal(err)
	}
	source := session.RecordMedia(index, session.Artifact{Path: path, Session: id, Title: "hero", Kind: "video", Created: time.Now()}, session.GeneratedMedia(path, clip, ""))
	got, err := b.GetMedia(agent.ID, source)
	if err != nil {
		t.Fatal(err)
	}
	return b, agent, source, got.Version, path
}

func TestMediaFrameLimitsAgree(t *testing.T) {
	if appwire.MediaFrameMaxBytes != session.MaxFrameBytes {
		t.Fatalf("wire %d and session %d disagree", appwire.MediaFrameMaxBytes, session.MaxFrameBytes)
	}
}

func TestAppMediaAttachFrameCarriesClipMomentAndKeepsTheClip(t *testing.T) {
	b, agent, source, version, clip := frameBackend(t)
	data := framePictureBytes(t)
	chip, err := b.AttachMediaFrame(agent.ID, source, version, 3200, data)
	if err != nil {
		t.Fatal(err)
	}
	ref := chip.Media.Source
	if ref.SourceID != source || ref.AtMS == nil || *ref.AtMS != 3200 || ref.Path != clip || chip.Title != "hero.frame-3200.png" {
		t.Fatalf("provenance: %+v %+v", chip, ref)
	}
	saved, err := os.ReadFile(chip.Path)
	if err != nil || !bytes.Equal(saved, data) {
		t.Fatal("snapshot bytes differ")
	}
	a := b.agent(agent.ID)
	var attach appAttach
	b.locked(func() { attach = a.attach[chip.ID] })
	composed, err := appComposedOf(agent.Workspace, []appAttach{attach}, nil)
	if err != nil || composed == nil || len(composed.media) != 1 || composed.media[0].Source.AtMS == nil || *composed.media[0].Source.AtMS != 3200 || composed.media[0].ExpectedSHA256 == "" {
		t.Fatalf("a plain send lost the frame's provenance: %+v %v", composed, err)
	}
	if err := b.RemoveAttachment(agent.ID, chip.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(chip.Path); !os.IsNotExist(err) {
		t.Fatal("an unsent frame leaked")
	}
	if _, err := os.Stat(clip); err != nil {
		t.Fatal("the clip was removed")
	}
	b.locked(func() {
		if b.referenceN != 0 || a.referenceN != 0 || b.referenceWorkers != 0 {
			t.Fatal("admission leaked")
		}
	})
}

func TestAppMediaAttachFrameRefusesForeignChangedAndBadBytes(t *testing.T) {
	b, agent, source, version, clip := frameBackend(t)
	data := framePictureBytes(t)
	if _, err := b.AttachMediaFrame(agent.ID, source, "sha256:stale", 1, data); err == nil {
		t.Fatal("a stale version attached")
	}
	if _, err := b.AttachMediaFrame(agent.ID, "not-a-source", version, 1, data); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("not found: %v", err)
	}
	if _, err := b.AttachMediaFrame(agent.ID, source, version, 1, []byte("not a png")); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("bad bytes: %v", err)
	}
	// An image source is not a clip: its frame is refused, not stored.
	picture := filepath.Join(agent.Workspace, "still.png")
	if err := os.WriteFile(picture, data, 0o600); err != nil {
		t.Fatal(err)
	}
	id, _, _ := b.mediaScope(agent.ID)
	still := session.RecordMedia(appMediaIndex(), session.Artifact{Path: picture, Session: id, Title: "still", Kind: "image", Created: time.Now()}, session.GeneratedMedia(picture, data, ""))
	got, err := b.GetMedia(agent.ID, still)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AttachMediaFrame(agent.ID, still, got.Version, 1, data); err == nil {
		t.Fatal("a still image accepted as a clip")
	}
	if err := os.WriteFile(clip, []byte("the clip was replaced by something longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AttachMediaFrame(agent.ID, source, version, 1, data); err == nil {
		t.Fatal("a changed clip attached")
	}
	a := b.agent(agent.ID)
	b.locked(func() {
		if len(a.attach) != 0 || b.referenceN != 0 || a.referenceN != 0 || b.referenceWorkers != 0 {
			t.Fatal("a refusal left admission or an attachment behind")
		}
	})
	for _, kind := range []string{"agent", "global", "workers"} {
		b.locked(func() {
			switch kind {
			case "agent":
				b.agents[agent.ID].referenceN = appwire.MaxAttachments
			case "global":
				b.referenceN = appReferenceMax
			case "workers":
				b.referenceWorkers = appReferenceWorkers
			}
		})
		if err := os.WriteFile(clip, append([]byte{0, 0, 0, 0x18}, []byte("ftypisom\x00\x00\x02\x00isomiso2")...), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := b.AttachMediaFrame(agent.ID, source, version, 1, data); wireCode(err) != appwire.CodeBusy {
			t.Fatalf("%s not bounded: %v", kind, err)
		}
		b.locked(func() { b.agents[agent.ID].referenceN = 0; b.referenceN = 0; b.referenceWorkers = 0 })
	}
}

func TestAppMediaAttachFrameUncertainDeliveryKeepsTheFrameAndCloseDropsUnsent(t *testing.T) {
	b, agent, source, version, _ := frameBackend(t)
	data := framePictureBytes(t)
	uncertain, err := b.AttachMediaFrame(agent.ID, source, version, 100, data)
	if err != nil {
		t.Fatal(err)
	}
	a := b.agent(agent.ID)
	b.locked(func() { a.attach[uncertain.ID].reference.attempted = true })
	if err := b.RemoveAttachment(agent.ID, uncertain.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(uncertain.Path); err != nil {
		t.Fatal("an unknown delivery lost its frame")
	}
	unsent, err := b.AttachMediaFrame(agent.ID, source, version, 200, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unsent.Path); !os.IsNotExist(err) {
		t.Fatal("close retained a definitely unsent frame")
	}
	if _, err := os.Stat(uncertain.Path); err != nil {
		t.Fatal("close removed an unknown-delivery frame")
	}
}
