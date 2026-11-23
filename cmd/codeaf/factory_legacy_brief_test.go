package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// stampStage flags a member as a factory stage the way legacy recovery does:
// the flag alone, no brief invented.
func stampStage(t *testing.T, b *appBackend, handle, brief string) {
	t.Helper()
	if err := teamstore.Update(b.profileDir, func(f *teamstore.File) error {
		for i := range f.Teams {
			for j := range f.Teams[i].Members {
				if f.Teams[i].Members[j].Handle == handle {
					f.Teams[i].Members[j].FactoryStage, f.Teams[i].Members[j].FactoryBrief = true, brief
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A PERSON'S EDIT MADE BEFORE A STAGE WAS FLAGGED WAS REAL, and the stage read
// it. Recovery must not hide it behind "no instructions recorded", and must
// not credit a manager, a sender or the factory with it; new edits stay refused.
func TestARecoveredStageKeepsTheEditsItAlreadyReceived(t *testing.T) {
	backend, id, web := startedWeb(t, &fakeConversation{}, "Own the web client.")
	if _, err := backend.MemberBriefSet(id, web, "Own the web client; skip e2e.", nil); err != nil {
		t.Fatal(err)
	}
	before, err := backend.MemberBrief(id, web)
	if err != nil || len(before.Revisions) != 1 {
		t.Fatalf("setup brief = %+v, %v", before, err)
	}
	stampStage(t, backend, "web", "")

	got, err := backend.MemberBrief(id, web)
	if err != nil {
		t.Fatal(err)
	}
	if !got.FactoryStage || got.Text != "Own the web client; skip e2e." {
		t.Fatalf("recovered stage lost its real words: %+v", got)
	}
	if got.From != "" {
		t.Fatalf("a sender was invented for a factory stage: %q", got.From)
	}
	if len(got.Revisions) != 1 || got.Revisions[0] != before.Revisions[0] || got.Revisions[0].By != "you" ||
		got.Revisions[0].Before != "Own the web client." || got.At == nil {
		t.Fatalf("history changed by recovery: %+v want %+v", got.Revisions, before.Revisions)
	}

	// New edits are still refused, and a refused edit writes nothing.
	if _, err := backend.MemberBriefSet(id, web, "something else", nil); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("set = %v", err)
	}
	after, _ := backend.MemberBrief(id, web)
	if after.Text != got.Text || len(after.Revisions) != 1 {
		t.Fatalf("a refused edit changed the brief: %+v", after)
	}
}

// Redaction still applies to words and history on the stage branch.
func TestARecoveredStagesEditHistoryIsRedacted(t *testing.T) {
	backend, id, web := startedWeb(t, &fakeConversation{}, "Own the web client.")
	const secret = "sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		for i := range f.Teams {
			for j := range f.Teams[i].Members {
				m := &f.Teams[i].Members[j]
				if m.Handle == "web" {
					m.FactoryStage = true
					m.Briefs = append(m.Briefs, teamstore.BriefRevision{By: teamstore.FromYou, Text: "use " + secret, Before: "old " + secret})
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := backend.MemberBrief(id, web)
	if err != nil || len(got.Revisions) != 1 {
		t.Fatalf("brief = %+v, %v", got, err)
	}
	if strings.Contains(got.Text, secret) || strings.Contains(got.Revisions[0].Text, secret) || strings.Contains(got.Revisions[0].Before, secret) {
		t.Fatalf("secret leaked: %+v", got)
	}
}

// A stage never edited reads exactly as before: its recorded words, no history.
func TestAFactoryStageNeverEditedHasNoHistory(t *testing.T) {
	backend, id, web := startedWeb(t, &fakeConversation{}, "Own the web client.")
	stampStage(t, backend, "web", "Review the diff.")
	got, err := backend.MemberBrief(id, web)
	if err != nil || !got.FactoryStage || got.Text != "Review the diff." || got.From != "" || got.At != nil || len(got.Revisions) != 0 {
		t.Fatalf("brief = %+v, %v", got, err)
	}
}
