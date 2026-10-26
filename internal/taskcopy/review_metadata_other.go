//go:build !linux && !darwin

package taskcopy

import (
	"context"
	"errors"
	"io/fs"
)

func portablePlatformMetadata(path string, info fs.FileInfo) error {
	return errors.New("portable apply cannot verify this platform's extended metadata; private copy retained")
}

func portablePlatformMetadataBatch(ctx context.Context, paths []string) error { return ctx.Err() }

func portableUnsupportedNativeMetadata(path string, info fs.FileInfo) error {
	return portablePlatformMetadata(path, info)
}

func SetPortableSymlinkModeAt(parentFD int, name string, mode fs.FileMode) error {
	return errors.New("this platform cannot preserve symlink permissions")
}
