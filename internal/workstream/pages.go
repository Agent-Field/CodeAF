package workstream

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/workstream/pagedoc"
)

const pageSelect = `SELECT id,workstream_id,coalesce(parent_id,''),title,owner,read_by_jobs,position,version,updated_at,updated_by,updated_from,doc,overview,deleted_at FROM workstream_pages WHERE id=?`

// ReadByJobsDefault chooses the creation default; later title changes do not
// alter an existing page's explicit Read by jobs setting.
func ReadByJobsDefault(title string) bool {
	title = strings.TrimSpace(title)
	return strings.EqualFold(title, "Spec") || strings.EqualFold(title, "Decisions")
}

func pageIn(ctx context.Context, tx *sql.Tx, id string) (Page, bool, error) {
	var p Page
	var at int64
	var source sql.NullString
	var deletedAt sql.NullInt64
	var overview bool
	var doc string
	err := tx.QueryRowContext(ctx, pageSelect, id).Scan(&p.ID, &p.WorkstreamID, &p.ParentID, &p.Title, &p.Owner, &p.ReadByJobs, &p.Position, &p.Version, &at, &p.UpdatedBy, &source, &doc, &overview, &deletedAt)
	if err != nil {
		return p, false, found(err)
	}
	p.UpdatedAt = instant(at)
	if deletedAt.Valid {
		p.DeletedAt = instant(deletedAt.Int64)
	}
	p.Doc = json.RawMessage(doc)
	p.UpdatedFrom, err = readSource(source)
	return p, overview, err
}
func livePageIn(ctx context.Context, tx *sql.Tx, id string) (Page, bool, error) {
	p, overview, err := pageIn(ctx, tx, id)
	if err == nil && !p.DeletedAt.IsZero() {
		err = ErrNotFound
	}
	return p, overview, err
}

const pageNodeColumns = `id,workstream_id,coalesce(parent_id,''),title,owner,read_by_jobs,position,version,updated_at,updated_by,updated_from,deleted_at`

func readPageNodes(rows *sql.Rows) ([]PageNode, error) {
	defer rows.Close()
	out := make([]PageNode, 0)
	for rows.Next() {
		var p PageNode
		var at int64
		var source sql.NullString
		var deletedAt sql.NullInt64
		if err := rows.Scan(&p.ID, &p.WorkstreamID, &p.ParentID, &p.Title, &p.Owner, &p.ReadByJobs, &p.Position, &p.Version, &at, &p.UpdatedBy, &source, &deletedAt); err != nil {
			return nil, err
		}
		p.UpdatedAt = instant(at)
		if deletedAt.Valid {
			p.DeletedAt = instant(deletedAt.Int64)
		}
		var err error
		p.UpdatedFrom, err = readSource(source)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func pageNodesIn(ctx context.Context, tx *sql.Tx, id string) ([]PageNode, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+pageNodeColumns+` FROM workstream_pages WHERE workstream_id=? AND deleted_at IS NULL ORDER BY position,id`, id)
	if err != nil {
		return nil, err
	}
	nodes, err := readPageNodes(rows)
	if err != nil {
		return nil, err
	}
	siblings := map[string][]PageNode{}
	for _, p := range nodes {
		siblings[p.ParentID] = append(siblings[p.ParentID], p)
	}
	out := make([]PageNode, 0)
	seen := map[string]bool{}
	// An explicit stack keeps exports safe for a very deep tree.
	var stack []PageNode
	push := func(nodes []PageNode) {
		for i := len(nodes) - 1; i >= 0; i-- {
			stack = append(stack, nodes[i])
		}
	}
	push(siblings[""])
	for len(stack) > 0 {
		last := len(stack) - 1
		p := stack[last]
		stack = stack[:last]
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		out = append(out, p)
		push(siblings[p.ID])
	}
	return out, nil
}
func (s *Store) Pages(ctx context.Context, id string) ([]PageNode, error) {
	out := make([]PageNode, 0)
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < 3 {
			return ErrNotFound
		}
		if _, err := getIn(ctx, tx, id); err != nil {
			return err
		}
		var err error
		out, err = pageNodesIn(ctx, tx, id)
		return err
	})
	return out, err
}

