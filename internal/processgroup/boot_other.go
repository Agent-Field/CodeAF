//go:build !linux && !darwin

package processgroup

// readBootID has no answer here. The start identity these platforms read is a
// wall-clock second (identity_unix.go), which is already unique across boots,
// and Windows reads none at all.
func readBootID() string { return "" }
