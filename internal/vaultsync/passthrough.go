package vaultsync

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
)

// BLIND PASS-THROUGH CARRIES A CHAT'S WITHHELD FILES AND NOTHING ELSE. With
// hosted sync storage off, the computer that has a chat sends its files live,
// through the blind relay pipe, to the computer taking it. Offer and Receive are
// the file-reading and file-writing halves of that: they read and write the real
// files directly and never touch the key vault, so a saved provider key, a
// credential, a preference, a shared memory or a team can never ride along.
// Everything a vault slot refuses (a file too large for one) is refused here
// too, because both roads ask the same questions of the same file (files.go).

// PassThroughPage is the bytes of file data in one page of an Offer. It is chosen
// so that a page and its sealing stay under the files door's 8 MiB response
// bound; a single file is at most keys.MaxSlotValue, so a page always has room
// for the one file that Offer sends whatever the budget.
const PassThroughPage = 4 << 20

// File is one withheld file as blind pass-through moves it.
type File struct {
	// Path is slash separated and relative to Files.Root.
	Path string
	// Mode is the permission bits only (mode.Perm()).
	Mode fs.FileMode
	// ModTime is when the sender saved the file. The newest whole file wins, so
	// it is carried and set on the copy that arrives.
	ModTime time.Time
	// Data is the exact bytes.
	Data []byte
}

// Received says, by path only, what Receive did with each incoming file. It
// never holds any content, so it is safe to log and to show.
type Received struct {
	// Written is the files that were new here or replaced an older copy.
	Written []string
	// Same is the files already byte-identical (and mode-identical) here, left alone.
	Same []string
	// Newer is the files where a different copy here was saved later than the
	// sender's, so the copy here was kept.
	Newer []string
	// Refused is the files left alone because the path is not one a sender may
	// name, the entry here is not a regular file, the file here or the incoming
	// one is too large to travel, or the write could not be made.
	Refused []string
}

// Offer reads the chat's withheld files in sorted path order, only those whose
// path sorts after the cursor (empty means from the start), and stops before the
// file that would take the page past budget bytes of data. A page always holds
// at least one file when any remain, so a budget smaller than one file still
// moves; more says files remain after this page, and the last path returned is
// the cursor for the next.
//
// A path that is not in this folder, that is gone, that is not a regular file,
// that is too large to travel (it stays on this computer, as with the vault) or
// that cannot be read is SKIPPED: it is neither an error nor more, so a folder
// full of such paths never keeps a take asking for another page.
func (f Files) Offer(after string, budget int) (files []File, more bool, err error) {
	rels, err := f.Withheld()
	if err != nil {
		return nil, false, err
	}
	used := 0
	for _, rel := range offerable(rels, after) {
		file, ok := f.look(rel)
		if !ok {
			continue
		}
		// The first file always goes, so a page is never empty while files remain.
		if len(files) > 0 && used+len(file.Data) > budget {
			return files, true, nil
		}
		files = append(files, file)
		used += len(file.Data)
	}
	return files, false, nil
}

