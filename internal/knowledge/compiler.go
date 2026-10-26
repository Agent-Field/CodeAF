package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

func knowledgeModel(profile string) string { return config.KnowledgeModelAt(profile) }
func (v *Vault) compile(ctx context.Context, key string, force bool) (err error) {
	if !v.Enabled() {
		return ErrDisabled
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(v.root, ".compiler.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		err = filelock.Lock(lock, true, true)
		if err == nil {
			break
		}
		if !filelock.IsBusy(err) {
			return err
		}
		if err = waitBuild(ctx, 50*time.Millisecond); err != nil {
			return err
		}
	}
	defer filelock.Unlock(lock)
	db, err := v.database()
	if err != nil {
		return err
	}
	defer db.Close()
	model := compilerModel{ctx: ctx, root: v.root, key: key, model: knowledgeModel(v.profile), db: db}
	defer func() {
		message := ""
		if err != nil {
			message = "Knowledge compilation failed; pending sources and completed progress are retained."
		}
		needsUpdate := err != nil
		if !needsUpdate {
			learning, loadErr := stateDocument(db, "learning")
			if loadErr != nil {
				err = loadErr
				needsUpdate = true
			} else {
				needsUpdate = hasPendingRefinement(learning)
			}
		}
		statusErr := v.compilerStatus(db, model.model, message, needsUpdate)
		if err == nil {
			err = statusErr
		}
	}()
	if err = v.compilerStatus(db, model.model, "", true); err != nil {
		return err
	}
	boundary, err := stateDocument(db, "organization_boundary")
	if err != nil {
		return err
	}
	through := int64(number(boundary["through"]))
	if through == 0 {
		if err = db.QueryRow("SELECT coalesce(max(rowid),0) FROM events WHERE partition='profile'").Scan(&through); err != nil {
			return err
		}
		if err = saveState(db, "organization_boundary", document{"through": through}); err != nil {
			return err
		}
	}
	vault, err := v.compilePending(db, "profile", model, force, through)
	if err != nil {
		return err
	}
	// First-build organization needs the complete admitted history, rather than
	// a tree whose page boundaries reflect extraction batches.
	var pending int
	if err = db.QueryRow("SELECT count(*) FROM events WHERE partition='profile' AND rowid<=? AND status IN ('pending','revisit')", through).Scan(&pending); err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}
	projection, err := stateDocument(db, "projection")
	if err != nil {
		return err
	}
	digest := identity(vault)
	learning, err := stateDocument(db, "learning")
	if err != nil {
		return err
	}
	if textValue(projection["input_hash"]) == digest && !hasPendingRefinement(learning) {
		_, err = db.Exec("DELETE FROM state WHERE key='organization_boundary'")
		return err
	}
	previousLearning := clone(learning)
	learning, err = extendLearning(vault, learning, model)
	if err != nil {
		return err
	}
	learning, err = refineLearning(vault, learning, previousLearning, model)
	if err != nil {
		return err
	}
	registry, err := stateDocument(db, "topics")
	if err != nil {
		return err
	}
	registry, err = reconcileTopics(learning, registry, nil)
	if err != nil {
		return err
	}
	published, err := publishTopics(vault, learning, registry)
	if err != nil {
		return err
	}
	published, err = curatePublishedGuides(published, model)
	if err != nil {
		return err
	}
	return v.commitPublication(published, learning, registry, digest)
}
func (v *Vault) compilerStatus(db *sql.DB, model, message string, needsUpdate bool) error {
	var cost float64
	if err := db.QueryRow("SELECT coalesce(sum(cost),0) FROM receipts").Scan(&cost); err != nil {
		return err
	}
	return saveState(db, "status", document{"model": model, "last_error": message, "needs_update": needsUpdate, "cost_usd": cost})
}
func (v *Vault) compilePending(db *sql.DB, partition string, model compilerModel, force bool, through int64) (document, error) {
	previous := emptyCompilation()
	revision := 0
	var raw string
	err := db.QueryRow("SELECT payload,revision FROM publications WHERE partition=?", partition).Scan(&raw, &revision)
	if err == nil {
		if err = json.Unmarshal([]byte(raw), &previous); err != nil {
			return nil, err
		}
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	rows, err := db.Query("SELECT e.id,e.payload,e.arrived,e.status,coalesce(w.length,0) FROM events e LEFT JOIN source_windows w ON w.partition=e.partition AND w.id=e.id WHERE e.partition=? AND e.rowid<=? AND e.status IN ('pending','revisit') ORDER BY e.arrived,e.id LIMIT 64", partition, through)
	if err != nil {
		return nil, err
	}
	selected := []document{}
	ids := []string{}
	revisited := []string{}
	size := 0
	oldest, newest := float64(0), float64(0)
	for rows.Next() {
		var id, payload, status string
		var arrived float64
		var windowLength int
		if err = rows.Scan(&id, &payload, &arrived, &status, &windowLength); err != nil {
			rows.Close()
			return nil, err
		}
		var source document
		if err = json.Unmarshal([]byte(payload), &source); err != nil {
			rows.Close()
			return nil, err
		}
		n := utf8.RuneCountInString(textValue(source["text"]))
		if windowLength > 0 {
			n = windowLength
		}
		if size+n > 24000 {
			break
		}
		if len(selected) == 0 {
			oldest = arrived
		}
		newest = arrived
		size += n
		selected = append(selected, source)
		ids = append(ids, id)
		if status == "revisit" {
			revisited = append(revisited, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return previous, nil
	}
	if !force && size < 24000 && float64(time.Now().UnixNano())/1e9 < min(newest+30, oldest+120) {
		return previous, nil
	}
	for _, id := range ids {
		if _, err = db.Exec("UPDATE events SET attempts=attempts+1 WHERE partition=? AND id=?", partition, id); err != nil {
			return nil, err
		}
	}
	vault, err := updateCompilation(selected, previous, revisited, model)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT OR REPLACE INTO publications VALUES(?,?,?)", partition, mustJSON(vault), revision+1); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err = tx.Exec("UPDATE events SET status='done' WHERE partition=? AND id=?", partition, id); err != nil {
			return nil, err
		}
	}
	for _, id := range ids {
		if _, err = tx.Exec("DELETE FROM source_windows WHERE partition=? AND id=?", partition, id); err != nil {
			return nil, err
		}
	}
	for _, claim := range items(vault["claims"]) {
		if _, err = tx.Exec("INSERT OR IGNORE INTO claim_keys VALUES(?,?)", partition, textValue(claim["id"])); err != nil {
			return nil, err
		}
	}
	receipt := document{"events": len(ids), "source_characters": size, "revisited_events": len(revisited), "publication_revision": revision + 1, "publication_bytes": len(mustJSON(vault))}
	if _, err = tx.Exec("INSERT INTO state(key,payload) VALUES('last_compile_receipt',?) ON CONFLICT(key) DO UPDATE SET payload=excluded.payload", mustJSON(receipt)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("knowledge capture publication: %w", err)
	}
	return vault, nil
}
