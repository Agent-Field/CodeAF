package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/knowledgefixture"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLibraryReadsTheTerminalVaultAndProtectsHumanEdits(t *testing.T) {
	profile := t.TempDir()
	v := knowledge.Open(profile)
	fixture := `{"relations":[],"sources":{"s":{"id":"s","session":"chat","text":"Use port 8443","role":"user","ts":"2026-11-01T12:00:00Z"}},"claims":[{"id":"claim","text":"Use port 8443","scope":"codeaf","kind":"fact","status":"active","evidence":[{"source":"s","quote":"Use port 8443"}]}],"notes":[{"id":"relay","title":"Relay","kind":"subject","scope":"codeaf","folder":["codeaf","Engine"],"claims":["claim"],"sections":[{"heading":"Ports","paragraphs":[{"text":"Use port 8443","claims":["claim"],"evidence":[{"source":"s","quote":"Use port 8443"}],"inline_links":[]}]}],"links":[]}]}`
	if e := knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), []byte(fixture), 0600); e != nil {
		t.Fatal(e)
	}
	b := &appBackend{profileDir: profile}
	snapshot, e := b.KnowledgeSnapshot()
	if e != nil || len(snapshot.Pages) != 1 || snapshot.Pages[0].ID != "relay" {
		t.Fatalf("snapshot identity: %v, %v", snapshot.Pages, e)
	}
	page, e := b.KnowledgeRead("relay")
	if e != nil || page.Sources["s"].Session != "chat" || len(page.ClaimDetails) != 1 {
		t.Fatalf("guide provenance: %v", e)
	}
	updated, e := b.KnowledgeEdit(appwire.KnowledgeMutation{ID: page.ID, Revision: page.Revision, Markdown: "## Ports\n\nUse port 9443"})
	if e != nil || updated.Revision == page.Revision {
		t.Fatalf("edit revision: %v", e)
	}
	terminal, e := v.Read(page.ID)
	if e != nil || knowledge.PageMarkdown(terminal) != "## Ports\n\nUse port 9443" {
		t.Fatalf("desk edit is not in terminal vault: %v", e)
	}
	if _, e = b.KnowledgeEdit(appwire.KnowledgeMutation{ID: page.ID, Revision: page.Revision, Markdown: "stale edit"}); e == nil {
		t.Fatal("stale edit overwrote human text")
	}
	if e = b.KnowledgeDelete(appwire.KnowledgeMutation{ID: page.ID, Revision: page.Revision}); e == nil {
		t.Fatal("stale delete removed current page")
	}
	renamed, e := b.KnowledgeRename(appwire.KnowledgeRename{ID: page.ID, Revision: updated.Revision, Title: "Connections"})
	if e != nil || renamed.Revision == updated.Revision {
		t.Fatalf("rename revision: %v", e)
	}
	terminal, e = v.Read(page.ID)
	if e != nil || terminal.Title != "Connections" || knowledge.PageMarkdown(terminal) != "## Ports\n\nUse port 9443" {
		t.Fatal("rename did not reach the shared vault or erased the body edit")
	}
	hits, e := b.KnowledgeSearch("9443", 10)
	if e != nil || len(hits) != 1 || hits[0].ID != page.ID {
		t.Fatalf("current human words not searchable: %v", e)
	}
}

func TestLibraryChoiceOpensItsGuideFromInboxAndClosesEverywhere(t *testing.T) {
	profile := t.TempDir()
	v := knowledge.Open(profile)
	fixture, err := os.ReadFile("../../internal/knowledge/testdata/library-conflict.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	b := &appBackend{profileDir: profile, state: &appState{path: filepath.Join(profile, "app", "state.json")}, now: func() time.Time { return now }}
	b.emit = b.newAppInbox(newAppRecorder())
	rows, counts, err := b.ListInbox(appwire.InboxWaiting, "")
	if err != nil || len(rows) != 1 || counts.Waiting != 1 {
		t.Fatalf("Library choice missing from waiting: %d, %v", len(rows), err)
	}
	target, _, err := b.OpenInbox(rows[0].ID)
	if err != nil || target != "library:relay" {
		t.Fatalf("choice opened a made-up agent: %q, %v", target, err)
	}
	if err = b.LaterInbox(rows[0].ID, true); err != nil {
		t.Fatal(err)
	}
	later, _, err := b.ListInbox(appwire.InboxLater, "")
	if err != nil || len(later) != 1 {
		t.Fatal("Later lost the Library choice")
	}
	page, err := b.KnowledgeRead("relay")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.KnowledgeSettle(appwire.KnowledgeSettlement{ID: "relay", Revision: page.Revision, Conflict: page.Conflicts[0].ID, Keep: "new"}); err != nil {
		t.Fatal(err)
	}
	_, counts, err = b.ListInbox(appwire.InboxWaiting, "")
	if err != nil || counts.Waiting != 0 || counts.Later != 0 {
		t.Fatal("settled choice still waits in Inbox")
	}
	page, err = b.KnowledgeRead("relay")
	if err != nil || len(page.Resolutions) != 1 || !strings.Contains(knowledge.PageMarkdown(page.Page), "8443") {
		t.Fatal("the guide did not retain the chosen account")
	}
}

func TestLibraryModelSettingUsesTheSameProfileRowAsTheTerminal(t *testing.T) {
	profile := t.TempDir()
	b := &appBackend{profileDir: profile}
	model := "deepseek/deepseek-v4.1-flash"
	if err := b.applyExtra(appwire.SettingsPatch{KnowledgeModel: &model}, profile); err != nil {
		t.Fatal(err)
	}
	if got := config.KnowledgeModelAt(profile); got != model {
		t.Fatalf("terminal reads %q", got)
	}
	var shown appwire.Settings
	settingsExtra(&shown, appStateFile{}, profile)
	if shown.KnowledgeModel != model {
		t.Fatal("Settings lost the saved knowledge model")
	}
	empty := ""
	if err := b.applyExtra(appwire.SettingsPatch{KnowledgeModel: &empty}, profile); err != nil {
		t.Fatal(err)
	}
	if got := config.KnowledgeModelAt(profile); got != config.DefaultKnowledgeModel {
		t.Fatalf("clearing selected %q", got)
	}
}

func TestLibraryLaterSurvivesRestartWhileLearningIsPaused(t *testing.T) {
	profile := t.TempDir()
	v := knowledge.Open(profile)
	raw, err := os.ReadFile("../../internal/knowledge/testdata/library-conflict.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteMemoryEnabled(profile, false); err != nil {
		t.Fatal(err)
	}
	backend := func() *appBackend {
		b := &appBackend{profileDir: profile, now: func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) }}
		b.state = &appState{path: filepath.Join(profile, "app", "state.json")}
		b.emit = b.newAppInbox(newAppRecorder())
		return b
	}
	b := backend()
	rows, _, err := b.ListInbox(appwire.InboxWaiting, "")
	if err != nil || len(rows) != 1 {
		t.Fatal("paused learning hid an existing Library question")
	}
	if err := b.LaterInbox(rows[0].ID, true); err != nil {
		t.Fatal(err)
	}
	restarted := backend()
	later, counts, err := restarted.ListInbox(appwire.InboxLater, "")
	if err != nil || len(later) != 1 || counts.Waiting != 0 {
		t.Fatal("restart forgot the Library question in Later")
	}
	if err := restarted.LaterInbox(later[0].ID, false); err != nil {
		t.Fatal(err)
	}
	waiting, _, err := backend().ListInbox(appwire.InboxWaiting, "")
	if err != nil || len(waiting) != 1 {
		t.Fatal("bringing a Library question back was not saved")
	}
}
