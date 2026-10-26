package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

type BuildPhase string

const (
	BuildUnbuilt BuildPhase = "unbuilt"
	BuildWaiting BuildPhase = "waiting"
	BuildRunning BuildPhase = "building"
	BuildReady   BuildPhase = "ready"
	BuildFailed  BuildPhase = "failed"
	BuildPartial BuildPhase = "partial"
)

type BuildStatus struct {
	Phase                    BuildPhase
	Chats, Imported, Sources int
	Problems                 int
	Error                    string
}

// Journal is one saved conversation, including an archived conversation. ID is
// an opaque locator owned by History, not a path the compiler may open.
type Journal struct {
	ID, Session, Project string
	Error                string
	Size                 int64
	Modified             time.Time
}
type JournalCursor struct {
	Offset            int64
	Skip              int
	Scope, PrefixHash string
}
type JournalBatch struct {
	Sources []Source
	Cursor  JournalCursor
}

// History owns journal format, discovery and source eligibility. Vault owns
// durable admission and cursor advancement in one database transaction.
type History interface {
	Inventory(context.Context) ([]Journal, error)
	Read(context.Context, Journal, JournalCursor) (JournalBatch, error)
}

func (v *Vault) StartBuild() error {
	db, err := v.database()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT OR IGNORE INTO state(key,payload) VALUES('config',?)", mustJSON(document{"enabled": true, "schema": 1, "created_at": time.Now().UTC().Format(time.RFC3339Nano)})); err != nil {
		return err
	}
	job, err := stateDocument(tx, "job")
	if err != nil {
		return err
	}
	if textValue(job["phase"]) == string(BuildRunning) {
		return nil
	}
	job["backfill"] = true
	job["phase"] = string(BuildRunning)
	job["error"] = ""
	if err = saveState(tx, "job", job); err != nil {
		return err
	}
	return tx.Commit()
}

