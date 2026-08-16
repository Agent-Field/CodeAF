package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// DigestBytes names content without storing it, for callers that need the same
// identity the store uses — a staged copy's suffix, a duplicate check.
func DigestBytes(data []byte) Ref {
	sum := sha256.Sum256(data)
	return Ref(hex.EncodeToString(sum[:]))
}

// Scheme prefixes a durable attachment reference.
const Scheme = "cas://"

// A reference is written as cas://<digest>/<the path the user attached>, and
// the shape is deliberate. The digest is the durable half: the bytes live in
// the store under it, so a job retried next week reads exactly what was
// attached even if the person has since moved, renamed, or deleted their copy.
// The path is the honest half: it is what the attachment is called, where it
// came from, and what every reader — a chip in the composer, the compiler's
// list of attached documents, filepath.Ext — needs to see. Keeping the path at
// the tail means those readers keep working on a reference unchanged, and a
// reference read by something that knows nothing about this format still says
// out loud which file it means.
func Reference(ref Ref, originalPath string) string {
	digest, err := normalizeRef(ref)
	if err != nil {
		return originalPath
	}
	cleaned := strings.TrimSpace(originalPath)
	if cleaned == "" {
		return Scheme + digest
	}
	return Scheme + digest + "/" + strings.TrimPrefix(filepath.ToSlash(cleaned), "/")
}

// ParseReference splits a durable reference into its digest and the path it
// was made from. Anything that is not a reference comes back unchanged as a
// path, so a caller can hold both kinds in one list.
func ParseReference(reference string) (ref Ref, originalPath string, ok bool) {
	if !strings.HasPrefix(reference, Scheme) {
		return "", reference, false
	}
	rest := strings.TrimPrefix(reference, Scheme)
	digest, path, found := strings.Cut(rest, "/")
	if _, err := normalizeRef(Ref(digest)); err != nil {
		return "", reference, false
	}
	if !found || strings.TrimSpace(path) == "" {
		return Ref(digest), "", true
	}
	return Ref(digest), filepath.FromSlash("/" + path), true
}

// SourcePath is what a reader that only understands paths should look at: the
// file the person attached. It is unchanged for anything that is already a
// path.
func SourcePath(reference string) string {
	_, path, ok := ParseReference(reference)
	if !ok || path == "" {
		return reference
	}
	return path
}
