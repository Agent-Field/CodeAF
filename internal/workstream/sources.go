package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

const (
	SourceFile       = "file"
	SourceRepo       = "repo"
	SourceIssue      = "issue"
	SourceLink       = "link"
	SourceKept       = "kept"
	SourcesMax       = 2000
	CreateSourcesMax = 200
	SourceTitleMax   = 200
	SourcePathMax    = 4096
	SourceURLMax     = 2048
	SourceIssueMax   = 2147483647
	SourceRefMax     = 128
	SourceWhyMax     = 500
)

type KeptRef struct {
	Kind    string  `json:"kind"`
	ID      string  `json:"id"`
	ChatID  *string `json:"chat_id,omitempty"`
	Factory *string `json:"factory,omitempty"`
}

// Pointers distinguish an absent field from a supplied field of another kind.
type SourceAdd struct {
	Kind       string   `json:"kind"`
	Path       *string  `json:"path,omitempty"`
	Repo       *string  `json:"repo,omitempty"`
	Number     *int     `json:"number,omitempty"`
	URL        *string  `json:"url,omitempty"`
	Ref        *KeptRef `json:"ref,omitempty"`
	Title      *string  `json:"title,omitempty"`
	ReadByJobs bool     `json:"read_by_jobs,omitempty"`
}

type SourceItem struct {
	ID           string
	WorkstreamID string
	Kind         string
	Title        string
	Path         string
	Repo         string
	Number       int
	URL          string
	Ref          *KeptRef
	ReadByJobs   bool
	Proposed     bool
	Why          string
	AddedBy      string
	AddedAt      time.Time
	Gone         bool
	target       string
}

type SourcePatch struct {
	Title      *string
	ReadByJobs *bool
	Keep       *bool
}

type preparedSource struct {
	SourceAdd
	target string
	title  string
}

const sourceItemSelect = `SELECT id,workstream_id,kind,title,coalesce(path,''),coalesce(repo,''),coalesce(number,0),coalesce(url,''),ref,read_by_jobs,proposed,coalesce(why,''),added_by,added_at,target FROM workstream_sources`

type sourceScanner interface{ Scan(...any) error }

func scanSourceItem(row sourceScanner) (SourceItem, error) {
	var out SourceItem
	var ref sql.NullString
	var at int64
	err := row.Scan(&out.ID, &out.WorkstreamID, &out.Kind, &out.Title, &out.Path, &out.Repo, &out.Number, &out.URL, &ref, &out.ReadByJobs, &out.Proposed, &out.Why, &out.AddedBy, &at, &out.target)
	if err != nil {
		return SourceItem{}, found(err)
	}
	out.AddedAt = instant(at)
	if ref.Valid {
		err = json.Unmarshal([]byte(ref.String), &out.Ref)
	}
	return out, err
}

func sourceItemIn(ctx context.Context, tx *sql.Tx, id string) (SourceItem, error) {
	return scanSourceItem(tx.QueryRowContext(ctx, sourceItemSelect+" WHERE id=?", id))
}

