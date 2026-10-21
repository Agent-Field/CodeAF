package cellstore

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// The engine keeps a path out of a snapshot when a .furrowpolicy says
// `exclude <path>`. The harness keeps its own in the policy file of the cell's
// directory (the engine reads it beside the workspace's), so nothing of ours
// lands in the person's folder; a cell that seals its own folder uses that
// folder's file. Below one marker line sit the paths withheld because they
// carry secrets, and below another the install folders left out because a
// lockfile that does travel rebuilds them (docs/STAGE-1-CONTRACTS.md section 19).
// Two more blocks set single paths apart: those this machine cannot read (a file
// with no permissions, a folder it may not list) and those whose name this
// machine's file system cannot keep beside another's (written when a chat is
// taken here, never by a seal).
// The harness owns only those four blocks. The first three are rewritten whole
// at every seal, so a path a person has cleaned of its secret, or whose
// permissions now allow a read, goes back into the next snapshot; the held block
// is kept exactly as it was read.
const (
	policyName  = ".furrowpolicy"
	withheldTag = "# codeaf: withheld from every seal because they hold secrets (managed block)"
	rebuiltTag  = "# codeaf: left out because a lockfile rebuilds them (managed block)"
	// unreadableTag and heldTag open the blocks of paths set apart one by one.
	unreadableTag = "# codeaf: left out because they cannot be read here (managed block)"
	heldTag       = "# codeaf: left out because this machine's file system cannot keep both names (managed block)"
	excludeWord   = "exclude "
)

// controlDirs are never scanned or excluded: the engine and the cell own them.
var controlDirs = map[string]bool{".git": true, ".furrow": true, ".cell": true}

// policyFile is a tree's .furrowpolicy split at the marker.
type policyFile struct {
	user     []string // lines above the block, kept verbatim
	withheld []string // paths the harness withholds because they hold secrets, sorted
	rebuilt  []string // install folders the harness leaves out, sorted
	// unreadable is the paths this machine cannot read, sorted; a seal recomputes it.
	unreadable []string
	// held is the paths whose name this machine cannot keep; only a take writes it.
	held    []string
	outside []string // the workspace's own lines, when the file is elsewhere
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
	block := &p.user
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		switch line {
		case withheldTag:
			block = &p.withheld
		case rebuiltTag:
			block = &p.rebuilt
		case unreadableTag:
			block = &p.unreadable
		case heldTag:
			block = &p.held
		default:
			*block = append(*block, managedLine(block == &p.user, line))
		}
	}
	return p
}

// managedLine is a line of a block: a user line is kept verbatim, and a managed
// one is the path its `exclude` names.
func managedLine(user bool, line string) string {
	if user {
		return line
	}
	return strings.TrimPrefix(line, excludeWord)
}

// with is the file with its secrets block replaced by paths.
func (p policyFile) with(paths []string) policyFile {
	p.withheld = sorted(paths)
	return p
}

// leaving is the file with its install-folder block replaced by folders.
func (p policyFile) leaving(folders []string) policyFile {
	p.rebuilt = sorted(folders)
	return p
}

// unreadableAre is the file with its unreadable block replaced by paths.
func (p policyFile) unreadableAre(paths []string) policyFile {
	p.unreadable = sorted(paths)
	return p
}

// holding is the file with paths added to its held block, which keeps what it
// had: a held name stays held until a person clears the block by hand.
func (p policyFile) holding(paths []string) policyFile {
	p.held = sorted(union(p.held, paths))
	return p
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range append(append([]string(nil), a...), b...) {
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

func sorted(paths []string) []string {
	out := append([]string(nil), paths...)
	sort.Strings(out)
	return out
}

func (p policyFile) String() string {
	lines := append([]string(nil), p.user...)
	lines = appendBlock(lines, withheldTag, p.withheld)
	lines = appendBlock(lines, rebuiltTag, p.rebuilt)
	lines = appendBlock(lines, unreadableTag, p.unreadable)
	lines = appendBlock(lines, heldTag, p.held)
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// appendBlock adds a managed block, which is absent when it holds nothing.
func appendBlock(lines []string, tag string, paths []string) []string {
	if len(paths) == 0 {
		return lines
	}
	lines = append(lines, tag)
	for _, path := range paths {
		lines = append(lines, excludeWord+path)
	}
	return lines
}

// isWithheld reports whether rel is one of the harness's withheld paths.
func (p policyFile) isWithheld(rel string) bool { return inRules(p.withheld, rel) }

// isRebuilt reports whether rel is inside an install folder the harness leaves
// out.
func (p policyFile) isRebuilt(rel string) bool { return inRules(p.rebuilt, rel) }

// isUnreadable reports whether rel is one of the paths set apart because this
// machine cannot read it.
func (p policyFile) isUnreadable(rel string) bool { return inRules(p.unreadable, rel) }

// isHeld reports whether rel is a path whose name this machine cannot keep.
func (p policyFile) isHeld(rel string) bool { return inRules(p.held, rel) }

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

// Withheld is the folder a seal of c captures and the paths in it that the
// guard keeps out of every seal because they hold secrets, as the last seal
// left them. A cell never screened has none. The paths are relative to the
// folder and slash-separated.
func (e Engine) Withheld(c cell.Cell) (tree string, paths []string, err error) {
	p, err := readPolicyAt(e.policyDir(c))
	return e.tree(c), p.withheld, err
}

// apart is every path set apart one by one, each with its default reason: what
// the record names so the next machine can read that it was left out.
func (p policyFile) apart() []Apart {
	var out []Apart
	for _, path := range p.unreadable {
		out = append(out, Apart{Path: path, Reason: reasonUnreadable})
	}
	for _, path := range p.held {
		out = append(out, Apart{Path: path, Reason: reasonHeld})
	}
	return out
}
