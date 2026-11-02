package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func knowledgeModel(profile string) string { return config.KnowledgeModelAt(profile) }

const knowledgeProjectionVersion = "knowledge-projection-4"

func (v *Vault) compile(ctx context.Context, key string, force bool) (err error) {
	if !v.Enabled() {
		return ErrDisabled
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if v.calls != nil {
		ctx, err = v.calls(ctx)
		if err != nil {
			return err
		}
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
	model := compilerModel{ctx: ctx, root: v.root, key: key, model: knowledgeModel(v.profile), db: db, calls: v.calls}
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
	boundaryKey := "organization_boundary"
	if owner := provider.SessionFrom(ctx); owner != "" {
		boundaryKey += ":" + owner
	}
	boundary, err := stateDocument(db, boundaryKey)
	if err != nil {
		return err
	}
	through := int64(number(boundary["through"]))
	if through == 0 || provider.SessionFrom(ctx) != "" {
		if err = db.QueryRow("SELECT coalesce(max(rowid),0) FROM events WHERE partition='profile'").Scan(&through); err != nil {
			return err
		}
		if err = saveState(db, boundaryKey, document{"through": through}); err != nil {
			return err
		}
	}
	before, err := pendingCompilerSources(db, ctx, through)
	if err != nil {
		return err
	}
	var vault document
	for {
		vault, err = v.compilePending(db, "profile", model, force, through)
		if err != nil {
			return err
		}
		remaining, err := pendingCompilerSources(db, ctx, through)
		if err != nil {
			return err
		}
		if remaining == 0 {
			break
		}
		// One trigger drains a finite forced snapshot. Debounced background passes
		// keep their existing single-batch admission when no progress is due yet.
		if !force || remaining >= before {
			return nil
		}
		before = remaining
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
	intent, err := stateDocument(db, "job")
	if err != nil {
		return err
	}
	request := textValue(intent["request"])
	explicitBuild := request != "" && request != textValue(projection["build_request"])
	sameInput := textValue(projection["input_hash"]) == digest
	if sameInput && projection["version"] == knowledgeProjectionVersion && !explicitBuild {
		_, err = db.Exec("DELETE FROM state WHERE key=?", boundaryKey)
		return err
	}
	needsLearning := !sameInput || (explicitBuild && hasPendingRefinement(learning))
	if !needsLearning && projection["version"] == knowledgeProjectionVersion {
		projection["build_request"] = request
		if err = saveState(db, "projection", projection); err != nil {
			return err
		}
		_, err = db.Exec("DELETE FROM state WHERE key=?", boundaryKey)
		return err
	}
	if needsLearning {
		previousLearning := clone(learning)
		learning, err = extendLearning(vault, learning, model)
		if err != nil {
			return err
		}
		learning, err = refineLearning(vault, learning, previousLearning, model)
		if err != nil {
			return err
		}
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
	published, err = curatePublishedGuides(published, model, !needsLearning)
	if err != nil {
		return err
	}
	if err = v.commitPublication(published, learning, registry, digest, request); err != nil {
		return err
	}
	_, err = db.Exec("DELETE FROM state WHERE key=?", boundaryKey)
	return err
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
	if repaired, changed := repairNoteIdentities(previous); changed {
		if _, err = db.Exec("UPDATE publications SET payload=?,revision=revision+1 WHERE partition=?", mustJSON(repaired), partition); err != nil {
			return nil, err
		}
		previous = repaired
		revision++
	}
	selection, args := compilerSourceSelection(model.ctx, "e.payload")
	rows, err := db.Query("SELECT e.id,e.payload,e.arrived,e.status,coalesce(w.length,0) FROM events e LEFT JOIN source_windows w ON w.partition=e.partition AND w.id=e.id WHERE e.partition=? AND e.rowid<=? AND e.status IN ('pending','revisit') AND ("+selection+") ORDER BY e.arrived,e.id LIMIT 64", append([]any{partition, through}, args...)...)
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
		delete(source, "job")
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

func pendingCompilerSources(db *sql.DB, ctx context.Context, through int64) (int, error) {
	var pending int
	selection, args := compilerSourceSelection(ctx, "payload")
	err := db.QueryRow("SELECT count(*) FROM events WHERE partition='profile' AND rowid<=? AND status IN ('pending','revisit') AND ("+selection+")", append([]any{through}, args...)...).Scan(&pending)
	return pending, err
}

func compilerSourceSelection(ctx context.Context, payload string) (string, []any) {
	owners, bound := ctx.Value(ownerSessionsKey{}).([]string)
	if !bound && provider.SessionFrom(ctx) != "" {
		owners = []string{provider.SessionFrom(ctx)}
	}
	if len(owners) == 0 {
		return "coalesce(json_extract(" + payload + ",'$.job'),0)=0", nil
	}
	clauses := make([]string, 0, len(owners))
	args := make([]any, 0, len(owners)*3)
	for _, owner := range owners {
		clauses = append(clauses, "(json_extract("+payload+",'$.session')=? OR substr(json_extract("+payload+",'$.session'),1,length(?))=?)")
		args = append(args, owner, "task:"+owner+":", "task:"+owner+":")
	}
	return strings.Join(clauses, " OR "), args
}
