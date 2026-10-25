// Package workspaceidentity separates durable workspace names from local paths.
// A record describes placement; it never grants permission to write or delete it.
package workspaceidentity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Agent-Field/codeaf/internal/sqliteuri"
	_ "modernc.org/sqlite"
)

// ErrUnknown means transfer or recovery has not admitted an identity locally.
var ErrUnknown = errors.New("workspace identity has not been admitted on this device; take or restore its task first")

type Role string

const (
	Project        Role = "project"
	Chat           Role = "chat"
	Run            Role = "run"
	Log            Role = "log"
	Temp           Role = "temp"
	Cache          Role = "cache"
	ExternalRemote Role = "external-remote"
)

// Record is a private device-local binding, not a deletion or access capability.
type Record struct {
	URI        string `json:"uri"`
	Role       Role   `json:"role"`
	Path       string `json:"path"`
	OwnerURI   string `json:"owner_uri,omitempty"`
	ProjectURI string `json:"project_uri,omitempty"`
}

// Links describe local ownership and an optional project reference. A chat can
// own runs across projects; its own record need not belong to one project.
type Links struct{ OwnerURI, ProjectURI string }

// Registry owns metadata only. Database handles are scoped to each operation,
// so opening a worker does not retain an engine-lifetime connection or cache.
type Registry struct{ root, scope string }

const schema = `
CREATE TABLE IF NOT EXISTS metadata (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL, namespace TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS workspaces (
 uri TEXT PRIMARY KEY, role TEXT NOT NULL, path TEXT NOT NULL,
 owner_uri TEXT NOT NULL, project_uri TEXT NOT NULL,
 UNIQUE(role,path)
);
CREATE TABLE IF NOT EXISTS project_aliases (
 alias_uri TEXT PRIMARY KEY,
 canonical_uri TEXT NOT NULL REFERENCES workspaces(uri)
);
CREATE INDEX IF NOT EXISTS project_aliases_canonical ON project_aliases(canonical_uri);
`

