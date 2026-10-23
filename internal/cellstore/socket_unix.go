//go:build !windows

package cellstore

import (
	"github.com/Agent-Field/codeaf/internal/home"
	"path/filepath"
	"strings"
)

// socketFor keys the daemon to its engine build, preserving Unix socket names.
func socketFor(bin string) string {
	return home.Join("v3", "engine-"+strings.TrimPrefix(filepath.Base(bin), "furrow-")+".sock")
}
