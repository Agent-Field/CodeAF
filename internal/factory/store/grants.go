package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
)

// accessSchema holds the access model's tables, created beside the workstream
// tables in the one SQLite file the org server already keeps: grants (the
// relationship tuples), resources (chats and sessions, which have no document
// of their own) and org_policy (a single row with a revision). Legacy owner,
// shares and private stay on the workstream and canvas documents and are read
// as tuples, so nothing here has to migrate.
const accessSchema = `
CREATE TABLE IF NOT EXISTS grants (
	id TEXT PRIMARY KEY,
	subject_kind TEXT NOT NULL,
	subject_id TEXT NOT NULL,
	resource_kind TEXT NOT NULL,
	resource_id TEXT NOT NULL,
	role TEXT NOT NULL,
	granted_by_kind TEXT NOT NULL,
	granted_by_id TEXT NOT NULL,
	sponsor TEXT NOT NULL,
	note TEXT NOT NULL,
	created_at TEXT NOT NULL,
	expires_at TEXT
);
CREATE INDEX IF NOT EXISTS grants_resource ON grants (resource_kind, resource_id);
CREATE INDEX IF NOT EXISTS grants_subject ON grants (subject_kind, subject_id);
CREATE INDEX IF NOT EXISTS grants_expiry ON grants (expires_at) WHERE expires_at IS NOT NULL;
CREATE TABLE IF NOT EXISTS grant_sweep (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	covered_ms INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS resources (
	kind TEXT NOT NULL,
	id TEXT NOT NULL,
	parent_kind TEXT NOT NULL,
	parent_id TEXT NOT NULL,
	owner_kind TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	created_by_kind TEXT NOT NULL,
	created_by_id TEXT NOT NULL,
	sponsor TEXT NOT NULL,
	label TEXT NOT NULL,
	visibility TEXT NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (kind, id)
);
CREATE INDEX IF NOT EXISTS resources_parent ON resources (kind, parent_kind, parent_id);
CREATE TABLE IF NOT EXISTS resource_move_cleanup (
	kind TEXT NOT NULL,
	id TEXT NOT NULL,
	PRIMARY KEY (kind, id)
);
CREATE TABLE IF NOT EXISTS org_policy (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	rev INTEGER NOT NULL,
	doc TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	updated_by TEXT NOT NULL
);
` + botsSchema

// grantColumns is the column list every grant read and write uses.
const grantColumns = `id, subject_kind, subject_id, resource_kind, resource_id, role,
	granted_by_kind, granted_by_id, sponsor, note, created_at, expires_at`

// AddGrant stores a grant and returns it with its new id. It checks nothing
// about who may give it: the serve layer decides that with acl.MayGrant.
func (st *Store) AddGrant(g acl.Grant) (acl.Grant, error) {
	return st.addGrant(g, nil, nil, nil)
}

// AddGrantAtPolicy rejects a grant authorized against a policy that has since changed.
func (st *Store) AddGrantAtPolicy(g acl.Grant, rev int, now func() time.Time, parent *acl.Resource) (acl.Grant, error) {
	if g.Resource.Kind != "canvas" {
		return st.addGrant(g, &rev, now, parent)
	}
	var added acl.Grant
	err := st.underLock(filepath.Join(st.root, canvasesLock), func() error {
		var err error
		added, err = st.addGrant(g, &rev, now, parent)
		return err
	})
	return added, err
}

// addGrant inserts the grant inside one transaction, first re-checking the
// policy revision and that the target still accepts grants when a revision is
// given, so a grant authorized against an older policy is never written.
func (st *Store) addGrant(g acl.Grant, rev *int, now func() time.Time, parent *acl.Resource) (acl.Grant, error) {
	err := st.wsTx(func(tx *sql.Tx) error {
		if rev != nil {
			if err := checkPolicyRev(tx, *rev); err != nil {
				return err
			}
			if err := st.checkGrantTarget(tx, g.Resource, parent); err != nil {
				return err
			}
			if g.ExpiresAt != nil {
				last, err := grantSweepWatermark(tx)
				if err != nil {
					return err
				}
				if !now().Before(*g.ExpiresAt) || !time.UnixMilli(last).Before(*g.ExpiresAt) {
					return ErrGrantExpiry
				}
			}
		}
		return st.insertGrant(tx, &g)
	})
	if err != nil {
		return acl.Grant{}, err
	}
	return g, nil
}

// ErrGrantTargetChanged tells the caller to check the resource again after a concurrent change.
var ErrGrantTargetChanged = errors.New("the item changed while you were sharing it; read it again")

// ErrGrantExpiry refuses a timestamp the server has already reached.
var ErrGrantExpiry = errors.New("an expiry must be later than now")

