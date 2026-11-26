package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/png"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestAppFilePreviewReadsOnlyTheSelectedRecordsIntakeAttachment(t *testing.T) {
	b, _ := dataBackend(t)
	s, err := b.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterApp([]byte(`{"id":"evidence","records":{"case":{"fields":{"name":{"type":"text"},"identity":{"type":"files"}}}},"capabilities":{"records":{"case":"rw"}}}`)); err != nil {
		t.Fatal(err)
	}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewGray(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	intake := data.IntakeDelivery{
		App: "evidence", Channel: "tests", Key: "attached", Type: "case", Set: map[string]any{"name": "First case"}, FileField: "identity",
		Files: []data.IntakeFile{{Name: "Evidence.png", MIME: "image/png", Bytes: pngBytes.Bytes()}},
	}
	receipt, err := s.Deliver(intake)
	if err != nil {
		t.Fatal(err)
	}
	p := appwire.FilePreviewParams{DataScope: appwire.DataScope{App: "evidence"}, Record: appwire.DataRecordRef{Type: "case", ID: receipt.Rows[0]},
		Blob: fmt.Sprintf("sha256:%x", sha256.Sum256(pngBytes.Bytes()))}
	w := newActionWire(t, b)
	raw, wireErr := w.call("files.preview", p)
	if wireErr != nil || !bytes.Contains(raw, []byte("Evidence.png")) || !bytes.Contains(raw, []byte("base64")) {
		t.Fatalf("preview = %s (%v)", raw, wireErr)
	}
	intake.Key, intake.Set, intake.Files = "unattached", map[string]any{"name": "Second case"}, nil
	receipt, err = s.Deliver(intake)
	if err != nil {
		t.Fatal(err)
	}
	p.Record.ID = receipt.Rows[0]
	if _, err := w.call("files.preview", p); err == nil {
		t.Fatal("preview returned an attachment from another record")
	}
}
