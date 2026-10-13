package shape

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/gateway"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/coder/websocket"
)

func TestOverlayRefreshOnlyBeforeScratchWrites(t *testing.T) {
	for _, op := range []string{"read", "create", "update", "delete", "failed write"} {
		t.Run(op, func(t *testing.T) {
			s, cfg, all, scope := setupDraft(t)
			d := openDraft(t, s, scope)
			ctx := context.Background()
			draftScope := scope
			draftScope.Data = svc.DataDraft
			before, err := all.Records.Query(ctx, draftScope, bridge.QueryParams{Type: "company"})
			if err != nil {
				t.Fatal(err)
			}
			target := before.Rows[0]
			switch op {
			case "create":
				_, err = all.Records.Create(ctx, draftScope, bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": "scratch"}})
			case "update":
				_, err = all.Records.Update(ctx, draftScope, bridge.UpdateRecordParams{Type: "company", ID: target.ID, Set: map[string]any{"name": "scratch"}})
			case "delete":
				row, createErr := all.Records.Create(ctx, scope, bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": "deletable"}})
				if createErr != nil {
					t.Fatal(createErr)
				}
				if _, err := s.Open(ctx, scope, bridge.Empty{}); err != nil {
					t.Fatal(err)
				}
				_, err = all.Records.Delete(ctx, draftScope, bridge.DeleteRecordParams{Type: "company", ID: row.ID})
			case "failed write":
				_, err = all.Records.Update(ctx, draftScope, bridge.UpdateRecordParams{Type: "company", IDs: []string{target.ID, "absent"}, Set: map[string]any{"name": "rolled back"}})
				needCode(t, err, bridge.CodeNotFound)
				err = nil
			}
			if err != nil {
				t.Fatal(err)
			}
			scratchBefore, err := all.Records.Query(ctx, draftScope, bridge.QueryParams{Type: "company"})
			if err != nil {
				t.Fatal(err)
			}
			real, err := all.Records.Create(ctx, scope, bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": "new real company"}})
			if err != nil {
				t.Fatal(err)
			}
			// The flag lives in SQLite, so it must survive a service restart too.
			restarted := New(cfg, all)
			if _, err := restarted.Open(ctx, scope, bridge.Empty{}); err != nil {
				t.Fatal(err)
			}
			after, err := all.Records.Query(ctx, draftScope, bridge.QueryParams{Type: "company"})
			if err != nil {
				t.Fatal(err)
			}
			clean := op == "read" || op == "failed write"
			found := false
			for _, row := range after.Rows {
				if row.ID == real.ID {
					found = true
				}
			}
			if found != clean {
				t.Fatalf("real refresh visible=%v, want %v", found, clean)
			}
			if !clean && !reflect.DeepEqual(scratchBefore, after) {
				t.Fatal("reopen discarded scratch writes")
			}
			db, err := openDatabase(filepath.Join(d.dir, "overlay.db"), "ro")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var dirty bool
			if err := db.QueryRow("SELECT dirty FROM _shape_scratch WHERE id=1").Scan(&dirty); err != nil || dirty == clean {
				t.Fatalf("scratch flag=%v, err=%v", dirty, err)
			}
		})
	}
}

func TestSQLiteSnapshotIncludesLiveWAL(t *testing.T) {
	s, cfg, all, scope := setupDraft(t)
	source := filepath.Join(cfg.Root, scope.App, "records.db")
	db, err := openDatabase(source, "rw")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	row := bridge.Record{ID: "company_wal", Type: "company", Version: 1, Fields: map[string]any{"name": "committed in WAL"}}
	data, _ := json.Marshal(row)
	if _, err := db.Exec("INSERT INTO records_company (id,record) VALUES (?,?)", row.ID, string(data)); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(source + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("test needs a live WAL: %v", err)
	}
	openDraft(t, s, scope)
	draftScope := scope
	draftScope.Data = svc.DataDraft
	got, err := all.Records.Get(context.Background(), draftScope, bridge.GetRecordParams{Type: "company", ID: row.ID})
	if err != nil || got.Fields["name"] != "committed in WAL" {
		t.Fatalf("snapshot missed WAL row: %+v, %v", got, err)
	}
}

func TestRule04GatewayDraftCRUDAndDiscard(t *testing.T) {
	s, cfg, all, scope := setupDraft(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server := httptest.NewServer(gateway.NewHandler(ctx, all))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	next := 0
	call := func(app, method string, params, result any) {
		t.Helper()
		next++
		raw, _ := json.Marshal(params)
		data, _ := json.Marshal(bridge.Envelope{ID: fmt.Sprint(next), Type: bridge.TypeCall, App: app, Method: method, Params: raw})
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			t.Fatal(err)
		}
		_, data, err = conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var envelope bridge.Envelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Error != nil || envelope.Type != bridge.TypeResult || envelope.ID != fmt.Sprint(next) {
			t.Fatalf("%s: %+v", method, envelope)
		}
		if result != nil {
			if err := json.Unmarshal(envelope.Result, result); err != nil {
				t.Fatal(err)
			}
		}
	}
	var before bridge.QueryResult
	call("crm", bridge.MethodRecordsQuery, bridge.QueryParams{Type: "company"}, &before)
	var opened bridge.Draft
	call("crm", bridge.MethodShapeOpen, bridge.Empty{}, &opened)
	if opened.State != "open" || opened.Base != 1 {
		t.Fatalf("open: %+v", opened)
	}
	var created, deleted bridge.Record
	call("crm@draft", bridge.MethodRecordsCreate, bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": "created scratch"}}, &created)
	call("crm@draft", bridge.MethodRecordsCreate, bridge.CreateRecordParams{Type: "company", Set: map[string]any{"name": "delete scratch"}}, &deleted)
	call("crm@draft", bridge.MethodRecordsUpdate, bridge.UpdateRecordParams{Type: "company", ID: before.Rows[0].ID, Set: map[string]any{"name": "edited scratch"}}, nil)
	call("crm@draft", bridge.MethodRecordsDelete, bridge.DeleteRecordParams{Type: "company", ID: deleted.ID}, nil)
	var realAfter, scratch bridge.QueryResult
	call("crm", bridge.MethodRecordsQuery, bridge.QueryParams{Type: "company"}, &realAfter)
	call("crm@draft", bridge.MethodRecordsQuery, bridge.QueryParams{Type: "company"}, &scratch)
	if !reflect.DeepEqual(before, realAfter) {
		t.Fatal("draft CRUD changed real rows or values")
	}
	if scratch.Total != before.Total+1 {
		t.Fatalf("scratch rows=%d, want %d", scratch.Total, before.Total+1)
	}
	names := map[string]string{}
	for _, row := range scratch.Rows {
		names[row.ID], _ = row.Fields["name"].(string)
	}
	if names[created.ID] != "created scratch" || names[before.Rows[0].ID] != "edited scratch" {
		t.Fatalf("overlay missing writes: %v", names)
	}
	if _, exists := names[deleted.ID]; exists {
		t.Fatal("scratch delete not applied")
	}
	assertDatabaseRows(t, filepath.Join(cfg.Root, "crm", "records.db"), before.Rows)
	assertDatabaseRows(t, filepath.Join(cfg.Root, "crm", "drafts", svc.Owner, "overlay.db"), scratch.Rows)
	call("crm", bridge.MethodShapeDiscard, bridge.Empty{}, nil)
	if _, err := os.Stat(filepath.Join(cfg.Root, "crm", "drafts", svc.Owner)); !os.IsNotExist(err) {
		t.Fatalf("discard left folder: %v", err)
	}
	call("crm", bridge.MethodRecordsQuery, bridge.QueryParams{Type: "company"}, &realAfter)
	if !reflect.DeepEqual(before, realAfter) {
		t.Fatal("discard changed real records")
	}
	var got bridge.ShapeGetResult
	call("crm", bridge.MethodShapeGet, bridge.Empty{}, &got)
	if got.Draft != nil {
		t.Fatal("get after discard is not null")
	}
	if _, err := s.draftFor(scope); err == nil {
		t.Fatal("discard kept in-memory draft")
	}
}

func assertDatabaseRows(t *testing.T, name string, want []bridge.Record) {
	t.Helper()
	db, err := openDatabase(name, "ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM records_company").Scan(&count); err != nil || count != len(want) {
		t.Fatalf("%s count=%d, want=%d, %v", name, count, len(want), err)
	}
	for _, row := range want {
		var raw string
		err := db.QueryRow("SELECT record FROM records_company WHERE id=?", row.ID).Scan(&raw)
		if err == sql.ErrNoRows {
			t.Fatalf("%s missing %s", name, row.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
		var got bridge.Record
		if err := json.Unmarshal([]byte(raw), &got); err != nil || !reflect.DeepEqual(got, row) {
			t.Fatalf("%s changed row: %+v, %v", name, got, err)
		}
	}
}
