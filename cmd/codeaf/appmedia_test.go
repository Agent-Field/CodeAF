package main

import (
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

func writePNG(t *testing.T, path string, w, h int) []byte {
	t.Helper()
	var buf = new(bytesBuffer)
	if err := png.Encode(buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.b, 0o644); err != nil {
		t.Fatal(err)
	}
	return buf.b
}

type bytesBuffer struct{ b []byte }

func (w *bytesBuffer) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }

// MEDIA.LIST AND MEDIA.GET answer from the engine's own record: a generated
// file in the workspace and an attachment outside it are both sources, a source
// of another conversation is not found, an unknown id is not found, and a file
// that has gone keeps what was recorded.
func TestAppMediaListGetOwnershipAndMissing(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	sessionID, _, err := backend.mediaScope(agent.ID)
	if err != nil {
		t.Fatal(err)
	}

	generated := filepath.Join(agent.Workspace, "made.png")
	data := writePNG(t, generated, 40, 30)
	genID := session.RecordMedia(index, session.Artifact{Path: generated, Session: sessionID, Title: "made", Kind: "image", Created: time.Now()},
		session.GeneratedMedia(generated, data, "paint/model"))
	elsewhere := filepath.Join(t.TempDir(), "picked.png")
	writePNG(t, elsewhere, 8, 6)
	session.RecordAttachment(index, sessionID, elsewhere)
	session.RecordMedia(index, session.Artifact{Path: generated, Session: "someone-else", Title: "theirs", Kind: "image", Created: time.Now()},
		session.GeneratedMedia(generated, data, "m"))

	page, err := backend.ListMedia(agent.ID, "", 50)
	if err != nil || len(page.Sources) != 2 || page.Next != "" {
		t.Fatalf("list = %+v %v", page, err)
	}
	attached, made := page.Sources[0], page.Sources[1]
	if attached.Origin != "attachment" || attached.State != appwire.MediaAvailable || attached.Width == nil || *attached.Width != 8 {
		t.Fatalf("attachment = %+v", attached)
	}
	if made.ID != genID || made.Origin != "generated" || made.State != appwire.MediaAvailable || made.Model != "paint/model" ||
		made.RequestedModel != "paint/model" || made.Charge != "unknown" || made.CostUSD != nil || made.Version == "" || made.Bytes == nil || *made.Bytes != int64(len(data)) {
		t.Fatalf("generated = %+v", made)
	}

	got, err := backend.GetMedia(agent.ID, genID)
	if err != nil || got.ID != genID || got.Path != made.Path {
		t.Fatalf("get = %+v %v", got, err)
	}
	var wire *appwire.Error
	if _, err := backend.GetMedia(agent.ID, session.NewMediaID()); !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("unknown id answered %v", err)
	}
	if _, err := backend.GetMedia(agent.ID, "/etc/passwd"); !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("a path as an id answered %v", err)
	}
	if _, err := backend.ListMedia(agent.ID, "garbage", 5); !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
		t.Fatalf("garbage cursor answered %v", err)
	}
	if _, err := backend.ListMedia("no-such-agent", "", 5); err == nil {
		t.Fatal("an unknown agent listed media")
	}

	// Gone: the record stays, nothing is guessed from the file.
	if err := os.Remove(generated); err != nil {
		t.Fatal(err)
	}
	gone, err := backend.GetMedia(agent.ID, genID)
	if err != nil || gone.State != appwire.MediaMissing || gone.Bytes == nil || *gone.Bytes != int64(len(data)) ||
		gone.LiveBytes != nil || gone.LiveVersion != "" || gone.Width == nil || *gone.Width != 40 || gone.Version != made.Version {
		t.Fatalf("missing = %+v %v", gone, err)
	}
}

// A legacy image row (no recorded size) is probed on get only, and says so.
func TestAppMediaLegacyImageIsProbedOnGetOnly(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	sessionID, _, _ := backend.mediaScope(agent.ID)
	path := filepath.Join(agent.Workspace, "old.png")
	writePNG(t, path, 12, 7)
	session.RecordArtifact(index, session.Artifact{Path: path, Session: sessionID, Title: "old", Kind: "image", Created: time.Now()})
	page, err := backend.ListMedia(agent.ID, "", 5)
	if err != nil || len(page.Sources) != 1 {
		t.Fatalf("list = %+v %v", page, err)
	}
	listed := page.Sources[0]
	if listed.Recorded || listed.Width != nil || listed.Bytes != nil || listed.Version != "" || listed.LiveBytes == nil {
		t.Fatalf("legacy listed = %+v", listed)
	}
	got, _ := backend.GetMedia(agent.ID, listed.ID)
	if got.Width == nil || *got.Width != 12 || !got.Probed || got.Bytes != nil {
		t.Fatalf("legacy get = %+v", got)
	}
}

// A file attached to a SENT message becomes a source (origin attachment); a
// non-media file and a message never sent do not.
func TestAppSendNotesAttachedMediaAsSources(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	picture := filepath.Join(agent.Workspace, "shot.png")
	writePNG(t, picture, 5, 4)
	notes := filepath.Join(agent.Workspace, "notes.txt")
	if err := os.WriteFile(notes, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	staged := func(path string) string {
		a, err := backend.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachFile, Path: path}, "")
		if err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	pic, txt := staged("shot.png"), staged("notes.txt")
	if page, _ := backend.ListMedia(agent.ID, "", 10); len(page.Sources) != 0 {
		t.Fatalf("a staged, unsent attachment is already a source: %+v", page.Sources)
	}
	if _, err := backend.SendWith(agent.ID, "look", "m-1", []string{pic, txt}); err != nil {
		t.Fatal(err)
	}
	page, err := backend.ListMedia(agent.ID, "", 10)
	if err != nil || len(page.Sources) != 1 || page.Sources[0].Origin != "attachment" || page.Sources[0].Kind != "image" ||
		page.Sources[0].Width == nil || *page.Sources[0].Width != 5 {
		t.Fatalf("sources = %+v %v", page.Sources, err)
	}
}

