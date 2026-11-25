//go:build linux

package sessionhost

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

var (
	sessionID    = regexp.MustCompile(`^hs_[0-9a-f]{24}$`)
	personHandle = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	errLayout    = errors.New("The chat folder is unavailable.")
)

type layout struct {
	dir string
}

func (l layout) home() string { return filepath.Join(l.dir, "home") }
func (l layout) work() string { return filepath.Join(l.dir, "work") }
func (l layout) tmp() string  { return filepath.Join(l.dir, "tmp") }

func personDir(person string) string {
	sum := sha256.Sum256([]byte(person))
	return fmt.Sprintf("p_%x", sum[:8])
}

func layoutFor(data, person, id string) (layout, error) {
	if !sessionID.MatchString(id) || !personHandle.MatchString(person) {
		return layout{}, errLayout
	}
	people := filepath.Join(data, "people")
	dir := filepath.Clean(filepath.Join(people, personDir(person), id))
	rel, err := filepath.Rel(people, dir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return layout{}, errLayout
	}
	return layout{dir: dir}, nil
}

func ownerUID(info os.FileInfo) int {
	return int(info.Sys().(*syscall.Stat_t).Uid)
}

func supervisorDir(path string, mode os.FileMode) error {
	if err := os.Mkdir(path, mode); err != nil {
		if errors.Is(err, os.ErrExist) {
			return checkSupervisorDir(path, mode)
		}
		return err
	}
	return setFreshMode(path, mode)
}

func setFreshMode(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || ownerUID(info) != os.Geteuid() {
		return errLayout
	}
	// Set the exact permissions even if the operator used a stricter umask.
	return os.Chmod(path, mode)
}

// ancestorMode is the mode of every folder above a chat's own folder that the
// supervisor makes (the data folder, people, and one per person). THEY MUST BE
// OPEN FOR READING TO EVERY CHAT'S USER, NOT ONLY FOR PASSING THROUGH. The
// engine's saved-history helper makes each new file durable by opening and
// syncing every folder above it, all the way up; a folder that allowed only
// passing through (0711, which these were) made that open fail with "Permission
// denied", and the person's first command in every new chat said "calls are
// not being sealed". What reading these folders shows another chat is their
// names (a person's hashed name, a chat's random identifier) and nothing inside:
// every chat's own folder stays closed (0700, its own user).
const ancestorMode os.FileMode = 0o755

// legacyAncestorMode is what the first stage of hosted chats made these
// folders as. A folder with exactly that mode is opened up on start.
const legacyAncestorMode os.FileMode = 0o711

// ancestorDir makes path as an ancestor folder, or accepts one that is there.
func ancestorDir(path string) error {
	err := os.Mkdir(path, ancestorMode)
	switch {
	case err == nil:
		return setFreshMode(path, ancestorMode)
	case errors.Is(err, os.ErrExist):
		return openAncestor(path)
	}
	return err
}

// openAncestor accepts an ancestor folder the supervisor owns, opening up one
// the first stage made, and refuses anything else (another owner, another mode,
// a link) as the rest of this file does. It never makes a folder.
func openAncestor(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || ownerUID(info) != os.Geteuid() {
		return errLayout
	}
	switch info.Mode().Perm() {
	case ancestorMode:
		return nil
	case legacyAncestorMode:
		return os.Chmod(path, ancestorMode)
	}
	return errLayout
}

func checkSupervisorDir(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || ownerUID(info) != os.Geteuid() || info.Mode().Perm() != mode {
		return errLayout
	}
	return nil
}

func (l layout) stat() (os.FileInfo, error) {
	parent := filepath.Dir(l.dir)
	for _, path := range []string{filepath.Dir(parent), parent} {
		if err := openAncestor(path); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(l.dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errLayout
	}
	return info, nil
}

// makePrivateDir reports whether it created the outer folder. Only that fresh
// folder can be rolled back by the caller or handed to a new owner.
func (l layout) makePrivateDir(uid int, chown func(string, int, int) error) (bool, error) {
	parent := filepath.Dir(l.dir)
	for _, path := range []string{filepath.Dir(parent), parent} {
		if err := ancestorDir(path); err != nil {
			return false, err
		}
	}
	if err := os.Mkdir(l.dir, 0o700); err != nil {
		return false, err
	}
	if err := setFreshMode(l.dir, 0o700); err != nil {
		return true, err
	}
	return true, chown(l.dir, uid, uid)
}
