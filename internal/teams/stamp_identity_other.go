//go:build !linux && !darwin

package teams

import "os"

// Readers still compare identity with os.SameFile on other platforms; wire
// stamps retain the portable size and modification time there.
func stampIdentity(os.FileInfo) string { return "" }
