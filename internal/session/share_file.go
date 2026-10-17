package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const shareFileBytes = int64(64 << 20)
const shareFileChunk = 64 << 10

// sharedFile is an inspection, not a claim that this tool created the file.
// A publisher must persist these exact facts before its first index append.
type sharedFile struct {
	Path  string
	Kind  string
	File  DeliverableFile
	Media MediaFact
}

func inspectSharedFile(ctx context.Context, roots []string, path string, remaining *int64) (sharedFile, error) {
	if ctx.Err() != nil {
		return sharedFile{}, ctx.Err()
	}
	if remaining == nil || *remaining < 0 || len(roots) > 8 || len(path) > shareOpPathMax || !utf8.ValidString(path) || strings.ContainsFunc(path, shareOpForbiddenRune) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return sharedFile{}, ErrShareOpInvalid
	}
	file, err := openSharedSource(roots, path)
	if err != nil {
		return sharedFile{}, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return sharedFile{}, err
	}
	if before.Size() < 0 || before.Size() > shareFileBytes || before.Size() > *remaining {
		return sharedFile{}, ErrShareOpInvalid
	}
	// Failed attempts consume their advertised bytes too; changing files cannot
	// multiply a call's read budget by repeatedly failing final verification.
	*remaining -= before.Size()
	hash := sha256.New()
	head := &shareFileHead{}
	read, err := io.CopyBuffer(io.MultiWriter(hash, head), referenceReader{ctx: ctx, reader: io.LimitReader(file, before.Size())}, make([]byte, shareFileChunk))
	if err != nil {
		return sharedFile{}, err
	}
	if err := verifySharedSource(ctx, roots, path, file, before, read); err != nil {
		return sharedFile{}, err
	}
	mime := http.DetectContentType(head.bytes)
	fact := DeliverableFile{Canonical: path, Bytes: read, ModNS: before.ModTime().UnixNano(), SHA256: hex.EncodeToString(hash.Sum(nil)), MIME: mime}
	fact.CtimeNS, fact.Dev, fact.Ino = fileIdentity(before)
	media := MediaFact{Origin: MediaPublished, Canonical: path, MIME: mime, Bytes: read, ModNS: fact.ModNS, CtimeNS: fact.CtimeNS, Dev: fact.Dev, Ino: fact.Ino, SHA256: fact.SHA256, Change: ShellUnknown}
	if w, h, ok := imageSize(head.bytes); ok {
		media.Width, media.Height = w, h
	}
	media.measureContainer(head.bytes)
	return sharedFile{Path: path, Kind: sharedFileKind(mime), File: fact, Media: media}, nil
}

func openSharedSource(roots []string, path string) (*os.File, error) {
	for _, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return openRegularIn(root, strings.Split(rel, string(filepath.Separator))...)
	}
	return nil, ErrShareOpInvalid
}

func verifySharedSource(ctx context.Context, roots []string, path string, file *os.File, before os.FileInfo, read int64) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	after, err := file.Stat()
	if err != nil {
		return err
	}
	if read != before.Size() || referenceIdentity(after) != referenceIdentity(before) {
		return errors.New("shared file changed during inspection")
	}
	current, err := openSharedSource(roots, path)
	if err != nil {
		return err
	}
	defer current.Close()
	now, err := current.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, now) || referenceIdentity(now) != referenceIdentity(before) {
		return errors.New("shared file was replaced during inspection")
	}
	return nil
}

func sharedFileKind(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case mime == "application/pdf":
		return "document"
	default:
		return "file"
	}
}

type shareFileHead struct{ bytes []byte }

func (h *shareFileHead) Write(p []byte) (int, error) {
	n := len(p)
	room := mediaProbeBytes - len(h.bytes)
	if room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		h.bytes = append(h.bytes, p...)
	}
	return n, nil
}
