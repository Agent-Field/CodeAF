package codeaf

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/bus"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/projectors"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sessioncore"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/storage"
	"golang.org/x/sys/unix"
)

const (
	codeafDataDirectory = ".codeaf"
	codeafDatabaseFile  = "codeaf.db"

	projectionReconcileVersion = 1
)

type projectionSource interface {
	List(prefix []string) ([][]string, error)
	ReadInto(key []string, dst any) error
}

type projectionRecordMark struct {
	Key      string `json:"key"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Changed  int64  `json:"changed"`
	Device   uint64 `json:"device"`
	Inode    uint64 `json:"inode"`
}

type projectionManifest struct {
	Version   int                    `json:"version"`
	ProjectID string                 `json:"projectID"`
	Records   []projectionRecordMark `json:"records"`
}

type durableSessions struct {
	store            *storage.Store
	projectionSource projectionSource
	sessions         *sessioncore.Service
	bus              *bus.Bus
	db               *sql.DB
	projectID        string
	workspace        string
	unsubscribe      func()
	projector        *projectors.Store
	lockPath         string

	operationMu     sync.Mutex
	projectionMu    sync.Mutex
	projectionError []error
}

func openDurableSessions(ctx context.Context, workspace string) (*durableSessions, error) {
	// storage.ts:231 and db.ts:33 place flat storage and codeaf.db below
	// Global.Path.data; codeaf scopes that data root to the workspace's .codeaf.
	dataDir := filepath.Join(workspace, codeafDataDirectory)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("codeaf sessions: create data directory: %w", err)
	}

	projectInfo, _, err := project.Discover(ctx, workspace)
	if err != nil {
		return nil, fmt.Errorf("codeaf sessions: discover project: %w", err)
	}
	projectID := string(projectInfo.ID)
	dbPath := filepath.Join(dataDir, codeafDatabaseFile)
	db, err := projectors.Open(ctx, dbPath, projectors.BusyRetryOptions{Log: io.Discard})
	if err != nil {
		return nil, err
	}
	closeOnError := func(err error) (*durableSessions, error) {
		_ = db.Close()
		return nil, err
	}
	if err := applyProjectSchema(ctx, db); err != nil {
		return closeOnError(err)
	}
	if err := upsertProject(ctx, db, projectInfo); err != nil {
		return closeOnError(err)
	}
	if err := projectors.ApplySchema(ctx, db); err != nil {
		return closeOnError(err)
	}

	instanceBus := bus.New(bus.Context{
		Directory: workspace, Project: projectID, Workspace: workspace,
	})
	durable := &durableSessions{
		store: storage.NewFromDataDir(dataDir), bus: instanceBus, db: db,
		projectID: projectID, workspace: workspace,
		projector: projectors.NewStore(db, projectors.StoreOptions{}),
		lockPath:  filepath.Join(dataDir, "projection.lock"),
	}
	durable.projectionSource = durable.store
	sessions, err := sessioncore.New(sessioncore.Options{
		Store: durable.store, Bus: instanceBus, ProjectID: projectID,
		Worktree: string(projectInfo.Worktree), Directory: workspace,
		Version: version,
	})
	if err != nil {
		durable.Close()
		return nil, err
	}
	durable.sessions = sessions
	if err := durable.reconcileProjection(ctx); err != nil {
		durable.Close()
		return nil, err
	}
	durable.unsubscribe = instanceBus.SubscribeAllCallback(durable.projectEvent)
	return durable, nil
}

func applyProjectSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS project (
		id text PRIMARY KEY,
		worktree text NOT NULL,
		vcs text,
		name text,
		icon_url text,
		icon_url_override text,
		icon_color text,
		time_created integer NOT NULL,
		time_updated integer NOT NULL,
		time_initialized integer,
		sandboxes text NOT NULL,
		commands text
	)`)
	if err != nil {
		return fmt.Errorf("codeaf sessions: apply project schema: %w", err)
	}
	return nil
}

