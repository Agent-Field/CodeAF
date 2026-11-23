// Package plandbtest makes SQLite itself refuse one plan-store COMMIT, for
// tests of what a caller answers when the change may or may not be on the disk.
//
// IT IS FOR _test.go FILES ONLY. Importing it registers a connection hook with
// the SQLite driver; a test in this package fails the build when a file that
// ships imports it ([TestNoShippedFileImportsTheCommitGate]).
package plandbtest

import (
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/sqlitefile"
	"modernc.org/libc"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Gate makes SQLite ITSELF refuse the COMMIT of the one transaction that
// writes the armed words into one table — a note carrying key ([GateNote]),
// or a task row a text column of which starts with them ([GateTask]) —
// on the write connections of the plan store at one path. Nothing in the production store knows it exists: the plan store
// commits with a bare tx.Commit, and the refusal is SQLite's own commit hook
// answering nonzero, which turns that COMMIT into a ROLLBACK and returns
// SQLITE_CONSTRAINT_COMMITHOOK from tx.Commit — the one step after the write that
// may fail. Every step before it (the lock, the load, the change, the save) has
// already succeeded, or the hook would never have been asked.
//
// The pinned driver (modernc.org/sqlite v1.29.1) has RegisterConnectionHook but
// not the RegisterCommitHook later versions export, so the hooks are installed
// through the same C API that method wraps (sqlite3_commit_hook,
// sqlite3_preupdate_hook and sqlite3_rollback_hook in modernc.org/sqlite/lib),
// on the connection handle the connection hook is given.
type Gate struct {
	// dsn is the write handle's DSN prefix for the one store path this gate
	// serves. A connection whose DSN does not start with it is never hooked.
	dsn string
	// table is the one table whose row write can mark the open transaction.
	table string
	// key is the words a written text column must equal, or start with when
	// prefix is set, to mark it: a note's client id, a brief's own words.
	key    string
	prefix bool
	// armed is false once the test disarms; a marked insert needs it set.
	armed atomic.Bool
	// marked is set by the pre-update hook on the armed insert and spent by the
	// commit hook or cleared by the rollback hook, so it never outlives one
	// transaction.
	marked atomic.Bool
	// refused counts the COMMITs this gate has turned into rollbacks.
	refused atomic.Int32
}

// commitGates is every live gate by the small integer each connection's hooks
// carry as their argument. A callback holds only that integer, never a gate or
// a test: a gate the test has cleaned up is gone from the map, and a hook that
// outlives it on a pooled connection finds nothing and does nothing.
var commitGates = struct {
	sync.Mutex
	next uintptr
	byID map[uintptr]*Gate
}{byID: map[uintptr]*Gate{}}

// THE CONNECTION HOOK IS REGISTERED BEFORE ANY TEST RUNS. The driver appends
// it to a slice every Open reads without a lock, so registering it in the middle
// of a test would race whatever connection another test, or a store an earlier
// one left behind, was opening at that moment.
func init() { sqlite.RegisterConnectionHook(hookCommitGate) }

// GateNote scopes a gate to the plan store at path and arms it for the
// note whose client id is key. It must run BEFORE the store at path is first
// opened: a connection is hooked when it opens, and the store keeps its one
// write connection for its whole life.
func GateNote(t testing.TB, path, key string) *Gate {
	t.Helper()
	return armCommitGate(t, &Gate{dsn: commitGateDSN(path), table: "notes", key: key})
}

// GateTask is [GateNote] for a task row: it arms on the write of
// a task whose text column starts with words, which is what a brief's
// amendment (prepended to the description) writes.
func GateTask(t testing.TB, path, words string) *Gate {
	t.Helper()
	return armCommitGate(t, &Gate{dsn: commitGateDSN(path), table: "tasks", key: words, prefix: true})
}

// The prefix is the write handle's own URI for path (plandb.StoreURI), query
// separator included, so a path with %, # or ? is gated at the name it opens.
func commitGateDSN(path string) string { return strings.TrimSuffix(sqlitefile.URI(path, "x"), "x") }

func armCommitGate(t testing.TB, gate *Gate) *Gate {
	t.Helper()
	gate.armed.Store(true)
	commitGates.Lock()
	commitGates.next++
	id := commitGates.next
	commitGates.byID[id] = gate
	commitGates.Unlock()
	t.Cleanup(func() {
		gate.armed.Store(false)
		commitGates.Lock()
		delete(commitGates.byID, id)
		commitGates.Unlock()
	})
	return gate
}

// Disarm lets every later commit through, the armed words' included.
func (g *Gate) Disarm() { g.armed.Store(false) }

// Arm makes the gate refuse the armed words' commit again.
func (g *Gate) Arm() { g.armed.Store(true) }

// Refused is how many COMMITs this gate has turned into rollbacks.
func (g *Gate) Refused() int32 { return g.refused.Load() }

func commitGateByID(id uintptr) *Gate {
	commitGates.Lock()
	defer commitGates.Unlock()
	return commitGates.byID[id]
}

// hookCommitGate is the connection hook. It runs for every connection the test
// binary opens, and hooks only a store's WRITE handle (BEGIN IMMEDIATE, the one
// that commits) at a path a live gate was scoped to.
func hookCommitGate(conn sqlite.ExecQuerierContext, dsn string) error {
	if !strings.Contains(dsn, "_txlock=immediate") {
		return nil
	}
	var id uintptr
	commitGates.Lock()
	for candidate, gate := range commitGates.byID {
		if strings.HasPrefix(dsn, gate.dsn) {
			id = candidate
		}
	}
	commitGates.Unlock()
	if id == 0 {
		return nil
	}
	handle := reflect.ValueOf(conn).Elem()
	db := uintptr(handle.FieldByName("db").Uint())
	tls := (*libc.TLS)(unsafe.Pointer(handle.FieldByName("tls").Pointer()))
	sqlite3.Xsqlite3_preupdate_hook(tls, db, cFunc(gatePreUpdate), id)
	sqlite3.Xsqlite3_commit_hook(tls, db, cFunc(gateCommit), id)
	sqlite3.Xsqlite3_rollback_hook(tls, db, cFunc(gateRollback), id)
	return nil
}

// cFunc is the driver's own conversion of a top-level Go function into the
// pointer its C callbacks are called through (cFuncPointer in modernc.org/sqlite).
func cFunc[T any](f T) uintptr { return *(*uintptr)(unsafe.Pointer(&struct{ f T }{f})) }

// gatePreUpdate marks the transaction when it writes a row of the gate's table
// any of whose text columns matches the armed words. It reads the new row
// through the pre-update API and runs no SQL.
func gatePreUpdate(tls *libc.TLS, id, db uintptr, op int32, _, table uintptr, _, _ int64) {
	gate := commitGateByID(id)
	if gate == nil || !gate.armed.Load() || (op != sqlite3.SQLITE_INSERT && op != sqlite3.SQLITE_UPDATE) || libc.GoString(table) != gate.table {
		return
	}
	slot := tls.Alloc(int(unsafe.Sizeof(uintptr(0))))
	defer tls.Free(int(unsafe.Sizeof(uintptr(0))))
	for column := int32(0); column < sqlite3.Xsqlite3_preupdate_count(tls, db); column++ {
		if sqlite3.Xsqlite3_preupdate_new(tls, db, column, slot) != sqlite3.SQLITE_OK {
			continue
		}
		// The slot is the driver's own C memory, never the Go heap; reading
		// it through unsafe.Add is how a uintptr address becomes a pointer
		// without the conversion vet rightly refuses for Go memory.
		value := *(*uintptr)(unsafe.Add(unsafe.Pointer(nil), slot))
		if sqlite3.Xsqlite3_value_type(tls, value) == sqlite3.SQLITE_TEXT && gate.matches(libc.GoString(sqlite3.Xsqlite3_value_text(tls, value))) {
			gate.marked.Store(true)
			return
		}
	}
}

func (g *Gate) matches(text string) bool {
	if g.prefix {
		return strings.HasPrefix(text, g.key)
	}
	return text == g.key
}

// gateCommit answers nonzero for a marked transaction, which is SQLite's own
// refusal of the COMMIT; the transaction is rolled back.
func gateCommit(_ *libc.TLS, id uintptr) int32 {
	gate := commitGateByID(id)
	if gate == nil || !gate.marked.Swap(false) {
		return 0
	}
	gate.refused.Add(1)
	return 1
}

func gateRollback(_ *libc.TLS, id uintptr) {
	if gate := commitGateByID(id); gate != nil {
		gate.marked.Store(false)
	}
}
