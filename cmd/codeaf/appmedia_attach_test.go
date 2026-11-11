package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

func referenceBackend(t *testing.T) (*appBackend, appwire.Agent, string, string, []byte) {
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
	path := filepath.Join(agent.Workspace, "original.png")
	data := writePNG(t, path, 2, 2)
	source := session.RecordMedia(index, session.Artifact{Path: path, Session: id, Title: "original", Kind: "image", Created: time.Now()}, session.GeneratedMedia(path, data, ""))
	got, err := b.GetMedia(agent.ID, source)
	if err != nil {
		t.Fatal(err)
	}
	return b, agent, source, got.Version, data
}
func TestAppMediaAttachIsImageWithAuthoritativeProvenance(t *testing.T) {
	b, agent, source, version, data := referenceBackend(t)
	chip, err := b.AttachMedia(agent.ID, source, version)
	if err != nil {
		t.Fatal(err)
	}
	if chip.Title != "original.png" || chip.ID == "" || chip.Media.Source.SourceID != source {
		t.Fatalf("wrong chip: %+v", chip)
	}
	a := b.agent(agent.ID)
	var attach appAttach
	b.locked(func() { attach = a.attach[chip.ID] })
	composed, err := appComposedOf(agent.Workspace, []appAttach{attach}, nil)
	if err != nil || composed == nil || len(composed.media) != 1 || composed.media[0].Source.SourceID != source || composed.media[0].ExpectedSHA256 == "" {
		t.Fatalf("plain send lost reference: %+v %v", composed, err)
	}
	original := filepath.Join(agent.Workspace, "original.png")
	if err := os.WriteFile(original, []byte("edited later"), 0o600); err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(chip.Path)
	if err != nil || string(copied) != string(data) {
		t.Fatal("original change altered reference")
	}
	if err := b.RemoveAttachment(agent.ID, chip.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(chip.Path); !os.IsNotExist(err) {
		t.Fatal("unsent copy leaked")
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatal("original removed")
	}
	b.locked(func() {
		if b.referenceN != 0 || a.referenceN != 0 || b.referenceWorkers != 0 {
			t.Fatal("admission leaked")
		}
	})
}
func TestAppMediaAttachRefusesStaleAndFullAdmission(t *testing.T) {
	b, agent, source, version, _ := referenceBackend(t)
	if _, err := b.AttachMedia(agent.ID, source, "stale"); err == nil {
		t.Fatal("stale source attached")
	}
	if _, err := b.AttachMedia(agent.ID, "not-a-source", version); wireCode(err) != appwire.CodeNotFound {
		t.Fatalf("not found: %v", err)
	}
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
		if _, err := b.AttachMedia(agent.ID, source, version); wireCode(err) != appwire.CodeBusy {
			t.Fatalf("%s not bounded: %v", kind, err)
		}
		b.locked(func() { b.agents[agent.ID].referenceN = 0; b.referenceN = 0; b.referenceWorkers = 0 })
	}
}
func TestAppMediaAttachUnknownDeliveryKeepsImageAndCloseDropsUnsent(t *testing.T) {
	b, agent, source, version, _ := referenceBackend(t)
	uncertain, err := b.AttachMedia(agent.ID, source, version)
	if err != nil {
		t.Fatal(err)
	}
	a := b.agent(agent.ID)
	b.locked(func() { a.attach[uncertain.ID].reference.attempted = true })
	if err := b.RemoveAttachment(agent.ID, uncertain.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(uncertain.Path); err != nil {
		t.Fatal("unknown delivery image removed")
	}
	unsent, err := b.AttachMedia(agent.ID, source, version)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unsent.Path); !os.IsNotExist(err) {
		t.Fatal("close retained definitely unsent image")
	}
	if _, err := os.Stat(uncertain.Path); err != nil {
		t.Fatal("close removed unknown-delivery image")
	}
}

type referenceConversation struct {
	*fakeConversation
	message session.Submission
}

func (c *referenceConversation) ComposedKnown() bool { return true }
func (c *referenceConversation) SubmitComposed(ctx context.Context, m session.Submission) (session.SubmissionReceipt, <-chan session.Event, error) {
	c.message = m
	events, err := c.fakeConversation.Submit(ctx, m.Text)
	return session.SubmissionReceipt{MessageID: "msg_0123456789abcdef01234567", Delivery: session.DeliveryImage, Media: []session.MessageMedia{{ID: m.Media[0].ID, Path: m.Media[0].Path, Source: m.Media[0].Source}}}, events, err
}
func TestAppMediaAttachOrdinarySendConsumesImageAndKeepsAcceptedSnapshot(t *testing.T) {
	b, agent, source, version, _ := referenceBackend(t)
	conversation := &referenceConversation{fakeConversation: &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}}
	b.locked(func() {
		if a := b.agents[agent.ID]; a.live != nil {
			a.live.conn.Conv = conversation
		}
	})
	b.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conversation, Workspace: agent.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	chip, err := b.AttachMedia(agent.ID, source, version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.SendWith(agent.ID, "Make 4 more like this", "reference-client", []string{chip.ID}); err != nil {
		t.Fatal(err)
	}
	if len(conversation.message.Media) != 1 || conversation.message.Media[0].Source.SourceID != source || conversation.message.Media[0].ExpectedSHA256 == "" {
		t.Fatal("ordinary send did not carry exact reference image")
	}
	if staged(b, agent.ID, chip.ID) {
		t.Fatal("accepted attachment ID retained")
	}
	if err := b.RemoveAttachment(agent.ID, chip.ID); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(chip.Path); err != nil {
		t.Fatal("accepted durable snapshot deleted")
	}
}

func TestAppMediaAttachKnownRefusalCanDiscardSnapshot(t *testing.T) {
	b, agent, source, version, _ := referenceBackend(t)
	chip, err := b.AttachMedia(agent.ID, source, version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.SendWith(agent.ID, "request", "refused-client", []string{chip.ID}); wireCode(err) != appwire.CodeUnknownMethod {
		t.Fatalf("wanted explicit unsupported refusal: %v", err)
	}
	if err := b.RemoveAttachment(agent.ID, chip.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(chip.Path); !os.IsNotExist(err) {
		t.Fatal("known-refused snapshot leaked")
	}
}
