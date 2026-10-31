package identity

import (
	"database/sql"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
	"path/filepath"
	"strings"
)

// Joinable accepts startup state plus personal memory and preferences, which
// merge into the new fleet (C30.4). Chats and unknown files still require the
// explicit replacement door; Pristine continues to mean genuinely unused.
func Joinable(home string) (bool, error) {
	rulesCopy := rules{}
	for key, judge := range startupRules {
		rulesCopy[key] = judge
	}
	rulesCopy["graph.db"] = memoryOnlyGraph
	for _, key := range []string{"provider-keys.ledger", "preferences-sync.ledger", "memory-sync.ledger", "teams-sync.ledger", "teams-traffic-sync.ledger", "state-sync.lock"} {
		rulesCopy[key] = never
	}
	used, err := within(rulesCopy)(home)
	return !used && err == nil, err
}

func memoryOnlyGraph(path string) (bool, error) {
	if sound, err := soundDatabase(path); !sound || err != nil {
		return true, err
	}
	uri, err := sqliteuri.FromPath(filepath.Clean(path))
	if err != nil {
		return true, err
	}
	uri.RawQuery = "mode=ro"
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return true, err
	}
	defer db.Close()
	tables, err := graphTables(db)
	if err != nil {
		return true, err
	}
	for _, table := range tables {
		if table == "memories" || strings.HasPrefix(table, "memory_") {
			continue
		}
		if used, err := holdsWork(db, table); used || err != nil {
			return true, err
		}
	}
	return false, nil
}