func upsertProject(ctx context.Context, db *sql.DB, info project.Info) error {
	now := time.Now().UnixMilli()
	created := info.Time.Created
	if created == 0 {
		created = now
	}
	updated := info.Time.Updated
	if updated == 0 {
		updated = now
	}
	sandboxes := info.Sandboxes
	if len(sandboxes) == 0 {
		sandboxes = []string{info.Worktree}
	}
	sandboxesJSON, err := json.Marshal(sandboxes)
	if err != nil {
		return err
	}
	var vcs any
	if info.VCS != nil {
		vcs = *info.VCS
	}
	_, err = db.ExecContext(ctx, `INSERT INTO project
		(id, worktree, vcs, time_created, time_updated, sandboxes)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			worktree = excluded.worktree,
			vcs = excluded.vcs,
			time_updated = excluded.time_updated,
			sandboxes = excluded.sandboxes`,
		string(info.ID), info.Worktree, vcs, created, updated, string(sandboxesJSON),
	)
	if err != nil {
		return fmt.Errorf("codeaf sessions: upsert project: %w", err)
	}
	return nil
}

func projectedSessionEvent(eventType string) bool {
	switch eventType {
	case projectors.EventSessionCreated,
		projectors.EventSessionUpdated,
		projectors.EventSessionDeleted,
		projectors.EventMessageUpdated,
		projectors.EventMessageRemoved,
		projectors.EventMessagePartUpdated,
		projectors.EventMessagePartRemoved:
		return true
	default:
		return false
	}
}

func (durable *durableSessions) recordProjectionError(err error) {
	if err == nil {
		return
	}
	durable.projectionMu.Lock()
	durable.projectionError = append(durable.projectionError, err)
	durable.projectionMu.Unlock()
}

func (durable *durableSessions) projectEvent(payload bus.Payload) {
	if !projectedSessionEvent(payload.Type) {
		return
	}
	data, err := json.Marshal(payload.Properties)
	if err == nil {
		err = durable.projector.Apply(context.Background(), projectors.Event{
			ID: payload.ID, Type: payload.Type, Data: data,
		})
	}
	durable.recordProjectionError(err)
}

func (durable *durableSessions) withProjection(
	operation string, mutate func() error,
) error {
	durable.operationMu.Lock()
	defer durable.operationMu.Unlock()
	return withAdvisoryFileLock(durable.lockPath, func() error {
		durable.projectionMu.Lock()
		durable.projectionError = nil
		durable.projectionMu.Unlock()
		mutationErr := mutate()
		durable.projectionMu.Lock()
		projectionErrs := durable.projectionError
		durable.projectionError = nil
		durable.projectionMu.Unlock()
		if mutationErr != nil {
			return mutationErr
		}
		if len(projectionErrs) != 0 {
			return fmt.Errorf("codeaf sessions: project %s: %w", operation, errors.Join(projectionErrs...))
		}
		// Live projectors have already committed. Move only the O(1) database
		// generation here; the filesystem manifest deliberately remains at the
		// last startup so that the next startup discovers the new log records.
		if err := durable.advanceProjectionGeneration(context.Background()); err != nil {
			log.Printf("codeaf sessions: leave projection generation stale after %s: %v", operation, err)
		}
		return nil
	})
}

func (durable *durableSessions) Messages(
	ctx context.Context, sessionID string,
) ([]msgmodel.WithParts, error) {
	return durable.sessions.Messages(ctx, sessionID)
}

func (durable *durableSessions) UpdateMessage(ctx context.Context, info msgmodel.Info) error {
	return durable.withProjection("message "+info.MessageID(), func() error {
		return durable.sessions.UpdateMessage(ctx, info)
	})
}

func (durable *durableSessions) UpdatePart(ctx context.Context, part msgmodel.Part) error {
	return durable.withProjection("part "+part.Base().ID, func() error {
		return durable.sessions.UpdatePart(ctx, part)
	})
}

func (durable *durableSessions) UpdatePartDelta(ctx context.Context, input msgmodel.PartDeltaEvent) {
	durable.sessions.UpdatePartDelta(ctx, input)
}

func (durable *durableSessions) UpdateMessageWithParts(
	ctx context.Context, info msgmodel.Info, parts ...msgmodel.Part,
) error {
	return durable.withProjection("message turn "+info.MessageID(), func() error {
		return durable.sessions.UpdateMessageWithParts(ctx, info, parts...)
	})
}

func (durable *durableSessions) CreateSession(
	ctx context.Context, input sessioncore.CreateInput,
) (sessioncore.Info, error) {
	var info sessioncore.Info
	err := durable.withProjection("session create", func() error {
		var err error
		info, err = durable.sessions.Create(ctx, input)
		return err
	})
	return info, err
}

func (durable *durableSessions) TouchSession(ctx context.Context, sessionID string) error {
	return durable.withProjection("session touch "+sessionID, func() error {
		return durable.sessions.Touch(ctx, sessionID)
	})
}