// checkGrantTarget re-reads the target inside the grant's transaction. A
// canvas or workstream that turned private since the caller checked must not
// receive a grant, because a private item's grants would grant nothing and
// would surprise whoever later makes it shared.
func (st *Store) checkGrantTarget(tx *sql.Tx, ref acl.Resource, parent *acl.Resource) error {
	workstream := ref.ID
	switch ref.Kind {
	case "canvas":
		canvas, kept, err := st.Canvas(ref.ID)
		if err != nil {
			return err
		}
		if !kept || canvas.Private && (canvas.Workstream == "" || canvas.PolicyPrivate) {
			return ErrGrantTargetChanged
		}
		workstream = canvas.Workstream
		if workstream == "" {
			return nil
		}
	case "workstream":
	default:
		var current acl.Resource
		err := tx.QueryRow(`SELECT parent_kind, parent_id FROM resources WHERE kind = ? AND id = ?`, ref.Kind, ref.ID).Scan(&current.Kind, &current.ID)
		if errors.Is(err, sql.ErrNoRows) || err == nil && parent != nil && current != *parent {
			return ErrGrantTargetChanged
		}
		return err
	}
	var raw string
	err := tx.QueryRow(`SELECT doc FROM workstreams WHERE id = ?`, workstream).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrGrantTargetChanged
	}
	if err != nil {
		return err
	}
	var ws factory.OrgWorkstream
	if err := json.Unmarshal([]byte(raw), &ws); err != nil {
		return err
	}
	if ws.Private {
		return ErrGrantTargetChanged
	}
	return nil
}

// insertGrant writes one tuple, stamping its id and time. An agent subject is
// refused here as well as at the door: a grant that named an agent could
// outlive the sponsor's own access, so an agent's rights are policy data only.
func (st *Store) insertGrant(tx *sql.Tx, g *acl.Grant) error {
	if g.Subject.Kind == "agent" {
		return errors.New("give an agent its permissions in the organization policy")
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = st.now()
	}
	id, err := cell.NewID(g.CreatedAt)
	if err != nil {
		return err
	}
	g.ID = "g_" + id
	var expiresAt any
	if g.ExpiresAt != nil {
		expiresAt = accessTime(*g.ExpiresAt)
	}
	_, err = tx.Exec(`INSERT INTO grants (`+grantColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.Subject.Kind, g.Subject.ID, g.Resource.Kind, g.Resource.ID, g.Role,
		g.GrantedBy.Kind, g.GrantedBy.ID, g.GrantedBy.Sponsor, g.Note, accessTime(g.CreatedAt), expiresAt)
	return err
}

// The fixed-width UTC representation keeps expiry comparisons chronological in SQL.
func accessTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

// RemoveGrant deletes a grant by id and keeps nothing; the audit trail is the
// record entry the serve layer writes.
func (st *Store) RemoveGrant(id string) error {
	return st.removeGrant(id, nil)
}

// RemoveGrantAtPolicy deletes a grant only while the policy is still at rev.
func (st *Store) RemoveGrantAtPolicy(id string, rev int) error {
	return st.removeGrant(id, &rev)
}

