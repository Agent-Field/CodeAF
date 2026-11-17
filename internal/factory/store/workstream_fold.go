package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A fold keeps a receipt in the same database transaction as its work. Undo
// compares every affected row before restoring, so a later edit is never lost.
type foldTable struct {
	Name    string
	Columns []string
	Rows    [][]string
}
type foldReceipt struct {
	From, To      string
	Before, After []foldTable
}

var ErrFoldInvalid = errors.New("cannot fold this workstream")

var ErrFoldChanged = errors.New("work changed after folding; Undo would replace that work")

func foldSnapshot(tx *sql.Tx, from, to string) ([]foldTable, error) {
	rows, err := tx.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var n string
		if err = rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, n)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	var out []foldTable
	for _, n := range names {
		if n == "workstream_folds" {
			continue
		}
		cols, err := tx.Query(`PRAGMA table_info("` + strings.ReplaceAll(n, `"`, `""`) + `")`)
		if err != nil {
			return nil, err
		}
		t := foldTable{Name: n}
		key := ""
		for cols.Next() {
			var cid, notnull, pk int
			var name, kind string
			var d any
			if err = cols.Scan(&cid, &name, &kind, &notnull, &d, &pk); err != nil {
				cols.Close()
				return nil, err
			}
			t.Columns = append(t.Columns, name)
			if name == "ws" {
				key = name
			}
		}
		if err = cols.Close(); err != nil {
			return nil, err
		}
		if n == "workstreams" {
			key = "id"
		}
		if key == "" {
			continue
		}
		quoted := make([]string, len(t.Columns))
		for i, c := range t.Columns {
			quoted[i] = `quote("` + strings.ReplaceAll(c, `"`, `""`) + `")`
		}
		rs, err := tx.Query(`SELECT `+strings.Join(quoted, ",")+` FROM "`+n+`" WHERE "`+key+`" IN (?,?)`, from, to)
		if err != nil {
			return nil, err
		}
		for rs.Next() {
			r := make([]string, len(quoted))
			ptr := make([]any, len(r))
			for i := range r {
				ptr[i] = &r[i]
			}
			if err = rs.Scan(ptr...); err != nil {
				rs.Close()
				return nil, err
			}
			t.Rows = append(t.Rows, r)
		}
		if err = rs.Close(); err != nil {
			return nil, err
		}
		slices.SortFunc(t.Rows, func(a, b []string) int { return strings.Compare(strings.Join(a, "\x00"), strings.Join(b, "\x00")) })
		out = append(out, t)
	}
	return out, nil
}

