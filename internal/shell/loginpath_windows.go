//go:build windows

package shell

import "os"

// InheritLoginPath leaves the Windows PATH as supplied by the app launcher.
func InheritLoginPath() string { return os.Getenv("PATH") }
