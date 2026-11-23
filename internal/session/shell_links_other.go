//go:build !unix

package session

import "os"

func linkCount(os.FileInfo) uint64 { return 1 }
