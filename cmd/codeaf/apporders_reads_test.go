package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/sqlitefile"
	"modernc.org/libc"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Trace the real store's SQL at its connection seam, scoped to a disposable
// database. Like plandbtest's commit gate, this uses the pinned driver's handle;
// no production query counters or alternate projection implementation are needed.
var orderSQLTraces = struct {
	sync.Mutex
	next uintptr
	byID map[uintptr]*orderSQLTrace
}{byID: map[uintptr]*orderSQLTrace{}}

type orderSQLTrace struct {
	dsn     string
	queries []string
}

func init() {
	sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, dsn string) error {
		orderSQLTraces.Lock()
		defer orderSQLTraces.Unlock()
		for id, trace := range orderSQLTraces.byID {
			if !strings.HasPrefix(dsn, trace.dsn) {
				continue
			}
			handle := reflect.ValueOf(conn).Elem()
			tls := (*libc.TLS)(unsafe.Pointer(handle.FieldByName("tls").Pointer()))
			callback := orderSQLStatement
			pointer := *(*uintptr)(unsafe.Pointer(&callback))
			sqlite3.Xsqlite3_trace_v2(tls, uintptr(handle.FieldByName("db").Uint()), sqlite3.SQLITE_TRACE_STMT, pointer, id)
		}
		return nil
	})
}

func orderSQLStatement(_ *libc.TLS, _ uint32, id, _, query uintptr) int32 {
	orderSQLTraces.Lock()
	defer orderSQLTraces.Unlock()
	if trace := orderSQLTraces.byID[id]; trace != nil {
		trace.queries = append(trace.queries, libc.GoString(query))
	}
	return 0
}

func tracedOrdersBackend(t *testing.T) (*appBackend, *appRecorder, *automation.Store, *orderSQLTrace) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "automations")
	trace := &orderSQLTrace{dsn: strings.TrimSuffix(sqlitefile.URI(filepath.Join(root, automation.DatabaseName), "x"), "x")}
	orderSQLTraces.Lock()
	orderSQLTraces.next++
	id := orderSQLTraces.next
	orderSQLTraces.byID[id] = trace
	orderSQLTraces.Unlock()
	t.Cleanup(func() {
		orderSQLTraces.Lock()
		delete(orderSQLTraces.byID, id)
		orderSQLTraces.Unlock()
	})
	store, err := automation.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rec := newAppRecorder()
	backend := newFakeBackend(rec, nil)
	backend.orderStore = func() *automation.Store { return store }
	t.Cleanup(func() { backend.Close() })
	return backend, rec, store, trace
}

func (trace *orderSQLTrace) take() []string {
	orderSQLTraces.Lock()
	defer orderSQLTraces.Unlock()
	queries := trace.queries
	trace.queries = nil
	return queries
}

// Contract A: 100 warm orders take three SQL queries, preserving filtering,
// row order, latest completed runs and latest asking ids, including duplicates.
func TestAppOrdersAThreeQueriesPreserveRows(t *testing.T) {
	backend, _, store, trace := tracedOrdersBackend(t)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	store.SetClock(func() time.Time { return now })
	backend.now = func() time.Time { return now }
	db, err := sql.Open("sqlite", sqlitefile.URI(filepath.Join(store.Root(), automation.DatabaseName), ""))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := range 100 {
		a := automation.Automation{ID: fmt.Sprintf("order-%03d", i), Title: fmt.Sprintf("order %03d", i), Workspace: store.Root(), Schedule: automation.Schedule{Every: "1h", Zone: "UTC"}, Action: automation.Action{Do: "check"}, Origin: automation.Origin{SessionID: "other"}}
		if i%2 == 0 {
			a.Origin.SessionID = "wanted"
		}
		if i == 2 {
			a.Origin.SessionID, a.Origin.Transcript = "wanted", "/somewhere/transcript.jsonl"
		}
		if _, err := store.Create(a); err != nil {
			t.Fatal(err)
		}
		for j := range i % 3 {
			if _, err := db.Exec(`INSERT INTO runs (automation_id, due_ms, why, phase) VALUES (?, ?, 'now', 'asking')`, a.ID, now.UnixMilli()+int64(j)); err != nil {
				t.Fatal(err)
			}
		}
		for j := range 2 {
			if _, err := db.Exec(`INSERT INTO runs (automation_id, due_ms, why, phase, outcome, line) VALUES (?, ?, 'slot', 'over', 'done', ?)`, a.ID, now.UnixMilli()+int64(j), fmt.Sprintf("result %d", j)); err != nil {
				t.Fatal(err)
			}
		}
	}
	all, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	asking, err := store.Asking()
	if err != nil {
		t.Fatal(err)
	}
	latest := map[string]automation.Run{}
	for _, run := range asking {
		latest[run.AutomationID] = run
	}
	for _, agent := range []string{"", "wanted", "absent"} {
		trace.take()
		orders, err := backend.Orders(agent)
		if err != nil {
			t.Fatal(err)
		}
		queries := trace.take()
		if len(queries) != 3 {
			t.Errorf("agent %q: %d SQL queries, want 3", agent, len(queries))
		}
		var want []appwire.Order
		for _, a := range all {
			view := orderView(nil, a, now)
			if agent != "" && view.Agent != agent {
				continue
			}
			if run, ok := latest[a.ID]; ok {
				v := orderRunView(run)
				view.Asking = &v
			}
			want = append(want, view)
		}
		if !reflect.DeepEqual(orders, want) {
			t.Fatalf("agent %q: order rows or latest approval ids changed", agent)
		}
	}
}

