package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/sqlitefile"
	"github.com/Agent-Field/codeaf/internal/workspace"
)

// A collection's reference makes a bare conversation intentional. Read-only mode
// also protects against the store disappearing between Stat and the SQLite open.
func collectionKeepsConversation(dir string) bool {
	path := home.Join("v3", "collections.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false
	} else if err != nil {
		return true
	}
	db, err := sql.Open("sqlite", sqlitefile.URI(path, "mode=ro&_pragma=busy_timeout(100)"))
	if err != nil {
		return true
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var kept bool
	// These membership columns exist in every collections schema. EXISTS avoids
	// loading collection metadata or interpreting any newer collection features.
	err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memberships m
 JOIN collections c ON c.id=m.collection_id WHERE m.kind=? AND m.ref_id=? AND m.session_id='')`,
		workspace.ConversationKind, filepath.Base(filepath.Clean(dir))).Scan(&kept)
	return kept || err != nil
}
