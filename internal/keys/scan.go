package keys

import (
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
)

// Finding names a file that must not be sealed into a tree and the rule that
// flagged it. It never carries a value.
type Finding struct {
	Path string // slash-separated, relative to the scanned root
	Rule string
}

type rule struct {
	name string
	hit  func(rel string, content []byte) bool
}

func nameRule(name string, globs ...string) rule {
	return rule{name, func(rel string, _ []byte) bool {
		base := path.Base(rel)
		for _, g := range globs {
			if ok, _ := path.Match(g, base); ok {
				return true
			}
		}
		return false
	}}
}

func contentRule(name, pattern string) rule {
	re := regexp.MustCompile(pattern)
	return rule{name, func(_ string, content []byte) bool { return re.Match(content) }}
}

var rules = []rule{
	nameRule("dotenv-file", ".env", ".env.*", "*.env"),
	nameRule("credentials-file", "*credentials*"),
	nameRule("private-key-file", "*.pem", "id_rsa*", "id_ed25519*"),
	contentRule("api-key-sk", `\bsk-[A-Za-z0-9_-]{16,}`),
	contentRule("github-token", `\bgh[pousr]_[A-Za-z0-9]{30,}`),
	contentRule("slack-token", `\bxox[bp]-[A-Za-z0-9-]{10,}`),
	contentRule("aws-access-key", `\bAKIA[0-9A-Z]{16}\b`),
	contentRule("private-key-block", `-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

// ScanTree walks root and reports every file matching a secret rule, one
// finding per file (first rule wins). Unreadable files are skipped.
func ScanTree(root string) []Finding {
	var out []Finding
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if r, ok := firstHit(rel, p); ok {
			out = append(out, Finding{Path: rel, Rule: r})
		}
		return nil
	})
	return out
}

func firstHit(rel, abs string) (string, bool) {
	content := readHead(abs)
	for _, r := range rules {
		if r.hit(rel, content) {
			return r.name, true
		}
	}
	return "", false
}

// readHead returns at most the first MiB of the file; secrets sit near the top.
func readHead(abs string) []byte {
	f, err := os.Open(abs)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf, _ := io.ReadAll(io.LimitReader(f, 1<<20))
	return buf
}
