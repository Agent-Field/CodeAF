package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// These bounds are shared by SQLite constraints and the domain validator.
const (
	WorkstreamGoAheadCapMax      = 10000
	WorkstreamGoAheadDurationMax = 90 * 24 * time.Hour
)

var ErrWorkstreamDelete = errors.New("mark a workstream done instead")

// WorkstreamTaken identifies the existing owner so adapters can explain an
// atomic refusal without another read that could race a rename.
type WorkstreamTaken struct {
	Factory        string
	Job            int
	WorkstreamID   string
	WorkstreamName string
}

func (e *WorkstreamTaken) Error() string { return "that job belongs to another workstream" }

// Write runs fn in the store's immediate transaction, including any schema
// upgrade. Returning an error rolls back everything. The callback must use tx
// rather than reenter the Store, which owns a single database connection.
func (s *Store) Write(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.beginWrite(ctx, true)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

// Read runs fn against a consistent read-only snapshot. Version 0 means blank;
// workstream readers must answer empty/not found for every version below 3.
// Reads never initialize or migrate the database, and share Write's busy mapping.
func (s *Store) Read(ctx context.Context, fn func(tx *sql.Tx, version int) error) error {
	tx, version, err := s.beginRead(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return storeError(fn(tx, version))
}

// beginRead keeps the schema decision and projected rows in one snapshot. A
// peer may migrate between separate transactions, adding fields an old projection
// cannot supply (notably factory on a new job ref).
func (s *Store) beginRead(ctx context.Context) (*sql.Tx, int, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, storeError(err)
	}
	state, err := inspectSchema(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return nil, 0, storeError(err)
	}
	return tx, state.version(), nil
}

// CreateIn preserves collection naming rules within a larger atomic write.
func CreateIn(ctx context.Context, tx *sql.Tx, id, name string, at int64) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := nameTaken(ctx, tx, name, ""); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO collections(id,name,created_at) VALUES (?,?,?)", id, name, at)
	return err
}

// RenameIn preserves Unicode name uniqueness within a larger atomic write.
func RenameIn(ctx context.Context, tx *sql.Tx, id, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := requireCollection(ctx, tx, id); err != nil {
		return err
	}
	if err := nameTaken(ctx, tx, name, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE collections SET name=? WHERE id=?", name, id)
	return err
}

func touchWorkstream(ctx context.Context, tx *sql.Tx, id string, at int64) error {
	_, err := tx.ExecContext(ctx, "UPDATE workstreams SET updated_at=max(updated_at+1,?) WHERE id=?", at, id)
	return err
}

func checkWorkstreamJob(ctx context.Context, tx *sql.Tx, id string, ref Ref) error {
	if ref.Kind != FactoryJobKind {
		return nil
	}
	var taken WorkstreamTaken
	var isWorkstream bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workstreams WHERE id=?)", id).Scan(&isWorkstream); err != nil {
		return err
	}
	if !isWorkstream {
		return nil
	}
	err := tx.QueryRowContext(ctx, `SELECT j.workstream_id,c.name FROM workstream_jobs j JOIN collections c ON c.id=j.workstream_id
 WHERE j.factory=? AND j.job=? AND j.workstream_id!=?`, ref.Factory, ref.ID, id).Scan(&taken.WorkstreamID, &taken.WorkstreamName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	n, _ := strconv.ParseInt(ref.ID, 10, 64)
	taken.Factory, taken.Job = ref.Factory, int(n)
	return &taken
}

func factoryColumn(version int) string {
	if version >= 3 {
		return "factory"
	}
	return "''"
}

func workstreamColumn(version int) string {
	if version >= 3 {
		return "EXISTS(SELECT 1 FROM workstreams w WHERE w.id=c.id)"
	}
	return "0"
}

