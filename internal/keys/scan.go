package keys

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Finding names a file that must not be sealed into a tree and the rule that
// flagged it. It never carries a value.
type Finding struct {
	Path string // slash-separated, relative to the scanned root
	Rule string
}

// RuleDotenv names the finding for an environment file: its variables belong
// in the vault, and the file itself stays out of a tree.
const RuleDotenv = "dotenv-file"

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

// contentRule matches a pattern in a file's head. lead is a literal every
// match contains; a file without it is rejected by a byte search before the
// regular expression runs, which keeps a scan of many clean files cheap.
func contentRule(name, lead, pattern string) rule {
	re := regexp.MustCompile(pattern)
	leadB := []byte(lead)
	return rule{name, func(_ string, content []byte) bool {
		return bytes.Contains(content, leadB) && re.Match(content)
	}}
}

// nameRules judge a path alone; contentRules read the file.
var nameRules = []rule{
	nameRule(RuleDotenv, ".env", ".env.*", "*.env"),
	nameRule("credentials-file", "*credentials*"),
	nameRule("private-key-file", "*.pem", "id_rsa*", "id_ed25519*"),
}

var contentRules = []rule{
	contentRule("api-key-sk", "sk-", `\bsk-[A-Za-z0-9_-]{16,}`),
	contentRule("github-token", "gh", `\bgh[pousr]_[A-Za-z0-9]{30,}`),
	contentRule("slack-token", "xox", `\bxox[bp]-[A-Za-z0-9-]{10,}`),
	contentRule("aws-access-key", "AKIA", `\bAKIA[0-9A-Z]{16}\b`),
	contentRule("private-key-block", "-----BEGIN", `-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

// ScanTree walks root and reports every file matching a secret rule, one
// finding per file (first rule wins). Unreadable files are skipped.
func ScanTree(root string) []Finding { return Scanner{}.Walk(root) }

// Scanner scans with two optional aids. Skip names paths left out (a directory
// it names is not entered); paths are slash-separated and relative to the root.
// Ledger remembers the files found clean, so a file that has not changed since
// is not read again.
type Scanner struct {
	Skip   func(rel string) bool
	Ledger *Ledger
}

// Walk scans every file under root.
func (s Scanner) Walk(root string) []Finding { return s.from(root, ".") }

// Paths scans the named files, and every file under a named directory.
func (s Scanner) Paths(root string, rels []string) []Finding {
	var out []Finding
	for _, rel := range rels {
		out = append(out, s.from(root, rel)...)
	}
	return out
}

func (s Scanner) from(root, start string) []Finding {
	var out []Finding
	filepath.WalkDir(filepath.Join(root, start), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel != "." && s.Skip != nil && s.Skip(rel) {
			return skipEntry(d)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if f, ok := s.file(rel, p, d); ok {
			out = append(out, f)
		}
		return nil
	})
	return out
}

// file judges one file, reading it only when the ledger has not seen it clean.
func (s Scanner) file(rel, abs string, d fs.DirEntry) (Finding, bool) {
	st, known := s.Ledger.known(abs, d)
	if known {
		return Finding{}, false
	}
	rule, hit := firstHit(rel, abs)
	if !hit {
		s.Ledger.note(abs, st)
	}
	return Finding{Path: rel, Rule: rule}, hit
}

func skipEntry(d fs.DirEntry) error {
	if d.IsDir() {
		return fs.SkipDir
	}
	return nil
}

// firstHit judges the path first, and reads the file only when its name is
// unremarkable.
func firstHit(rel, abs string) (string, bool) {
	if !isTemplate(rel) {
		if name, ok := firstRule(nameRules, rel, nil); ok {
			return name, true
		}
	}
	return firstRule(contentRules, rel, readHead(abs))
}

// isTemplate is a committed placeholder (.env.example): its name says nothing
// about secrets, so only its content is judged.
func isTemplate(rel string) bool {
	base := path.Base(rel)
	for _, suffix := range []string{".example", ".sample", ".template"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

func firstRule(list []rule, rel string, content []byte) (string, bool) {
	for _, r := range list {
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