// FoldWorkstream moves a leaf workstream whole, or restores a receipt. The
// caller's guard runs under the transaction lock, against the current copies.
func (st *Store) FoldWorkstream(from, to string, undo bool, now time.Time, guard func(factory.OrgWorkstream, factory.OrgWorkstream) error) (factory.OrgWorkstream, error) {
	if _, err := st.filesDB(true); err != nil {
		return factory.OrgWorkstream{}, err
	}
	var out factory.OrgWorkstream
	err := st.wsTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS workstream_folds (id TEXT PRIMARY KEY, receipt TEXT NOT NULL)`); err != nil {
			return err
		}
		all, err := readWorkstreams(tx)
		if err != nil {
			return err
		}
		if undo {
			var raw string
			if err = tx.QueryRow(`SELECT receipt FROM workstream_folds WHERE id=?`, from).Scan(&raw); err != nil {
				return err
			}
			var rec foldReceipt
			if err = json.Unmarshal([]byte(raw), &rec); err != nil {
				return err
			}
			if rec.To != to {
				return ErrFoldChanged
			}
			var original factory.OrgWorkstream
			// The old source is retained only inside its receipt, never in Work or Archived.
			for _, t := range rec.Before {
				if t.Name == "workstreams" {
					for _, r := range t.Rows {
						for i, c := range t.Columns {
							if c == "doc" {
								var w factory.OrgWorkstream
								literal := r[i]
								var doc string
								if err = tx.QueryRow(`SELECT ` + literal).Scan(&doc); err != nil {
									return err
								}
								if json.Unmarshal([]byte(doc), &w) == nil && w.ID == from {
									original = w
								}
							}
						}
					}
				}
			}
			if err = guard(original, all[to]); err != nil {
				return err
			}
			current, err := foldSnapshot(tx, from, to)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, rec.After) {
				return ErrFoldChanged
			}
			for _, t := range rec.Before {
				key := "ws"
				if t.Name == "workstreams" {
					key = "id"
				}
				if _, err = tx.Exec(`DELETE FROM "`+t.Name+`" WHERE "`+key+`" IN (?,?)`, from, to); err != nil {
					return err
				}
				columns := make([]string, len(t.Columns))
				for i, c := range t.Columns {
					columns[i] = `"` + c + `"`
				}
				for _, r := range t.Rows {
					if _, err = tx.Exec(`INSERT INTO "` + t.Name + `" (` + strings.Join(columns, ",") + `) VALUES (` + strings.Join(r, ",") + `)`); err != nil {
						return err
					}
				}
			}
			if _, err = tx.Exec(`DELETE FROM workstream_folds WHERE id=?`, from); err != nil {
				return err
			}
			// Revisions stay monotonic after Undo, so an old writer cannot
			// mistake the restored contents for the copy it originally read.
			restoredRows, err := readWorkstreams(tx)
			if err != nil {
				return err
			}
			for _, id := range []string{from, to} {
				cur := restoredRows[id]
				cur.Rev = max(cur.Rev, all[to].Rev) + 1
				if err = putWorkstream(tx, cur); err != nil {
					return err
				}
			}
			restored, err := readWorkstreams(tx)
			out = restored[from]
			return err
		}
		a, ok := all[from]
		if !ok {
			return ErrPartGone
		}
		b, ok := all[to]
		if !ok || from == to {
			return ErrPartGone
		}
		if err = guard(a, b); err != nil {
			return err
		}
		for _, w := range all {
			if w.Parent == from {
				return fmt.Errorf("%w: fold the workstreams inside this one first", ErrFoldInvalid)
			}
		}
		var active int
		if err := tx.QueryRow(`SELECT count(*) FROM tasks WHERE ws=? AND json_extract(doc,'$.state') IN ('running','finishing')`, from).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("%w: stop working tasks before folding this workstream", ErrFoldInvalid)
		}
		before, err := foldSnapshot(tx, from, to)
		if err != nil {
			return err
		}
		// Identity conflicts refuse the whole move, including task threads and folders.
		for _, p := range a.Pages {
			if slices.ContainsFunc(b.Pages, func(q factory.WorkstreamPage) bool { return p.ID == q.ID }) {
				return fmt.Errorf("%w: the destination already has a page with this identity", ErrFoldInvalid)
			}
			if p.Kind == "overview" {
				p.Kind = "page"
			}
			b.Pages = append(b.Pages, p)
		}
		b.Decisions = append(b.Decisions, a.Decisions...)
		b.Sources = append(b.Sources, a.Sources...)
		for _, j := range a.Jobs {
			if !slices.Contains(b.Jobs, j) {
				b.Jobs = append(b.Jobs, j)
			}
		}
		for _, l := range a.Links {
			if l != from && l != to && !slices.Contains(b.Links, l) {
				b.Links = append(b.Links, l)
			}
		}
		b.UpdatedAt = now
		b.Rev++
		for _, t := range before {
			if t.Name == "workstreams" || t.Name == "workstream_file_seeds" {
				continue
			}
			if _, err = tx.Exec(`UPDATE "`+t.Name+`" SET ws=? WHERE ws=?`, to, from); err != nil {
				return fmt.Errorf("fold %s: %w", t.Name, err)
			}
		}
		// File and task documents carry their workstream as well as their row key.
		for _, table := range []string{"workstream_files", "tasks"} {
			if _, err = tx.Exec(`UPDATE `+table+` SET doc=json_set(doc,'$.ws',?) WHERE ws=? AND json_extract(doc,'$.ws')=?`, to, to, from); err != nil {
				return err
			}
		}
		if err = putWorkstream(tx, normalizeWorkstream(b)); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM workstreams WHERE id=?`, from); err != nil {
			return err
		}
		after, err := foldSnapshot(tx, from, to)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(foldReceipt{from, to, before, after})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO workstream_folds(id,receipt) VALUES (?,?)`, from, string(raw)); err != nil {
			return err
		}
		out = b
		return nil
	})
	return out, err
}
