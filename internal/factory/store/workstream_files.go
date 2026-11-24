package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── A WORKSTREAM'S FILES, KEPT ─────────────────────────────────────────────
//
// Files ([factory.WorkstreamFile]) live in the workstreams database beside
// the workstreams themselves, one row per file, so a change to one file is a
// write of that one row and never moves a workstream's rev:
//
//   - workstream_files: one row per file, the file as JSON.
//   - workstream_file_seeds: one row per workstream whose files were folded
//     in from its pages, sources and canvases and given a Decisions document.
//     The fold runs once per workstream; what it made is the workstream's
//     own from then on, to rename, move or delete.
//
// AN UPLOAD'S BYTES are kept on disk, content-addressed by their sha256, under
// WorkstreamBlobsDir. Two uploads of the same bytes share one copy, and a copy
// is let go only when no file names it.
//
// The tables are made on first use rather than in workstreamsSchema, so a
// server that never kept a file never carries them.

// WorkstreamBlobsDir is the folder upload bytes are kept in. Its name begins
// with the workstreams' prefix ([IsWorkstreamsFile]), so a look at the floor
// never reads it as a job.
const WorkstreamBlobsDir = "factory-workstreams.blobs"

// ErrFileStale is a file write whose rev is not the one kept.
var ErrFileStale = errors.New("this file changed since you read it; read it again, then make the change")

// ErrFileGone is a file that is not kept.
var ErrFileGone = errors.New("file not found")

const workstreamFilesSchema = `
CREATE TABLE IF NOT EXISTS workstream_files (
	ws  TEXT NOT NULL,
	id  TEXT NOT NULL,
	rev INTEGER NOT NULL,
	doc TEXT NOT NULL,
	PRIMARY KEY (ws, id)
);
CREATE TABLE IF NOT EXISTS workstream_file_seeds (
	ws TEXT PRIMARY KEY,
	at TEXT NOT NULL
);
`

// filesDB is the workstreams database with the file tables made. create is
// false for a read, which answers nil when the server never kept anything.
func (st *Store) filesDB(create bool) (*sql.DB, error) {
	db, err := st.wsDB(create)
	if err != nil || db == nil {
		return db, err
	}
	if _, err := db.Exec(workstreamFilesSchema); err != nil {
		return nil, err
	}
	return db, nil
}

func readFiles(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, ws string) (map[string]factory.WorkstreamFile, error) {
	rows, err := q.Query(`SELECT id, rev, doc FROM workstream_files WHERE ws = ?`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]factory.WorkstreamFile{}
	for rows.Next() {
		var id, doc string
		var rev int
		if err := rows.Scan(&id, &rev, &doc); err != nil {
			return nil, err
		}
		var f factory.WorkstreamFile
		if err := json.Unmarshal([]byte(doc), &f); err != nil {
			return nil, errors.New("the workstream's files cannot be read")
		}
		f.ID, f.WS, f.Rev = id, ws, rev
		if f.UsedBy == nil {
			f.UsedBy = []string{}
		}
		out[id] = f
	}
	return out, rows.Err()
}

// WorkstreamFiles are every file ws keeps, deleted ones included, by id. A
// workstream with none answers an empty map.
func (st *Store) WorkstreamFiles(ws string) (map[string]factory.WorkstreamFile, error) {
	db, err := st.filesDB(false)
	if err != nil || db == nil {
		return map[string]factory.WorkstreamFile{}, err
	}
	return readFiles(db, ws)
}

// FilesSeeded is whether ws's files were already folded in.
func (st *Store) FilesSeeded(ws string) (bool, error) {
	db, err := st.filesDB(false)
	if err != nil || db == nil {
		return false, err
	}
	var n int
	err = db.QueryRow(`SELECT count(*) FROM workstream_file_seeds WHERE ws = ?`, ws).Scan(&n)
	return n > 0, err
}

// FileEdit is what one EditWorkstreamFiles call does to one workstream's
// files: edit sees them all, may add, change or delete entries, and names
// the ids it changed. Seed, when true, marks the workstream as folded in in
// the same transaction.
type FileEdit func(files map[string]factory.WorkstreamFile) (changed []string, seed bool, err error)

