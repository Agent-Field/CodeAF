package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ErrMediaReference means a source is not an unchanged, recorded image that may
// be carried by another message. No caller-supplied path grants access.
var ErrMediaReference = errors.New("that recorded image is no longer available as a reference")

// MediaReferenceCopy is an immutable owned snapshot. Remove discards only the
// newly created snapshot, never the source. The caller keeps it on accepted or
// uncertain delivery, since a durable message may now refer to it.
type MediaReferenceCopy struct {
	Path   string
	SHA256 string
	dir    string
	name   string
	owned  os.FileInfo
}

func (c MediaReferenceCopy) Remove() error {
	// Cleanup uses only the private creation record, never mutable exported Path.
	if c.dir == "" || c.name == "" || c.owned == nil {
		return nil
	}
	root, err := os.OpenRoot(c.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(c.name)
	if errors.Is(err, os.ErrNotExist) {
		return os.Remove(c.dir)
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !os.SameFile(c.owned, info) {
		return ErrMediaReference
	}
	if err := root.Remove(c.name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(c.dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// CopyMediaReference snapshots one authoritative source into the engine-owned
// destination. Admission and ownership belong to the caller. It uses a 32KiB
// streaming buffer, at most the ordinary image-message byte limit, and verifies
// the SAME open file before/after copying. No pixel decoding or base64 occurs.
func CopyMediaReference(ctx context.Context, row MediaRow, roots []string, version, destination string) (MediaReferenceCopy, error) {
	file, before, live, err := openMediaReference(ctx, row, roots, version)
	if err != nil {
		return MediaReferenceCopy{}, err
	}
	defer file.Close()
	dir, err := os.MkdirTemp(destination, ".reference-")
	if err != nil {
		return MediaReferenceCopy{}, err
	}
	copy := MediaReferenceCopy{Path: filepath.Join(dir, filepath.Base(live.Path)), dir: dir, name: filepath.Base(live.Path)}
	out, err := os.OpenFile(copy.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = os.Remove(dir)
		return MediaReferenceCopy{}, err
	}
	copy.owned, err = statCreated(out)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(copy.Path)
		_ = os.Remove(dir)
		return MediaReferenceCopy{}, err
	}
	keep := false
	defer func() {
		_ = out.Close()
		if !keep {
			_ = copy.Remove()
		}
	}()
	hash := sha256.New()
	n, err := io.CopyBuffer(io.MultiWriter(out, hash), io.LimitReader(referenceReader{ctx, file}, before.Size()+1), make([]byte, 32<<10))
	if err != nil {
		return MediaReferenceCopy{}, err
	}
	after, err := file.Stat()
	if err != nil || n != before.Size() || referenceIdentity(after) != live.Version || ResolveMedia(row, roots).Version != live.Version {
		return MediaReferenceCopy{}, ErrMediaReference
	}
	if row.Fact.SHA256 != "" && hex.EncodeToString(hash.Sum(nil)) != row.Fact.SHA256 {
		return MediaReferenceCopy{}, ErrMediaReference
	}
	if err := ctx.Err(); err != nil {
		return MediaReferenceCopy{}, err
	}
	if err := out.Close(); err != nil {
		return MediaReferenceCopy{}, err
	}
	copy.SHA256 = hex.EncodeToString(hash.Sum(nil))
	keep = true
	return copy, nil
}

// statCreated reads the creation record of a file this package just made. It is a
// variable so a test can make the one call that Remove depends on fail.
var statCreated = func(f *os.File) (os.FileInfo, error) { return f.Stat() }

func openMediaReference(ctx context.Context, row MediaRow, roots []string, version string) (*os.File, os.FileInfo, MediaLive, error) {
	live, err := referenceLive(row, roots, version)
	if err != nil {
		return nil, nil, MediaLive{}, err
	}
	return openLiveMediaReference(ctx, live)
}

// The path was validated before this call, but a writer may replace it before
// open. Nonblocking open prevents a replacement FIFO from holding staging
// admission forever; the descriptor stat below still requires the exact file.
func openLiveMediaReference(ctx context.Context, live MediaLive) (*os.File, os.FileInfo, MediaLive, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, MediaLive{}, err
	}
	file, err := openRecordFile(live.Path)
	if err != nil {
		return nil, nil, MediaLive{}, ErrMediaReference
	}
	keep := false
	defer func() {
		if !keep {
			_ = file.Close()
		}
	}()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || referenceIdentity(before) != live.Version {
		return nil, nil, MediaLive{}, ErrMediaReference
	}
	if before.Size() <= 0 || before.Size() > maxImageBytes {
		return nil, nil, MediaLive{}, markError("reference image is outside the %s image limit", byteLimit(maxImageBytes))
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, MediaLive{}, err
	}
	var head [512]byte
	nhead, readErr := file.ReadAt(head[:], 0)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, nil, MediaLive{}, ErrMediaReference
	}
	if !sniffedImageTypes[sniffMedia(head[:nhead])] {
		return nil, nil, MediaLive{}, ErrMediaReference
	}
	keep = true
	return file, before, live, nil
}

func referenceLive(row MediaRow, roots []string, version string) (MediaLive, error) {
	if !row.Recorded || row.Kind != "image" || version == "" || row.Version() != version {
		return MediaLive{}, ErrMediaReference
	}
	live := ResolveMedia(row, roots)
	if live.State != MediaAvailable {
		return MediaLive{}, ErrMediaReference
	}
	return live, nil
}

func referenceIdentity(info os.FileInfo) string {
	ctime, dev, ino := fileIdentity(info)
	return fileVersion(info.Size(), info.ModTime().UnixNano(), ctime, dev, ino)
}

type referenceReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r referenceReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
