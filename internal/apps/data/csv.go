package data

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// A CSV IMPORT IS A ONE-TIME COPY INTO A TYPE, and its dry run is the import
// itself, rolled back. The rows are read, mapped, coerced, checked for
// required and unique values and written inside the writer's transaction
// exactly as the real import does, and then the transaction is thrown away.
// So the preview cannot promise a row the import would refuse, or the other
// way round, and a duplicate between two rows of the same file is caught by
// both.

// The bounds of an import.
const (
	ImportBytesMax    = 50 << 20
	ImportRowsMax     = 50_000
	importPreviewRows = 20
	importProblemsMax = 100
)

// ImportOptions are a CSV import's choices. Mapping is column header → field
// name; a column mapped to "" is skipped. With no mapping, a column is read
// into the field its header names (by name or label, any case).
type ImportOptions struct {
	Path    string
	Mapping map[string]string
	DryRun  bool
}

// ImportProblem is one row the import could not take, and why.
type ImportProblem struct {
	Line    int
	Field   string
	Message string
}

// ImportResult is an import's account: the columns found and the mapping
// used, the first rows as they would be (or were) made, the rows refused,
// and the counts. Batch is the undoable batch of a real import.
type ImportResult struct {
	DryRun   bool
	Columns  []string
	Mapping  map[string]string
	Rows     []Row
	Problems []ImportProblem
	Created  int
	Failed   int
	Batch    string
}

// errDryRun rolls a dry run's transaction back.
var errDryRun = errors.New("dry run")

// ImportCSV copies the rows of a CSV file into typ.
//
// AN IMPORT IS THE PERSON'S, IN THE DATA PLACE, NEVER AN APP'S. It reads a
// file by path, and an app (its screens, its agent) reaching for a path would
// be a way to read any file on the machine into records it can see. So a call
// with an app is refused before the path is so much as looked at.
func (s *Store) ImportCSV(sc Scope, typ string, opts ImportOptions) (*ImportResult, error) {
	sc, flushAudit := s.WithPolicyAudit(sc)
	defer flushAudit()
	if sc.App != "" || sc.isAgent() {
		return nil, &Error{Code: CodeNotAllowed, Need: "import", Message: "importing a file is done in the Data place, not by an app"}
	}
	sch := s.schema.Load()
	t, err := sch.allow(sc, typ, true)
	if err != nil {
		return nil, err
	}
	header, lines, err := readCSV(opts.Path)
	if err != nil {
		return nil, err
	}
	mapping, err := importMapping(t, header, opts.Mapping)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{DryRun: opts.DryRun, Columns: header, Mapping: mapping, Rows: []Row{}, Problems: []ImportProblem{}}
	err = s.writeAs(sc, func(w *writeTx) error {
		res.Rows, res.Problems, res.Created, res.Failed, res.Batch = []Row{}, []ImportProblem{}, 0, 0, ""
		t, err := w.schema.allow(sc, typ, true)
		if err != nil {
			return err
		}
		batch, err := w.newBatch(sc, OpImport, t)
		if err != nil {
			return err
		}
		if err := w.importRows(t, sc, header, lines, res, nil); err != nil {
			return err
		}
		if opts.DryRun || res.Created == 0 {
			return errDryRun
		}
		res.Batch = batch
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return nil, err
	}
	return res, nil
}

// importRows uses the same row validation for a Data import and an intake delivery.
func (w *writeTx) importRows(t *Type, sc Scope, header []string, lines [][]string, res *ImportResult, created func(string)) error {
	mapping := res.Mapping
	for i, cells := range lines {
		line := i + 2 // the header is line 1
		set := map[string]any{}
		for col, cell := range cells {
			if col >= len(header) {
				break
			}
			field := mapping[header[col]]
			if field == "" || strings.TrimSpace(cell) == "" {
				continue
			}
			set[field] = cell
		}
		if len(set) == 0 {
			continue
		}
		if err := w.resolveTitles(t, set); err != nil {
			res.problem(line, err)
			continue
		}
		r, err := w.savepoint(func() (*record, error) { return w.createOne(t, sc, set, OpImport) })
		if err != nil {
			if _, ok := AsError(err); !ok {
				return err
			}
			res.problem(line, err)
			continue
		}
		res.Created++
		if created != nil {
			created(r.ID)
		}
		if len(res.Rows) < importPreviewRows && w.s.LiveRowAllowed(sc, t, r.Data) {
			res.Rows = append(res.Rows, w.s.row(t, r, sc, false))
		}
	}
	return nil
}

// problem notes one refused row.
func (r *ImportResult) problem(line int, err error) {
	r.Failed++
	if len(r.Problems) >= importProblemsMax {
		return
	}
	p := ImportProblem{Line: line, Message: err.Error()}
	if e, ok := AsError(err); ok {
		p.Field, p.Message = e.Field, e.Message
	}
	r.Problems = append(r.Problems, p)
}

// savepoint runs one row's writes so that a refused row leaves nothing
// behind: its insert and history row are rolled back, and the rows before
// and after it are kept.
func (w *writeTx) savepoint(fn func() (*record, error)) (*record, error) {
	if _, err := w.tx.Exec(`SAVEPOINT row`); err != nil {
		return nil, err
	}
	records, items, cursor, txCursor := len(w.change.Records), len(w.change.Items), w.change.Cursor, w.cursor
	r, err := fn()
	if err != nil {
		if _, rerr := w.tx.Exec(`ROLLBACK TO row`); rerr != nil {
			return nil, rerr
		}
		// A row a create refused never happened: it leaves no before for a
		// live query and no event under the cursor.
		for _, rc := range w.change.Records[records:] {
			delete(w.change.befores, rc.ID)
		}
		w.change.Records, w.change.Items, w.change.Cursor = w.change.Records[:records], w.change.Items[:items], cursor
		w.cursor = txCursor
		_, _ = w.tx.Exec(`RELEASE row`)
		return nil, err
	}
	_, err = w.tx.Exec(`RELEASE row`)
	return r, err
}

// resolveTitles turns a ref value that is not an id into the id of the
// record whose title it is ("Northwind" for a company), so a file exported
// from another tool imports its links. A name no record has is refused; a
// name two records share is refused rather than guessed.
func (w *writeTx) resolveTitles(t *Type, set map[string]any) error {
	for name, v := range set {
		f := t.field(name)
		if f == nil || f.Type != TypeRef && f.Type != TypeRefs {
			continue
		}
		var parts []string
		switch x := v.(type) {
		case string:
			if f.Type == TypeRefs {
				parts = strings.Split(x, ",")
			} else {
				parts = []string{x}
			}
		case []any:
			for _, item := range x {
				s, _ := item.(string)
				parts = append(parts, s)
			}
		default:
			continue
		}
		target := w.schema.typeNamed(f.To)
		title := ""
		if target != nil {
			title = target.titleField()
		}
		ids := make([]any, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if validID(p) || title == "" {
				ids = append(ids, p)
				continue
			}
			rows, err := w.tx.Query(`SELECT id FROM records WHERE type_id = ? AND deleted_at IS NULL AND `+
				jsonPath("data_json", target.Fields[title].ID)+` = ? COLLATE NOCASE LIMIT 2`, target.ID, p)
			if err != nil {
				return err
			}
			var found []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				found = append(found, id)
			}
			rows.Close()
			switch len(found) {
			case 0:
				return fieldErr(name, "there is no %s named %q", f.To, p)
			case 1:
				ids = append(ids, found[0])
			default:
				return fieldErr(name, "more than one %s is named %q", f.To, p)
			}
		}
		if f.Type == TypeRef {
			if len(ids) == 0 {
				set[name] = nil
			} else {
				set[name] = ids[0]
			}
		} else {
			set[name] = ids
		}
	}
	return nil
}