// A hundred sources each carrying 32 KiB of text that JSON escapes to six times
// its size never make one reply past the bound: pages end early, the cursor
// resumes exactly where the page stopped, and every source arrives once.
func TestAppMediaPagesStayUnderTheReplyBound(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	sessionID, _, _ := backend.mediaScope(agent.ID)
	path := filepath.Join(agent.Workspace, "p.png")
	data := writePNG(t, path, 2, 2)
	said := strings.Repeat("<", 32<<10)
	var want []string
	for i := 0; i < 100; i++ {
		want = append(want, session.RecordMedia(index, session.Artifact{Path: path, Session: sessionID, Title: "t", Kind: "audio", Created: time.Now()},
			session.GeneratedMedia(path, data, "m").WithSaid(said)))
	}
	var got []string
	cursor, pages := "", 0
	for {
		page, err := backend.ListMedia(agent.ID, cursor, appwire.MediaPageMax)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(page)
		if err != nil || len(raw) > appwire.MediaReplyBudget {
			t.Fatalf("a page of %d bytes (%v) passes the %d reply bound", len(raw), err, appwire.MediaReplyBudget)
		}
		for _, s := range page.Sources {
			got = append(got, s.ID)
		}
		pages++
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if pages < 4 || len(got) != 100 {
		t.Fatalf("%d pages, %d sources", pages, len(got))
	}
	for i, id := range got {
		if id != want[99-i] {
			t.Fatalf("source %d repeated or out of order", i)
		}
	}
}

// A document the agent read is a source of kind document: media.get returns the
// full words verbatim, the parser's page count and a known-false text layer,
// and a document whose parser reported nothing carries neither field.
func TestAppMediaGetReturnsDocumentEvidenceFacts(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}})
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	old := appMediaIndex
	appMediaIndex = func() string { return index }
	t.Cleanup(func() { appMediaIndex = old })
	sessionID, _, err := backend.mediaScope(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(agent.Workspace, "contract.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4 stand-in"), 0o644); err != nil {
		t.Fatal(err)
	}
	words := strings.Repeat("The parties agree to the terms set out below. ", 40)
	layer := false
	scan := session.RecordMedia(index, session.Artifact{Path: pdf, Session: sessionID, Title: "contract.pdf", Kind: "document", Created: time.Now()},
		session.MediaFact{Origin: session.MediaEvidence, MIME: "application/pdf", Verb: session.EvidenceRead, Saw: words,
			SeenBy: "vendor/reader", Working: "chat/model", PageCount: 14, TextLayer: &layer})
	billed := session.RecordMedia(index, session.Artifact{Path: pdf, Session: sessionID, Title: "contract.pdf", Kind: "document", Created: time.Now()},
		session.MediaFact{Origin: session.MediaEvidence, MIME: "application/pdf", Verb: session.EvidenceRead, Saw: "billed words"})
	// Another deliverable that merely has the kind "document" is not a source.
	session.RecordArtifact(index, session.Artifact{Path: pdf, Session: sessionID, Title: "export", Kind: "document", Created: time.Now()})

	page, err := backend.ListMedia(agent.ID, "", 50)
	if err != nil || len(page.Sources) != 2 {
		t.Fatalf("list = %+v %v", page, err)
	}
	got, err := backend.GetMedia(agent.ID, scan)
	if err != nil || got.Kind != "document" || got.Origin != "evidence" || got.Verb != "read" || got.Saw != words ||
		got.SeenBy != "vendor/reader" || got.WorkingModel != "chat/model" || got.PageCount == nil || *got.PageCount != 14 ||
		got.TextLayer == nil || *got.TextLayer || got.State != appwire.MediaAvailable || got.Charge != "unknown" {
		t.Fatalf("get = %+v %v", got, err)
	}
	thin, err := backend.GetMedia(agent.ID, billed)
	if err != nil || thin.PageCount != nil || thin.TextLayer != nil || thin.Saw != "billed words" {
		t.Fatalf("unreported facts reached the wire: %+v %v", thin, err)
	}
	if raw, _ := json.Marshal(thin); strings.Contains(string(raw), "page_count") || strings.Contains(string(raw), "text_layer") {
		t.Fatalf("serialized %s", raw)
	}
}

func TestAppMediaProjectsOneBatchChargeWithoutDividingIt(t *testing.T) {
	cost := 0.17
	row := session.MediaRow{Kind: "image", Recorded: true, Fact: session.MediaFact{ID: "s", Batch: "bat_actual", VariantIndex: 2, Returned: 3, Requested: 4, Charge: session.MediaCharged, CostScope: session.MediaCostScopeCall, CallCostUSD: &cost}}
	got := appMediaSource(row, session.MediaLive{}, false)
	if got.Batch != "bat_actual" || got.VariantIndex != 2 || got.ReturnedCount != 3 || got.RequestedCount == nil || *got.RequestedCount != 4 || got.CallCostUSD == nil || *got.CallCostUSD != cost || got.CostUSD != nil {
		t.Fatalf("wrong batch projection: %+v", got)
	}
}