// EditWorkstreamFiles changes any number of ws's files in one save, under the
// workstreams' cross-process lock, and answers every file as saved. Each
// changed file still present has its rev counted up by one; one edit removed
// is deleted for good. An error from edit writes nothing.
func (st *Store) EditWorkstreamFiles(ws string, now string, edit FileEdit) (map[string]factory.WorkstreamFile, error) {
	if _, err := st.filesDB(true); err != nil {
		return nil, err
	}
	var out map[string]factory.WorkstreamFile
	err := st.wsTx(func(tx *sql.Tx) error {
		files, err := readFiles(tx, ws)
		if err != nil {
			return err
		}
		before := make(map[string]int, len(files))
		for id, f := range files {
			before[id] = f.Rev
		}
		changed, seed, err := edit(files)
		if err != nil {
			return err
		}
		for _, id := range changed {
			f, ok := files[id]
			if !ok {
				if _, err := tx.Exec(`DELETE FROM workstream_files WHERE ws = ? AND id = ?`, ws, id); err != nil {
					return err
				}
				continue
			}
			f.ID, f.WS, f.Rev = id, ws, before[id]+1
			if f.UsedBy == nil {
				f.UsedBy = []string{}
			}
			files[id] = f
			doc, err := json.Marshal(f)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO workstream_files (ws, id, rev, doc) VALUES (?, ?, ?, ?)
				ON CONFLICT (ws, id) DO UPDATE SET rev = excluded.rev, doc = excluded.doc`, ws, id, f.Rev, string(doc)); err != nil {
				return err
			}
		}
		if seed {
			if _, err := tx.Exec(`INSERT INTO workstream_file_seeds (ws, at) VALUES (?, ?) ON CONFLICT DO NOTHING`, ws, now); err != nil {
				return err
			}
		}
		out = files
		return nil
	})
	return out, err
}

// DropWorkstreamFiles lets go of every file ws keeps and its seed mark, when
// the workstream itself is deleted. Blobs no file names any more are let go
// by the next [Store.DropUnusedBlob].
func (st *Store) DropWorkstreamFiles(ws string) error {
	db, err := st.filesDB(false)
	if err != nil || db == nil {
		return err
	}
	return st.wsTx(func(tx *sql.Tx) error {
		for _, q := range []string{`DELETE FROM workstream_files WHERE ws = ?`, `DELETE FROM workstream_file_seeds WHERE ws = ?`} {
			if _, err := tx.Exec(q, ws); err != nil {
				return err
			}
		}
		return nil
	})
}

// WorkstreamParent is the workstream ws sits inside, empty for one at the
// top. It is read straight from the kept document, so a workstream saved by a
// build that knows about nesting is answered rightly by one that does not
// decode the field.
func (st *Store) WorkstreamParent(ws string) (string, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return "", err
	}
	var parent sql.NullString
	err = db.QueryRow(`SELECT json_extract(doc, '$.parent') FROM workstreams WHERE id = ?`, ws).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return parent.String, nil
}

// ── blobs ──

var blobName = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (st *Store) blobPath(sha string) (string, error) {
	if !blobName.MatchString(sha) {
		return "", ErrFileGone
	}
	return filepath.Join(st.root, WorkstreamBlobsDir, sha[:2], sha), nil
}

// PutBlob keeps data and answers the sha256 it is kept under. The same bytes
// kept twice are one copy.
func (st *Store) PutBlob(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	path, err := st.blobPath(sha)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return sha, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".blob-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return sha, os.Rename(tmp.Name(), path)
}

// BlobPath is where the bytes kept under sha are on disk, for a reader that
// takes a path (a PDF's text layer).
func (st *Store) BlobPath(sha string) (string, error) {
	path, err := st.blobPath(sha)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", ErrFileGone
	}
	return path, nil
}

// Blob is the bytes kept under sha.
func (st *Store) Blob(sha string) ([]byte, error) {
	path, err := st.blobPath(sha)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrFileGone
	}
	return data, err
}

// DropUnusedBlob lets go of the bytes kept under sha when no file of any
// workstream names them any more.
func (st *Store) DropUnusedBlob(sha string) error {
	path, err := st.blobPath(sha)
	if err != nil {
		return nil
	}
	db, err := st.filesDB(false)
	if err != nil || db == nil {
		return err
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM workstream_files WHERE json_extract(doc, '$.upload.blob') = ?`, sha).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// SortFiles orders files folders first, then by name, the id breaking a tie
// so the order never wobbles between two reads.
func SortFiles(files []factory.WorkstreamFile) {
	sort.Slice(files, func(i, j int) bool {
		fi, fj := files[i].Kind == factory.FileFolder, files[j].Kind == factory.FileFolder
		if fi != fj {
			return fi
		}
		if files[i].Name != files[j].Name {
			return files[i].Name < files[j].Name
		}
		return files[i].ID < files[j].ID
	})
}

// ErrFileFolderMove refuses a folder sent to another workstream: what is
// inside it would have to follow, and a move between workstreams is of files.
var ErrFileFolderMove = errors.New("a folder moves only within its own workstream")

// ErrFileNoFolder refuses a move into a folder the other workstream does not
// keep live.
var ErrFileNoFolder = errors.New("that folder is not one of that workstream's folders")

// ErrFileIDTaken refuses a move that would land on a file id the other
// workstream already keeps.
var ErrFileIDTaken = errors.New("that workstream already keeps a file with this id")

// MoveWorkstreamFiles sends ids from one workstream's Files to another's, all
// or none, in one save (Bring along: what a chat made lands in the new
// workstream's Files once). EACH FILE STAYS ITSELF: its id, its maker, the
// task it came from and an upload's bytes (kept by their sha256, which no
// workstream owns). A document takes its page and the page's versions with
// it, so its words and its history follow. Folder is where they land in to
// ("" is the top); a folder, a deleted file and a file from elsewhere are
// refused. It answers the moved files as to now keeps them; the caller moves
// a document's live text (the docs store keys it by workstream).
func (st *Store) MoveWorkstreamFiles(from, to string, ids []string, folder, now, by string) ([]factory.WorkstreamFile, error) {
	if _, err := st.filesDB(true); err != nil {
		return nil, err
	}
	var out []factory.WorkstreamFile
	err := st.wsTx(func(tx *sql.Tx) error {
		src, err := readFiles(tx, from)
		if err != nil {
			return err
		}
		dst, err := readFiles(tx, to)
		if err != nil {
			return err
		}
		if folder != "" {
			if f, ok := dst[folder]; !ok || f.DeletedAt != nil || f.Kind != factory.FileFolder {
				return ErrFileNoFolder
			}
		}
		var pages []string
		for _, id := range ids {
			f, ok := src[id]
			if !ok || f.DeletedAt != nil {
				return ErrFileGone
			}
			if f.Kind == factory.FileFolder {
				return ErrFileFolderMove
			}
			if _, taken := dst[id]; taken {
				return ErrFileIDTaken
			}
			f.WS, f.Folder, f.Rev, f.ChangedBy = to, folder, f.Rev+1, by
			if at, err := time.Parse(time.RFC3339Nano, now); err == nil {
				f.ChangedAt = at
			}
			doc, err := json.Marshal(f)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM workstream_files WHERE ws = ? AND id = ?`, from, id); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO workstream_files (ws, id, rev, doc) VALUES (?, ?, ?, ?)`, to, id, f.Rev, string(doc)); err != nil {
				return err
			}
			dst[id] = f
			out = append(out, f)
			if f.Kind == factory.FileDocument && f.PageID != "" {
				pages = append(pages, f.PageID)
			}
		}
		if len(pages) == 0 {
			return nil
		}
		all, err := readWorkstreams(tx)
		if err != nil {
			return err
		}
		a, okA := all[from]
		b, okB := all[to]
		if !okA || !okB {
			return ErrFileGone
		}
		for _, pid := range pages {
			at := slices.IndexFunc(a.Pages, func(p factory.WorkstreamPage) bool { return p.ID == pid })
			if at < 0 {
				continue
			}
			p := a.Pages[at]
			p.Parent = ""
			a.Pages = slices.Delete(slices.Clone(a.Pages), at, at+1)
			b.Pages = append(slices.Clone(b.Pages), p)
			if _, err := tx.Exec(`UPDATE page_versions SET ws = ? WHERE ws = ? AND page = ?`, to, from, pid); err != nil {
				return err
			}
		}
		if err := putWorkstream(tx, normalizeWorkstream(a)); err != nil {
			return err
		}
		return putWorkstream(tx, normalizeWorkstream(b))
	})
	return out, err
}