// importMapping checks a mapping, or makes one from the headers.
func importMapping(t *Type, header []string, given map[string]string) (map[string]string, error) {
	out := map[string]string{}
	if len(given) > 0 {
		known := map[string]bool{}
		for _, h := range header {
			known[h] = true
		}
		used := map[string]string{}
		for col, field := range given {
			if !known[col] {
				return nil, errorf(CodeInvalid, "the file has no column %q", col)
			}
			if field == "" {
				continue
			}
			f := t.field(field)
			if f == nil || f.Computed != "" {
				return nil, fieldErr(field, "%s records have no field %q to import into", t.Name, field)
			}
			if other, dup := used[field]; dup {
				return nil, fieldErr(field, "columns %q and %q both map to %s", other, col, field)
			}
			used[field] = col
			out[col] = field
		}
		return out, nil
	}
	used := map[string]bool{}
	for _, h := range header {
		norm := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(h)), "_"))
		for _, name := range t.Order {
			f := t.field(name)
			if f == nil || f.Computed != "" || used[name] {
				continue
			}
			if norm == name || strings.EqualFold(strings.TrimSpace(h), f.Label) {
				out[h] = name
				used[name] = true
				break
			}
		}
	}
	return out, nil
}

// readCSV reads a whole CSV file: its header and its rows.
func readCSV(path string) ([]string, [][]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil, errorf(CodeInvalid, "path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, errorf(CodeNotFound, "the file could not be read: %v", err)
	}
	if info.IsDir() {
		return nil, nil, errorf(CodeInvalid, "%s is a folder, not a CSV file", path)
	}
	if info.Size() > ImportBytesMax {
		return nil, nil, errorf(CodeInvalid, "the file is larger than %d MB", ImportBytesMax>>20)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, errorf(CodeNotFound, "the file could not be read: %v", err)
	}
	return parseCSV(raw)
}

func parseCSV(raw []byte) ([]string, [][]string, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(raw))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	header, err := r.Read()
	if err == io.EOF {
		return nil, nil, errorf(CodeInvalid, "the file is empty")
	}
	if err != nil {
		return nil, nil, errorf(CodeInvalid, "the file is not CSV: %v", err)
	}
	seen := map[string]bool{}
	for i, h := range header {
		header[i] = strings.TrimSpace(h)
		if header[i] == "" {
			header[i] = fmt.Sprintf("column %d", i+1)
		}
		if seen[header[i]] {
			return nil, nil, errorf(CodeInvalid, "the file has two columns named %q", header[i])
		}
		seen[header[i]] = true
	}
	var rows [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, errorf(CodeInvalid, "line %d is not CSV: %v", len(rows)+2, err)
		}
		rows = append(rows, rec)
		if len(rows) > ImportRowsMax {
			return nil, nil, errorf(CodeInvalid, "the file has more than %d rows", ImportRowsMax)
		}
	}
	return header, rows, nil
}