// Open opens a private registry. Legacy receipt migration and namespace creation
// commit in one transaction. Failed migration never resets an existing identity.
func Open(path string) (*Registry, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("workspace registry requires an absolute path")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("workspace registry directory must be private (0700)")
	}
	registry := &Registry{root: resolved}
	db, err := registry.database(true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		return nil, err
	}
	var version int
	err = tx.QueryRow("SELECT version,namespace FROM metadata WHERE id=1").Scan(&version, &registry.scope)
	if errors.Is(err, sql.ErrNoRows) {
		registry.scope, err = registry.migrate(tx)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec("INSERT INTO metadata(id,version,namespace) VALUES(1,1,?)", registry.scope); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if version != 1 || !validID(registry.scope) {
		return nil, errors.New("unsupported or corrupt workspace registry")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return registry, nil
}

// Register admits a workspace once per role and canonical path. Repeated
// admission keeps its identity and refuses a different owner or project.
func (r *Registry) Register(role Role, path string, links ...Links) (Record, error) {
	if !validRole(role) {
		return Record{}, errors.New("unknown workspace role")
	}
	if len(links) > 1 {
		return Record{}, errors.New("workspace has more than one owner")
	}
	binding, err := validateBinding(role, path)
	if err != nil {
		return Record{}, err
	}
	var link Links
	if len(links) == 1 {
		link = links[0]
	}
	var record Record
	err = r.transaction(func(tx *sql.Tx) error {
		link, err = validatedLinks(tx, link)
		if err != nil {
			return err
		}
		previous, err := byPath(tx, role, binding)
		if err == nil {
			if previous.OwnerURI != link.OwnerURI || link.ProjectURI != "" && previous.ProjectURI != link.ProjectURI {
				return errors.New("workspace path already has another owner or project")
			}
			record = previous
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		id, err := randomID()
		if err != nil {
			return err
		}
		record = Record{URI: "codeaf-workspace://" + r.scope + "/" + id, Role: role, Path: binding, OwnerURI: link.OwnerURI, ProjectURI: link.ProjectURI}
		if role == Project {
			record.ProjectURI = record.URI
		}
		return insert(tx, record)
	})
	return record, err
}

// Resolve accepts local identities and explicitly adopted relay identities.
// Missing parked trees keep their binding; existing components cannot redirect it.
func (r *Registry) Resolve(uri string) (Record, error) {
	if !validURI(uri) {
		return Record{}, errors.New("malformed workspace identity")
	}
	db, err := r.database(false)
	if err != nil {
		return Record{}, err
	}
	defer db.Close()
	record, err := byURI(db, uri)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrUnknown
	}
	if err != nil {
		return Record{}, err
	}
	if err := validateResolvedBinding(record.Role, record.Path); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Lookup finds an already admitted role/path without creating an identity.
// It is used when a nested task's ground is an existing owned parent run.
func (r *Registry) Lookup(role Role, path string) (Record, error) {
	if !validRole(role) {
		return Record{}, errors.New("unknown workspace role")
	}
	binding, err := validateBinding(role, path)
	if err != nil {
		return Record{}, err
	}
	db, err := r.database(false)
	if err != nil {
		return Record{}, err
	}
	defer db.Close()
	return byPath(db, role, binding)
}

// LookupRecorded reads an admitted role/path even when its directory is absent.
// It returns metadata, never permission to reopen, write, or delete the path.
func (r *Registry) LookupRecorded(role Role, path string) (Record, error) {
	if !validRole(role) {
		return Record{}, errors.New("unknown workspace role")
	}
	if err := validateResolvedBinding(role, path); err != nil {
		return Record{}, err
	}
	db, err := r.database(false)
	if err != nil {
		return Record{}, err
	}
	defer db.Close()
	record, err := byPath(db, role, path)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrUnknown
	}
	return record, err
}

// Bind explicitly relocates an already admitted identity without changing its
// role or links. Equivalent projects require an explicit group move instead.
// Callers establish relocation authority before calling it.
func (r *Registry) Bind(uri, path string) (Record, error) {
	if !validURI(uri) {
		return Record{}, errors.New("malformed workspace identity")
	}
	var record Record
	err := r.transaction(func(tx *sql.Tx) error {
		var err error
		record, err = byURI(tx, uri)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnknown
		}
		if err != nil {
			return err
		}
		binding, err := validateBinding(record.Role, path)
		if err != nil {
			return err
		}
		if binding == record.Path {
			return nil
		}
		if err := refuseProjectGroupMove(tx, record); err != nil {
			return err
		}
		record.Path = binding
		collision, err := byPath(tx, record.Role, record.Path)
		if err == nil && collision.URI != uri {
			return errors.New("workspace path already has another identity")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.Exec("UPDATE workspaces SET path=? WHERE uri=?", record.Path, uri)
		return err
	})
	return record, err
}

// Adopt binds an authenticated incoming identity to restored local material.
// Only an accepted transfer may call it. Ordinary registration cannot invent a
// foreign identity, and importing one grants no permission to delete its files.
// A new incoming project may alias an existing local project with the same owner;
// canonical path registration remains stable and neither identity is replaced.
func (r *Registry) Adopt(incoming Record, path string, links Links) (Record, error) {
	if !validURI(incoming.URI) || !validRole(incoming.Role) {
		return Record{}, errors.New("malformed incoming workspace identity")
	}
	binding, err := validateBinding(incoming.Role, path)
	if err != nil {
		return Record{}, err
	}
	candidate := Record{URI: incoming.URI, Role: incoming.Role, Path: binding}
	err = r.transaction(func(tx *sql.Tx) error {
		var err error
		links, err = validatedLinks(tx, links)
		if err != nil {
			return err
		}
		candidate.OwnerURI, candidate.ProjectURI = links.OwnerURI, links.ProjectURI
		if candidate.Role == Project {
			candidate.ProjectURI = candidate.URI
		}
		if incoming.OwnerURI != "" && incoming.OwnerURI != candidate.OwnerURI || incoming.ProjectURI != "" && incoming.ProjectURI != candidate.ProjectURI {
			return errors.New("incoming ownership links do not match the accepted transfer")
		}
		previous, err := byURI(tx, incoming.URI)
		exists := err == nil
		if exists {
			if previous.Role != candidate.Role || previous.OwnerURI != candidate.OwnerURI || previous.ProjectURI != candidate.ProjectURI {
				return errors.New("transfer cannot change an existing identity's role or ownership")
			}
			if previous.Path == candidate.Path {
				return nil
			}
			if err := refuseProjectGroupMove(tx, previous); err != nil {
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		collision, err := byPath(tx, candidate.Role, candidate.Path)
		if err == nil && collision.URI != candidate.URI {
			// Accepted transfer establishes project equivalence, not a second claim
			// over a run or chat. The receiver's canonical identity stays unchanged.
			if !exists && candidate.Role == Project && collision.OwnerURI == candidate.OwnerURI {
				_, err = tx.Exec("INSERT INTO project_aliases(alias_uri,canonical_uri) VALUES(?,?)", candidate.URI, collision.URI)
				return err
			}
			return errors.New("restored workspace path already has another identity")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if exists {
			_, err = tx.Exec("UPDATE workspaces SET path=? WHERE uri=?", candidate.Path, candidate.URI)
			return err
		}
		return insert(tx, candidate)
	})
	return candidate, err
}

// An individual binding cannot relocate a project shared by admitted aliases.
// A future explicit group move must validate every affected local reference.
func refuseProjectGroupMove(tx *sql.Tx, record Record) error {
	if record.Role != Project {
		return nil
	}
	var shared bool
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM project_aliases WHERE alias_uri=? OR canonical_uri=?)", record.URI, record.URI).Scan(&shared); err != nil {
		return err
	}
	if shared {
		return errors.New("project has equivalent identities; individual relocation requires an explicit group move")
	}
	return nil
}

type queryer interface{ QueryRow(string, ...any) *sql.Row }

const columns = "uri,role,path,owner_uri,project_uri"

func scan(row *sql.Row) (Record, error) {
	var record Record
	if err := row.Scan(&record.URI, &record.Role, &record.Path, &record.OwnerURI, &record.ProjectURI); err != nil {
		return Record{}, err
	}
	if !validURI(record.URI) || !validRole(record.Role) {
		return Record{}, errors.New("corrupt workspace registry record")
	}
	return record, nil
}
func byURI(db queryer, uri string) (Record, error) {
	return scan(db.QueryRow("SELECT "+columns+` FROM workspaces WHERE uri=?
 UNION ALL SELECT a.alias_uri,w.role,w.path,w.owner_uri,a.alias_uri
 FROM project_aliases a JOIN workspaces w ON w.uri=a.canonical_uri
 WHERE a.alias_uri=? AND w.role=?`, uri, uri, Project))
}
func byPath(db queryer, role Role, path string) (Record, error) {
	return scan(db.QueryRow("SELECT "+columns+" FROM workspaces WHERE role=? AND path=?", role, path))
}
func insert(tx *sql.Tx, record Record) error {
	_, err := tx.Exec("INSERT INTO workspaces("+columns+") VALUES(?,?,?,?,?)", record.URI, record.Role, record.Path, record.OwnerURI, record.ProjectURI)
	return err
}
func validatedLinks(tx *sql.Tx, links Links) (Links, error) {
	if links.OwnerURI != "" {
		if !validURI(links.OwnerURI) {
			return Links{}, errors.New("malformed workspace owner identity")
		}
		owner, err := byURI(tx, links.OwnerURI)
		if err != nil {
			return Links{}, err
		}
		if links.ProjectURI == "" {
			links.ProjectURI = owner.ProjectURI
		}
	}
	if links.ProjectURI != "" {
		if !validURI(links.ProjectURI) {
			return Links{}, errors.New("malformed workspace project identity")
		}
		project, err := byURI(tx, links.ProjectURI)
		if err != nil {
			return Links{}, err
		}
		if project.Role != Project {
			return Links{}, errors.New("workspace project reference is not a project")
		}
	}
	return links, nil
}
func (r *Registry) transaction(change func(*sql.Tx) error) error {
	db, err := r.database(false)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := change(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Registry) database(create bool) (*sql.DB, error) {
	path := filepath.Join(r.root, "registry.sqlite")
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("workspace registry database is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	uri := sqliteuri.File(path)
	uri.RawQuery = "mode=rw&_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)"
	if create {
		uri.RawQuery = strings.Replace(uri.RawQuery, "mode=rw&", "mode=rwc&", 1)
	}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}
func validURI(uri string) bool {
	const prefix = "codeaf-workspace://"
	if !strings.HasPrefix(uri, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(uri, prefix), "/")
	return len(parts) == 2 && validID(parts[0]) && validID(parts[1])
}
func validRole(role Role) bool {
	switch role {
	case Project, Chat, Run, Log, Temp, Cache, ExternalRemote:
		return true
	}
	return false
}

func validateBinding(role Role, path string) (string, error) {
	if role == ExternalRemote {
		u, err := url.Parse(path)
		if err != nil || u.Scheme == "" || u.Host == "" || u.Scheme == "file" || u.Scheme == "codeaf-workspace" || u.User != nil {
			return "", errors.New("external workspace requires an explicit remote URI without credentials")
		}
		return path, nil
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("workspace binding must be a clean absolute directory")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("workspace binding is not a directory")
	}
	return resolved, nil
}

// A parked workspace may be absent. Existing path components must still match
// their recorded spelling, so reopening cannot silently follow a substituted link.
func validateResolvedBinding(role Role, path string) error {
	if role == ExternalRemote {
		_, err := validateBinding(role, path)
		return err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("invalid workspace binding path")
	}
	current := path
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if resolved != current {
				return errors.New("workspace binding traverses a symbolic link")
			}
			info, err := os.Stat(current)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return errors.New("workspace binding is not a directory")
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return err
		}
		current = parent
	}
}

func randomID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
