package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestAppWireCarriesASharedFileAsSharedAndNeverMade is the frozen wire fact a
// shared row becomes: deliverables say shared_by_agent with the sharing call
// and the copy's original; media.get and media.list say origin shared with no
// model, price or change, and charge unknown; a shared PDF is a file and no
// media source at all.
func TestAppWireCarriesASharedFileAsSharedAndNeverMade(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	id, roots, err := backend.mediaScope(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	picture := sharedWireRow(t, id, filepath.Join(agent.Workspace, "chart.png"), "image", "image/png", []byte("\x89PNG\r\n\x1a\nfake"))
	original := filepath.Join(t.TempDir(), "tree", "chart.png")
	picture.Deliver.Source = &session.DeliverableFile{Canonical: original, Bytes: picture.Deliver.File.Bytes, SHA256: picture.Deliver.File.SHA256, MIME: "image/png"}
	pdf := sharedWireRow(t, id, filepath.Join(agent.Workspace, "report.pdf"), "file", "application/pdf", []byte("%PDF-1.7\n"))
	for _, row := range []session.Artifact{picture, pdf} {
		if !session.RecordArtifact(index, row) {
			t.Fatal("row not written")
		}
	}
	page, err := backend.ListDeliverables(appwire.DeliverablesQuery{Scope: "agent", Agent: agent.ID, Limit: 10})
	if err != nil || len(page.Deliverables) != 2 {
		t.Fatalf("list %+v %v", page, err)
	}
	for _, got := range page.Deliverables {
		if got.Origin != "shared_by_agent" || got.Authored || got.State != "" || got.Call != "call-share" || got.Run != "run-share" || got.Turn != "scope#2" {
			t.Fatalf("shared deliverable on the wire: %+v", got)
		}
	}
	shown, pdfShown := page.Deliverables[1], page.Deliverables[0]
	if shown.Kind != "image" || shown.Media == "" || shown.Original != original {
		t.Fatalf("shared picture %+v", shown)
	}
	if pdfShown.Kind != "file" || pdfShown.MIME != "application/pdf" || pdfShown.Media != "" || pdfShown.Original != "" {
		t.Fatalf("shared pdf %+v", pdfShown)
	}
	source, err := backend.GetMedia(agent.ID, shown.Media)
	if err != nil {
		t.Fatal(err)
	}
	if source.Origin != "shared" || source.Charge != "unknown" || source.Model != "" || source.CostUSD != nil || source.Change != "" || source.Declared != "" || source.Original != original {
		t.Fatalf("shared media on the wire: %+v", source)
	}
	if source.Tool != "call-share" || source.Run != "run-share" || source.Message != "scope#2" || source.State != "available" {
		t.Fatalf("shared media attribution/state: %+v (roots %v)", source, roots)
	}
	listed, err := backend.ListMedia(agent.ID, "", 10)
	if err != nil || len(listed.Sources) != 1 || listed.Sources[0].ID != shown.Media {
		t.Fatalf("media.list holds %+v %v; only the picture is a source", listed.Sources, err)
	}
}

func sharedWireRow(t *testing.T, owner, path, kind, mime string, data []byte) session.Artifact {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	file := &session.DeliverableFile{Canonical: path, Bytes: info.Size(), ModNS: info.ModTime().UnixNano(), SHA256: hash, MIME: mime}
	row := session.Artifact{
		Path: path, Session: owner, Title: filepath.Base(path), Kind: kind, Created: time.Now(),
		Deliver: &session.DeliverableFact{
			ID: session.NewDeliverableID(), At: time.Now(), Origin: session.DeliverOriginSharedByAgent,
			Run: "run-share", Turn: "scope#2", Call: "call-share", File: file, Version: "sha256:" + hash,
		},
	}
	if kind == "image" {
		row.Media = &session.MediaFact{
			ID: session.NewMediaID(), Origin: session.MediaShared, Canonical: path, MIME: mime, Bytes: info.Size(),
			ModNS: file.ModNS, SHA256: hash, Run: "run-share", Message: "scope#2", Tool: "call-share",
		}
	}
	return row
}
