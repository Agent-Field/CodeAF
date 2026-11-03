package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

func workstreamSourceStoredRows(t *testing.T, collections *workspace.Store) map[string][][]any {
	t.Helper()
	out := map[string][][]any{}
	err := collections.Read(context.Background(), func(tx *sql.Tx, _ int) error {
		for _, table := range []string{"collections", "memberships", "workstreams", "workstream_jobs", "workstream_pages", "workstream_page_versions", "workstream_records", "workstream_goaheads", "workstream_plans"} {
			rows, err := tx.Query("SELECT * FROM " + table + " ORDER BY 1,2")
			if err != nil {
				return err
			}
			columns, err := rows.Columns()
			if err != nil {
				rows.Close()
				return err
			}
			for rows.Next() {
				values := make([]any, len(columns))
				pointers := make([]any, len(columns))
				for i := range values {
					pointers[i] = &values[i]
				}
				if err := rows.Scan(pointers...); err != nil {
					rows.Close()
					return err
				}
				out[table] = append(out[table], values)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func workstreamSourceSchemaVersion(t *testing.T, collections *workspace.Store) int {
	t.Helper()
	var version int
	if err := collections.Read(context.Background(), func(tx *sql.Tx, _ int) error { return tx.QueryRow("PRAGMA user_version").Scan(&version) }); err != nil {
		t.Fatal(err)
	}
	return version
}

// WP-12 (i): the real source.add path reads schema three without writing, then
// migrates atomically while retaining every earlier row except its required
// workstream updated_at touch. Every existing domain read keeps its behavior.
func TestWorkstreamSourceAddMigratesSchemaThreeAndPreservesRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "collections.db")
	collections, err := workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	s := New(collections, Options{Now: func() time.Time { return now }})
	w := createStream(t, s, "Schema three sources", workspace.Ref{Kind: workspace.FactoryJobKind, ID: "7", Factory: "f"})
	pages, err := s.Pages(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SavePage(ctx, pages[0].ID, 1, json.RawMessage(`{"type":"doc","content":[]}`), Attribution{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SetPlan(ctx, w.ID, 0, []Milestone{{ID: "m", Title: "Milestone"}}, []Wave{{N: 1, Items: []PlanItem{}}}, Attribution{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "decision", Text: "Keep rows.", Kept: true}); err != nil {
		t.Fatal(err)
	}
	proposal, _, err := s.ProposeRecord(ctx, RecordPropose{WorkstreamID: w.ID, Kind: "goahead", Text: "Allow wave.", GoAhead: &GoAheadProposal{Scope: "wave", Wave: 1, CapUSD: 1, EndsAt: now.Add(time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.KeepRecord(ctx, proposal.ID); err != nil {
		t.Fatal(err)
	}
	if err := collections.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("DROP TABLE workstream_sources; PRAGMA user_version=3")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := collections.Close(); err != nil {
		t.Fatal(err)
	}
	bytesBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	collections, err = workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer collections.Close()
	s = New(collections, Options{Now: func() time.Time { return now }})
	before := workstreamSourceStoredRows(t, collections)
	items, err := s.Sources(ctx, w.ID)
	if err != nil || items == nil || len(items) != 0 || workstreamSourceSchemaVersion(t, collections) != 3 {
		t.Fatalf("schema three sources: %v %v", items, err)
	}
	if _, err := s.Get(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := collections.Members(ctx, w.ID); err != nil || len(got) != 2 || got[0].Factory != "f" {
		t.Fatalf("schema three members: %+v %v", got, err)
	}
	if _, err := s.Pages(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PageHistory(ctx, pages[0].ID, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Records(ctx, w.ID, "", "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GoAheads(ctx, w.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Plan(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
	bytesAfter, err := os.ReadFile(path)
	if err != nil || string(bytesBefore) != string(bytesAfter) {
		t.Fatalf("schema three reads wrote: %v", err)
	}
	item, _, _, err := s.AddSource(ctx, w.ID, SourceAdd{Kind: SourceLink, URL: workstreamSourcePtr("https://example.com")})
	if err != nil || workstreamSourceSchemaVersion(t, collections) != workspace.WorkstreamSourcesSchemaVersion {
		t.Fatalf("migration: %+v %v", item, err)
	}
	after := workstreamSourceStoredRows(t, collections)
	// updated_at is the tenth column in the unchanged workstreams DDL. Source writes
	// advance that timestamp; migration itself changes no existing value.
	if after["workstreams"][0][9].(int64) <= before["workstreams"][0][9].(int64) {
		t.Fatal("source write did not touch updated_at")
	}
	after["workstreams"][0][9] = before["workstreams"][0][9]
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("migration changed existing rows: before=%v after=%v", before, after)
	}
	items, err = s.Sources(ctx, w.ID)
	if err != nil || len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("migrated sources: %v %v", items, err)
	}
}
