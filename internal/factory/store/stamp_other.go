//go:build !linux && !darwin

package store

import "os"

// Other platforms retain the portable size and modification-time check.
func fileIdentity(info os.FileInfo) identity { return identity{} }
