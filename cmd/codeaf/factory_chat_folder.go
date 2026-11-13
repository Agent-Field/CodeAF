package main

// Which folder a factory chat works in, and the one folder it never may.
//
// A CHAT'S FOLDER IS ITS PROJECT: its tools run there and every save sweeps it
// for secrets before it seals. The factory hosts used to hand their chats the
// folder the engine process happened to start in — os.Getwd for a window, a
// worker and a runner, and the codeaf home itself for the desk's own factory —
// whenever a job had no checkout of its own. Every AgentField step's chat, the
// foreman, and a stage with no worktree therefore worked in ~/.codeaf, and on
// one desk twenty-three of them swept its graph database, the factory's clones
// and the credentials into the key vault on every save (3.3 GB, 22 s of CPU a
// save). This file is where that is decided instead, for every factory door.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

// errFactoryFolderIsHome is the plain refusal of a factory chat whose folder is
// codeaf's own: the codeaf home, a folder above it, or a folder inside it that
// is not one of the factory's own job folders.
var errFactoryFolderIsHome = errors.New("a factory chat cannot work in codeaf's own folder")

// factoryJobAreas are the folders under a factory floor whose children are
// one job's own folder each: a worktree (work/), a clone (repos/), and the
// empty folder a job with nothing on this machine works in (chats/). They are
// the only places inside the codeaf home a factory chat may work.
var factoryJobAreas = []string{"work", "repos", "chats"}

// factoryFolderRefusal is nil when a factory chat may work in dir, and
// [errFactoryFolderIsHome] naming dir when it may not. EVERY FACTORY DOOR THAT
// MINTS A CHAT ASKS IT FIRST, so a folder that holds codeaf's state can come
// back from no path: not the engine's working directory, not a host that named
// the home, not a recorded checkout that was wrong.
//
// It is decided by where the folder is, never by what is in it. The codeaf home
// and every folder above it are refused outright. A folder inside the home is
// refused unless it lies inside one of floor's [factoryJobAreas], which is
// where a local job's own worktree and clone live. An empty dir is not this
// rule's to judge; its caller already says it has no folder.
func factoryFolderRefusal(floor *store.Store, dir string) error {
	dir = strings.TrimSpace(dir)
	state := strings.TrimSpace(home.Dir())
	if dir == "" || state == "" {
		return nil
	}
	if pathWithin(dir, state) {
		return fmt.Errorf("%w (%s)", errFactoryFolderIsHome, dir)
	}
	if !pathWithin(state, dir) {
		return nil
	}
	if floor != nil && strings.TrimSpace(floor.Root()) != "" {
		for _, area := range factoryJobAreas {
			root := filepath.Join(floor.Root(), area)
			if pathWithin(root, dir) && !pathWithin(dir, root) {
				return nil
			}
		}
	}
	return fmt.Errorf("%w (%s)", errFactoryFolderIsHome, dir)
}

// factoryHostWorkspace is the folder a factory host may fall back to for its
// chats: dir, unless dir is the codeaf home, above it or inside it, which is
// no fallback at all (""). A host whose engine was started in ~/.codeaf, or
// that named the home itself, then has no fallback, and each door below falls
// back to a job's own empty folder instead.
func factoryHostWorkspace(dir string) string {
	dir = strings.TrimSpace(dir)
	state := strings.TrimSpace(home.Dir())
	if dir == "" || state == "" {
		return dir
	}
	if pathWithin(dir, state) || pathWithin(state, dir) {
		return ""
	}
	return dir
}

// factoryJobFolder is the job's own empty folder under the floor,
// `<floor>/chats/<job>`, made when it is missing. It is the folder of every
// chat of a job that has nothing on this machine to work in: a job AgentField
// runs (factory.RunsElsewhere) never runs its code here, so its chats need
// a folder only because a chat has one, and an empty one sweeps nothing.
func factoryJobFolder(floor *store.Store, job int) (string, error) {
	if floor == nil || strings.TrimSpace(floor.Root()) == "" || job <= 0 {
		return "", errors.New("codeaf does not know which folder this item's conversation belongs in")
	}
	own := filepath.Join(floor.Root(), "chats", strconv.Itoa(job))
	if err := os.MkdirAll(own, 0o700); err != nil {
		return "", fmt.Errorf("the item's conversation has no folder: %w", err)
	}
	return own, nil
}

// factoryForemanFolder is the foreman's folder when its host has no workspace
// of its own: `<floor>/chats/foreman`, empty, made when missing. The foreman
// reads the floor through its door, never through files.
func factoryForemanFolder(floor *store.Store) (string, error) {
	if floor == nil || strings.TrimSpace(floor.Root()) == "" {
		return "", errors.New("codeaf does not know which folder the foreman's conversation belongs in")
	}
	own := filepath.Join(floor.Root(), "chats", "foreman")
	if err := os.MkdirAll(own, 0o700); err != nil {
		return "", fmt.Errorf("the foreman's conversation has no folder: %w", err)
	}
	return own, nil
}

// pathWithin reports whether path is root or inside it, comparing both as
// written and with symbolic links resolved, so a home reached through a link
// (macOS's /var and /private/var) is still recognised.
func pathWithin(root, path string) bool {
	if home.Contains(root, path) {
		return true
	}
	r, rerr := filepath.EvalSymlinks(root)
	p, perr := filepath.EvalSymlinks(path)
	if rerr != nil {
		r = root
	}
	if perr != nil {
		p = path
	}
	return (rerr == nil || perr == nil) && home.Contains(r, p)
}
