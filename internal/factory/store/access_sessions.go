package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
)

const (
	SessionKind       = "session"
	AfiSessionPrefix  = "afi:"
	TaskSessionPrefix = "task:"
)

func AfiSessionID(ws, person string) string { return AfiSessionPrefix + ws + ":" + person }

func TaskSessionID(ws, task string) string { return TaskSessionPrefix + ws + ":" + task }

// BackfillAfiSession keeps the first registration for a pane while its
// workstream exists. An in-flight legacy request cannot undo a purge.
func (st *Store) BackfillAfiSession(parent acl.Resource, person string, now time.Time) error {
	person = factory.NormalizeHandle(person)
	r := Resource{Resource: acl.Resource{Kind: SessionKind, ID: AfiSessionID(parent.ID, person)},
		Parent: parent, Owner: acl.Subject{Kind: "person", ID: person},
		CreatedBy: acl.Principal{Kind: "person", ID: person}, Visibility: acl.Private, CreatedAt: now}
	if _, found, err := st.Resource(r.Kind, r.ID); found || err != nil {
		return err
	}
	return st.wsTx(func(tx *sql.Tx) error {
		var exists int
		err := tx.QueryRow(`SELECT 1 FROM workstreams WHERE id = ?`, parent.ID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return writeResource(tx, r, false, true)
	})
}

// BackfillTaskSession reads the task's owner and checks that it is live in
// the same transaction as registration, so a late run cannot undo removal.
func (st *Store) BackfillTaskSession(ws, task string, now time.Time) error {
	ref := acl.Resource{Kind: SessionKind, ID: TaskSessionID(ws, task)}
	if _, found, err := st.Resource(ref.Kind, ref.ID); found || err != nil {
		return err
	}
	return st.wsTx(func(tx *sql.Tx) error {
		return backfillTaskSession(tx, ws, task, now)
	})
}

func backfillTaskSession(tx *sql.Tx, ws, task string, now time.Time) error {
	var rawTask, rawParent string
	err := tx.QueryRow(`SELECT t.doc, w.doc FROM tasks t JOIN workstreams w ON w.id=t.ws
		WHERE t.ws=? AND t.id=? AND NOT EXISTS (SELECT 1 FROM task_deletions d WHERE d.ws=t.ws AND d.id=t.id)`, ws, task).Scan(&rawTask, &rawParent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var t factory.WorkstreamTask
	if err := json.Unmarshal([]byte(rawTask), &t); err != nil {
		return err
	}
	var parent factory.OrgWorkstream
	if err := json.Unmarshal([]byte(rawParent), &parent); err != nil {
		return err
	}
	kind := "workstream"
	if parent.Space {
		kind = "space"
	}
	return writeResource(tx, Resource{
		Resource:   acl.Resource{Kind: SessionKind, ID: TaskSessionID(ws, task)},
		Parent:     acl.Resource{Kind: kind, ID: ws},
		Owner:      acl.Subject{Kind: t.Owner.Kind, ID: t.Owner.ID},
		CreatedBy:  acl.Principal{Kind: "person", ID: t.CreatedBy.ID},
		Visibility: acl.Owner, CreatedAt: now,
	}, false, true)
}
