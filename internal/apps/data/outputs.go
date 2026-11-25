package data

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const outputMigration = `
	CREATE TABLE output_blobs(hash TEXT PRIMARY KEY, content BLOB NOT NULL);
	CREATE TABLE app_outputs(
		id TEXT PRIMARY KEY,
		app TEXT NOT NULL,
		source_type TEXT NOT NULL,
		action TEXT NOT NULL,
		version INTEGER NOT NULL,
		name TEXT NOT NULL,
		mime TEXT NOT NULL,
		blob TEXT NOT NULL REFERENCES output_blobs(hash),
		inbox INTEGER NOT NULL,
		actor TEXT NOT NULL,
		actor_kind TEXT NOT NULL,
		at TEXT NOT NULL,
		sources TEXT NOT NULL,
		run TEXT NOT NULL,
		step TEXT NOT NULL,
		run_key TEXT,
		UNIQUE(app, run_key)
	);
	CREATE INDEX app_outputs_inbox ON app_outputs(app, inbox, id DESC);
`

// Output is an immutable file receipt. Run, when present, names the durable
// invocation. Sources name the exact input revisions when rendering rows.
type Output struct {
	ID         string         `json:"id"`
	App        string         `json:"app"`
	SourceType string         `json:"source_type"`
	Action     string         `json:"action"`
	Version    int            `json:"version"`
	Name       string         `json:"name"`
	MIME       string         `json:"mime"`
	Blob       string         `json:"blob"`
	Inbox      bool           `json:"inbox,omitempty"`
	Actor      string         `json:"actor"`
	ActorKind  string         `json:"actor_kind"`
	At         string         `json:"at"`
	Sources    []OutputSource `json:"sources,omitempty"`
	Run        string         `json:"run,omitempty"`
	Step       string         `json:"step,omitempty"`
}

type OutputSource struct {
	ID  string `json:"id"`
	Rev int64  `json:"rev"`
}

const outputFields = `id, app, source_type, action, version, name, mime, blob, inbox, actor, actor_kind, at, sources, run, step`

type rowScanner interface{ Scan(...any) error }

func scanOutput(row rowScanner) (Output, error) {
	var o Output
	var sources string
	err := row.Scan(&o.ID, &o.App, &o.SourceType, &o.Action, &o.Version, &o.Name, &o.MIME, &o.Blob, &o.Inbox,
		&o.Actor, &o.ActorKind, &o.At, &sources, &o.Run, &o.Step)
	if err == nil {
		err = json.Unmarshal([]byte(sources), &o.Sources)
	}
	return o, err
}

// RunOutput lets a resumed run acknowledge a file already committed in its
// previous attempt, even if the underlying records have since changed.
func (s *Store) RunOutput(sc Scope, key string) (Output, bool, error) {
	if key == "" {
		return Output{}, false, nil
	}
	o, err := scanOutput(s.rdb.QueryRow("SELECT "+outputFields+" FROM app_outputs WHERE app=? AND run_key=?", sc.App, key))
	if err == nil {
		err = s.allowOutput(sc, o)
	}
	return o, err == nil, ignoreMissing(err)
}

// SaveOutput commits the blob, receipt and inbox delivery together.
func (s *Store) SaveOutput(sc Scope, o Output, key string, content []byte) (Output, error) {
	if sc.App == "" || o.App != sc.App || (key != "" && (!sc.Flow || o.Run == "" || o.Step == "")) {
		return Output{}, errorf(CodeInvalid, "an output needs an app and valid run provenance")
	}
	err := s.write(func(w *writeTx) error {
		if _, err := w.schema.allow(sc, o.SourceType, false); err != nil {
			return err
		}
		p, err := readPublication(w.tx, sc.App)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && (p.Version != o.Version || p.Archived || p.Deleted) {
			return errorf(CodeStale, "the app has changed since rendering started")
		}
		var runKey any
		if key != "" {
			previous, err := scanOutput(w.tx.QueryRow("SELECT "+outputFields+" FROM app_outputs WHERE app=? AND run_key=?", sc.App, key))
			switch {
			case err == nil:
				o = previous
				return nil
			case !errors.Is(err, sql.ErrNoRows):
				return err
			}
			runKey = key
		}
		o.ID, o.At = s.ids.next(w.now), w.now.UTC().Format(stamp)
		o.Actor, o.ActorKind = w.schema.actor(sc), sc.kind("")
		o.Blob = fmt.Sprintf("sha256:%x", sha256.Sum256(content))
		if _, err := w.tx.Exec("INSERT OR IGNORE INTO output_blobs VALUES(?, ?)", o.Blob, content); err != nil {
			return err
		}
		_, err = w.tx.Exec(`INSERT INTO app_outputs VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			o.ID, o.App, o.SourceType, o.Action, o.Version, o.Name, o.MIME, o.Blob, o.Inbox, o.Actor,
			o.ActorKind, o.At, canonicalJSON(o.Sources), o.Run, o.Step, runKey)
		return err
	})
	return o, err
}

func (s *Store) allowOutput(sc Scope, o Output) error {
	if sc.App == "" || sc.App != o.App {
		return errorf(CodeNotFound, "there is no output with that id")
	}
	_, err := s.Type(sc, o.SourceType)
	return err
}

// OutputFile reads a file by its scoped receipt, since knowing the blob hash
// is neither a capability nor a path into the data store.
func (s *Store) OutputFile(sc Scope, id string) (Output, []byte, error) {
	o, err := scanOutput(s.rdb.QueryRow("SELECT "+outputFields+" FROM app_outputs WHERE app=? AND id=?", sc.App, id))
	if errors.Is(err, sql.ErrNoRows) {
		err = errorf(CodeNotFound, "there is no output with that id")
	}
	if err == nil {
		err = s.allowOutput(sc, o)
	}
	if err != nil {
		return Output{}, nil, err
	}
	var content []byte
	err = s.rdb.QueryRow("SELECT content FROM output_blobs WHERE hash=?", o.Blob).Scan(&content)
	return o, content, err
}

// OutputPage keeps a cursor even if a revoked grant hid some receipts.
type OutputPage struct {
	Items []Output `json:"items"`
	Next  string   `json:"next,omitempty"`
}

// Outputs lists saved files, optionally limited to one run or inbox deliveries.
func (s *Store) Outputs(sc Scope, run, before string, inbox bool) (OutputPage, error) {
	if sc.App == "" {
		return OutputPage{}, errorf(CodeInvalid, "an app is required to list outputs")
	}
	const pageSize = 100
	rows, err := s.rdb.Query("SELECT "+outputFields+` FROM app_outputs
		WHERE app=? AND (?='' OR run=?) AND (?='' OR id<?) AND (?=0 OR inbox=1)
		ORDER BY id DESC LIMIT ?`, sc.App, run, run, before, before, inbox, pageSize+1)
	if err != nil {
		return OutputPage{}, err
	}
	defer rows.Close()
	page := OutputPage{Items: []Output{}}
	last := ""
	for i := 0; rows.Next(); i++ {
		if i == pageSize {
			page.Next = last
			break
		}
		o, err := scanOutput(rows)
		if err != nil {
			return OutputPage{}, err
		}
		last = o.ID
		if err := s.allowOutput(sc, o); err == nil {
			page.Items = append(page.Items, o)
		}
	}
	return page, rows.Err()
}
