//go:build windows

package main

import "os"

// appSyncRoot is nothing on Windows, which cannot flush a folder opened for
// reading; the claim file itself is synced before it is linked or renamed into
// place, as internal/session's rooted counters are.
func appSyncRoot(*os.Root) error { return nil }
