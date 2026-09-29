package cellstore

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The engine keeps a path out of a snapshot when a .furrowpolicy says
// `exclude <path>`. The harness keeps its own in the policy file of the cell's
// directory (the engine reads it beside the workspace's), so nothing of ours
// lands in the person's folder; a cell that seals its own folder uses that
// folder's file. Below one marker line sit the paths withheld because they
// carry secrets. The harness owns only that block: it is rewritten whole at
// every seal, so a path a person has cleaned of its secret goes back into the
// next snapshot.
const (
	policyName  = ".furrowpolicy"
	withheldTag = "# codeaf: withheld from every seal because they hold secrets (managed block)"
	excludeWord = "exclude "
)

// controlDirs are never scanned or excluded: the engine and the cell own them.
var controlDirs = map[string]bool{".git": true, ".furrow": true, ".cell": true}

// policyFile is a tree's .furrowpolicy split at the marker.
type policyFile struct {
	user     []string // lines above the block, kept verbatim
	withheld []string // paths the harness withholds, sorted
	outside  []string // the workspace's own lines, when the file is elsewhere
}

// readPolicy reads the harness's policy file in dir and, when dir is not the
// tree, the person's own file in the tree.
func readPolicy(tree, dir string) (policyFile, error) {
	p, err := readPolicyAt(dir)
	if err != nil || dir == tree {
		return p, err
	}
	theirs, err := readPolicyAt(tree)
	p.outside = theirs.user
	return p, err
}

func readPolicyAt(dir string) (policyFile, error) {
	raw, err := os.ReadFile(filepath.Join(dir, policyName))
	if errors.Is(err, os.ErrNotExist) {
		return policyFile{}, nil
	}
	if err != nil {
		return policyFile{}, err
	}
	return parsePolicy(string(raw)), nil
}

func parsePolicy(text string) policyFile {
	var p policyFile
	managed := false
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		switch {
		case line == withheldTag:
			managed = true
		case managed:
			p.withheld = append(p.withheld, strings.TrimPrefix(line, excludeWord))
		default:
			p.user = append(p.user, line)
		}
	}
	return p
}

// with is the file with its managed block replaced by paths.
func (p policyFile) with(paths []string) policyFile {
	p.withheld = append([]string(nil), paths...)
	sort.Strings(p.withheld)
	return p
}

func (p policyFile) String() string {
	lines := append([]string(nil), p.user...)
	if len(p.withheld) > 0 {
		lines = append(lines, withheldTag)
		for _, path := range p.withheld {
			lines = append(lines, excludeWord+path)
		}
	}
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// isWithheld reports whether rel is one of the harness's withheld paths.
func (p policyFile) isWithheld(rel string) bool { return inRules(p.withheld, rel) }

// isLeftOut reports whether rel is a control directory or a path the person
// excluded: nothing there is sealed, so nothing there is scanned.
func (p policyFile) isLeftOut(rel string) bool {
	first, _, _ := strings.Cut(rel, "/")
	return controlDirs[first] || inRules(p.userRules(), rel)
}

func (p policyFile) userRules() []string {
	var rules []string
	for _, line := range append(append([]string(nil), p.user...), p.outside...) {
		if rule, ok := strings.CutPrefix(strings.TrimSpace(line), excludeWord); ok {
			rules = append(rules, strings.TrimSuffix(strings.TrimSpace(rule), "/"))
		}
	}
	return rules
}

// inRules is the engine's own test: a rule names a path or a subtree.
func inRules(rules []string, rel string) bool {
	for _, rule := range rules {
		if rel == rule || strings.HasPrefix(rel, rule+"/") {
			return true
		}
	}
	return false
}

// write puts the file in dir, only when it differs.
func (p policyFile) write(dir string) error {
	path := filepath.Join(dir, policyName)
	want := p.String()
	have, err := os.ReadFile(path)
	if string(have) == want && (err == nil || want == "") {
		return nil
	}
	if want == "" {
		return ignoreMissing(os.Remove(path))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(want), 0o644)
}

func ignoreMissing(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