func (durable *durableSessions) RemoveSession(ctx context.Context, sessionID string) error {
	return durable.withProjection("session remove "+sessionID, func() error {
		return durable.sessions.Remove(ctx, sessionID)
	})
}

type replayProjectionEvent struct {
	event projectors.Event
	time  int64
}

func (durable *durableSessions) reconcileProjection(ctx context.Context) error {
	return withAdvisoryFileLock(durable.lockPath, func() error {
		if err := durable.ensureProjectionReconcileSchema(ctx); err != nil {
			return err
		}
		current, err := durable.projectionManifest()
		if err != nil {
			return fmt.Errorf("codeaf sessions: inspect flat projection source: %w", err)
		}
		previous, markedGeneration, valid, err := durable.loadProjectionMark(ctx)
		if err != nil {
			return err
		}
		generation, err := durable.projectionGeneration(ctx, durable.db)
		if err != nil {
			return err
		}
		if valid && generation == markedGeneration {
			changed, removed := changedProjectionRecords(previous, current)
			if len(changed) == 0 && !removed {
				return nil
			}
			if !removed {
				events, quarantined, eventErr := durable.incrementalProjectionEvents(changed, current)
				if eventErr != nil {
					return fmt.Errorf("codeaf sessions: read incremental projection source: %w", eventErr)
				}
				if !quarantined {
					return durable.commitProjectionReplay(ctx, events, current, false)
				}
			}
		}
		return durable.fullProjectionReconcile(ctx)
	})
}

const projectionReconcileSchema = `
CREATE TABLE IF NOT EXISTS codeaf_projection_generation (
	id integer PRIMARY KEY CHECK (id = 1),
	generation integer NOT NULL
);
INSERT OR IGNORE INTO codeaf_projection_generation (id, generation) VALUES (1, 0);
CREATE TABLE IF NOT EXISTS codeaf_projection_reconcile (
	id integer PRIMARY KEY CHECK (id = 1),
	format_version integer NOT NULL,
	project_id text NOT NULL,
	generation integer NOT NULL,
	manifest text NOT NULL
);
CREATE TRIGGER IF NOT EXISTS codeaf_projection_session_insert
AFTER INSERT ON session BEGIN
	UPDATE codeaf_projection_generation SET generation = generation + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS codeaf_projection_session_update
AFTER UPDATE ON session BEGIN
	UPDATE codeaf_projection_generation SET generation = generation + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS codeaf_projection_session_delete
AFTER DELETE ON session BEGIN
	UPDATE codeaf_projection_generation SET generation = generation + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS codeaf_projection_message_insert
AFTER INSERT ON message BEGIN
	UPDATE codeaf_projection_generation SET generation = generation + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS codeaf_projection_message_update
AFTER UPDATE ON message BEGIN
	UPDATE codeaf_projection_generation SET generation = generation + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS codeaf_projection_message_delete
AFTER DELETE ON message BEGIN
	UPDATE codeaf_projection_generation SET generation = generation + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS codeaf_projection_part_insert
AFTER INSERT ON part BEGIN
	UPDATE codeaf_projection_generation SET generation = generation + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS codeaf_projection_part_update
AFTER UPDATE ON part BEGIN
	UPDATE codeaf_projection_generation SET generation = generation + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS codeaf_projection_part_delete
AFTER DELETE ON part BEGIN
	UPDATE codeaf_projection_generation SET generation = generation + 1 WHERE id = 1;
END;`

func (durable *durableSessions) ensureProjectionReconcileSchema(ctx context.Context) error {
	if _, err := durable.db.ExecContext(ctx, projectionReconcileSchema); err != nil {
		return fmt.Errorf("codeaf sessions: apply projection reconciliation schema: %w", err)
	}
	return nil
}

type projectionGenerationReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (durable *durableSessions) projectionGeneration(
	ctx context.Context, reader projectionGenerationReader,
) (int64, error) {
	var generation int64
	if err := reader.QueryRowContext(ctx,
		"SELECT generation FROM codeaf_projection_generation WHERE id = 1",
	).Scan(&generation); err != nil {
		return 0, fmt.Errorf("codeaf sessions: read projection generation: %w", err)
	}
	return generation, nil
}

