package store

import (
	"database/sql"
	"errors"
	"sync"
)

// The store's driver is modernc's pure-Go SQLite, and handing it a statement as
// a string means it tokenizes, parses and plans that string again — every time.
// For a read taken once per open that cost is invisible. For the handful of
// reads the resident takes at 2Hz forever, and for the mailbox poll every leaf
// takes at every turn boundary, it is most of what the read costs at all: the
// planner work does not shrink because the answer is empty.
//
// So the hottest read statements are prepared once and kept. The cache is keyed
// by the SQL text rather than by a field per statement because several of these
// reads compose their SQL from a fixed set of clauses — [Store.queryNodesLimitOrdered]
// is one statement per call site — and a map keyed by the finished text names
// exactly those variants without anyone having to enumerate them. The set is
// bounded by the code that builds it, so the map cannot grow without a new call
// site being written.
//
// database/sql owns the hard part: a *sql.Stmt re-prepares itself on whichever
// pooled connection it lands on, so nothing here has to reason about connection
// lifetime. A statement that will not prepare is not an error — the caller falls
// back to the unprepared path and gets the same answer more slowly.
type statementCache struct {
	mu    sync.RWMutex
	ready map[string]*sql.Stmt
}

// prepared returns the cached statement for query, preparing it on first sight.
// A nil result means "use the database directly", which is always correct.
func (s *Store) prepared(query string) *sql.Stmt {
	if s == nil || s.db == nil {
		return nil
	}
	s.statements.mu.RLock()
	statement, ok := s.statements.ready[query]
	s.statements.mu.RUnlock()
	if ok {
		return statement
	}
	prepared, err := s.db.Prepare(query)
	if err != nil {
		return nil
	}
	s.statements.mu.Lock()
	defer s.statements.mu.Unlock()
	if s.statements.ready == nil {
		s.statements.ready = make(map[string]*sql.Stmt)
	}
	// Two readers may have prepared the same text at once. The first one to
	// arrive here owns the cache entry and the loser closes its own copy, so the
	// map holds exactly one statement per text for the life of the handle.
	if existing, raced := s.statements.ready[query]; raced {
		_ = prepared.Close()
		return existing
	}
	s.statements.ready[query] = prepared
	return prepared
}

// queryPrepared is db.Query through the statement cache.
func (s *Store) queryPrepared(query string, args ...any) (*sql.Rows, error) {
	if statement := s.prepared(query); statement != nil {
		return statement.Query(args...)
	}
	return s.db.Query(query, args...)
}

// queryRowPrepared is db.QueryRow through the statement cache.
func (s *Store) queryRowPrepared(query string, args ...any) *sql.Row {
	if statement := s.prepared(query); statement != nil {
		return statement.QueryRow(args...)
	}
	return s.db.QueryRow(query, args...)
}

// closeStatements releases every cached statement. It runs before the database
// itself closes: a statement outliving its handle is the one way this cache can
// leak a connection.
func (s *Store) closeStatements() error {
	if s == nil {
		return nil
	}
	s.statements.mu.Lock()
	defer s.statements.mu.Unlock()
	var failures []error
	for query, statement := range s.statements.ready {
		if err := statement.Close(); err != nil {
			failures = append(failures, err)
		}
		delete(s.statements.ready, query)
	}
	return errors.Join(failures...)
}
