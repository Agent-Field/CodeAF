//go:build !linux && !darwin

package processgroup

// readBootID has no answer here, so persisted identities cannot be restored.
// Captured groups still keep their existing in-process signal protection.
func readBootID() string { return "" }
