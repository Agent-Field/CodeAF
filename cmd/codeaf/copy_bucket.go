package main

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A bucket remains below NAME_MAX, with room for platform-specific suffixes.
const copyBucketBytes = 240

// copyBucketName preserves an arriving copy's bucket instead of encoding its
// receiver's home into the next name. Ordinary short project names stay intact.
func copyBucketName(origin string) string {
	identity := filepath.Clean(origin)
	if canonical, err := filepath.EvalSymlinks(identity); err == nil {
		identity = canonical
	}
	name := "copy-" + encodeWorkspace(origin)
	if bucket := ownedCopyBucket(identity); bucket != "" {
		name = bucket
	}
	if len(name) <= copyBucketBytes {
		return name
	}
	suffix := fmt.Sprintf("-%x", sha256.Sum256([]byte(identity)))[:17]
	prefix := name[:copyBucketBytes-len(suffix)]
	for !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + suffix
}

// Only the exact workspace of an existing owned copy can name its bucket.
// A nested project, or a path that merely looks like a copy, keeps its identity.
func ownedCopyBucket(work string) string {
	if filepath.Base(work) != "work" {
		return ""
	}
	root := filepath.Dir(work)
	bucket := filepath.Dir(root)
	projects := filepath.Dir(bucket)
	if !strings.HasPrefix(filepath.Base(bucket), "copy-") || filepath.Base(projects) != "projects" || filepath.Base(filepath.Dir(projects)) != "v3" {
		return ""
	}
	meta, err := session.LoadMeta(root)
	if err != nil || !meta.Owned || meta.Origin == "" || meta.ID != filepath.Base(root) {
		return ""
	}
	canonical := filepath.Clean(meta.Workspace)
	if resolved, err := filepath.EvalSymlinks(canonical); err == nil {
		canonical = resolved
	}
	if canonical != work {
		return ""
	}
	return filepath.Base(bucket)
}

func takeCopyBucket(origin string) string { return takeCopyBucketAt(home.Dir(), origin) }
func takeCopyBucketAt(dir, origin string) string {
	return filepath.Join(dir, "v3", "projects", copyBucketName(origin))
}
