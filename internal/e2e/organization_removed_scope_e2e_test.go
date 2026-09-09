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

// Force the identity read from the long working-day counterexample. The full
// turn, including completion, must retain unavailable scope without hiding the
// still-readable record. This is also a cheap live regression for that failure.
func testOrganizationRemovedScopeRead(t *testing.T) {
	w := newWorld(t)
	pinEveryTextModel(t)
	started := time.Now()
	ctx := context.Background()
	dir := t.TempDir()
	reader, place := w.open(dir, orgSeam(w))
	_, sourcePlace := w.open(t.TempDir(), orgSeam(w))
	s, err := workspace.Open(orgStorePath())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	g, err := s.Create(ctx, "Contract review")
	if err != nil {
		t.Fatal(err)
	}
	ref := workspace.Ref{Kind: workspace.ConversationKind, ID: session.PlaceSession(place)}
	if err = s.Add(ctx, g.ID, ref); err != nil {
		t.Fatal(err)
	}
	value := "FIELD-" + orgNonce(t)
	r, err := s.CreateContext(ctx, "Response identifier", "The response identifier is "+value, workspace.Ref{Kind: workspace.ConversationKind, ID: session.PlaceSession(sourcePlace)}, []workspace.Ref{{Kind: workspace.CollectionKind, ID: g.ID}})
	if err != nil {
		t.Fatal(err)
	}
	organizationSay(t, w, reader, "Inspect the current shared response identifier and state its value and record ID. Do not change anything.")
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.Remove(ctx, g.ID, ref); err != nil {
		t.Fatal(err)
	}
	reader = w.openAt(dir, place, orgSeam(w))
	out := organizationSay(t, w, reader, fmt.Sprintf("Check the previously used shared record %s by ID, even though this chat's collection membership changed. Then use the write tool to create scope.json with readable_value (the exact stored identifier), applicable_here (from the read receipt), and current_value (the identifier only if it currently applies here; otherwise empty). Reading a record must not restore its applicability. Write only scope.json; do not change records, collections or take other actions.", r.ID))
	saw := false
	for _, c := range out.Calls {
		if c.Name != "shared_context" {
			continue
		}
		var a struct{ Action, ID string }
		if err = json.Unmarshal([]byte(c.Args), &a); err != nil {
			t.Fatal(err)
		}
		if a.Action != "read" && a.Action != "list" && a.Action != "history" {
			t.Fatalf("inspection mutated context: %s", c.Args)
		}
		if a.Action == "read" && a.ID == r.ID && !c.Failed {
			var got struct {
				Applicable *bool  `json:"applicable_here"`
				Text       string `json:"text"`
			}
			if err = json.Unmarshal([]byte(c.Output), &got); err != nil {
				t.Fatal(err)
			}
			if got.Applicable == nil || *got.Applicable || got.Text != r.Text {
				t.Fatalf("wrong readable scope: %s", c.Output)
			}
			saw = true
		}
	}
	if !saw {
		t.Fatal("the real turn did not read the globally available record")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "scope.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Readable   string `json:"readable_value"`
		Applicable *bool  `json:"applicable_here"`
		Current    string `json:"current_value"`
	}
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Readable != value || got.Applicable == nil || *got.Applicable || got.Current != "" {
		t.Fatalf("completed turn re-promoted out-of-scope information: %s", raw)
	}
	current, err := s.Context(ctx, r.ID)
	if err != nil || current.Revision != 1 || current.Withdrawn || current.Text != r.Text {
		t.Fatal("record was altered instead of distinguishing scope")
	}
	members, err := s.Members(ctx, g.ID)
	if err != nil || len(members) != 0 {
		t.Fatal("inspection re-added collection membership")
	}
	t.Logf("REMOVED_SCOPE artifact=%s", raw)
	usd, models := ledgerSince(t, started)
	t.Logf("RECEIPT domain=removed_scope_read cost=$%.6f models=%v elapsed=%s", usd, models, time.Since(started))
	if usd > orgCap {
		t.Fatalf("scope case exceeded $%.2f: $%.4f", orgCap, usd)
	}
	for _, model := range models {
		if model != e2eModel {
			t.Fatalf("unexpected model %s", model)
		}
	}
}