func (durable *durableSessions) loadProjectionMark(
	ctx context.Context,
) (projectionManifest, int64, bool, error) {
	var version int
	var projectID, raw string
	var generation int64
	err := durable.db.QueryRowContext(ctx, `SELECT format_version, project_id, generation, manifest
		FROM codeaf_projection_reconcile WHERE id = 1`).Scan(
		&version, &projectID, &generation, &raw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return projectionManifest{}, 0, false, nil
	}
	if err != nil {
		return projectionManifest{}, 0, false,
			fmt.Errorf("codeaf sessions: read projection reconciliation mark: %w", err)
	}
	var manifest projectionManifest
	if version != projectionReconcileVersion || projectID != durable.projectID ||
		json.Unmarshal([]byte(raw), &manifest) != nil || !validProjectionManifest(manifest, durable.projectID) {
		return projectionManifest{}, 0, false, nil
	}
	return manifest, generation, true, nil
}

func validProjectionManifest(manifest projectionManifest, projectID string) bool {
	if manifest.Version != projectionReconcileVersion || manifest.ProjectID != projectID {
		return false
	}
	previous := ""
	for _, record := range manifest.Records {
		if record.Key == "" || record.Key <= previous {
			return false
		}
		parts := strings.Split(record.Key, "/")
		if len(parts) < 2 || (parts[0] != "session" && parts[0] != "message" && parts[0] != "part") {
			return false
		}
		previous = record.Key
	}
	return true
}

func (durable *durableSessions) projectionManifest() (projectionManifest, error) {
	records := []projectionRecordMark{}
	for _, prefix := range []string{"session", "message", "part"} {
		keys, err := durable.projectionSource.List([]string{prefix})
		if err != nil {
			return projectionManifest{}, err
		}
		for _, key := range keys {
			if !validProjectionKey(key) {
				continue
			}
			pathParts := append([]string{durable.store.Dir}, key...)
			path := filepath.Join(pathParts...) + ".json"
			info, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return projectionManifest{}, err
			}
			record := projectionRecordMark{
				Key: strings.Join(key, "/"), Size: info.Size(), Modified: info.ModTime().UnixNano(),
			}
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				record.Device = uint64(stat.Dev)
				record.Inode = stat.Ino
				record.Changed = statChangedNanos(stat)
			}
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Key < records[j].Key })
	return projectionManifest{
		Version: projectionReconcileVersion, ProjectID: durable.projectID, Records: records,
	}, nil
}

func validProjectionKey(key []string) bool {
	if len(key) == 2 && key[0] == "session" {
		return true
	}
	if len(key) == 3 && key[0] == "message" {
		return true
	}
	return len(key) == 4 && key[0] == "part"
}

func changedProjectionRecords(previous, current projectionManifest) ([]projectionRecordMark, bool) {
	old := make(map[string]projectionRecordMark, len(previous.Records))
	for _, record := range previous.Records {
		old[record.Key] = record
	}
	changed := make([]projectionRecordMark, 0)
	for _, record := range current.Records {
		if prior, exists := old[record.Key]; !exists || prior != record {
			changed = append(changed, record)
		}
		delete(old, record.Key)
	}
	return changed, len(old) != 0
}

