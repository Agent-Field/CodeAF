package factory

import (
	"strconv"
	"strings"
)

// PatchHunk is one hunk of a unified diff: the file it changes, the
// function or section git named on its header line, where it starts in the
// new file, how many lines it adds and removes, and its text from its "@@"
// line on. Generated says the file is one nobody edits by hand.
type PatchHunk struct {
	File      string `json:"file"`
	Symbol    string `json:"symbol,omitempty"`
	Start     int    `json:"start"`
	Added     int    `json:"added"`
	Removed   int    `json:"removed"`
	Generated bool   `json:"generated,omitempty"`
	Text      string `json:"-"`
}

// Lines is how many lines the hunk changes.
func (h PatchHunk) Lines() int { return h.Added + h.Removed }

// PatchHunks splits a unified diff (git diff's text, as [Change.Patch] keeps
// it) into its hunks, in order. Lines before a file's first hunk (its
// "diff --git", index and "---"/"+++" lines) name the file; nothing else is
// read from them.
func PatchHunks(patch string) []PatchHunk {
	var out []PatchHunk
	file := ""
	var cur *PatchHunk
	var body strings.Builder
	flush := func() {
		if cur != nil {
			cur.Text = body.String()
			out = append(out, *cur)
			cur = nil
		}
		body.Reset()
	}
	for _, line := range strings.SplitAfter(patch, "\n") {
		bare := strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(bare, "diff --git "):
			flush()
			file = diffGitFile(bare)
		case cur == nil && strings.HasPrefix(bare, "+++ "):
			if name := strings.TrimPrefix(strings.TrimPrefix(bare, "+++ "), "b/"); name != "/dev/null" {
				file = name
			}
		case strings.HasPrefix(bare, "@@ "):
			flush()
			start, symbol := hunkHeader(bare)
			cur = &PatchHunk{File: file, Symbol: symbol, Start: start, Generated: GeneratedPath(file)}
			body.WriteString(line)
		case cur != nil:
			body.WriteString(line)
			countHunkLine(cur, bare)
		}
	}
	flush()
	return out
}

func countHunkLine(h *PatchHunk, line string) {
	switch {
	case strings.HasPrefix(line, "+"):
		h.Added++
	case strings.HasPrefix(line, "-"):
		h.Removed++
	}
}

// diffGitFile is the new path of a "diff --git a/x b/x" line.
func diffGitFile(line string) string {
	if i := strings.LastIndex(line, " b/"); i >= 0 {
		return line[i+3:]
	}
	return ""
}

// hunkHeader reads "@@ -a,b +c,d @@ symbol" as c and symbol.
func hunkHeader(line string) (start int, symbol string) {
	rest := strings.TrimPrefix(line, "@@ ")
	end := strings.Index(rest, " @@")
	if end < 0 {
		return 0, ""
	}
	for _, part := range strings.Fields(rest[:end]) {
		if strings.HasPrefix(part, "+") {
			n, _, _ := strings.Cut(part[1:], ",")
			start, _ = strconv.Atoi(n)
		}
	}
	return start, strings.TrimSpace(rest[end+3:])
}

// generatedMarks are the path parts that mark a file nobody edits by hand:
// lockfiles, vendored trees, generated sources.
var generatedMarks = []string{
	"vendor/", "node_modules/", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "go.sum", "Cargo.lock",
	".pb.go", "_generated.", ".gen.", "generated/", ".min.js",
}

// GeneratedPath says whether a path is a lockfile, vendored or generated.
func GeneratedPath(path string) bool {
	for _, m := range generatedMarks {
		if strings.Contains(path, m) {
			return true
		}
	}
	return false
}
