package main

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/home"
)

// A bucket remains below NAME_MAX, with room for platform-specific suffixes.
const copyBucketBytes = 240

// copyBucketName preserves an arriving copy's bucket instead of encoding its
// receiver's home into the next name. Ordinary short project names stay intact.
func copyBucketName(origin string) string {
	name := "copy-" + encodeWorkspace(origin)
	for at := filepath.Clean(origin); at != filepath.Dir(at); at = filepath.Dir(at) {
		parent := filepath.Dir(at)
		if strings.HasPrefix(filepath.Base(at), "copy-") && filepath.Base(parent) == "projects" && filepath.Base(filepath.Dir(parent)) == "v3" {
			name = filepath.Base(at)
			break
		}
	}
	if len(name) <= copyBucketBytes {
		return name
	}
	suffix := fmt.Sprintf("-%x", sha256.Sum256([]byte(name)))[:17]
	prefix := name[:copyBucketBytes-len(suffix)]
	for !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + suffix
}

func takeCopyBucket(origin string) string { return takeCopyBucketAt(home.Dir(), origin) }
func takeCopyBucketAt(dir, origin string) string {
	return filepath.Join(dir, "v3", "projects", copyBucketName(origin))
}
