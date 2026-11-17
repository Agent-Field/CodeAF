//go:build !windows

package fsreplace

import "os"

func rename(old, new string) error { return os.Rename(old, new) }
