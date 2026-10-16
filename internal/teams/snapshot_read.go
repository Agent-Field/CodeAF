package teams

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// SnapshotMaxBytes is the largest teams file [ReadSnapshot] will decode. A
// larger one is [ErrSnapshotTooLarge], never a prefix of its teams.
const SnapshotMaxBytes = 2 << 20

// snapshotChunk is how much [ReadSnapshot] reads between looks at its context.
const snapshotChunk = 64 << 10

var (
	// ErrSnapshotTooLarge is a teams file over the reader's byte limit.
	ErrSnapshotTooLarge = errors.New("teams: the file is larger than a snapshot reads")
	// ErrSnapshotVersion is a teams file written by a build newer than this one.
	ErrSnapshotVersion = errors.New("teams: the file was written by a newer build")
	// ErrSnapshotUnreadable is a teams file that is not valid JSON of its shape.
	ErrSnapshotUnreadable = errors.New("teams: the file is unreadable")
)

// Revision is the lowercase hex SHA-256 of the exact bytes a snapshot decoded.
type Revision string

// ReadSnapshot is the teams file exactly as it is on disk, for a reader that
// must not change anything: no lock, no tidy, no colour, no migration, no
// rename and no write of any kind. [Load] repairs and writes, so a read-only
// door cannot use it.
//
// It reads at most limit bytes (SnapshotMaxBytes when limit is not positive),
// in chunks, and looks at ctx between them, so a cancelled or expired context
// ends the read with the context's error. A file over the limit is
// [ErrSnapshotTooLarge]; a file from a newer build is [ErrSnapshotVersion];
// bytes that are not JSON of the file's shape are [ErrSnapshotUnreadable]. A
// missing file (and a missing first-build file beside it) is an empty File at
// the revision of no bytes, and no error. Any other failure to read is that
// error and never an empty File.
//
// The decode is [read]'s: the first build's spaces.json is read when there is
// no teams.json, and a pre-current file with no teams of its own reads its
// legacy list. The File's teams are as stored, so they may break the rules
// [tidy] would impose; the caller checks the facts it relies on.
func ReadSnapshot(ctx context.Context, profileDir string, limit int64) (*File, Revision, error) {
	if limit <= 0 {
		limit = SnapshotMaxBytes
	}
	raw, legacy, err := readBounded(ctx, profileDir, limit)
	if err != nil {
		return nil, "", err
	}
	if raw == nil {
		return &File{Version: Version, localIdentities: true}, revisionOf(nil), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	var d *disk
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrSnapshotUnreadable, err)
	}
	if d == nil {
		return nil, "", ErrSnapshotUnreadable
	}
	if d.Version > Version {
		return nil, "", ErrSnapshotVersion
	}
	return &File{Version: Version, Teams: d.list(legacy), localIdentities: true}, revisionOf(raw), nil
}

func revisionOf(raw []byte) Revision {
	sum := sha256.Sum256(raw)
	return Revision(hex.EncodeToString(sum[:]))
}

// readBounded is the bytes of teams.json, or of spaces.json when there is no
// teams.json, nil for neither. legacy says which it was.
func readBounded(ctx context.Context, profileDir string, limit int64) (raw []byte, legacy bool, err error) {
	raw, err = readChunks(ctx, Path(profileDir), limit)
	if errors.Is(err, os.ErrNotExist) {
		legacy = true
		raw, err = readChunks(ctx, legacyPath(profileDir), limit)
		if errors.Is(err, os.ErrNotExist) {
			// Migration publishes the primary before renaming the legacy file.
			// Both earlier opens can miss across that change; look once more at
			// the primary so an existing store never turns into an empty reading.
			raw, err = readChunks(ctx, Path(profileDir), limit)
			if errors.Is(err, os.ErrNotExist) {
				return nil, false, nil
			}
			return raw, false, err
		}
	}
	return raw, legacy, err
}

// readChunks reads one file, at most limit bytes, checking ctx before each
// chunk. It reads one byte past the limit to tell a file of exactly limit
// bytes from a longer one.
func readChunks(ctx context.Context, path string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var raw []byte
	buf := make([]byte, snapshotChunk)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		readSize := int64(len(buf))
		if remaining := limit - int64(len(raw)); remaining < readSize {
			readSize = remaining + 1
		}
		n, err := f.Read(buf[:readSize])
		raw = append(raw, buf[:n]...)
		if int64(len(raw)) > limit {
			return nil, ErrSnapshotTooLarge
		}
		if errors.Is(err, io.EOF) {
			if raw == nil {
				raw = []byte{}
			}
			return raw, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
