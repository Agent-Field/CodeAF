//go:build e2e && !linux && !darwin

package e2e

import "os"

func wanAllocated(os.FileInfo) (int64, bool) { return 0, false }
