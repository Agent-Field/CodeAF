//go:build e2e

package knowledge_test

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/knowledgefixture"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var libraryLiveKey = config.APIKeyAt(func() string {
	if p := config.ProfileDir(); p != "" {
		return p
	}
	return home.InheritedDir()
}())

func TestLibraryRecallWithDeepSeekFlash1017(t *testing.T) {
	if strings.TrimSpace(libraryLiveKey) == "" {
		t.Skip("No provider credential found through config.APIKeyAt")
	}
	profile := t.TempDir()
	v := knowledge.Open(profile)
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(config.KeyKnowledgeModel)
	if !ok {
		t.Fatal("knowledge model setting missing")
	}
	if err := row.Apply("deepseek/deepseek-v4.1-flash-1017"); err != nil {
		t.Fatal(err)
	}
	fixture := `{"relations":[],"sources":{"s":{"id":"s","session":"chat","text":"The relay listens on port 8443.","role":"user","ts":"2026-11-01T12:00:00Z"}},"claims":[{"id":"port","text":"The relay listens on port 8443.","scope":"codeaf","kind":"fact","status":"active","evidence":[{"source":"s","quote":"The relay listens on port 8443."}]}],"notes":[{"id":"relay","title":"Relay port","kind":"subject","scope":"codeaf","folder":["codeaf"],"claims":["port"],"sections":[{"heading":"Port","paragraphs":[{"text":"The relay listens on port 8443.","claims":["port"],"evidence":[{"source":"s","quote":"The relay listens on port 8443."}]}]}]}]}`
	if err := knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteMemoryEnabled(profile, false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	answer, err := v.RecallSaved(ctx, libraryLiveKey, "Which port does the relay listen on?", "codeaf")
	if err != nil {
		t.Fatalf("DeepSeek Flash 1017 recall failed: %T", err)
	}
	if answer.Abstain || !strings.Contains(answer.Answer, "8443") || len(answer.Citations) == 0 {
		t.Fatal("Expected a sourced answer naming port 8443")
	}
	if answer.Citations[0].Page != "relay" || answer.Citations[0].Section != "Port" {
		t.Fatal("Citation does not open its exact guide section")
	}
	if answer.Citations[0].Source != "s" {
		t.Fatal("Citation did not identify the supplied source")
	}
	if v.Enabled() {
		t.Fatal("An explicit Library question resumed learning")
	}
	t.Log("deepseek/deepseek-v4.1-flash-1017 answered 8443 with a citation to the supplied chat")
}

func TestLibraryRecallKeepsTheOlderAccountChosenByYou(t *testing.T) {
	if strings.TrimSpace(libraryLiveKey) == "" {
		t.Skip("No provider credential found through config.APIKeyAt")
	}
	profile := t.TempDir()
	v := knowledge.Open(profile)
	raw, err := os.ReadFile("testdata/library-conflict.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture := strings.Replace(string(raw), `"notes":[`, `"notes":[{"id":"mirror","title":"Earlier relay notes","claims":["old","new"]},`, 1)
	if err := knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := v.ReadDetail("relay")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.SettlePage(page.ID, page.Revision, page.Conflicts[0].ID, "old", ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	answer, err := v.Recall(ctx, libraryLiveKey, "Which port should the relay use?", "")
	if err != nil {
		t.Fatal(err)
	}
	if answer.Abstain || !strings.Contains(answer.Answer, "7443") || strings.Contains(answer.Answer, "8443") || len(answer.Citations) == 0 || answer.Citations[0].Claim != "old" {
		t.Fatalf("the chosen account was not recalled: %+v", answer)
	}
	t.Log("deepseek/deepseek-v4.1-flash-1017 recalled the older account chosen by you, with its citation")
}