func (durable *durableSessions) writeProjectionMarkTx(
	ctx context.Context, tx *sql.Tx, manifest projectionManifest,
) error {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	generation, err := durable.projectionGeneration(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO codeaf_projection_reconcile
		(id, format_version, project_id, generation, manifest) VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET format_version = excluded.format_version,
		project_id = excluded.project_id, generation = excluded.generation, manifest = excluded.manifest`,
		projectionReconcileVersion, durable.projectID, generation, string(raw),
	)
	return err
}

func (durable *durableSessions) advanceProjectionGeneration(ctx context.Context) error {
	_, err := durable.db.ExecContext(ctx, `UPDATE codeaf_projection_reconcile
		SET generation = (SELECT generation FROM codeaf_projection_generation WHERE id = 1)
		WHERE id = 1`)
	return err
}

func (durable *durableSessions) commitProjectionReplay(
	ctx context.Context, events []replayProjectionEvent, manifest projectionManifest, prune bool,
) error {
	tx, err := durable.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("codeaf sessions: begin projection replay: %w", err)
	}
	var replayTime int64
	projector := projectors.NewStore(durable.db, projectors.StoreOptions{
		Now: func() int64 { return replayTime },
	})
	if prune {
		if err := durable.pruneProjectionTx(ctx, tx, events); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("codeaf sessions: prune projection replay: %w", err)
		}
	}
	for _, item := range events {
		replayTime = item.time
		if err := projector.ApplyReconcileTx(ctx, tx, item.event); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("codeaf sessions: replay %s: %w", item.event.Type, err)
		}
	}
	if err := durable.writeProjectionMarkTx(ctx, tx, manifest); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("codeaf sessions: write projection reconciliation mark: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("codeaf sessions: commit projection replay: %w", err)
	}
	return nil
}

func (durable *durableSessions) pruneProjectionTx(
	ctx context.Context, tx *sql.Tx, events []replayProjectionEvent,
) error {
	ids := map[string][]string{"session": {}, "message": {}, "part": {}}
	for _, item := range events {
		var data struct {
			SessionID string `json:"sessionID"`
			Info      struct {
				ID string `json:"id"`
			} `json:"info"`
			Part struct {
				ID string `json:"id"`
			} `json:"part"`
		}
		if err := json.Unmarshal(item.event.Data, &data); err != nil {
			return err
		}
		switch item.event.Type {
		case projectors.EventSessionCreated:
			ids["session"] = append(ids["session"], data.SessionID)
		case projectors.EventMessageUpdated:
			ids["message"] = append(ids["message"], data.Info.ID)
		case projectors.EventMessagePartUpdated:
			ids["part"] = append(ids["part"], data.Part.ID)
		}
	}
	encoded := map[string]string{}
	for kind, values := range ids {
		raw, err := json.Marshal(values)
		if err != nil {
			return err
		}
		encoded[kind] = string(raw)
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{
			query: `DELETE FROM part WHERE session_id IN
				(SELECT id FROM session WHERE project_id = ?)
				AND id NOT IN (SELECT value FROM json_each(?))`,
			args: []any{durable.projectID, encoded["part"]},
		},
		{
			query: `DELETE FROM message WHERE session_id IN
				(SELECT id FROM session WHERE project_id = ?)
				AND id NOT IN (SELECT value FROM json_each(?))`,
			args: []any{durable.projectID, encoded["message"]},
		},
		{
			query: `DELETE FROM session WHERE project_id = ?
				AND id NOT IN (SELECT value FROM json_each(?))`,
			args: []any{durable.projectID, encoded["session"]},
		},
	} {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return err
		}
	}
	return nil
}

func (durable *durableSessions) fullProjectionReconcile(ctx context.Context) error {
	events, _, err := durable.flatProjectionEvents()
	if err != nil {
		return fmt.Errorf("codeaf sessions: read flat projection source: %w", err)
	}
	manifest, err := durable.projectionManifest()
	if err != nil {
		return fmt.Errorf("codeaf sessions: inspect reconciled projection source: %w", err)
	}
	return durable.commitProjectionReplay(ctx, events, manifest, true)
}

func (durable *durableSessions) incrementalProjectionEvents(
	changed []projectionRecordMark, current projectionManifest,
) ([]replayProjectionEvent, bool, error) {
	changedByKind := map[string][]projectionRecordMark{}
	currentKeys := make(map[string]bool, len(current.Records))
	for _, record := range current.Records {
		currentKeys[record.Key] = true
	}
	for _, record := range changed {
		parts := strings.Split(record.Key, "/")
		changedByKind[parts[0]] = append(changedByKind[parts[0]], record)
	}

	type sessionState struct {
		checked bool
		own     bool
		info    sessioncore.Info
	}
	sessions := map[string]sessionState{}
	quarantined := false
	requiresFull := false
	var sessionReadErr error
	readSession := func(sessionID string) (sessioncore.Info, bool) {
		if state := sessions[sessionID]; state.checked {
			return state.info, state.own
		}
		key := []string{"session", sessionID}
		var info sessioncore.Info
		if err := durable.projectionSource.ReadInto(key, &info); err != nil {
			durable.quarantineProjectionRecord(key, err)
			quarantined = true
			sessions[sessionID] = sessionState{checked: true}
			return sessioncore.Info{}, false
		}
		if info.ID != sessionID {
			requiresFull = true
		}
		state := sessionState{checked: true, own: info.ProjectID == durable.projectID, info: info}
		if !state.own {
			var projected bool
			err := durable.db.QueryRow(
				"SELECT EXISTS(SELECT 1 FROM session WHERE id = ? AND project_id = ?)",
				sessionID, durable.projectID,
			).Scan(&projected)
			if sessionReadErr == nil {
				sessionReadErr = err
			}
			if projected {
				requiresFull = true
			}
		}
		sessions[sessionID] = state
		return info, state.own
	}

	events := make([]replayProjectionEvent, 0, len(changed))
	for _, record := range changedByKind["session"] {
		key := strings.Split(record.Key, "/")
		info, own := readSession(key[1])
		if !own {
			continue
		}
		event, err := projectionEvent(projectors.EventSessionCreated, map[string]any{
			"sessionID": info.ID, "info": info,
		})
		if err != nil {
			return nil, quarantined, err
		}
		events = append(events, replayProjectionEvent{event: event, time: int64(info.Time.Updated)})
	}
	for _, record := range changedByKind["message"] {
		key := strings.Split(record.Key, "/")
		info, own := readSession(key[1])
		if !own {
			continue
		}
		var raw json.RawMessage
		if err := durable.projectionSource.ReadInto(key, &raw); err != nil {
			durable.quarantineProjectionRecord(key, err)
			quarantined = true
			continue
		}
		message, err := msgmodel.UnmarshalInfo(raw)
		if err != nil {
			durable.quarantineProjectionRecord(key, err)
			quarantined = true
			continue
		}
		if message.MessageID() != key[2] || projectionMessageSessionID(message) != key[1] {
			requiresFull = true
		}
		created := messageCreatedMS(message)
		event, err := projectionEvent(projectors.EventMessageUpdated, msgmodel.UpdatedEvent{
			SessionID: info.ID, Info: message,
		})
		if err != nil {
			return nil, quarantined, err
		}
		events = append(events, replayProjectionEvent{event: event, time: int64(created)})
	}
	for _, record := range changedByKind["part"] {
		key := strings.Split(record.Key, "/")
		info, own := readSession(key[1])
		if !own || !currentKeys[strings.Join([]string{"message", key[1], key[2]}, "/")] {
			continue
		}
		var raw json.RawMessage
		if err := durable.projectionSource.ReadInto(key, &raw); err != nil {
			durable.quarantineProjectionRecord(key, err)
			quarantined = true
			continue
		}
		part, err := msgmodel.UnmarshalPart(raw)
		if err != nil {
			durable.quarantineProjectionRecord(key, err)
			quarantined = true
			continue
		}
		base := part.Base()
		if base.ID != key[3] || base.SessionID != key[1] || base.MessageID != key[2] {
			requiresFull = true
		}
		created := uint64(0)
		var messageRaw json.RawMessage
		messageKey := []string{"message", key[1], key[2]}
		if err := durable.projectionSource.ReadInto(messageKey, &messageRaw); err == nil {
			if message, parseErr := msgmodel.UnmarshalInfo(messageRaw); parseErr == nil {
				created = messageCreatedMS(message)
			}
		}
		stamp := latestJSONTimestamp(part, created)
		event, err := projectionEvent(projectors.EventMessagePartUpdated, msgmodel.PartUpdatedEvent{
			SessionID: info.ID, Part: part, Time: stamp,
		})
		if err != nil {
			return nil, quarantined, err
		}
		events = append(events, replayProjectionEvent{event: event, time: int64(stamp)})
	}
	if sessionReadErr != nil {
		return nil, false, sessionReadErr
	}
	return events, quarantined || requiresFull, nil
}

func (durable *durableSessions) flatProjectionEvents() ([]replayProjectionEvent, bool, error) {
	sessionKeys, err := durable.projectionSource.List([]string{"session"})
	if err != nil {
		return nil, false, err
	}
	events := make([]replayProjectionEvent, 0, len(sessionKeys))
	quarantined := false
	for _, sessionKey := range sessionKeys {
		var info sessioncore.Info
		if err := durable.projectionSource.ReadInto(sessionKey, &info); err != nil {
			durable.quarantineProjectionRecord(sessionKey, err)
			quarantined = true
			continue
		}
		if info.ProjectID != durable.projectID {
			continue
		}
		event, err := projectionEvent(projectors.EventSessionCreated, map[string]any{
			"sessionID": info.ID, "info": info,
		})
		if err != nil {
			return nil, quarantined, err
		}
		events = append(events, replayProjectionEvent{event: event, time: int64(info.Time.Updated)})

		messageKeys, err := durable.projectionSource.List([]string{"message", info.ID})
		if err != nil {
			return nil, quarantined, err
		}
		for _, messageKey := range messageKeys {
			var raw json.RawMessage
			if err := durable.projectionSource.ReadInto(messageKey, &raw); err != nil {
				durable.quarantineProjectionRecord(messageKey, err)
				quarantined = true
				continue
			}
			message, err := msgmodel.UnmarshalInfo(raw)
			if err != nil {
				durable.quarantineProjectionRecord(messageKey, err)
				quarantined = true
				continue
			}
			created := messageCreatedMS(message)
			event, err := projectionEvent(projectors.EventMessageUpdated, msgmodel.UpdatedEvent{
				SessionID: info.ID, Info: message,
			})
			if err != nil {
				return nil, quarantined, err
			}
			events = append(events, replayProjectionEvent{event: event, time: int64(created)})

			partKeys, err := durable.projectionSource.List([]string{"part", info.ID, message.MessageID()})
			if err != nil {
				return nil, quarantined, err
			}
			for _, partKey := range partKeys {
				var partRaw json.RawMessage
				if err := durable.projectionSource.ReadInto(partKey, &partRaw); err != nil {
					durable.quarantineProjectionRecord(partKey, err)
					quarantined = true
					continue
				}
				part, err := msgmodel.UnmarshalPart(partRaw)
				if err != nil {
					durable.quarantineProjectionRecord(partKey, err)
					quarantined = true
					continue
				}
				stamp := latestJSONTimestamp(part, created)
				event, err := projectionEvent(projectors.EventMessagePartUpdated, msgmodel.PartUpdatedEvent{
					SessionID: info.ID, Part: part, Time: stamp,
				})
				if err != nil {
					return nil, quarantined, err
				}
				events = append(events, replayProjectionEvent{event: event, time: int64(stamp)})
			}
		}
	}
	return events, quarantined, nil
}

func (durable *durableSessions) quarantineProjectionRecord(key []string, cause error) {
	parts := append([]string{durable.store.Dir}, key...)
	source := filepath.Join(parts...) + ".json"
	relative, err := filepath.Rel(durable.store.Dir, source)
	if err != nil {
		relative = filepath.Base(source)
	}
	destination := filepath.Join(
		durable.store.Dir,
		"quarantine",
		relative+".corrupt-"+strconv.FormatInt(time.Now().UnixNano(), 10),
	)
	moveErr := os.MkdirAll(filepath.Dir(destination), 0o755)
	if moveErr == nil {
		moveErr = os.Rename(source, destination)
	}
	if moveErr != nil {
		log.Printf("codeaf sessions: ignored unreadable projection source %s: %v (quarantine failed: %v)", source, cause, moveErr)
		return
	}
	log.Printf("codeaf sessions: quarantined unreadable projection source %s as %s: %v", source, destination, cause)
}

func projectionEvent(eventType string, properties any) (projectors.Event, error) {
	data, err := json.Marshal(properties)
	if err != nil {
		return projectors.Event{}, err
	}
	return projectors.Event{Type: eventType, Data: data}, nil
}

func messageCreatedMS(info msgmodel.Info) uint64 {
	switch message := info.(type) {
	case msgmodel.User:
		return message.Time.Created
	case msgmodel.Assistant:
		return message.Time.Created
	default:
		return 0
	}
}

func projectionMessageSessionID(info msgmodel.Info) string {
	switch message := info.(type) {
	case msgmodel.User:
		return message.SessionID
	case msgmodel.Assistant:
		return message.SessionID
	default:
		return ""
	}
}

func latestJSONTimestamp(value any, fallback uint64) uint64 {
	data, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if decoder.Decode(&decoded) != nil {
		return fallback
	}
	latest := fallback
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, nested := range value {
				if key == "start" || key == "end" || key == "created" || key == "updated" {
					if number, ok := nested.(json.Number); ok {
						if stamp, err := number.Int64(); err == nil && stamp >= 0 && uint64(stamp) > latest {
							latest = uint64(stamp)
						}
					}
				}
				visit(nested)
			}
		case []any:
			for _, nested := range value {
				visit(nested)
			}
		}
	}
	visit(decoded)
	return latest
}

func withAdvisoryFileLock(path string, fn func() error) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(int(file.Fd()), unix.LOCK_UN)
	return fn()
}

func (durable *durableSessions) Close() {
	if durable == nil {
		return
	}
	if durable.unsubscribe != nil {
		durable.unsubscribe()
	}
	if durable.bus != nil {
		durable.bus.Dispose()
	}
	if durable.db != nil {
		_ = durable.db.Close()
	}
}