// Claims are maintained by triggers so even SQL writes through Write cannot
// accidentally bypass the unique job owner. Plain collections have no claims.
var workstreamSchema = fmt.Sprintf(`
CREATE TABLE workstreams (
 id TEXT PRIMARY KEY REFERENCES collections(id) ON DELETE CASCADE,
 why TEXT NOT NULL DEFAULT '', origin_chat_id TEXT NOT NULL DEFAULT '', agent_id TEXT NOT NULL,
 autonomy TEXT NOT NULL DEFAULT 'propose' CHECK(autonomy IN ('propose','small','off')),
 wake_morning INTEGER NOT NULL DEFAULT 0, wake_related_issues INTEGER NOT NULL DEFAULT 0,
 budget_usd REAL CHECK(budget_usd IS NULL OR budget_usd>0),
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, started_at INTEGER, done_at INTEGER,
 how_it_went TEXT NOT NULL DEFAULT '', client_id TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX workstreams_client ON workstreams(client_id) WHERE client_id!='';
CREATE TABLE workstream_jobs (
 member_seq INTEGER PRIMARY KEY REFERENCES memberships(seq) ON DELETE CASCADE,
 workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 factory TEXT NOT NULL, job TEXT NOT NULL
);
CREATE UNIQUE INDEX workstream_job_owner ON workstream_jobs(factory,job);
CREATE TRIGGER workstream_job_insert AFTER INSERT ON memberships
 WHEN NEW.kind='factory_job' AND EXISTS(SELECT 1 FROM workstreams WHERE id=NEW.collection_id)
 BEGIN INSERT INTO workstream_jobs VALUES(NEW.seq,NEW.collection_id,NEW.factory,NEW.ref_id); END;
CREATE TRIGGER workstream_job_update AFTER UPDATE ON memberships
 BEGIN
 DELETE FROM workstream_jobs WHERE member_seq=OLD.seq;
 INSERT INTO workstream_jobs SELECT NEW.seq,NEW.collection_id,NEW.factory,NEW.ref_id
 WHERE NEW.kind='factory_job' AND EXISTS(SELECT 1 FROM workstreams WHERE id=NEW.collection_id);
 END;
CREATE TRIGGER workstream_job_promote AFTER INSERT ON workstreams
 BEGIN INSERT INTO workstream_jobs SELECT seq,collection_id,factory,ref_id FROM memberships
 WHERE collection_id=NEW.id AND kind='factory_job'; END;
CREATE TABLE workstream_pages (
 id TEXT PRIMARY KEY, workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 parent_id TEXT REFERENCES workstream_pages(id) ON DELETE CASCADE,
 title TEXT NOT NULL, owner TEXT NOT NULL CHECK(owner IN ('you','agent')),
 read_by_jobs INTEGER NOT NULL DEFAULT 0, position INTEGER NOT NULL CHECK(position>=0),
 version INTEGER NOT NULL CHECK(version>0), updated_at INTEGER NOT NULL,
 updated_by TEXT NOT NULL CHECK(updated_by IN ('you','agent')), updated_from TEXT,
 doc TEXT NOT NULL, overview INTEGER NOT NULL DEFAULT 0 CHECK(overview IN (0,1)),
 deleted_at INTEGER, deleted_root TEXT
);
CREATE INDEX workstream_page_tree ON workstream_pages(workstream_id,parent_id,position);
CREATE UNIQUE INDEX workstream_overview ON workstream_pages(workstream_id) WHERE overview=1;
CREATE TABLE workstream_page_versions (
 page_id TEXT NOT NULL REFERENCES workstream_pages(id) ON DELETE CASCADE,
 version INTEGER NOT NULL, saved_at INTEGER NOT NULL, saved_by TEXT NOT NULL CHECK(saved_by IN ('you','agent')),
 source TEXT, title TEXT NOT NULL, doc TEXT NOT NULL, bytes INTEGER NOT NULL,
 PRIMARY KEY(page_id,version)
);
CREATE TABLE workstream_records (
 id TEXT PRIMARY KEY, workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('decision','question','goahead')), text TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('proposed','kept','dropped','superseded')),
 actor TEXT NOT NULL CHECK(actor IN ('you','agent')), source TEXT, created_at INTEGER NOT NULL,
 decided_at INTEGER, supersedes TEXT REFERENCES workstream_records(id), superseded_by TEXT REFERENCES workstream_records(id),
 goahead TEXT, goahead_id TEXT
);
CREATE INDEX workstream_record_list ON workstream_records(workstream_id,created_at);
CREATE TABLE workstream_goaheads (
 id TEXT PRIMARY KEY, workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
 scope TEXT NOT NULL CHECK(scope IN ('job','wave','milestone')),
 factory TEXT, job INTEGER, wave INTEGER, milestone_id TEXT,
 cap_usd REAL NOT NULL CHECK(cap_usd>0 AND cap_usd<=%d), ends_at INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('active','ended')),
 end_reason TEXT CHECK(end_reason IN ('expired','revoked','workstream_done')),
 granted_at INTEGER NOT NULL, ended_at INTEGER,
 CHECK((scope='job' AND factory IS NOT NULL AND length(factory)>0 AND job IS NOT NULL AND job>0 AND wave IS NULL AND milestone_id IS NULL)
 OR (scope='wave' AND wave IS NOT NULL AND wave>0 AND factory IS NULL AND job IS NULL AND milestone_id IS NULL)
 OR (scope='milestone' AND milestone_id IS NOT NULL AND length(milestone_id)>0 AND factory IS NULL AND job IS NULL AND wave IS NULL)),
 CHECK(ends_at>granted_at AND ends_at<=granted_at+%d)
);
CREATE INDEX workstream_goahead_list ON workstream_goaheads(workstream_id,state);
CREATE TABLE workstream_plans (
 workstream_id TEXT PRIMARY KEY REFERENCES workstreams(id) ON DELETE CASCADE,
 version INTEGER NOT NULL CHECK(version>0), updated_at INTEGER NOT NULL,
 updated_by TEXT NOT NULL CHECK(updated_by IN ('you','agent')),
 milestones TEXT NOT NULL, waves TEXT NOT NULL
);
`, WorkstreamGoAheadCapMax, WorkstreamGoAheadDurationMax.Milliseconds())

// Opening a schema-3 file verifies the tables needed by both domain reads and
// collection ownership checks. A partial schema is refused rather than repaired.
var workstreamSchemaReads = []string{
	"SELECT id,why,origin_chat_id,agent_id,autonomy,wake_morning,wake_related_issues,budget_usd,created_at,updated_at,started_at,done_at,how_it_went,client_id FROM workstreams LIMIT 0",
	"SELECT member_seq,workstream_id,factory,job FROM workstream_jobs LIMIT 0",
	"SELECT id,workstream_id,parent_id,title,owner,read_by_jobs,position,version,updated_at,updated_by,updated_from,doc,overview,deleted_at,deleted_root FROM workstream_pages LIMIT 0",
	"SELECT page_id,version,saved_at,saved_by,source,title,doc,bytes FROM workstream_page_versions LIMIT 0",
	"SELECT id,workstream_id,kind,text,state,actor,source,created_at,decided_at,supersedes,superseded_by,goahead,goahead_id FROM workstream_records LIMIT 0",
	"SELECT id,workstream_id,scope,factory,job,wave,milestone_id,cap_usd,ends_at,state,end_reason,granted_at,ended_at FROM workstream_goaheads LIMIT 0",
	"SELECT workstream_id,version,updated_at,updated_by,milestones,waves FROM workstream_plans LIMIT 0",
}
