package home

import (
	"fmt"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/env"
)

// Logf is the one-line reporting door boot adoption uses when best effort was
// not enough. It matches log.Printf without making this package choose a log.
type Logf func(string, ...any)

// Adopt moves an untouched legacy state root to the current name once. It is a
// boot operation, not root resolution: the real binary calls it from main, and
// the testing guard makes an accidental library call harmless in test binaries.
func Adopt(logf Logf) {
	if testing.Testing() {
		return
	}
	adoptLogin(logf)
}

func adoptLogin(logf Logf) {
	if _, set := env.Lookup(EnvVar); set {
		return
	}
	base, err := os.UserHomeDir()
	if err != nil {
		adoptionFailed(logf, err)
		return
	}
	adopt(base, logf, adoptionOS{
		lstat:   os.Lstat,
		stat:    os.Stat,
		rename:  os.Rename,
		symlink: os.Symlink,
	})
}

type adoptionOS struct {
	lstat   func(string) (os.FileInfo, error)
	stat    func(string) (os.FileInfo, error)
	rename  func(string, string) error
	symlink func(string, string) error
}

func adopt(base string, logf Logf, operations adoptionOS) {
	current, legacy := DefaultUnder(base), legacyUnder(base)
	if _, err := operations.lstat(current); err == nil {
		info, statErr := operations.stat(current)
		if statErr == nil && info.IsDir() {
			return
		}
		if statErr != nil {
			adoptionFailed(logf, fmt.Errorf("%s cannot be used as the state folder: %w", current, statErr))
		} else {
			adoptionFailed(logf, fmt.Errorf("%s is not a directory", current))
		}
		return
	} else if !os.IsNotExist(err) {
		adoptionFailed(logf, err)
		return
	}
	info, err := operations.lstat(legacy)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		adoptionFailed(logf, err)
		return
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	if err := operations.rename(legacy, current); err != nil {
		adoptionFailed(logf, err)
		return
	}
	if err := operations.symlink(".codeaf", legacy); err != nil {
		adoptionFailed(logf, err)
	}
}

func adoptionFailed(logf Logf, err error) {
	if logf != nil {
		logf("codeaf: could not adopt the former state folder: %v", err)
	}
}
