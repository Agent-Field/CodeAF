package head

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The board gives every job exactly one line, and that line is the same line
// whatever the user asked. It is the right answer to "what's going on" and the
// wrong answer to "what happened with the finance thing" — the findings live in
// the full summary and in the files the work wrote, and the head never saw
// either, so it could only say "it was completed". Status instead of substance.
//
// The fix is not a bigger board. Breadth and depth are different budgets: the
// board is the floor and must never starve, so it keeps its own bytes and its
// one line per job, and depth is bought separately, for the few jobs this
// particular message is about. Which jobs those are is a relevance question,
// answered by the same in-memory BM25 the redirect path already ranks with, at
// the same floor — never by a phrase list, because the next phrasing is always
// one nobody wrote down.

const (
	// deepSliceLimit is how many jobs one message may open in full. Three is
	// about as many as a person names in a sentence; past that the message is a
	// survey, and one line each is the honest answer to a survey.
	deepSliceLimit = 3
	// deepResultBytes is one job's share of the depth. A finding longer than
	// this is a document, and the file path beside it is how a document is read.
	deepResultBytes = 1200
	// maxDeepContextBytes is depth's own budget, deliberately separate from
	// maxGraphContextBytes. Depth is allowed to be expensive; it is never
	// allowed to evict the board, because a board that drops the running job is
	// how the head once denied a subtree it was rendering at that moment.
	maxDeepContextBytes = 6 << 10
	// deepDedupProbeBytes is how much of a result must already be in the
	// rendered thread before the slice is redundant. A whole first line is the
	// unit the thread shows, and re-sending it teaches the model nothing.
	deepDedupProbeBytes = 120
	// deepDedupFloorBytes keeps the dedup probe from firing on a result whose
	// first line is "done" — a short line matches half the thread by accident.
	deepDedupFloorBytes = 24
	// deepFileCap bounds the paths named per job. A job that wrote more files
	// than this wrote a directory, and the first few say where it is.
	deepFileCap = 6
)

// renderDeep is the relevance-directed half of the graph context: for the few
// jobs this message is actually about, what they found rather than how they
// ended. It returns the empty string for every message that is about nothing on
// the graph — a greeting, a new request — so those prompts stay byte-for-byte
// what they are today.
//
// thread is the already-rendered recent thread. A result the model can read
// there is not worth spending depth on twice.
func (h *Head) renderDeep(message, thread string) string {
	if h == nil || h.store == nil {
		return ""
	}
	// The reference is what the message is about with its steering vocabulary
	// stripped, which is exactly what redirection needs to know too. Matching on
	// the raw sentence would score "what", "happened" and "the" against briefs
	// that merely share English with it.
	reference := redirectReference(message)
	if reference == "" {
		return ""
	}
	// No status filter: what a job found is most interesting once it is over,
	// and the board already covers what is still moving.
	targets, err := h.store.SearchSurgeryTargets(reference, false)
	if err != nil || len(targets) == 0 {
		return ""
	}
	now := time.Now()
	var rendered strings.Builder
	// The header is written first and counted, so maxDeepContextBytes bounds
	// everything this function can add to the prompt rather than most of it.
	rendered.WriteString(deepContextHeader)
	opened := 0
	for _, target := range targets {
		if opened == deepSliceLimit {
			break
		}
		// RedirectAnchorScore is the floor at which the user's words are read as
		// being about a job at all. Below it the overlap is a coincidence of
		// vocabulary, and depth bought with a coincidence is the pollution this
		// is meant to avoid.
		if target.Score < RedirectAnchorScore || !beltAddressable(target.Node) {
			continue
		}
		result := nodeResult(target.Node)
		if result == "" || deepAlreadyInThread(thread, result) {
			continue
		}
		block := h.renderDeepSlice(target.Node, result, now)
		if rendered.Len()+len(block) > maxDeepContextBytes {
			break
		}
		rendered.WriteString(block)
		opened++
	}
	if opened == 0 {
		return ""
	}
	return strings.TrimSpace(rendered.String())
}

// deepContextHeader says what the block is for in the router's own register.
// Without it the model reads a second board and answers from the shorter lines
// out of habit.
const deepContextHeader = "What these jobs actually found (the board above gives one line each; " +
	"this is the substance, and it is what a question about findings is answered from):\n"

// renderDeepSlice is one job opened up: the board's own identifying line, then
// the finding itself, then where it was written down.
func (h *Head) renderDeepSlice(node store.Node, result string, now time.Time) string {
	var block strings.Builder
	fmt.Fprintf(&block, "- %s | %s | %s", node.ID, node.Status, surgeryTargetLabel(node))
	if impact, err := h.store.Impact(node.ID, now); err == nil && impact.Cost > 0 {
		fmt.Fprintf(&block, " | $%.2f", impact.Cost)
	}
	if age := store.AgeLabel(node.FinishedAt, now); age != "" {
		block.WriteString(" | finished " + age)
	}
	body := truncateBytes(result, deepResultBytes)
	block.WriteString("\n  result: " + indentBlock(body) + "\n")
	if files := unnamedFiles(node, body); len(files) > 0 {
		block.WriteString("  files: " + strings.Join(files, ", ") + "\n")
	}
	return block.String()
}

// unnamedFiles is the files line's whole reason to exist: a path the rendered
// result already shows is not worth a second mention, but a path the truncation
// cut off or the prose buried is the only way back to the work itself.
func unnamedFiles(node store.Node, body string) []string {
	files := make([]string, 0, deepFileCap)
	for _, file := range resultFiles(node) {
		if !strings.Contains(body, file) {
			files = append(files, file)
		}
	}
	return files
}

// nodeResult is what a node has to say for itself, in the order the head should
// prefer it: the summary it settled with, the digest its fold kept, and failing
// both the error that ended it — a failure is still a finding.
func nodeResult(node store.Node) string {
	for _, candidate := range []string{node.Summary, node.FoldDigest, node.Error} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// deepAlreadyInThread is the pollution guard. The thread is in the same prompt,
// so a result the user can already read there buys nothing and costs the budget
// a different job could have used.
func deepAlreadyInThread(thread, result string) bool {
	probe := truncateBytes(firstLine(result), deepDedupProbeBytes)
	probe = strings.TrimSuffix(probe, "…")
	if len(probe) < deepDedupFloorBytes {
		return false
	}
	return strings.Contains(thread, probe)
}

// resultFiles names the artifacts a job points at. A folded job carries them
// durably; everything else has them only where the worker wrote them down,
// which is its own summary — inline or on a line of its own.
func resultFiles(node store.Node) []string {
	files := make([]string, 0, deepFileCap)
	seen := make(map[string]bool, deepFileCap)
	add := func(path string) {
		path = strings.Trim(strings.TrimSpace(path), `"'(),;:.`)
		if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "~/") &&
			!strings.HasPrefix(path, "cas://") {
			return
		}
		if len(path) < 2 || seen[path] || len(files) == deepFileCap {
			return
		}
		seen[path] = true
		files = append(files, path)
	}
	for _, pointer := range node.FoldPointers {
		add(pointer)
	}
	for _, field := range strings.Fields(nodeResult(node)) {
		add(field)
	}
	return files
}

// indentBlock keeps a multi-line finding readable inside a bulleted context
// block without altering a word of it.
func indentBlock(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "\n", "\n    ")
}
