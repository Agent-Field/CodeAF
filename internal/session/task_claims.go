package session

import (
	"os"
	"path/filepath"
	"strings"
)

// CRITICAL: prose is context, not an executable assertion. Finding a quoted
// phrase beside "deleted" cannot distinguish removal from "none were deleted",
// or a change to one surface from a reference retained elsewhere. The existing
// checker receives the report and the actual work together and interprets them.
// A keyword match must never approve work or create a repair obligation.
const (
	// These bounds limit supplemental note context, not the worker's report.
	claimLimit     = 24
	claimNoteLimit = 64 << 10
	claimTextLimit = 300
)

// declaredClaim carries a note's statement with its source. It says nothing
// about whether that statement is true or has already been checked.
type declaredClaim struct {
	text   string
	source string
}

// landingNoteClaims reads the clauses a landing DECLARED.
//
// A LANDING NOTE IS A DOCUMENT WITH FRONTMATTER THAT LISTS WHAT STOPPED BEING
// TRUE. That is a convention, not a file format anybody owns, and it is spelled
// generically here on purpose: a repository's change entry is one instance of it,
// and a project that keeps a release note, a handover or a status document in the
// same shape gets the same checking for free. What makes a file a landing note is
// that THIS landing wrote it and that it declares an `invalidates:` list; nothing
// about its name or where it lives is consulted.
func landingNoteClaims(dir string, wrote []string) []declaredClaim {
	var claims []declaredClaim
	for _, path := range wrote {
		if len(claims) >= claimLimit {
			break
		}
		relative := filepath.ToSlash(strings.TrimSpace(path))
		if relative == "" {
			continue
		}
		body, err := readCapped(filepath.Join(dir, filepath.FromSlash(relative)), claimNoteLimit)
		if err != nil {
			continue
		}
		for _, clause := range declaredInvalidations(body) {
			if len(claims) >= claimLimit {
				break
			}
			claims = append(claims, declaredClaim{
				text:   clip(clause, claimTextLimit),
				source: relative,
			})
		}
	}
	return claims
}

// declaredInvalidations pulls the `invalidates:` clauses out of a document's
// frontmatter.
//
// IT IS READ BY HAND AND NOT BY A YAML LIBRARY, which is a size decision and
// also an honesty one. The binary carries no YAML parser and this is not a
// reason to make it carry one; and what is wanted here is not a document model
// but one list of sentences, tolerantly read — a scanner that gives up quietly
// on a shape it does not know is exactly right, because a landing note nobody
// can parse is a landing that simply declares nothing.
//
// The three spellings a writer actually uses are all taken: a quoted one-liner,
// a bare one-liner, and a folded block opened with `>-` or `|`.
func declaredInvalidations(body string) []string {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	var (
		clauses []string
		inList  bool
		folded  []string
	)
	flush := func() {
		if len(folded) == 0 {
			return
		}
		if clause := strings.TrimSpace(strings.Join(folded, " ")); clause != "" {
			clauses = append(clauses, clause)
		}
		folded = nil
	}
	for _, raw := range lines[1:] {
		if strings.TrimSpace(raw) == "---" {
			break
		}
		trimmed := strings.TrimSpace(raw)
		indented := raw != trimmed
		switch {
		case !indented && strings.HasPrefix(trimmed, "invalidates:"):
			flush()
			inList = true
			continue
		case !indented && trimmed != "":
			// Another key at the top level ends the list.
			flush()
			inList = false
			continue
		}
		if !inList {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			flush()
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			if item == ">-" || item == ">" || item == "|" || item == "|-" || item == "" {
				// A folded clause: its text is on the indented lines below.
				continue
			}
			if clause := unquote(item); clause != "" {
				clauses = append(clauses, clause)
			}
			continue
		}
		if trimmed != "" {
			folded = append(folded, trimmed)
		}
	}
	flush()
	return clauses
}

// unquote takes the quoting off a one-line clause without pretending to be a
// parser: a clause a writer wrapped in quotes is the same claim without them.
func unquote(text string) string {
	text = strings.TrimSpace(text)
	for _, pair := range [][2]string{{`"`, `"`}, {"'", "'"}} {
		if len(text) >= 2 && strings.HasPrefix(text, pair[0]) && strings.HasSuffix(text, pair[1]) {
			return strings.TrimSpace(text[1 : len(text)-1])
		}
	}
	return text
}

// readCapped reads at most n bytes of a file.
func readCapped(path string, n int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	buffer := make([]byte, n)
	read, err := file.Read(buffer)
	if read == 0 && err != nil {
		return "", err
	}
	return string(buffer[:read]), nil
}