// RemoveGrantsOfSubject deletes every grant that names one subject, lapsed ones
// included, in one transaction, and answers how many it deleted. It is what
// removing a person from the organization does to what others gave them: a
// grant left behind would apply again to anyone who later joins under the same
// handle. Like RemoveGrant it keeps nothing; the serve layer writes the record.
func (st *Store) RemoveGrantsOfSubject(kind, id string) (int, error) {
	removed := 0
	err := st.wsTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM grants WHERE subject_kind = ? AND subject_id = ?`, kind, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		removed = int(n)
		return err
	})
	return removed, err
}

// removeGrant deletes the row inside one transaction, first checking the
// policy revision when one is given.
func (st *Store) removeGrant(id string, rev *int) error {
	return st.wsTx(func(tx *sql.Tx) error {
		if rev != nil {
			if err := checkPolicyRev(tx, *rev); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`DELETE FROM grants WHERE id = ?`, id)
		return err
	})
}

// GrantsOn lists every stored grant on one resource, lapsed ones included.
// Callers filter with Grant.Live, so the audit rows stay readable.
func (st *Store) GrantsOn(kind, id string) ([]acl.Grant, error) {
	return st.readGrants(`resource_kind = ? AND resource_id = ?`, kind, id)
}

// GrantsOnMany reads the grants on many resources in a few queries, so a list
// of items does not cost a query per item.
func (st *Store) GrantsOnMany(refs []acl.Resource) (map[acl.Resource][]acl.Grant, error) {
	out := make(map[acl.Resource][]acl.Grant, len(refs))
	// Keep batches below SQLite's parameter limit.
	for batch := range slices.Chunk(refs, 400) {
		args := make([]any, 0, 2*len(batch))
		for _, ref := range batch {
			args = append(args, ref.Kind, ref.ID)
		}
		values := strings.TrimSuffix(strings.Repeat("(?, ?),", len(batch)), ",")
		grants, err := st.readGrants(`(resource_kind, resource_id) IN (VALUES `+values+`)`, args...)
		if err != nil {
			return nil, err
		}
		for _, g := range grants {
			out[g.Resource] = append(out[g.Resource], g)
		}
	}
	return out, nil
}

// GrantsOfSubject lists the grants that name one subject, lapsed ones included,
// for an audit of what a person or team has been given.
func (st *Store) GrantsOfSubject(kind, id string) ([]acl.Grant, error) {
	return st.readGrants(`subject_kind = ? AND subject_id = ?`, kind, id)
}

func (st *Store) readGrants(where string, args ...any) ([]acl.Grant, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT `+grantColumns+` FROM grants WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	return scanGrants(rows)
}

// scanGrants reads every grant row and closes the rows.
func scanGrants(rows *sql.Rows) ([]acl.Grant, error) {
	defer rows.Close()
	out := []acl.Grant{}
	for rows.Next() {
		var g acl.Grant
		var created string
		var expires sql.NullString
		if err := rows.Scan(&g.ID, &g.Subject.Kind, &g.Subject.ID, &g.Resource.Kind, &g.Resource.ID, &g.Role,
			&g.GrantedBy.Kind, &g.GrantedBy.ID, &g.GrantedBy.Sponsor, &g.Note, &created, &expires); err != nil {
			return nil, err
		}
		at, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		g.CreatedAt = at
		if expires.Valid {
			expiresAt, err := time.Parse(time.RFC3339Nano, expires.String)
			if err != nil {
				return nil, err
			}
			g.ExpiresAt = &expiresAt
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GrantedResources lists every resource that has at least one stored grant,
// lapsed ones included.
func (st *Store) GrantedResources() ([]acl.Resource, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT DISTINCT resource_kind, resource_id FROM grants ORDER BY resource_kind, resource_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []acl.Resource{}
	for rows.Next() {
		var ref acl.Resource
		if err := rows.Scan(&ref.Kind, &ref.ID); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// Resource is the registry row for a thing that has no document of its own: a
// chat or a session. It records the space it sits in, its owner, who created
// it (the agent and the sponsor, when an agent did), a label and the
// visibility it started with. Its grants live in the grants table.
type Resource struct {
	acl.Resource
	Parent     acl.Resource
	Owner      acl.Subject
	CreatedBy  acl.Principal
	Label      string
	Visibility string
	CreatedAt  time.Time
}

// resourceColumns is the column list every resource read and write uses.
const resourceColumns = `kind, id, parent_kind, parent_id, owner_kind, owner_id,
	created_by_kind, created_by_id, sponsor, label, visibility, created_at`

// PutResource registers a resource, replacing the row when it already exists.
func (st *Store) PutResource(r Resource) error {
	return st.putResource(r, true, nil, nil, nil)
}

// BackfillResource keeps the first registration, including choices made by the creator.
func (st *Store) BackfillResource(r Resource) error {
	return st.putResource(r, false, nil, nil, nil)
}

// RegisterResource commits the initial grant with the resource it makes visible.
func (st *Store) RegisterResource(r Resource, grant *acl.Grant, rev int, save func() error) error {
	return st.putResource(r, false, grant, &rev, save)
}

// putResource writes the registry row and, with it, the grant that makes it
// visible, in one transaction, so a chat made readable by the organization is
// never registered without its grant. replace says whether an existing row is
// overwritten; save runs inside the transaction for a caller that must write
// something else with it.
func (st *Store) putResource(r Resource, replace bool, grant *acl.Grant, rev *int, save func() error) error {
	return st.wsTx(func(tx *sql.Tx) error {
		if rev != nil {
			if err := checkPolicyRev(tx, *rev); err != nil {
				return err
			}
		}
		if err := writeResource(tx, r, replace, rev == nil); err != nil {
			return err
		}
		if grant != nil {
			g := *grant
			g.Resource = r.Resource
			if err := st.insertGrant(tx, &g); err != nil {
				return err
			}
		}
		if save != nil {
			return save()
		}
		return nil
	})
}

// writeResource keeps backfill and creation on the same registry schema.
func writeResource(tx *sql.Tx, r Resource, replace, keepFirst bool) error {
	q := `INSERT INTO resources (` + resourceColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if replace {
		q += ` ON CONFLICT (kind, id) DO UPDATE SET
			parent_kind=excluded.parent_kind, parent_id=excluded.parent_id, owner_kind=excluded.owner_kind,
			owner_id=excluded.owner_id, created_by_kind=excluded.created_by_kind, created_by_id=excluded.created_by_id,
			sponsor=excluded.sponsor, label=excluded.label, visibility=excluded.visibility, created_at=excluded.created_at`
	} else if keepFirst {
		q += ` ON CONFLICT (kind, id) DO NOTHING`
	}
	_, err := tx.Exec(q, r.Kind, r.ID, r.Parent.Kind, r.Parent.ID, r.Owner.Kind, r.Owner.ID, r.CreatedBy.Kind,
		r.CreatedBy.ID, r.CreatedBy.Sponsor, r.Label, r.Visibility, accessTime(r.CreatedAt))
	return err
}

// scanResource reads one registry row.
func scanResource(row interface{ Scan(...any) error }) (Resource, error) {
	var r Resource
	var created string
	err := row.Scan(&r.Kind, &r.ID, &r.Parent.Kind, &r.Parent.ID, &r.Owner.Kind, &r.Owner.ID, &r.CreatedBy.Kind,
		&r.CreatedBy.ID, &r.CreatedBy.Sponsor, &r.Label, &r.Visibility, &created)
	if err == nil {
		r.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	}
	return r, err
}

// Resource returns one registry row, and false when none is kept.
func (st *Store) Resource(kind, id string) (Resource, bool, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return Resource{}, false, err
	}
	r, err := scanResource(db.QueryRow(`SELECT `+resourceColumns+` FROM resources WHERE kind = ? AND id = ?`, kind, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Resource{}, false, nil
	}
	return r, err == nil, err
}

// Resources lists all registrations when kind is empty.
func (st *Store) Resources(kind string) ([]Resource, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return nil, err
	}
	q := `SELECT ` + resourceColumns + ` FROM resources`
	var args []any
	if kind != "" {
		q += ` WHERE kind = ?`
		args = append(args, kind)
	}
	rows, err := db.Query(q+` ORDER BY kind, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Resource{}
	for rows.Next() {
		r, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResourcesIn lists the registrations of one kind that sit directly under
// parent, so a list of one workstream's chats reads only that workstream's rows.
func (st *Store) ResourcesIn(kind string, parent acl.Resource) ([]Resource, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT `+resourceColumns+` FROM resources WHERE kind = ? AND parent_kind = ? AND parent_id = ? ORDER BY id`,
		kind, parent.Kind, parent.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Resource{}
	for rows.Next() {
		r, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DropGrantsOn deletes every stored grant on one resource and returns the rows
// it deleted. A move into a private space uses it, because a private item's
// grants grant nothing and must not be left to reappear if it is shared again.
func (st *Store) DropGrantsOn(kind, id string) ([]acl.Grant, error) {
	var dropped []acl.Grant
	err := st.wsTx(func(tx *sql.Tx) error {
		var err error
		dropped, err = dropGrantsOn(tx, kind, id)
		return err
	})
	return dropped, err
}

func dropGrantsOn(tx *sql.Tx, kind, id string) ([]acl.Grant, error) {
	rows, err := tx.Query(`SELECT `+grantColumns+` FROM grants WHERE resource_kind = ? AND resource_id = ? ORDER BY id`, kind, id)
	if err != nil {
		return nil, err
	}
	dropped, err := scanGrants(rows)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(`DELETE FROM grants WHERE resource_kind = ? AND resource_id = ?`, kind, id)
	return dropped, err
}

// DeleteResource removes a registry row and every grant on it.
func (st *Store) DeleteResource(kind, id string) error {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return err
	}
	return st.wsTx(func(tx *sql.Tx) error {
		return deleteResource(tx, kind, id)
	})
}

// deleteResource is DeleteResource inside a transaction a caller already holds.
func deleteResource(tx *sql.Tx, kind, id string) error {
	if _, err := tx.Exec(`DELETE FROM grants WHERE resource_kind = ? AND resource_id = ?`, kind, id); err != nil {
		return err
	}
	if kind == "chat" {
		if err := clearResourceMove(tx, acl.Resource{Kind: kind, ID: id}); err != nil {
			return err
		}
	}
	_, err := tx.Exec(`DELETE FROM resources WHERE kind = ? AND id = ?`, kind, id)
	return err
}

// deleteChildResources removes the registrations kept inside a workstream that
// is going away, with their grants. A chat outlives nothing it was made in: a
// row left behind would keep a label, and an organization-visible one would
// keep showing it to every member, for a conversation that no longer exists.
func deleteChildResources(tx *sql.Tx, parentID string) error {
	rows, err := tx.Query(`SELECT kind, id FROM resources WHERE parent_id = ?`, parentID)
	if err != nil {
		return err
	}
	var children []acl.Resource
	for rows.Next() {
		var ref acl.Resource
		if err := rows.Scan(&ref.Kind, &ref.ID); err != nil {
			_ = rows.Close()
			return err
		}
		children = append(children, ref)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, ref := range children {
		if err := deleteResource(tx, ref.Kind, ref.ID); err != nil {
			return err
		}
	}
	return nil
}
