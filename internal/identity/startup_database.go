package identity

import (
	"database/sql"
	"strings"

	"github.com/Agent-Field/codeaf/internal/sqliteuri"
	"github.com/Agent-Field/codeaf/internal/store"
)

// databaseWork reads only named schemas. A new table is evidence we do not know
// what can be lost, even when that table currently has no rows.
func databaseWork(path string, predicates map[string]string, mergeMemory bool) (bool, error) {
	if sound, err := soundDatabase(path); !sound || err != nil {
		return true, err
	}
	uri, err := sqliteuri.FromPath(path)
	if err != nil {
		return true, err
	}
	uri.RawQuery = "mode=ro"
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return true, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return true, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return true, err
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return true, err
	}
	for _, name := range names {
		// Joinable has always merged the graph's memories instead of discarding them.
		if mergeMemory && (name == "memories" || strings.HasPrefix(name, "memory_")) {
			continue
		}
		predicate, known := predicates[name]
		if !known {
			return true, nil
		}
		var n int
		query := `SELECT count(*) FROM "` + strings.ReplaceAll(name, `"`, `""`) + `" WHERE ` + predicate
		if err := db.QueryRow(query).Scan(&n); err != nil {
			return true, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// startupTables names every ordinary table, with all its rows treated as work.
func startupTables(names ...string) map[string]string {
	out := map[string]string{"sqlite_sequence": "0"}
	for _, name := range names {
		out[name] = "1"
	}
	return out
}

// addStartupIndexes names only the FTS tables derived from checked source tables.
// SQLite seeds their configuration and data blocks even before there is content.
func addStartupIndexes(out map[string]string, bases ...string) {
	for _, base := range bases {
		for _, suffix := range []string{"", "_data", "_idx", "_content", "_docsize", "_config"} {
			out[base+suffix] = "0"
		}
	}
}

func graphStartupTables() map[string]string {
	out := startupTables("events", "nodes", "edges", "messages", "commands", "sessions", "agent_questions", "usage", "usage_turns", "transcript", "surprises", "task_budgets", "role_bindings", "self_receipts", "self_inquiry_lines", "services", "facts", "question_practices", "scope_aliases", "retrospective_watermark", "resident_watermarks", "meta_parameters")
	for name, predicate := range workRows {
		out[name] = predicate
	}
	// Store.ensureSpine writes this root event before the person starts work.
	out["events"] = "NOT coalesce(kind='" + string(store.EventSpineCreated) + "' AND node_id='" + graphRoot + "' AND json_extract(payload,'$.id')='" + graphRoot + "', 0)"
	// These journal rows belong to the memories that Joinable has always merged.
	// Every other event kind remains work, including an unknown future kind.
	memoryKinds := []store.EventKind{store.EventMemoryAdd, store.EventMemoryUpdate, store.EventMemorySupersede, store.EventMemoryForget, store.EventMemoryRestore, store.EventMemorySkipped, store.EventMemoryWriteFailed, store.EventMemoryRehomed, store.EventMemoryMoved, store.EventMemoryReset, store.EventMemoryInteraction,
		// Older graphs retain these retired memory-retrieval telemetry events.
		"memory_ranking", "memory_always", "contextual_evidence",
	}
	quoted := make([]string, len(memoryKinds))
	for i, kind := range memoryKinds {
		quoted[i] = "'" + string(kind) + "'"
	}
	out["events"] += " AND coalesce(kind NOT IN (" + strings.Join(quoted, ",") + "),1)"
	addStartupIndexes(out, "messages_fts", "services_fts", "facts_fts", "graph_fts")
	return out
}

func automationStartup(path string) (bool, error) {
	out := startupTables("counter", "automations", "runs")
	// automation.Open seeds counter id 1; clock sequencing advances it without
	// creating an automation. The automations and runs tables decide actual use.
	out["counter"] = "id <> 1 OR seq < 0"
	// A run's asked mark is work; the walk revision is seeded on open and only
	// follows the automations table, which already decides use.
	out["run_asked"] = "1"
	out["origin_revision"] = "0"
	return databaseWork(path, out, false)
}

func knowledgeStartup(path string) (bool, error) {
	out := startupTables("state", "events", "publications", "receipts", "overrides", "pages", "history_errors", "cursors", "proposals", "source_windows", "claim_keys", "proposal_sources", "watch_terms", "watches", "wakes")
	// Vault.runPass enables an empty inventory and records its waiting job. No
	// other state document, error, source, or person-edited configuration is startup.
	out["state"] = `NOT coalesce(json_valid(payload) AND (
 (key='config' AND json_extract(payload,'$.enabled')=1 AND json_extract(payload,'$.schema')=1
 AND NOT EXISTS(SELECT 1 FROM json_each(payload) WHERE key NOT IN ('enabled','schema','created_at')))
 OR (key='job' AND json_extract(payload,'$.phase')='waiting'
 AND json_extract(payload,'$.backfill')=0 AND json_extract(payload,'$.sources')=0
 AND json_extract(payload,'$.problems')=0 AND json_extract(payload,'$.error')=''
 AND NOT EXISTS(SELECT 1 FROM json_each(payload) WHERE key NOT IN ('backfill','chats','error','imported','inventory_hash','phase','problems','sources')))), 0)`
	// Vault.runPass saves offsets even when the history reader admits no sources.
	// Every source and publication table above must still be empty.
	out["cursors"] = "skip <> 0 OR offset < 0"
	addStartupIndexes(out, "page_fts")
	return databaseWork(path, out, false)
}