// Contract A: an answer arriving at projection time changes only the next
// snapshot. Closing one run never closes another order's approval.
func TestAppOrdersAConcurrentAnswerKeepsSnapshotAndExactClosure(t *testing.T) {
	backend, rec, store := ordersBackend(t)
	var runs []automation.Run
	for range 2 {
		a := proposedOrder(t, store)
		if _, err := store.Ratify(a.ID); err != nil {
			t.Fatal(err)
		}
		run, err := store.QueueAsking(a.ID)
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, run)
	}
	open := map[int64]appwire.Ask{}
	if !backend.sayOrderAsks(store, open) || len(open) != 2 {
		t.Fatal("both approvals must open")
	}
	now := backend.now()
	var once sync.Once
	backend.now = func() time.Time {
		once.Do(func() {
			done := make(chan error, 1)
			go func() { _, err := store.Answer(runs[0].ID, automation.ReplySkip); done <- err }()
			if err := <-done; err != nil {
				t.Error(err)
			}
		})
		return now
	}
	orders, err := backend.Orders("")
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range orders {
		if order.Asking == nil {
			t.Errorf("in-flight snapshot lost approval for %s", order.ID)
		}
	}
	orders, err = backend.Orders("")
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range orders {
		if order.ID == runs[0].AutomationID && order.Asking != nil {
			t.Fatal("next snapshot retained answered approval")
		}
	}
	if !backend.sayOrderAsks(store, open) || len(open) != 1 || open[runs[1].ID].ID != appOrderAskID(runs[1].ID) {
		t.Fatal("wrong approval closed")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	closed := 0
	for i, event := range rec.events {
		if event == appwire.EventAskClosed {
			closed++
			if rec.data[i].(appwire.AskClosed).ID != appOrderAskID(runs[0].ID) {
				t.Fatal("closed another run")
			}
		}
	}
	if closed != 1 {
		t.Fatalf("closed %d approvals, want 1", closed)
	}
}

// Contract A: each asks/watch projection reads Asking once, and a failed read
// must preserve open approvals and leave the refresh stamp available to retry.
func TestAppOrdersAAsksAndRefreshReuseAsking(t *testing.T) {
	backend, _, store, trace := tracedOrdersBackend(t)
	for range 3 {
		a := proposedOrder(t, store)
		a.Origin.SessionID = "wanted"
		if _, err := store.Update(a); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Ratify(a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.QueueAsking(a.ID); err != nil {
			t.Fatal(err)
		}
	}
	countAsking := func() {
		t.Helper()
		count := 0
		for _, query := range trace.take() {
			if strings.Contains(query, "WHERE phase = ? ORDER BY id") {
				count++
			}
		}
		if count != 1 {
			t.Errorf("read Asking %d times, want 1", count)
		}
	}
	trace.take()
	if asks := backend.orderAsksOf("wanted"); len(asks) != 3 {
		t.Fatalf("got %d asks", len(asks))
	}
	countAsking()
	stamp := ""
	open := map[int64]appwire.Ask{}
	backend.ordersRefresh(store, &stamp, 0, map[string]string{}, open)
	countAsking()
	if len(open) != 3 {
		t.Fatal("refresh did not open all approvals")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	stamp = "retry"
	backend.ordersRefresh(store, &stamp, 0, map[string]string{}, open)
	if stamp != "retry" || len(open) != 3 {
		t.Fatal("failed read consumed stamp or closed approvals")
	}
}

// Contract A: an Asking-only query failure omits optional asking state on a
// list, but neither closes asks nor advances a watch past that failed snapshot.
func TestAppOrdersAAskingFailurePreservesApprovals(t *testing.T) {
	backend, _, store := ordersBackend(t)
	a := proposedOrder(t, store)
	if _, err := store.Ratify(a.ID); err != nil {
		t.Fatal(err)
	}
	run, err := store.QueueAsking(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	open := map[int64]appwire.Ask{}
	if !backend.sayOrderAsks(store, open) {
		t.Fatal("approval did not open")
	}
	db, err := sql.Open("sqlite", sqlitefile.URI(filepath.Join(store.Root(), automation.DatabaseName), ""))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The fixture fails only decoding an asking row; List's completed-run
	// query remains healthy. No production hooks alter error behavior.
	if _, err := db.Exec(`ALTER TABLE runs RENAME TO saved_runs;
CREATE VIEW runs AS SELECT id, automation_id, due_ms, why, phase, started_ms,
finished_ms, outcome, CASE WHEN phase = 'asking' THEN abs(-9223372036854775808)
ELSE line END AS line, detail, usd, transcript, seq FROM saved_runs`); err != nil {
		t.Fatal(err)
	}
	orders, err := backend.Orders("")
	if err != nil || len(orders) != 1 || orders[0].Asking != nil {
		t.Fatalf("optional failure = %+v, %v", orders, err)
	}
	if backend.sayOrderAsks(store, open) || open[run.ID].ID != appOrderAskID(run.ID) {
		t.Fatal("failed asking read closed the approval")
	}
	stamp := "retry"
	cursor, changed := backend.ordersRefresh(store, &stamp, 0, map[string]string{}, open)
	if !changed || cursor != 0 || stamp != "retry" || len(open) != 1 {
		t.Fatal("failed asking snapshot consumed watch state")
	}
}
