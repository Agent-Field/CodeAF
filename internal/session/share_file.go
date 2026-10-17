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

// shareKindFile is the kind of a shared file that is neither media nor named
// Markdown or Mermaid text: a PDF, an archive, a spreadsheet, anything. Its type
// is its sniffed MIME, and it is never a media source.
const shareKindFile = "file"
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
	read, err := readSharedSource(ctx, roots, path, remaining, nil)
	if err != nil {
		return sharedFile{}, err
	}
	return sharedFileFacts(path, read.info, read.bytes, read.sum, read.head), nil
}

// sharedRead is one complete, verified pass over a source file.
type sharedRead struct {
	info  os.FileInfo
	bytes int64
	sum   string
	head  []byte
}

// readSharedSource streams path once through 64 KiB buffers into its hash, the
// bounded header and, when copy is not nil, an owned copy — so a copy holds
// exactly the bytes the hash describes. The file is checked again afterwards
// by descriptor and by path; a change during the read is a refusal.
func readSharedSource(ctx context.Context, roots []string, path string, remaining *int64, copy io.Writer) (sharedRead, error) {
	if err := validSharedInspection(ctx, roots, path, remaining); err != nil {
		return sharedRead{}, err
	}
	file, err := openSharedSource(roots, path)
	if err != nil {
		return sharedRead{}, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return sharedRead{}, err
	}
	if before.Size() < 0 || before.Size() > shareFileBytes || before.Size() > *remaining {
		return sharedRead{}, ErrShareOpInvalid
	}
	// Failed attempts consume their advertised bytes too; changing files cannot
	// multiply a call's read budget by repeatedly failing final verification.
	*remaining -= before.Size()
	hash := sha256.New()
	head := &shareFileHead{}
	sinks := []io.Writer{hash, head}
	if copy != nil {
		sinks = append(sinks, copy)
	}
	read, err := io.CopyBuffer(io.MultiWriter(sinks...), referenceReader{ctx: ctx, reader: io.LimitReader(file, before.Size())}, make([]byte, shareFileChunk))
	if err != nil {
		return sharedRead{}, err
	}
	if err := verifySharedSource(ctx, roots, path, file, before, read); err != nil {
		return sharedRead{}, err
	}
	return sharedRead{info: before, bytes: read, sum: hex.EncodeToString(hash.Sum(nil)), head: head.bytes}, nil
}

// copySharedFile takes a stable engine-owned copy of path into dir, which must
// not exist yet, in the same single pass that hashes it. It answers the copy's
// facts and the original's separately; the original is only ever read.
//
// THE COPY IS LINKED INTO PLACE, never renamed over anything: a name already
// there is a refusal, so a copy can never replace a file somebody else owns.
func copySharedFile(ctx context.Context, roots []string, path, dir string, remaining *int64) (copied, source sharedFile, err error) {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return sharedFile{}, sharedFile{}, err
	}
	tmp, err := os.CreateTemp(dir, ".share-*")
	if err != nil {
		return sharedFile{}, sharedFile{}, err
	}
	defer os.Remove(tmp.Name())
	read, err := readSharedSource(ctx, roots, path, remaining, tmp)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err := errors.Join(err, syncErr, closeErr); err != nil {
		return sharedFile{}, sharedFile{}, err
	}
	final := filepath.Join(dir, filepath.Base(path))
	if err := os.Link(tmp.Name(), final); err != nil {
		return sharedFile{}, sharedFile{}, err
	}
	info, err := ownedCopyInfo(dir, final, read.bytes)
	if err != nil {
		return sharedFile{}, sharedFile{}, err
	}
	return sharedFileFacts(final, info, read.bytes, read.sum, read.head), sharedFileFacts(path, read.info, read.bytes, read.sum, read.head), nil
}

// ownedCopyInfo makes the copy's name durable and answers the copy's identity
// as its own descriptor reports it.
func ownedCopyInfo(dir, final string, bytes int64) (os.FileInfo, error) {
	if err := syncShareOpDir(dir); err != nil {
		return nil, err
	}
	file, err := openRegularNoFollow(final)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() != bytes {
		return nil, errors.New("the owned copy is not the size that was read")
	}
	return info, nil
}

// validSharedInspection is every refusal made before a file is opened: a live
// context, an honest budget, at most eight roots, and one exact absolute path.
func validSharedInspection(ctx context.Context, roots []string, path string, remaining *int64) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if remaining == nil || *remaining < 0 || len(roots) > 8 || len(path) > shareOpPathMax {
		return ErrShareOpInvalid
	}
	if !utf8.ValidString(path) || strings.ContainsFunc(path, shareOpForbiddenRune) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrShareOpInvalid
	}
	return nil
}

// sharedFileFacts is what the bytes just read say, and nothing more: the
// header decides the type, an unrecognised header stays a generic file.
func sharedFileFacts(path string, before os.FileInfo, read int64, sum string, head []byte) sharedFile {
	// ONE type for the file and its media fact: the media sniffer's where it
	// recognised the header (it knows containers the generic sniffer does not),
	// else the generic sniffer's, so the kind and both records always agree.
	mime := shareSniff(head)
	fact := DeliverableFile{Canonical: path, Bytes: read, ModNS: before.ModTime().UnixNano(), SHA256: sum, MIME: mime}
	fact.CtimeNS, fact.Dev, fact.Ino = fileIdentity(before)
	media := MediaFact{Origin: MediaShared, Canonical: path, MIME: mime, Bytes: read, ModNS: fact.ModNS, CtimeNS: fact.CtimeNS, Dev: fact.Dev, Ino: fact.Ino, SHA256: fact.SHA256}
	if w, h, ok := imageSize(head); ok {
		media.Width, media.Height = w, h
	}
	media.measureContainer(head)
	return sharedFile{Path: path, Kind: sharedFileKind(path, mime), File: fact, Media: media}
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

// shareSniff is the type the head of a shared file says. Media is the media
// sniffer's; an ISO container the generic sniffer does not know (the "isom"
// brand real renders carry) is video when its header has a frame size and audio
// when it has only sound, read by the same box reader media rows use; anything
// else is the generic sniffer's word, octet-stream included.
func shareSniff(head []byte) string {
	if mime := sniffMedia(head); mime != "" {
		return mime
	}
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		if _, _, ok := mp4FrameSize(head); ok {
			return "video/mp4"
		}
		if _, sound, ok := mp4Facts(head); ok && sound {
			return "audio/mp4"
		}
	}
	return http.DetectContentType(head)
}

// sharedFileKind is the kind the HEADER says, through the type [shareSniff]
// read from it: media is media; text that is named as Markdown or Mermaid is
// that; everything else is a generic file whose type stays its MIME.
func sharedFileKind(path, mime string) string {
	if kind := mediaKindOf(mime); kind != "" {
		return kind
	}
	switch kind := deliverableKindExt(path); {
	case (kind == DeliverableMarkdown || kind == DeliverableMermaid) && strings.HasPrefix(mime, "text/plain"):
		return kind
	}
	return shareKindFile
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
