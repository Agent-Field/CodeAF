package session

import "os"

// The small facts the app backend needs from this package's file checks, named
// once so it never grows a second copy of them.

// WithinAny says whether path (already canonical) lies inside one of roots.
func WithinAny(roots []string, path string) bool { return withinAny(roots, path) }

// FileIdentity is a file's change time, device and inode as the platform
// reports them (zeros where it reports none).
func FileIdentity(info os.FileInfo) (ctimeNS int64, dev, ino uint64) { return fileIdentity(info) }

// FileVersion is the "file:" form of a version.
func FileVersion(size, modNS, ctimeNS int64, dev, ino uint64) string {
	return fileVersion(size, modNS, ctimeNS, dev, ino)
}

// MediaSourceOf is the media source id an index row carries, where the row is a
// media source at all.
func MediaSourceOf(row Artifact) (string, bool) {
	media, ok := mediaRowOf(row)
	if !ok {
		return "", false
	}
	return media.Fact.ID, true
}