// Run belongs to the application process, so changing chats and closing Memory
// cannot cancel a build. The OS lock is held for its lifetime and released on a
// crash; another running process waits and takes ownership without a stale lease.
func (v *Vault) Run(ctx context.Context, history History, key func() string) error {
	if err := os.MkdirAll(v.root, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(v.root, ".worker.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	for {
		err = filelock.Lock(lock, true, true)
		if err == nil {
			break
		}
		if !filelock.IsBusy(err) {
			return err
		}
		if err = waitBuild(ctx, 2*time.Second); err != nil {
			return err
		}
	}
	defer filelock.Unlock(lock)
	delay := time.Second
	lastInventory := ""
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		err = v.runPass(ctx, history, key, &lastInventory)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			db, openErr := v.database()
			if openErr != nil {
				return openErr
			}
			job, loadErr := stateDocument(db, "job")
			if loadErr != nil {
				db.Close()
				return loadErr
			}
			job["phase"] = string(BuildFailed)
			job["error"] = "Knowledge build paused: " + err.Error() + ". Completed progress is retained; retrying automatically."
			saveErr := saveState(db, "job", job)
			db.Close()
			if saveErr != nil {
				return saveErr
			}
			delay = min(120*time.Second, max(5*time.Second, delay*2))
		} else {
			delay = 2 * time.Second
		}
		if err = waitBuild(ctx, delay); err != nil {
			return err
		}
	}
}
func waitBuild(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (v *Vault) runPass(ctx context.Context, history History, key func() string, lastInventory *string) error {
	journals, err := history.Inventory(ctx)
	if err != nil {
		return err
	}
	db, err := v.database()
	if err != nil {
		return err
	}
	defer db.Close()
	settings, err := stateDocument(db, "config")
	if err != nil {
		return err
	}
	job, err := stateDocument(db, "job")
	if err != nil {
		return err
	}
	if !flag(settings["enabled"]) {
		// A successful empty inventory may arm future capture. Existing history
		// always waits for an explicit Build action before sources enter the vault.
		fingerprint := identity(journals)
		if textValue(job["inventory_hash"]) == fingerprint {
			return nil
		}
		available, problems := 0, 0
		for _, journal := range journals {
			if journal.Error != "" {
				problems++
				continue
			}
			cursor := JournalCursor{Scope: journal.Project}
			for cursor.Offset < journal.Size {
				batch, err := history.Read(ctx, journal, cursor)
				if err != nil {
					problems++
					break
				}
				if len(batch.Sources) > 0 {
					available++
					break
				}
				if batch.Cursor.Offset <= cursor.Offset {
					break
				}
				cursor = batch.Cursor
			}
		}
		job["inventory_hash"] = fingerprint
		job["chats"] = available
		job["problems"] = problems
		job["phase"] = string(BuildUnbuilt)
		job["error"] = ""
		if problems > 0 {
			job["error"] = "Some saved chats could not be read. Build will process readable chats and retry the rest."
		}
		if err = saveState(db, "job", job); err != nil {
			return err
		}
		if available > 0 || problems > 0 {
			return nil
		}
		if _, err = db.Exec("INSERT OR IGNORE INTO state(key,payload) VALUES('config',?)", mustJSON(document{"enabled": true, "schema": 1, "created_at": time.Now().UTC().Format(time.RFC3339Nano)})); err != nil {
			return err
		}
	}
	imported := int(number(job["imported"]))
	problems := int(number(job["problems"]))
	fingerprint := identity(journals)
	if fingerprint != *lastInventory || problems > 0 {
		imported, problems = 0, 0
	journalLoop:
		for _, journal := range journals {
			if journal.Error != "" {
				if _, err = db.Exec("INSERT INTO history_errors(journal,message) VALUES(?,?) ON CONFLICT(journal) DO UPDATE SET message=excluded.message", journal.ID, journal.Error); err != nil {
					return err
				}
				problems++
				continue
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			cursor := JournalCursor{Scope: journal.Project}
			cursorErr := db.QueryRow("SELECT offset,skip,scope,prefix_hash FROM cursors WHERE journal=?", journal.ID).Scan(&cursor.Offset, &cursor.Skip, &cursor.Scope, &cursor.PrefixHash)
			if cursorErr != nil && !errors.Is(cursorErr, sql.ErrNoRows) {
				return cursorErr
			}
			for cursor.Offset < journal.Size {
				batch, err := history.Read(ctx, journal, cursor)
				if err != nil {
					if _, saveErr := db.Exec("INSERT INTO history_errors(journal,message) VALUES(?,?) ON CONFLICT(journal) DO UPDATE SET message=excluded.message", journal.ID, err.Error()); saveErr != nil {
						return saveErr
					}
					problems++
					continue journalLoop
				}
				if batch.Cursor.Offset < cursor.Offset {
					return errors.New("knowledge journal cursor moved backwards")
				}
				tx, err := db.Begin()
				if err != nil {
					return err
				}
				for _, source := range batch.Sources {
					if err = validateCapture(source); err == nil {
						err = v.captureTx(tx, source)
					}
					if err != nil {
						tx.Rollback()
						if _, saveErr := db.Exec("INSERT INTO history_errors(journal,message) VALUES(?,?) ON CONFLICT(journal) DO UPDATE SET message=excluded.message", journal.ID, err.Error()); saveErr != nil {
							return saveErr
						}
						problems++
						continue journalLoop
					}
				}
				_, err = tx.Exec("INSERT INTO cursors(journal,offset,skip,scope,prefix_hash) VALUES(?,?,?,?,?) ON CONFLICT(journal) DO UPDATE SET offset=excluded.offset,skip=excluded.skip,scope=excluded.scope,prefix_hash=excluded.prefix_hash", journal.ID, batch.Cursor.Offset, batch.Cursor.Skip, batch.Cursor.Scope, batch.Cursor.PrefixHash)
				if err == nil {
					err = tx.Commit()
				} else {
					tx.Rollback()
				}
				if err != nil {
					return err
				}
				if batch.Cursor.Offset == cursor.Offset {
					break
				}
				cursor = batch.Cursor
				job["phase"] = string(BuildRunning)
				job["chats"] = len(journals)
				job["imported"] = imported
				job["error"] = ""
				if err = saveState(db, "job", job); err != nil {
					return err
				}
			}
			if _, err = db.Exec("DELETE FROM history_errors WHERE journal=?", journal.ID); err != nil {
				return err
			}
			imported++
		}
		if problems == 0 {
			*lastInventory = fingerprint
		}
	}
	var total, pending int
	if err = db.QueryRow("SELECT count(*) FROM events WHERE partition='profile'").Scan(&total); err != nil {
		return err
	}
	job["problems"] = problems
	job["sources"] = total
	job["chats"] = len(journals)
	job["imported"] = imported
	job["error"] = ""
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = db.QueryRow("SELECT count(*) FROM events WHERE partition='profile' AND status IN ('pending','revisit')").Scan(&pending); err != nil {
			return err
		}
		status, err := stateDocument(db, "status")
		if err != nil {
			return err
		}
		if pending == 0 && !flag(status["needs_update"]) {
			break
		}
		job["phase"] = string(BuildRunning)
		if err = saveState(db, "job", job); err != nil {
			return err
		}
		if err = v.Flush(ctx, key(), flag(job["backfill"])); err != nil {
			return err
		}
		var remaining int
		if err = db.QueryRow("SELECT count(*) FROM events WHERE partition='profile' AND status IN ('pending','revisit')").Scan(&remaining); err != nil {
			return err
		}
		if remaining > 0 && remaining >= pending {
			return nil
		}
	}
	job["backfill"] = false
	job["phase"] = string(BuildReady)
	if problems > 0 {
		job["phase"] = string(BuildPartial)
		job["error"] = "Some saved chats could not be read. Other chats have been processed; unreadable chats will be retried."
	}
	if total == 0 && problems == 0 {
		job["phase"] = string(BuildWaiting)
	}
	return saveState(db, "job", job)
}
