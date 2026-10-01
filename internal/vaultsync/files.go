package vaultsync

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// fileScopePrefix keeps the withheld files apart from the variables of a
// project: Entries(project) never offers a file to a tool's environment.
const fileScopePrefix = "home:file:"

// Files carries the files a chat's seals withhold because they hold secrets:
// one vault slot per path, holding the exact bytes and the mode, merged by the
// newest whole file, deleted by a tombstone. It is the credentials.json rule
// applied to every withheld path of a workspace, so a file comes back on the
// other machine byte for byte at the path it had.
type Files struct {
	// Root is the workspace folder the paths are relative to.
	Root string
	// Chat is the chat's id in the directory, the same on every machine. Each
	// chat has its own slots, so one chat's folder can never delete another's file.
	Chat string
	// Withheld lists the paths this machine keeps out of its seals, slash
	// separated and relative to Root.
	Withheld func() ([]string, error)
}

func (f Files) capture(v VaultFile) error { return f.each(v, Carried.capture) }

func (f Files) captureEdits(v VaultFile) error { return f.each(v, Carried.captureEdits) }

func (f Files) restore(v VaultFile) error { return f.each(v, Carried.restore) }

// each applies step to the slot of every path this machine or the vault knows.
// A path only the vault knows must be visited so it is written here, and one
// only this machine knows so that its removal travels.
func (f Files) each(v VaultFile, step func(Carried, VaultFile) error) error {
	rels, err := f.paths(v)
	if err != nil {
		return err
	}
	for _, rel := range rels {
		if err := step(f.slot(rel), v); err != nil {
			return err
		}
	}
	return nil
}

// paths is the sorted union of the withheld paths and every path the vault has
// a slot for, live or deleted. A name that would leave the workspace is dropped:
// the vault is data from other machines and never decides where a write lands.
func (f Files) paths(v VaultFile) ([]string, error) {
	local, err := f.Withheld()
	if err != nil {
		return nil, err
	}
	ids, err := v.IDs(f.prefix())
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, rel := range local {
		seen[rel] = true
	}
	for _, id := range ids {
		seen[strings.TrimPrefix(id, f.prefix())] = true
	}
	var out []string
	for rel := range seen {
		if filepath.IsLocal(filepath.FromSlash(rel)) {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (f Files) scope() string { return fileScopePrefix + f.Chat }

// prefix starts the id of every slot of this chat's files.
func (f Files) prefix() string { return f.scope() + "/" }

func (f Files) slot(rel string) Carried {
	return Carried{
		ID: f.prefix() + rel, Scope: f.scope(), Name: rel,
		Medium: fileMedium{root: f.Root, path: filepath.Join(f.Root, filepath.FromSlash(rel))},
	}
}

// fileMedium is one file as a Medium. Its content is the mode and the bytes in
// one text, so the vault's whole-slot rules keep the two together.
type fileMedium struct{ root, path string }

func (m fileMedium) Read() (string, time.Time, error) {
	info, err := os.Lstat(m.path)
	if err != nil {
		return "", time.Time{}, err
	}
	if !info.Mode().IsRegular() {
		return "", time.Time{}, ErrDamaged
	}
	raw, err := os.ReadFile(m.path)
	return encodeFile(info.Mode().Perm(), raw), info.ModTime(), err
}

func (m fileMedium) Realized(content string) string { return content }

func (m fileMedium) Clear() error {
	return keepingFolderTimes(m.root, m.path, func() error { return ignoreMissing(os.Remove(m.path)) })
}

// Write puts the file back with its mode, and leaves a file that already is that
// alone so an unchanged file keeps its save time.
func (m fileMedium) Write(content string) error {
	mode, data, err := decodeFile(content)
	if err != nil {
		return ErrDamaged
	}
	if have, _, err := m.Read(); err == nil && have == content {
		return nil
	}
	return keepingFolderTimes(m.root, m.path, func() error {
		if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
			return err
		}
		return writeAtomic(m.path, data, mode)
	})
}

// keepingFolderTimes runs change, which adds or removes the withheld file at
// path, and gives every folder between root and the file the save time it had
// before. A folder's time is part of what a seal records, so a change that
// moved it would read as an edit of the tree at the next takeover, yet putting
// a withheld file back, or taking it away, is the vault's doing and not the
// person's. Folders that change made are new and keep the time they got.
func keepingFolderTimes(root, path string, change func() error) error {
	before := map[string]os.FileInfo{}
	for dir := filepath.Dir(path); within(root, dir); dir = filepath.Dir(dir) {
		if info, err := os.Stat(dir); err == nil {
			before[dir] = info
		}
		if dir == root {
			break
		}
	}
	err := change()
	for dir, info := range before {
		_ = os.Chtimes(dir, info.ModTime(), info.ModTime())
	}
	return err
}

// within says whether dir is root or a folder below it.
func within(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// encodeFile is "<octal mode>:<base64 bytes>": text, so a binary key survives
// the vault's JSON.
func encodeFile(mode fs.FileMode, data []byte) string {
	return fmt.Sprintf("%04o:%s", mode, base64.StdEncoding.EncodeToString(data))
}

func decodeFile(content string) (fs.FileMode, []byte, error) {
	octal, b64, ok := strings.Cut(content, ":")
	if !ok {
		return 0, nil, errors.New("vaultsync: a file slot has no mode")
	}
	mode, err := strconv.ParseUint(octal, 8, 32)
	if err != nil {
		return 0, nil, err
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	return fs.FileMode(mode).Perm(), data, err
}
