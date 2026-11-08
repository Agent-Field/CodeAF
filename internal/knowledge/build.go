package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

const ordinaryBuildAttemptLimit = 3

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
	// JobOwner groups stage and task journals without changing source identities.
	JobOwner string
	Job      bool
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
	if err := config.WriteMemoryEnabled(v.profile, true); err != nil {
		return err
	}
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
	job["backfill"] = true
	job["request"] = time.Now().UTC().Format(time.RFC3339Nano)
	job["phase"] = string(BuildRunning)
	job["error"] = ""
	if err = saveState(tx, "job", job); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	v.forgetConsent()
	wakeBuild(v.root)
	return nil
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
	wake := buildWake(v.root)
	// A Build pressed before this loop began is answered by its first pass.
	select {
	case <-wake:
	default:
	}
	seen, quiet := "", false
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		before := seen
		v.passSeen = ""
		err = v.runPass(ctx, history, key, &lastInventory)
		seen = v.passSeen
		quiet = err == nil && seen != "" && seen == before
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
			job["error"] = "Knowledge build paused: " + err.Error() + ". " + buildRetryNotice(number(job["build_attempts"]))
			saveErr := saveState(db, "job", job)
			db.Close()
			if saveErr != nil {
				return saveErr
			}
			delay = min(120*time.Second, max(5*time.Second, delay*2))
		} else if quiet {
			// A PASS THAT FOUND THE SAME CHATS AS THE LAST ONE BACKS OFF. Each
			// pass lists every saved chat and opens the database, and an idle
			// engine has nothing new for it; a new chat is still found within
			// buildQuietMax, and a person's Build wakes the loop at once.
			delay = min(buildQuietMax, max(buildEvery, delay*2))
		} else {
			delay = buildEvery
		}
		if err = waitBuildOrWake(ctx, delay, wake); err != nil {
			return err
		}
	}
}

// buildEvery is how soon a pass follows one that found something new;
// buildQuietMax is the longest a quiet vault waits between passes. A test
// shortens both.
var (
	buildEvery    = 2 * time.Second
	buildQuietMax = 30 * time.Second
)

// buildWakes is each vault folder's wake, shared by every [Vault] opened on
// it: the one running the loop and the one a person's Build was pressed on
// are separate values.
var buildWakes sync.Map

func buildWake(root string) chan struct{} {
	w, _ := buildWakes.LoadOrStore(filepath.Clean(root), make(chan struct{}, 1))
	return w.(chan struct{})
}

// wakeBuild asks the loop running on root for a pass now. It never blocks: a
// wake already waiting is the same ask.
func wakeBuild(root string) {
	select {
	case buildWake(root) <- struct{}{}:
	default:
	}
}

func waitBuildOrWake(ctx context.Context, delay time.Duration, wake <-chan struct{}) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	case <-wake:
	}
	return nil
}
func buildRetryNotice(attempts float64) string {
	if attempts >= ordinaryBuildAttemptLimit {
		return fmt.Sprintf("Completed progress is retained; automatic retries stopped after %d attempts; use Build or add new sources.", ordinaryBuildAttemptLimit)
	}
	return "Completed progress is retained; retrying automatically."
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

// markJobEvents marks one job session's saved events, so the compiler leaves them out. It runs on every pass with
// Memory on, so it seeks through profile_session_lookup, which holds only events not marked yet.
const markJobEvents = "UPDATE events SET payload=json_set(payload,'$.job',json('true')) WHERE partition='profile' AND json_extract(payload,'$.session')=? AND coalesce(json_extract(payload,'$.job'),0)=0"

// profileSessionLookup is built here rather than with the schema: an open must not wait for it, and it is needed only
// with Memory on. On an existing large database it holds the write lock while it builds (busyWaitMillis).
const profileSessionLookup = "CREATE INDEX IF NOT EXISTS profile_session_lookup ON events(json_extract(payload,'$.session')) WHERE partition='profile' AND coalesce(json_extract(payload,'$.job'),0)=0"

func markJobSessions(db *sql.DB, journals []Journal) error {
	if _, err := db.Exec(profileSessionLookup); err != nil {
		return err
	}
	for _, journal := range journals {
		if journal.Job {
			if _, err := db.Exec(markJobEvents, journal.Session); err != nil {
				return err
			}
		}
	}
	return nil
}

func (v *Vault) runPass(ctx context.Context, history History, key func() string, lastInventory *string) error {
	journals, err := history.Inventory(ctx)
	if err != nil {
		return err
	}
	v.passSeen = identity([]any{journals, v.Enabled()})
	db, err := v.database()
	if err != nil {
		return err
	}
	defer db.Close()
	job, err := stateDocument(db, "job")
	if err != nil {
		return err
	}
	if !v.Enabled() {
		// Discovery is local. No inventory, including an empty one, opts in.
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
		return nil
	}

	if err = markJobSessions(db, journals); err != nil {
		return err
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
					source.Job = journal.Job
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
	var latest int64
	var admitted int
	var arrived float64
	if err = db.QueryRow("SELECT coalesce(max(rowid),0),count(*),coalesce(max(arrived),0) FROM events WHERE partition='profile' AND coalesce(json_extract(payload,'$.job'),0)=0").Scan(&latest, &admitted, &arrived); err != nil {
		return err
	}
	// An empty vault has no build input or retry budget to persist.
	if total > 0 {
		attemptInput := identity([]any{latest, admitted, arrived, job["request"], knowledgeProjectionVersion})
		if job["build_input"] != attemptInput {
			job["build_input"] = attemptInput
			job["build_attempts"] = 0
		}
		if number(job["build_attempts"]) >= ordinaryBuildAttemptLimit {
			if job["phase"] != string(BuildFailed) {
				job["phase"] = string(BuildFailed)
				job["error"] = "Knowledge build interrupted. " + buildRetryNotice(number(job["build_attempts"]))
				return saveState(db, "job", job)
			}
			return nil
		}
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
		if !v.Enabled() {
			return nil
		}
		if err = db.QueryRow("SELECT count(*) FROM events WHERE partition='profile' AND status IN ('pending','revisit') AND coalesce(json_extract(payload,'$.job'),0)=0").Scan(&pending); err != nil {
			return err
		}
		if pending == 0 && !flag(job["backfill"]) {
			projection, err := stateDocument(db, "projection")
			if err != nil {
				return err
			}
			if total == 0 || projection["version"] == knowledgeProjectionVersion {
				break
			}
		}
		job["phase"] = string(BuildRunning)
		// Reserve the attempt durably before compilation can spend money.
		job["build_attempts"] = number(job["build_attempts"]) + 1
		if err = saveState(db, "job", job); err != nil {
			return err
		}
		if err = v.Flush(ctx, key(), flag(job["backfill"])); err != nil {
			if errors.Is(err, ErrDisabled) {
				return nil
			}
			return err
		}
		job["build_attempts"] = 0
		if err = saveState(db, "job", job); err != nil {
			return err
		}
		var remaining int
		if err = db.QueryRow("SELECT count(*) FROM events WHERE partition='profile' AND status IN ('pending','revisit') AND coalesce(json_extract(payload,'$.job'),0)=0").Scan(&remaining); err != nil {
			return err
		}
		if remaining == 0 {
			break
		}
		if remaining >= pending {
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