// offerable is the sorted, deduplicated paths past the cursor that the other
// computer would accept: a path Receive would refuse is not even read, so the
// bytes of a file with nowhere to land never leave this computer.
func offerable(rels []string, after string) []string {
	seen := map[string]bool{}
	var out []string
	for _, rel := range rels {
		if rel > after && !seen[rel] && unsafePath(rel) == nil {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// look reads one withheld file the way the vault does: through the confined
// root, from the one opened file, and refusing one too large to travel.
func (f Files) look(rel string) (File, bool) {
	mode, data, at, err := f.medium(rel).read()
	if err != nil {
		return File{}, false
	}
	return File{Path: rel, Mode: mode, ModTime: at, Data: data}, true
}

func (f Files) medium(rel string) fileMedium {
	return fileMedium{root: f.Root, path: filepath.Join(f.Root, filepath.FromSlash(rel))}
}

// errUnsafePath is why a path another computer named is not written. It names no
// path itself: Receive says which, and never anything about the file.
var errUnsafePath = errors.New("vaultsync: the path is not one a file may be written to")

// controlFolders are the folders the engine and the cell keep inside a chat's
// folder (cellstore keeps the same three apart from every scan). They are never
// withheld, so a file claiming to live in one is not a withheld file.
var controlFolders = []string{".git", ".furrow", ".cell"}

// unsafePath is nil when rel is a path a withheld file may be written to.
// THE SENDER IS ANOTHER COMPUTER AND NEVER DECIDES WHERE A WRITE LANDS: a path
// must be written the one way this folder's own paths are (slash separated and
// already clean), must stay inside the folder, and must not name a control
// folder at any depth. A path that merely reaches a control folder by way of
// ".." is caught the same way, by the folder's name.
func unsafePath(rel string) error {
	switch {
	case rel == "" || rel == ".",
		strings.ContainsAny(rel, "\\\x00"),
		// An NTFS colon names an alternate stream of a file, not a file.
		runtime.GOOS == "windows" && strings.Contains(rel, ":"),
		path.Clean(rel) != rel,
		!filepath.IsLocal(filepath.FromSlash(rel)):
		return errUnsafePath
	}
	for _, part := range strings.Split(rel, "/") {
		for _, control := range controlFolders {
			// A case-insensitive file system gives ".GIT" the same folder.
			if strings.EqualFold(part, control) {
				return errUnsafePath
			}
		}
	}
	return nil
}

// Receive installs files under Root by the rule the vault applies to whole
// files: THE NEWEST WHOLE FILE WINS. A path with no file here is written, an
// identical file here is left alone, and a different file here that was saved
// later than the incoming one is kept; otherwise (older, or saved at the same
// time) the incoming file replaces it, since the computer taking a chat takes
// what the holder has. A write keeps the exact bytes and mode, goes through the
// same confined, pinned-parent, temp-then-rename road as the vault's restore,
// and dates the file as the sender did.
//
// A file that cannot be placed never stops the others. Every file is answered
// once in Received, and the error joins one error per file that could not be
// written (confinement refused it, the disk refused it), each naming the path
// only. A path the sender may not name, an entry here that is not a regular
// file and a file too large to travel are refusals, not errors: they are left
// alone exactly as the vault leaves them.
func (f Files) Receive(files []File) (Received, error) {
	var got Received
	var errs []error
	for _, in := range files {
		outcome, err := f.receiveOne(in)
		switch outcome {
		case wrote:
			got.Written = append(got.Written, in.Path)
		case identical:
			got.Same = append(got.Same, in.Path)
		case keptNewer:
			got.Newer = append(got.Newer, in.Path)
		default:
			got.Refused = append(got.Refused, in.Path)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("vaultsync: %q: %w", in.Path, err))
		}
	}
	return got, errors.Join(errs...)
}

// placement is where one incoming file ended up, which is the list of Received
// it belongs in.
type placement int

const (
	refusedFile placement = iota
	wrote
	identical
	keptNewer
)

func (f Files) receiveOne(in File) (placement, error) {
	if unsafePath(in.Path) != nil || !canTravel(int64(len(in.Data))) {
		return refusedFile, nil
	}
	mode := in.Mode.Perm()
	m := f.medium(in.Path)
	haveMode, haveData, haveAt, err := m.read()
	switch {
	case err == nil:
		if haveMode == realizedMode(mode) && bytes.Equal(haveData, in.Data) {
			return identical, nil
		}
		if haveAt.After(in.ModTime) {
			return keptNewer, nil
		}
	case errors.Is(err, os.ErrNotExist):
		// nothing here: the file is new
	case errors.Is(err, ErrDamaged) || errors.Is(err, keys.ErrTooLarge):
		// Not a regular file, or one too large to travel: left exactly as it is.
		return refusedFile, nil
	default:
		// Confinement, permission or a path through a plain file: say which path.
		return refusedFile, err
	}
	if err := m.writeAt(encodeFile(mode, in.Data), in.ModTime); err != nil {
		return refusedFile, err
	}
	return wrote, nil
}