// claimsBlock gives the existing checker declarations that live in changed
// notes. The worker report already travels whole in the same packet; splitting
// and repeating it here would add tokens without adding evidence.
func claimsBlock(claims []declaredClaim) string {
	if len(claims) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("CLAIMS IN THE CHANGED NOTES — these are statements to check, not evidence. " +
		"Interpret each in its source context and compare it with the actual work. " +
		"Name material contradictions or claims you could not check in your evidence.\n")
	for _, made := range claims {
		out.WriteString("- " + made.text + " (" + made.source + ")\n")
	}
	out.WriteString("\n")
	return out.String()
}

// ── the divider is a landing too ────────────────────────────────────────────

// landingFiles is what a check stands on: what this node's own worker wrote, and
// what the parts it handed out wrote and merged into its tree.
//
// ── WHY THE SECOND HALF EXISTS ──
//
// A node that divides its work is a node whose tree, at the moment it merges
// upward, holds far more than its own worker ever wrote: every part came home
// into it. Before this, the check was handed the parent's OWN list — the files
// one worker touched — so the parts' work was laid over the ground as though it
// had always been there, and the biggest artifact of a divided run was the one
// thing nothing looked at. The parts were each checked, correctly, and then the
// tree they were assembled into went home unexamined.
//
// A DIVIDER IS A PART LIKE ANY OTHER. Its deliverable is the assembled tree, so
// the assembled tree is what its check stands on: staged the same way, restored
// the same way, and named in the checker's packet as what it is.
type landingFiles struct {
	// own is what this node's worker wrote.
	own []string
	// parts is what the parts it handed out wrote and brought home, in the order
	// the parts finished. The two halves are disjoint, and a path they share is
	// filed here — see [landingFilesFor].
	parts []string
}

// all is everything the landing carries — what the check stages, restores and
// checks.
func (f landingFiles) all() []string { return alsoChanged(f.own, f.parts) }

// divided reports whether any part contributed to this landing.
func (f landingFiles) divided() bool { return len(f.parts) > 0 }

// landingFilesFor reads what a node's landing carries, split into the two halves
// the checker's packet needs: the paths this node's own worker wrote, and the
// paths its parts wrote and brought home into the same tree.
//
// ONLY A PART THAT LANDED COUNTS. A part that was refused kept its branch and
// never merged, so its files are not in the parent's tree and claiming them would
// be the check standing on work that is not there.
//
// IT IS READ FROM THE PARTS RATHER THAN INTO THEM, and that is what makes it
// safe to call on a ledger that has ALREADY absorbed them (task_ledger.go). The
// parts are whatever the graph says landed; own is the rest of the list. So a
// re-audit handed the complete family ledger still sees its own half as its own
// and its parts' half as its parts', and `Files it wrote:` stays a true claim
// about this node however many times the list has been folded.
//
// A PATH BOTH WROTE IS FILED UNDER THE PARTS, and that is a decision rather
// than a detail. It used to be filed under the node, which cannot survive being
// asked twice: the second reading has no way to tell a path the node wrote from
// one it absorbed, so the split would drift with every re-audit. Filing it with
// the writer the graph can still name keeps one answer at every age — and it
// costs the packet nothing, because the path is named in the packet either way,
// staged either way, restored either way, and on [all] exactly once either way.
// The only thing that changes is which of the two true sentences carries it.
func landingFilesFor(node *TaskNode, changed []string) landingFiles {
	if node == nil || node.graph == nil {
		return landingFiles{own: changed}
	}
	// ONE SET, ONE PASS. The parts are gathered against the same `seen` the
	// split below reads, so a family of five parts costs one map and one walk of
	// each list rather than a fresh map per part and a linear scan of the node's
	// own list per path.
	seen := make(map[string]bool, len(changed))
	files := landingFiles{}
	for _, child := range node.graph.children(node.id) {
		if child.stateNow() != TaskDone {
			continue
		}
		_, wrote, _, _ := child.leavings()
		for _, path := range wrote {
			if seen[path] {
				continue
			}
			seen[path] = true
			files.parts = append(files.parts, path)
		}
	}
	if len(files.parts) == 0 {
		files.own = changed
		return files
	}
	for _, path := range changed {
		if seen[path] {
			continue
		}
		seen[path] = true
		files.own = append(files.own, path)
	}
	return files
}

// containsPath reports whether a list already names a path.
func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
