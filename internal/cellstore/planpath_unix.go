//go:build !windows

package cellstore

import "path/filepath"

func planRelative(root, physical string) (string, error) { return filepath.Rel(root, physical) }
