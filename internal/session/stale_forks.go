package session

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/processgroup"
)

var furrowStagingName = regexp.MustCompile(`^\.([^/]+)\.furrow-fork-([0-9]+)-[0-9]+$`)
var furrowForksName = regexp.MustCompile(`^([0-9]+)\.furrow-forks$`)

func removeForkStaging(destination string) {
	parent := filepath.Dir(destination)
	entries, _ := os.ReadDir(parent)
	for _, entry := range entries {
		match := furrowStagingName.FindStringSubmatch(entry.Name())
		if entry.IsDir() && len(match) == 3 && match[1] == filepath.Base(destination) {
			_ = os.RemoveAll(filepath.Join(parent, entry.Name()))
		}
	}
}

func sweepMovingForks(ctx context.Context, moving string, now time.Time, note func(string)) {
	entries, err := os.ReadDir(moving)
	if err != nil {
		note(fmt.Sprintf("sweep: could not read %s: %v", moving, err))
		return
	}
	for _, entry := range entries {
		if contextDone(ctx) {
			return
		}
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(moving, entry.Name())
		state, err := os.Lstat(filepath.Join(dir, cell.StateDir))
		if err != nil || !state.IsDir() {
			continue
		}
		ready, err := os.Lstat(filepath.Join(dir, cell.CopyReadyPath))
		if err != nil || !ready.Mode().IsRegular() || sessionIsOpen(dir) {
			continue
		}
		sweepAbandonedForks(ctx, dir, now, note)
	}
}

func sweepAbandonedForks(ctx context.Context, dir string, now time.Time, note func(string)) {
	trees := filepath.Join(dir, placeTrees)
	info, err := os.Lstat(trees)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		note(fmt.Sprintf("sweep: could not read %s: %v", trees, err))
		return
	}
	if !info.IsDir() {
		return
	}
	entries, err := os.ReadDir(trees)
	if err != nil {
		note(fmt.Sprintf("sweep: could not read %s: %v", trees, err))
		return
	}
	for _, entry := range entries {
		if contextDone(ctx) {
			return
		}
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if staging := furrowStagingName.FindStringSubmatch(name); staging != nil {
			// A fork still being written belongs to a live process.
			if pid, err := strconv.Atoi(staging[2]); err != nil || processgroup.ProcessAlive(pid) {
				continue
			}
		} else {
			match := furrowForksName.FindStringSubmatch(name)
			if len(match) != 2 {
				continue
			}
			if _, err := os.Lstat(filepath.Join(trees, match[1])); !errors.Is(err, fs.ErrNotExist) {
				continue
			}
		}
		path := filepath.Join(trees, name)
		newest, ok := newestUnder(path)
		if !ok || !newest.Before(now.Add(-sweepTTL)) {
			continue
		}
		if contextDone(ctx) {
			return
		}
		if err := os.RemoveAll(path); err != nil {
			note(fmt.Sprintf("sweep: could not remove abandoned fork folder %s: %v", path, err))
		} else {
			note(fmt.Sprintf("sweep: removed abandoned fork folder %s", path))
		}
	}
}