func (s *Store) GetSource(ctx context.Context, id string) (SourceItem, error) {
	var out SourceItem
	err := s.workspace.Read(ctx, func(tx *sql.Tx, version int) error {
		if version < workspace.WorkstreamSourcesSchemaVersion {
			return ErrNotFound
		}
		var err error
		out, err = sourceItemIn(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Store) Sources(ctx context.Context, id string) ([]SourceItem, error) {
	out := []SourceItem{}
	err := s.workspace.Read(ctx, func(tx *sql.Tx, version int) error {
		if version < 3 {
			return ErrNotFound
		}
		if _, err := getIn(ctx, tx, id); err != nil {
			return err
		}
		if version < workspace.WorkstreamSourcesSchemaVersion {
			return nil
		}
		rows, err := tx.QueryContext(ctx, sourceItemSelect+" WHERE workstream_id=? ORDER BY seq", id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanSourceItem(rows)
			if err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	// Filesystem observations must not hold the database's read transaction.
	for i := range out {
		path := sourceItemPath(out[i])
		if path == "" {
			continue
		}
		_, err := os.Stat(path)
		out[i].Gone = errors.Is(err, os.ErrNotExist)
	}
	return out, nil
}

func sourceItemPath(item SourceItem) string {
	if item.Kind == SourceFile {
		return item.Path
	}
	if item.Kind == SourceRepo && filepath.IsAbs(item.Repo) {
		return item.Repo
	}
	return ""
}

func sourceWriteAllowed(ctx context.Context, tx *sql.Tx, id string, proposed bool) error {
	w, err := getIn(ctx, tx, id)
	if err != nil {
		return err
	}
	if w.DoneAt.IsZero() {
		return nil
	}
	if proposed {
		return ErrDone
	}
	return ErrConflict
}

func (s *Store) AddSource(ctx context.Context, id string, p SourceAdd) (SourceItem, bool, Change, error) {
	return s.addSource(ctx, id, p, false, "")
}

func (s *Store) ProposeSource(ctx context.Context, id string, p SourceAdd, why string) (SourceItem, bool, Change, error) {
	if err := ValidateSourceWhy(why); err != nil {
		return SourceItem{}, false, Change{}, err
	}
	if p.ReadByJobs {
		return SourceItem{}, false, Change{}, invalid("a proposal cannot be read by jobs")
	}
	return s.addSource(ctx, id, p, true, why)
}

func (s *Store) addSource(ctx context.Context, id string, p SourceAdd, proposed bool, why string) (SourceItem, bool, Change, error) {
	prepared, err := prepareSource(p)
	if err != nil {
		return SourceItem{}, false, Change{}, err
	}
	// Check references before a writer can initialize or migrate the file.
	err = s.workspace.Read(ctx, func(tx *sql.Tx, version int) error {
		if version < 3 {
			return ErrNotFound
		}
		if err := sourceWriteAllowed(ctx, tx, id, proposed); err != nil {
			return err
		}
		return sourceKeptIn(ctx, tx, id, &prepared)
	})
	if err != nil {
		return SourceItem{}, false, Change{}, err
	}
	var out SourceItem
	var already bool
	var change Change
	err = s.workspace.Write(ctx, func(tx *sql.Tx) error {
		if err := sourceWriteAllowed(ctx, tx, id, proposed); err != nil {
			return err
		}
		if err := sourceKeptIn(ctx, tx, id, &prepared); err != nil {
			return err
		}
		var wrote bool
		var err error
		out, already, wrote, err = s.addSourceIn(ctx, tx, id, prepared, proposed, why)
		if err != nil || !wrote {
			return err
		}
		if err := touch(ctx, tx, id, s.now().UnixMilli()); err != nil {
			return err
		}
		change = changed(id, PartSources)
		return nil
	})
	if err != nil {
		return SourceItem{}, false, Change{}, err
	}
	return out, already, change, nil
}

func (s *Store) addSourceIn(ctx context.Context, tx *sql.Tx, id string, p preparedSource, proposed bool, why string) (SourceItem, bool, bool, error) {
	existing, err := scanSourceItem(tx.QueryRowContext(ctx, sourceItemSelect+" WHERE workstream_id=? AND kind=? AND target=?", id, p.Kind, p.target))
	if err == nil {
		if proposed || !existing.Proposed {
			return existing, true, false, nil
		}
		title := existing.Title
		if p.Title != nil {
			title = *p.Title
		}
		_, err = tx.ExecContext(ctx, "UPDATE workstream_sources SET proposed=0,title=?,read_by_jobs=? WHERE id=?", title, p.ReadByJobs, existing.ID)
		if err != nil {
			return SourceItem{}, false, false, err
		}
		out, err := sourceItemIn(ctx, tx, existing.ID)
		return out, true, true, err
	}
	if !errors.Is(err, ErrNotFound) {
		return SourceItem{}, false, false, err
	}
	if err := sourceLimitIn(ctx, tx, id); err != nil {
		return SourceItem{}, false, false, err
	}
	return s.insertSourceIn(ctx, tx, id, p, proposed, why)
}

func sourceLimitIn(ctx context.Context, tx *sql.Tx, id string) error {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM workstream_sources WHERE workstream_id=?", id).Scan(&count); err != nil {
		return err
	}
	if count >= SourcesMax {
		return ErrLimit
	}
	return nil
}

func (s *Store) insertSourceIn(ctx context.Context, tx *sql.Tx, id string, p preparedSource, proposed bool, why string) (SourceItem, bool, bool, error) {
	sourceID, err := s.newID()
	if err != nil {
		return SourceItem{}, false, false, err
	}
	var ref any
	if p.Ref != nil {
		b, err := json.Marshal(p.Ref)
		if err != nil {
			return SourceItem{}, false, false, err
		}
		ref = string(b)
	}
	by := "you"
	if proposed {
		by = "agent"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workstream_sources(id,workstream_id,kind,target,title,path,repo,number,url,ref,why,read_by_jobs,proposed,added_by,added_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, sourceID, id, p.Kind, p.target, p.title, p.Path, p.Repo, p.Number, p.URL, ref, nullString(why), p.ReadByJobs, proposed, by, s.now().UnixMilli())
	if err != nil {
		return SourceItem{}, false, false, err
	}
	out, err := sourceItemIn(ctx, tx, sourceID)
	return out, false, true, err
}

func ValidateSourcePatch(p SourcePatch) error {
	if p.Title == nil && p.ReadByJobs == nil && p.Keep == nil {
		return invalid("source update needs title, read_by_jobs or keep")
	}
	if p.Keep != nil && !*p.Keep {
		return invalid("keep can only be true")
	}
	return sourceTitle(p.Title)
}

func (s *Store) UpdateSource(ctx context.Context, id string, p SourcePatch) (SourceItem, Change, error) {
	if err := ValidateSourcePatch(p); err != nil {
		return SourceItem{}, Change{}, err
	}
	var out SourceItem
	var change Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		item, err := sourceItemIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := sourceWriteAllowed(ctx, tx, item.WorkstreamID, false); err != nil {
			return err
		}
		out = item
		if err := sourcePatchItem(&out, p); err != nil {
			return err
		}
		if out.Title == item.Title && out.ReadByJobs == item.ReadByJobs && out.Proposed == item.Proposed {
			return nil
		}
		if _, err := tx.ExecContext(ctx, "UPDATE workstream_sources SET title=?,read_by_jobs=?,proposed=? WHERE id=?", out.Title, out.ReadByJobs, out.Proposed, id); err != nil {
			return err
		}
		if err := touch(ctx, tx, item.WorkstreamID, s.now().UnixMilli()); err != nil {
			return err
		}
		change = changed(item.WorkstreamID, PartSources)
		return nil
	})
	if err != nil {
		return SourceItem{}, Change{}, err
	}
	return out, change, nil
}

func sourcePatchItem(item *SourceItem, p SourcePatch) error {
	if p.Keep != nil {
		item.Proposed = false
	}
	if p.ReadByJobs != nil {
		if *p.ReadByJobs && item.Proposed {
			return invalid("keep a proposal before enabling read_by_jobs")
		}
		item.ReadByJobs = *p.ReadByJobs
	}
	if p.Title != nil {
		item.Title = *p.Title
	}
	return nil
}

func (s *Store) RemoveSource(ctx context.Context, id string) (SourceItem, Change, error) {
	var out SourceItem
	var change Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = sourceItemIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := sourceWriteAllowed(ctx, tx, out.WorkstreamID, false); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM workstream_sources WHERE id=?", id); err != nil {
			return err
		}
		if err := touch(ctx, tx, out.WorkstreamID, s.now().UnixMilli()); err != nil {
			return err
		}
		change = changed(out.WorkstreamID, PartSources)
		return nil
	})
	if err != nil {
		return SourceItem{}, Change{}, err
	}
	return out, change, nil
}
