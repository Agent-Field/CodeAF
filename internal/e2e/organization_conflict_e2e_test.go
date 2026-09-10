//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// A person's choice in one chat is still a conversation instruction, not a
// silently accepted shared decision. Other readers must retain both sources.
func testOrganizationConflictingSources(t *testing.T) {
	w := newWorld(t)
	pinEveryTextModel(t)
	started := time.Now()
	ctx := context.Background()
	s, err := workspace.Open(orgStorePath())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	g, err := s.Create(ctx, "Release planning")
	if err != nil {
		t.Fatal(err)
	}
	targets := []workspace.Ref{{Kind: workspace.CollectionKind, ID: g.ID}}
	rawTargets, _ := json.Marshal(targets)
	type reader struct {
		a   *session.Agent
		p   session.Place
		dir string
	}
	readers := make([]reader, 2)
	for i := range readers {
		dir := t.TempDir()
		a, p := w.open(dir, orgSeam(w))
		readers[i] = reader{a, p, dir}
		if err = s.Add(ctx, g.ID, workspace.Ref{Kind: workspace.ConversationKind, ID: session.PlaceSession(p)}); err != nil {
			t.Fatal(err)
		}
	}
	dates := []string{"2026-10-13", "2026-10-15"}
	expected := map[string]struct{ source, date string }{}
	for i, date := range dates {
		source, p := w.open(t.TempDir(), orgSeam(w))
		title := fmt.Sprintf("Launch date source %d", i+1)
		out := organizationSay(t, w, source, fmt.Sprintf("Record shared information titled %q. This source says the launch date is %s, reference %s. Explicit targets: %s. This is an independent source, not authorization or a resolution of other sources. Create only this record; do not revise other records.", title, date, orgNonce(t), rawTargets))
		if _, at := orgActionReceipt(out, "shared_context", "create"); at < 0 {
			t.Fatal("missing source create receipt")
		}
		all, e := s.ContextFor(ctx, targets)
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range all {
			if r.Title == title {
				expected[r.ID] = struct{ source, date string }{session.PlaceSession(p), date}
			}
		}
	}
	if len(expected) != 2 {
		t.Fatalf("expected two independent sources, got %v", expected)
	}
	inspect := func(i int, file, preface, date string) {
		t.Helper()
		out := organizationSay(t, w, readers[i].a, preface+" Prepare a launch-date check using current applicable shared information. Flag unresolved disagreements instead of silently selecting a source. Use the write tool to create "+file+" as JSON with source_status (conflict when the shared sources disagree), draft_date (the date I explicitly chose for this conversation's draft, or empty if I have not chosen), needs_choice (true only if I have not chosen a date for this draft), and evidence (one entry for each source, with id (exact record identifier), source_id (exact originating conversation identifier only, without a kind label or prefix), and date). Keep each source's date in evidence even if I made a choice for this draft. Write only the requested JSON file; do not create or revise an announcement document. Report only; do not change shared context, collections, tasks or external systems.")
		for _, c := range out.Calls {
			if c.Name == "write" || c.Name == "edit" {
				var a struct {
					Path string `json:"path"`
				}
				if err := json.Unmarshal([]byte(c.Args), &a); err != nil {
					t.Fatal(err)
				}
				target := a.Path
				if !filepath.IsAbs(target) {
					target = filepath.Join(readers[i].dir, target)
				}
				if !organizationSameFile(target, filepath.Join(readers[i].dir, file)) {
					t.Errorf("report wrote another file: %s", target)
				}
			}
			if c.Name != "shared_context" && c.Name != "collections" {
				continue
			}
			var a struct {
				Action string `json:"action"`
			}
			if err = json.Unmarshal([]byte(c.Args), &a); err != nil {
				t.Fatal(err)
			}
			switch a.Action {
			case "read", "list", "history", "find", "show":
			default:
				t.Fatalf("report changed organization: %s", c.Args)
			}
		}
		raw, e := os.ReadFile(filepath.Join(readers[i].dir, file))
		if e != nil {
			t.Fatal(e)
		}
		var got struct {
			SourceStatus string `json:"source_status"`
			DraftDate    string `json:"draft_date"`
			NeedsChoice  bool   `json:"needs_choice"`
			Evidence     []struct {
				ID     string `json:"id"`
				Source string `json:"source_id"`
				Date   string `json:"date"`
			} `json:"evidence"`
		}
		if e = json.Unmarshal(raw, &got); e != nil {
			t.Fatalf("invalid artifact: %s", raw)
		}
		if got.SourceStatus != "conflict" || got.DraftDate != date || got.NeedsChoice != (date == "") || len(got.Evidence) != 2 {
			t.Errorf("incorrect decision/evidence: %s", raw)
		}
		seen := map[string]bool{}
		for _, item := range got.Evidence {
			want, ok := expected[item.ID]
			if !ok || seen[item.ID] || item.Source != want.source || item.Date != want.date {
				t.Errorf("source lost or rewritten: %s", raw)
			}
			seen[item.ID] = true
		}
		t.Logf("CONFLICT reader=%d artifact=%s %s", i, file, raw)
	}
	inspect(0, "disagreement.json", "", "")
	inspect(0, "local-choice.json", "For my announcement draft in THIS conversation, use 2026-10-15. That is my choice for this draft only; leave the shared records as they are. ", "2026-10-15")
	if err = readers[0].a.Close(); err != nil {
		t.Fatal(err)
	}
	readers[0].a = w.openAt(readers[0].dir, readers[0].p, orgSeam(w))
	inspect(0, "reopened-choice.json", "Continue our launch-date check in this conversation. ", "2026-10-15")
	inspect(1, "independent-reader.json", "This is a separate draft, and I have not chosen a date here. ", "")
	all, err := s.ContextFor(ctx, targets)
	if err != nil || len(all) != 2 {
		t.Fatalf("records changed: %v %v", all, err)
	}
	for _, r := range all {
		h, e := s.ContextHistory(ctx, r.ID)
		if e != nil || len(h) != 1 || r.Revision != 1 || r.Withdrawn {
			t.Fatalf("local choice mutated shared record: %+v %v", r, e)
		}
	}
	usd, models := ledgerSince(t, started)
	t.Logf("RECEIPT domain=conflicting_sources cost=$%.6f models=%v elapsed=%s", usd, models, time.Since(started))
	if usd > orgCap {
		t.Fatalf("conflict case exceeded $%.2f: $%.4f", orgCap, usd)
	}
	for _, model := range models {
		if model != e2eModel {
			t.Fatalf("unexpected model %s", model)
		}
	}
}
