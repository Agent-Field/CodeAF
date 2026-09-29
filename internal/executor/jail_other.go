//go:build !linux

package executor

// DefaultJail is nil where no jail exists: processes run unconfined.
func DefaultJail() Jail { return nil }
