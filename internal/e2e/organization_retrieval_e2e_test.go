//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// The reference is outside both the automatic snapshot and the first metadata
// page; its answer is beyond two text windows. Storage setup is a fixture, while
// discovery, continued reads and the output file must come from the real model.
func testOrganizationLargeRetrieval(t *testing.T) {
	w := newWorld(t)
	pinEveryTextModel(t)
	started := time.Now()
	ctx := context.Background()
	folder := t.TempDir()
	reader, place := w.open(folder, orgSeam(w))
	readerRef := workspace.Ref{Kind: workspace.ConversationKind, ID: session.PlaceSession(place)}
	_, sourcePlace := w.open(t.TempDir(), orgSeam(w))
	source := workspace.Ref{Kind: workspace.ConversationKind, ID: session.PlaceSession(sourcePlace)}
	store, err := workspace.Open(orgStorePath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	group, err := store.Create(ctx, "Harbour reference library")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Add(ctx, group.ID, readerRef); err != nil {
		t.Fatal(err)
	}
	targets := []workspace.Ref{{Kind: workspace.CollectionKind, ID: group.ID}}
	for i := 0; i < 30; i++ {
		if _, err = store.CreateContext(ctx, fmt.Sprintf("Survey %02d", i), fmt.Sprintf("Survey %d records no allocation change.", i), readerRef, targets); err != nil {
			t.Fatal(err)
		}
	}
	answer := "AF-" + orgNonce(t)
	var body strings.Builder
	for n := 0; body.Len() < 8100; n++ {
		fmt.Fprintf(&body, "Section %d: berth %d has clearance %d metres; fenders remain serviceable and the surveyed access route is unchanged. This survey section makes no allocation change.\n", n, 100+n, 8+n%5)
	}
	body.WriteString("Closing reference. The berth allocation code is ")
	answerAt := body.Len()
	body.WriteString(answer + ".")
	title := "Final berth allocation reference"
	record, err := store.CreateContext(ctx, title, body.String(), source, targets)
	if err != nil {
		t.Fatal(err)
	}
	// These calls verify actual scope and ordering instead of assuming that the
	// last record inserted is also the last record returned by the store.
	scope := []workspace.Ref{readerRef}
	page, err := store.ContextPage(ctx, scope, true, 0, workspace.MaxContextPage)
	if err != nil || page.More || len(page.Records) != 31 {
		t.Fatalf("library=%+v err=%v", page, err)
	}
	index := -1
	for i, r := range page.Records {
		if r.ID == record.ID {
			index = i
		}
	}
	if index < 25 {
		t.Fatalf("reference unexpectedly within first metadata page: %d", index)
	}
	snapshot, err := session.OrganizationContext(ctx, &session.Organization{Path: orgStorePath()}, readerRef)
	if err != nil || snapshot == "" || strings.Contains(snapshot, record.ID) || strings.Contains(snapshot, answer) {
		t.Fatalf("fixture does not require retrieval beyond snapshot: %v", err)
	}
	before, _ := json.Marshal(page.Records)
	const witness = `{"berths":48,"authority":"report only"}`
	if err = os.WriteFile(filepath.Join(folder, "manifest.json"), []byte(witness), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("RETRIEVAL fixture=storage records=31 target_index=%d answer_rune=%d text_runes=%d", index, answerAt, len([]rune(body.String())))
	prompt := fmt.Sprintf("Find the shared reference titled %q among the information applicable to this conversation. Read the complete reference and report the berth allocation code in its closing paragraph. The short automatic snapshot is not the whole library. Use the write tool to create retrieval.json as one JSON object with value (the exact code), record_id, revision, source_id and status (current). Cite the exact record ID, revision and originating conversation from the shared information. Follow pagination when the tools indicate more data, and pin the returned revision while continuing a text read. Do not modify shared information, collections or manifest.json; report only and take no other actions.", title)
	if strings.Contains(prompt, answer) || strings.Contains(prompt, record.ID) {
		t.Fatal("answer or identity leaked into prompt")
	}
	out := organizationSay(t, w, reader, prompt)
	listed, read := -1, -1
	for at, c := range out.Calls {
		switch c.Name {
		case "shared_context", "collections":
			var a struct {
				Action     string `json:"action"`
				ID         string `json:"id"`
				Offset     int    `json:"offset"`
				TextOffset int    `json:"text_offset"`
				Revision   int    `json:"revision"`
			}
			if err = json.Unmarshal([]byte(c.Args), &a); err != nil {
				t.Fatal(err)
			}
			switch a.Action {
			case "list", "find", "show", "read", "history":
			default:
				t.Errorf("reading attempted mutation: %s", c.Args)
			}
			if c.Failed || c.Name != "shared_context" {
				continue
			}
			// A page may be clipped in the event display even though the model
			// received it whole. Compare its requested range with the verified
			// store position; the output nonce independently proves retrieval.
			if a.Action == "list" && a.Offset > 0 && a.Offset <= index && index < a.Offset+25 {
				listed = at
			}
			// Tool events contain a bounded display copy. Check which requested text
			// window covers the nonce and verify the resulting artifact independently.
			if a.Action == "read" && a.ID == record.ID && a.TextOffset > 0 && a.TextOffset <= answerAt && a.TextOffset+4000 >= answerAt+len(answer) {
				read = at
				if a.Revision != record.Revision {
					t.Errorf("continued read did not pin revision: %s", c.Args)
				}
			}
		case "write", "edit":
			var a struct {
				Path string `json:"path"`
			}
			if err = json.Unmarshal([]byte(c.Args), &a); err != nil {
				t.Fatal(err)
			}
			target := a.Path
			if !filepath.IsAbs(target) {
				target = filepath.Join(folder, target)
			}
			if filepath.Clean(target) != filepath.Join(folder, "retrieval.json") {
				t.Errorf("reading wrote another path: %s", target)
			}
		}
	}
	if listed < 0 || read <= listed {
		t.Errorf("missing ordered list/text pagination: list=%d read=%d calls=%v", listed, read, out.names())
	}
	raw, err := os.ReadFile(filepath.Join(folder, "retrieval.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got organizationEvidence
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("invalid JSON: %s", raw)
	}
	if got.Value != answer || got.RecordID != record.ID || got.Revision != record.Revision || got.SourceID != source.ID || got.Status != "current" {
		t.Errorf("wrong retrieved value or provenance: %s", raw)
	}
	t.Logf("RETRIEVAL artifact=%s", raw)
	after, err := store.ContextPage(ctx, scope, true, 0, workspace.MaxContextPage)
	if err != nil {
		t.Fatal(err)
	}
	afterJSON, _ := json.Marshal(after.Records)
	if after.More || !bytes.Equal(before, afterJSON) {
		t.Error("reading changed applicable records")
	}
	members, err := store.Members(ctx, group.ID)
	if err != nil || len(members) != 1 || members[0] != readerRef {
		t.Errorf("reading changed membership: %v %v", members, err)
	}
	raw, err = os.ReadFile(filepath.Join(folder, "manifest.json"))
	if err != nil || string(raw) != witness {
		t.Error("reading changed manifest")
	}
	usd, models := ledgerSince(t, started)
	t.Logf("RECEIPT domain=large_retrieval cost=$%.6f models=%v elapsed=%s", usd, models, time.Since(started))
	if usd > orgCap {
		t.Fatalf("retrieval exceeded $%.2f: $%.4f", orgCap, usd)
	}
	for _, model := range models {
		if model != e2eModel {
			t.Fatalf("unexpected model %s", model)
		}
	}
}