// DeletedPages lists delete roots; descendants remain readable but are not
// separate entries unless they were deleted by their own delete operation.
func (s *Store) DeletedPages(ctx context.Context, id string) ([]PageNode, error) {
	out := make([]PageNode, 0)
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < 3 {
			return ErrNotFound
		}
		if _, err := getIn(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+pageNodeColumns+` FROM workstream_pages WHERE workstream_id=? AND deleted_at IS NOT NULL AND deleted_root=id ORDER BY deleted_at DESC,id`, id)
		if err != nil {
			return err
		}
		out, err = readPageNodes(rows)
		return err
	})
	return out, err
}
func (s *Store) Page(ctx context.Context, id string, markdown bool) (Page, error) {
	return s.page(ctx, id, 0, markdown)
}

// PageAtVersion reads saved content while preserving the page's current tree metadata.
func (s *Store) PageAtVersion(ctx context.Context, id string, version int, markdown bool) (Page, error) {
	if version < 1 {
		return Page{}, invalid("version must be positive")
	}
	return s.page(ctx, id, version, markdown)
}

func (s *Store) page(ctx context.Context, id string, version int, markdown bool) (Page, error) {
	var p Page
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < 3 {
			return ErrNotFound
		}
		var err error
		p, _, err = pageIn(ctx, tx, id)
		if err != nil || version == 0 || version == p.Version {
			return err
		}
		var at int64
		var source sql.NullString
		var doc string
		err = tx.QueryRowContext(ctx, `SELECT title,doc,version,saved_at,saved_by,source FROM workstream_page_versions WHERE page_id=? AND version=?`, id, version).
			Scan(&p.Title, &doc, &p.Version, &at, &p.UpdatedBy, &source)
		if err != nil {
			return found(err)
		}
		p.Doc, p.UpdatedAt = json.RawMessage(doc), instant(at)
		p.UpdatedFrom, err = readSource(source)
		return err
	})
	if err == nil && markdown {
		var md string
		md, err = pagedoc.Markdown(p.Doc)
		p.Markdown = &md
	}
	return p, err
}
func checkDoc(doc json.RawMessage) error {
	if len(doc) > DocMax {
		return ErrTooLarge
	}
	raw := strings.TrimSpace(string(doc))
	if len(raw) == 0 || raw[0] != '{' || !json.Valid(doc) || !utf8.Valid(doc) {
		return invalid("doc must be a JSON object")
	}
	return nil
}
func saveVersion(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO workstream_page_versions(page_id,version,saved_at,saved_by,source,title,doc,bytes)
 SELECT id,version,updated_at,updated_by,updated_from,title,doc,length(CAST(doc AS BLOB)) FROM workstream_pages WHERE id=? AND deleted_at IS NULL`, id)
	return err
}
func (s *Store) SavePage(ctx context.Context, id string, base int, doc json.RawMessage, who Attribution) (Page, Change, error) {
	if err := checkDoc(doc); err != nil {
		return Page{}, Change{}, err
	}
	return s.savePage(ctx, id, base, doc, 0, who)
}
func (s *Store) RestorePage(ctx context.Context, id string, version, base int, who Attribution) (Page, Change, error) {
	if version < 1 {
		return Page{}, Change{}, invalid("version must be positive")
	}
	return s.savePage(ctx, id, base, nil, version, who)
}
func (s *Store) savePage(ctx context.Context, id string, base int, doc json.RawMessage, restore int, who Attribution) (Page, Change, error) {
	if base < 0 {
		return Page{}, Change{}, invalid("base_version must be non-negative")
	}
	by, err := actor(who.By)
	if err != nil {
		return Page{}, Change{}, err
	}
	source, err := sourceJSON(who.From)
	if err != nil {
		return Page{}, Change{}, err
	}
	var out Page
	var c Change
	err = s.workspace.Write(ctx, func(tx *sql.Tx) error {
		p, _, err := pageIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.DeletedAt.IsZero() {
			if restore > 0 {
				return invalid("deleted page restore requires only id")
			}
			return ErrNotFound
		}
		if p.Version != base {
			return &VersionConflict{ID: id, Version: p.Version, UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy}
		}
		title := p.Title
		if restore > 0 {
			var raw string
			err := tx.QueryRowContext(ctx, "SELECT title,doc FROM workstream_page_versions WHERE page_id=? AND version=?", id, restore).Scan(&title, &raw)
			if err != nil {
				return found(err)
			}
			doc = json.RawMessage(raw)
		}
		if err := checkDoc(doc); err != nil {
			return err
		}
		at := s.now().UnixMilli()
		if _, err := tx.ExecContext(ctx, "UPDATE workstream_pages SET title=?,doc=?,version=version+1,updated_at=?,updated_by=?,updated_from=? WHERE id=?", title, string(doc), at, by, source, id); err != nil {
			return err
		}
		if err := saveVersion(ctx, tx, id); err != nil {
			return err
		}
		if err := touch(ctx, tx, p.WorkstreamID, at); err != nil {
			return err
		}
		out, _, err = pageIn(ctx, tx, id)
		c = changed(p.WorkstreamID, PartPages)
		return err
	})
	if err != nil {
		return Page{}, Change{}, err
	}
	return out, c, nil
}

// placePage validates every target before changing sibling positions. It packs
// positions after a move so a later insert never relies on gaps or old offsets.
func placePage(ctx context.Context, tx *sql.Tx, p Page, parent, after string, last bool, at int64, by string, source any) (bool, error) {
	if parent != "" {
		seen := map[string]bool{}
		for id := parent; id != ""; {
			if id == p.ID || seen[id] {
				return false, ErrCycle
			}
			seen[id] = true
			var workstreamID, next string
			err := tx.QueryRowContext(ctx, "SELECT workstream_id,coalesce(parent_id,'') FROM workstream_pages WHERE id=? AND deleted_at IS NULL", id).Scan(&workstreamID, &next)
			if err != nil {
				return false, found(err)
			}
			if workstreamID != p.WorkstreamID {
				return false, invalid("parent is in another workstream")
			}
			id = next
		}
	}
	if after != "" {
		var workstreamID, siblingParent string
		err := tx.QueryRowContext(ctx, "SELECT workstream_id,coalesce(parent_id,'') FROM workstream_pages WHERE id=? AND deleted_at IS NULL", after).Scan(&workstreamID, &siblingParent)
		if err != nil {
			return false, found(err)
		}
		if workstreamID != p.WorkstreamID || siblingParent != parent || after == p.ID {
			return false, invalid("after_id must be another sibling in this workstream")
		}
	}
	nodes, err := pageNodesIn(ctx, tx, p.WorkstreamID)
	if err != nil {
		return false, err
	}
	siblings := make([]PageNode, 0)
	old := make([]PageNode, 0)
	for _, n := range nodes {
		if n.ID == p.ID {
			continue
		}
		if n.ParentID == parent {
			siblings = append(siblings, n)
		}
		if p.ParentID != parent && n.ParentID == p.ParentID {
			old = append(old, n)
		}
	}
	index := 0
	if last {
		index = len(siblings)
	}
	if after != "" {
		for i, n := range siblings {
			if n.ID == after {
				index = i + 1
				break
			}
		}
	}
	// Root position zero belongs to the immutable Overview identity, even after a rename.
	if parent == "" && index == 0 && len(siblings) > 0 {
		index = 1
	}
	if p.ParentID == parent && p.Position == index {
		return false, nil
	}
	siblings = append(siblings, PageNode{})
	copy(siblings[index+1:], siblings[index:])
	siblings[index] = p.PageNode
	for i, n := range siblings {
		if n.ID == p.ID {
			if _, err := tx.ExecContext(ctx, "UPDATE workstream_pages SET parent_id=?,position=?,updated_at=?,updated_by=?,updated_from=? WHERE id=?", nullString(parent), i, at, by, source, p.ID); err != nil {
				return false, err
			}
		} else if n.Position != i {
			if _, err := tx.ExecContext(ctx, "UPDATE workstream_pages SET position=? WHERE id=?", i, n.ID); err != nil {
				return false, err
			}
		}
	}
	for i, n := range old {
		if n.Position != i {
			if _, err := tx.ExecContext(ctx, "UPDATE workstream_pages SET position=? WHERE id=?", i, n.ID); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}
func (s *Store) CreatePage(ctx context.Context, p PageCreate) (Page, Change, error) {
	if err := validTitle(p.Title); err != nil {
		return Page{}, Change{}, err
	}
	owner, err := actor(p.Owner)
	if err != nil {
		return Page{}, Change{}, err
	}
	by, err := actor(p.By)
	if err != nil {
		return Page{}, Change{}, err
	}
	source, err := sourceJSON(p.From)
	if err != nil {
		return Page{}, Change{}, err
	}
	if p.Doc == nil {
		p.Doc = json.RawMessage(emptyDoc)
	}
	if err := checkDoc(p.Doc); err != nil {
		return Page{}, Change{}, err
	}
	var out Page
	var c Change
	err = s.workspace.Write(ctx, func(tx *sql.Tx) error {
		if _, err := getIn(ctx, tx, p.WorkstreamID); err != nil {
			return err
		}
		id, err := s.newID()
		if err != nil {
			return err
		}
		at := s.now().UnixMilli()
		// A temporary root position distinct from every packed position makes the
		// common placement helper handle creation and movement identically.
		if _, err := tx.ExecContext(ctx, `INSERT INTO workstream_pages(id,workstream_id,title,owner,read_by_jobs,position,version,updated_at,updated_by,updated_from,doc) VALUES (?,?,?,?,?,2147483647,1,?,?,?,?)`, id, p.WorkstreamID, p.Title, owner, p.ReadByJobs, at, by, source, string(p.Doc)); err != nil {
			return err
		}
		page, _, err := livePageIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := placePage(ctx, tx, page, p.ParentID, p.AfterID, p.AfterID == "", at, by, source); err != nil {
			return err
		}
		if err := saveVersion(ctx, tx, id); err != nil {
			return err
		}
		if err := touch(ctx, tx, p.WorkstreamID, at); err != nil {
			return err
		}
		out, _, err = pageIn(ctx, tx, id)
		c = changed(p.WorkstreamID, PartPages)
		return err
	})
	if err != nil {
		return Page{}, Change{}, err
	}
	return out, c, nil
}
func (s *Store) UpdatePage(ctx context.Context, id string, p PagePatch) (PageNode, Change, error) {
	if p.Title != nil {
		if err := validTitle(*p.Title); err != nil {
			return PageNode{}, Change{}, err
		}
	}
	if p.Owner != nil {
		if *p.Owner != "you" && *p.Owner != "agent" {
			return PageNode{}, Change{}, invalid("owner must be you or agent")
		}
	}
	by, err := actor(p.By)
	if err != nil {
		return PageNode{}, Change{}, err
	}
	source, err := sourceJSON(p.From)
	if err != nil {
		return PageNode{}, Change{}, err
	}
	var out PageNode
	var c Change
	err = s.workspace.Write(ctx, func(tx *sql.Tx) error {
		page, _, err := livePageIn(ctx, tx, id)
		if err != nil {
			return err
		}
		title, owner, read := page.Title, page.Owner, page.ReadByJobs
		if p.Title != nil {
			title = *p.Title
		}
		if p.Owner != nil {
			owner = *p.Owner
		}
		if p.ReadByJobs != nil {
			read = *p.ReadByJobs
		}
		if title == page.Title && owner == page.Owner && read == page.ReadByJobs {
			out = page.PageNode
			return nil
		}
		at := s.now().UnixMilli()
		if _, err := tx.ExecContext(ctx, "UPDATE workstream_pages SET title=?,owner=?,read_by_jobs=?,updated_at=?,updated_by=?,updated_from=? WHERE id=?", title, owner, read, at, by, source, id); err != nil {
			return err
		}
		if err := touch(ctx, tx, page.WorkstreamID, at); err != nil {
			return err
		}
		page, _, err = pageIn(ctx, tx, id)
		out = page.PageNode
		c = changed(page.WorkstreamID, PartPages)
		return err
	})
	if err != nil {
		return PageNode{}, Change{}, err
	}
	return out, c, nil
}
func (s *Store) MovePage(ctx context.Context, id, parent, after string) (PageNode, Change, error) {
	var out PageNode
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		p, overview, err := livePageIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if overview {
			return invalid("Overview cannot be moved")
		}
		at := s.now().UnixMilli()
		moved, err := placePage(ctx, tx, p, parent, after, false, at, "you", nil)
		if err != nil {
			return err
		}
		if moved {
			if err := touch(ctx, tx, p.WorkstreamID, at); err != nil {
				return err
			}
			c = changed(p.WorkstreamID, PartPages)
		}
		p, _, err = pageIn(ctx, tx, id)
		out = p.PageNode
		return err
	})
	if err != nil {
		return PageNode{}, Change{}, err
	}
	return out, c, nil
}
func (s *Store) DeletePage(ctx context.Context, id string) ([]string, Change, error) {
	deleted := []string{}
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		p, overview, err := livePageIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if overview {
			return invalid("Overview cannot be deleted")
		}
		nodes, err := pageNodesIn(ctx, tx, p.WorkstreamID)
		if err != nil {
			return err
		}
		gone := map[string]bool{id: true}
		for _, n := range nodes {
			if gone[n.ID] || gone[n.ParentID] {
				gone[n.ID] = true
				deleted = append(deleted, n.ID)
			}
		}
		at := s.now().UnixMilli()
		for _, deletedID := range deleted {
			if _, err := tx.ExecContext(ctx, "UPDATE workstream_pages SET deleted_at=?,deleted_root=? WHERE id=?", at, id, deletedID); err != nil {
				return err
			}
		}
		i := 0
		for _, n := range nodes {
			if !gone[n.ID] && n.ParentID == p.ParentID {
				if _, err := tx.ExecContext(ctx, "UPDATE workstream_pages SET position=? WHERE id=?", i, n.ID); err != nil {
					return err
				}
				i++
			}
		}
		if err := touch(ctx, tx, p.WorkstreamID, at); err != nil {
			return err
		}
		c = changed(p.WorkstreamID, PartPages)
		return nil
	})
	if err != nil {
		return nil, Change{}, err
	}
	return deleted, c, nil
}

// UndeletePage restores only descendants removed by the same delete. Like a
// move, it records the person as the actor and does not create a saved version.
func (s *Store) UndeletePage(ctx context.Context, id string) (Page, Change, error) {
	var out Page
	var c Change
	err := s.workspace.Write(ctx, func(tx *sql.Tx) error {
		p, _, err := pageIn(ctx, tx, id)
		if err != nil {
			return err
		}
		if p.DeletedAt.IsZero() {
			return invalid("live page restore requires version and base_version")
		}
		var root string
		if err := tx.QueryRowContext(ctx, "SELECT deleted_root FROM workstream_pages WHERE id=?", id).Scan(&root); err != nil {
			return found(err)
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+pageNodeColumns+` FROM workstream_pages WHERE workstream_id=? AND deleted_root=? AND deleted_at=? ORDER BY position,id`, p.WorkstreamID, root, p.DeletedAt.UnixMilli())
		if err != nil {
			return err
		}
		nodes, err := readPageNodes(rows)
		if err != nil {
			return err
		}
		children := map[string][]string{}
		for _, n := range nodes {
			children[n.ParentID] = append(children[n.ParentID], n.ID)
		}
		parent := p.ParentID
		if parent != "" {
			if _, _, err := livePageIn(ctx, tx, parent); errors.Is(err, ErrNotFound) {
				parent = ""
			} else if err != nil {
				return err
			}
		}
		at := s.now().UnixMilli()
		stack := []string{id}
		seen := map[string]bool{}
		for len(stack) > 0 {
			last := len(stack) - 1
			restoredID := stack[last]
			stack = stack[:last]
			if seen[restoredID] {
				continue
			}
			seen[restoredID] = true
			if _, err := tx.ExecContext(ctx, `UPDATE workstream_pages SET deleted_at=NULL,deleted_root=NULL,updated_at=?,updated_by='you',updated_from=NULL WHERE id=?`, at, restoredID); err != nil {
				return err
			}
			stack = append(stack, children[restoredID]...)
		}
		// Force placement even when the old ordinal happens to equal the append
		// index: the restored page still needs its parent and attribution written.
		p.Position = -1
		if _, err := placePage(ctx, tx, p, parent, "", true, at, "you", nil); err != nil {
			return err
		}
		if err := touch(ctx, tx, p.WorkstreamID, at); err != nil {
			return err
		}
		out, _, err = pageIn(ctx, tx, id)
		c = changed(p.WorkstreamID, PartPages)
		return err
	})
	if err != nil {
		return Page{}, Change{}, err
	}
	return out, c, nil
}

func (s *Store) PageHistory(ctx context.Context, id string, limit int) ([]PageVersion, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return nil, invalid("history limit must be 1–200")
	}
	out := []PageVersion{}
	err := s.workspace.Read(ctx, func(tx *sql.Tx, v int) error {
		if v < 3 {
			return ErrNotFound
		}
		if _, _, err := livePageIn(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT version,saved_at,saved_by,source,title,bytes FROM workstream_page_versions WHERE page_id=? ORDER BY version DESC LIMIT ?", id, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p PageVersion
			var at int64
			var source sql.NullString
			if err := rows.Scan(&p.Version, &at, &p.SavedBy, &source, &p.Title, &p.Bytes); err != nil {
				return err
			}
			p.SavedAt = instant(at)
			p.From, err = readSource(source)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}
