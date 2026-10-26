package knowledge_test

import (
	"context"
	"database/sql"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/knowledgefixture"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureIsOptInDurableAndProfileShared(t *testing.T) {
	profile := t.TempDir()
	v := knowledge.Open(profile)
	if v.Enabled() {
		t.Fatal("must start disabled")
	}
	s := knowledge.Source{ID: "turn-1", Session: "chat-1", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Use PostgreSQL for durable records", Kind: "user_statement"}
	if err := v.Capture(s); err != nil {
		t.Fatal(err)
	}
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err := v.Capture(s); err != nil {
		t.Fatal(err)
	}
	if err := v.Capture(s); err != nil {
		t.Fatal(err)
	}
	got, err := knowledge.Open(profile).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got.Pending != 1 {
		t.Fatalf("want one pending source, got %d", got.Pending)
	}
	if !knowledge.Open(profile).Enabled() {
		t.Fatal("profile consent was not shared")
	}
	s.Text = "Changed under same identity"
	if v.Capture(s) == nil {
		t.Fatal("immutable source overwritten")
	}
}

func TestEmptyVaultFlushUsesEmbeddedRuntimeWithoutModelOrCheckout(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	created := v.EnabledSince()
	if created.IsZero() {
		t.Fatal("missing consent time")
	}
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if !v.EnabledSince().Equal(created) {
		t.Fatal("consent time changed")
	}
	if err := v.Flush(context.Background(), "", true); err != nil {
		t.Fatal(err)
	}
	s, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if s.Pending != 0 || len(s.Pages) != 0 || s.CostUSD != 0 {
		t.Fatalf("unexpected empty vault: %+v", s)
	}
}

func TestPublishedPagesExposeCitedDetailTextForRecall(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(v.Directory(), "knowledge"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := `{"sources":{"s1":{"role":"user","ts":"2026-10-25T12:00:00Z","text":"Cedar queues jobs."}},"claims":[{"id":"c1","text":"Cedar queues jobs.","kind":"fact","status":"active","evidence":[{"source":"s1","quote":"Cedar queues jobs."}]}],"notes":[{"id":"routing","title":"Routing","claims":["c1"]}]}`
	if err := knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	hits, err := v.Search("queues", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("want cited detail hit, got %d", len(hits))
	}
	page, err := v.Read("routing")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Evidence) != 1 || page.Evidence[0].Source != "s1" || len(page.Sections) != 1 {
		t.Fatalf("missing readable sourced detail: %+v", page)
	}
}

func TestAdmittedCaptureReplayDoesNotRequeue(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	source := knowledge.Source{ID: "stable", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Existing durable source"}
	if err := v.Capture(source); err != nil {
		t.Fatal(err)
	}
	if err := v.Capture(source); err != nil {
		t.Fatal(err)
	}
	status, err := v.Status()
	if err != nil || status.Pending != 1 {
		t.Fatalf("replay duplicated: %+v %v", status, err)
	}
	source.Text = "changed"
	if v.Capture(source) == nil {
		t.Fatal("immutable identity overwritten")
	}
}

func TestCompilerFailureRetainsExactlyOnePendingSourceAcrossReplay(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	source := knowledge.Source{ID: "stable", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar uses a durable queue."}
	if err := v.Capture(source); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "", true); err == nil {
		t.Fatal("missing required key unexpectedly compiled")
	}
	if err := v.Capture(source); err != nil {
		t.Fatal(err)
	}
	snapshot, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Pending != 1 || snapshot.LastError == "" {
		t.Fatalf("missing retained pending error: %+v", snapshot)
	}
}

func TestArticleParagraphsHydrateOriginalClaimCitations(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(v.Directory(), "knowledge"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := `{"sources":{"s1":{"role":"user","ts":"2026-10-25T12:00:00Z"}},"claims":[{"id":"c1","text":"Cedar queues jobs.","kind":"fact","status":"active","evidence":[{"source":"s1","quote":"Cedar queues jobs."}]}],"notes":[{"id":"article","title":"Cedar","sections":[{"heading":"Queues","paragraphs":[{"text":"Cedar queues jobs.","claims":["c1"]}]}]}]}`
	if err := knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := v.Read("article")
	if err != nil {
		t.Fatal(err)
	}
	evidence := page.Sections[0].Paragraphs[0].Evidence
	if len(evidence) != 1 || evidence[0].Source != "s1" || evidence[0].Role != "user" {
		t.Fatalf("missing article citation metadata: %+v", evidence)
	}
}

func TestStatusChecksPendingWithoutReadingArticleProjection(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	source := knowledge.Source{ID: "pending", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues jobs."}
	if err := v.Capture(source); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(v.Directory(), "knowledge"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := corruptKnowledgePage(v); err != nil {
		t.Fatal(err)
	}
	status, err := v.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || status.Pending != 1 || status.Pages != nil {
		t.Fatalf("incorrect metadata status: %+v", status)
	}
	if _, err := v.Snapshot(); err == nil {
		t.Fatal("broken article projection unexpectedly parsed")
	}
}

func corruptKnowledgePage(v *knowledge.Vault) error {
	uri := sqliteuri.File(filepath.Join(v.Directory(), "knowledge.sqlite"))
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("INSERT INTO pages(id,title,scope,payload) VALUES('broken','Broken','','invalid article projection')")
	return err
}
