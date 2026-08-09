// Package cochange is a bug-for-bug port of src/session/co-change.ts.
//
// W5d: Change-coupling graph — mine "files that change together" from a
// caller-supplied `git log --name-only --pretty=format:%H` dump. Pure by
// design: no filesystem/network/git I/O, no clock, no RNG (so there is no
// injectable now()/random() here — the TS module reads neither).
//
// Coupling score = Jaccard over per-file commit sets:
//
//	|A∩B| / |A∪B| = pairCount(a,b) / (total(a) + total(b) - pairCount(a,b))
//
// Fidelity notes (deliberate — do NOT "fix"):
//
//   - Every `<` / `>` on strings in the TS source is a JS relational compare,
//     i.e. UTF-16 CODE-UNIT order, not UTF-8 byte order. Those differ for
//     astral-plane characters (surrogate pairs sort below U+E000..U+FFFF), so
//     pairKey, uniqueSorted and the relatedFiles tie-break all go through
//     compareUTF16 rather than strings.Compare.
//
//   - JS `\s` is a much larger class than RE2 `\s` (it adds U+00A0, U+1680,
//     U+2000-U+200A, U+2028, U+2029, U+202F, U+205F, U+3000, U+FEFF). Every
//     `\s` in the TS regexes is expanded to the literal JS class below, so a
//     block separator of "\n \n" splits on both sides.
//
//   - JS `.` excludes \n, \r, U+2028 and U+2029; Go's `.` only excludes \n.
//     The rename regexes therefore use an explicit jsDot class — a line with
//     an interior lone "\r" must NOT match the rename patterns, exactly as in
//     V8.
//
//   - counts are float64, not int, because CoChangeGraph is an *interface* in
//     TS: a caller can hand-forge a graph whose totals hold NaN/fractions, and
//     coupling() then propagates NaN into relatedFiles' comparator (where
//     ECMA-262 SortCompare maps a NaN comparator result to +0).
//
//   - coupling(g, a, a) returns 1 even when `a` is not in the graph at all.
//     Suspected bug; kept.
//
//   - relatedFiles' `k` is a JS optional parameter: nil means "argument
//     omitted" and takes DefaultRelatedK. A caller-supplied 0/NaN is honoured
//     verbatim (NaN survives `k <= 0` and is then swallowed by slice(0, NaN)).
package cochange

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// MaxCommitFiles mirrors MAX_COMMIT_FILES: commits touching more than this
// many files are skipped as bulk noise.
const MaxCommitFiles = 30 // W5-TODO(knobs)

// DefaultRelatedK mirrors DEFAULT_RELATED_K: default top-k for RelatedFiles
// when the caller omits k.
const DefaultRelatedK = 10 // W5-TODO(knobs)

// CoChangeCommit mirrors the TS interface; JSON keys keep object-literal order.
type CoChangeCommit struct {
	Hash  string   `json:"hash"`
	Files []string `json:"files"`
}

// CoChangeGraph mirrors the TS interface. The TS fields are ReadonlyMap, whose
// iteration order is insertion order — jscompat.OrderedMap reproduces that.
type CoChangeGraph struct {
	// Totals holds per-file commit touch counts (after bulk-commit filtering).
	Totals *jscompat.OrderedMap[string, float64] `json:"totals"`
	// Pairs holds undirected pair co-occurrence counts; key = PairKey(a, b).
	Pairs *jscompat.OrderedMap[string, float64] `json:"pairs"`
}

// RelatedFile mirrors the TS interface. Score is a JSNumber so a NaN score
// serializes to JSON null exactly like JSON.stringify(NaN).
type RelatedFile struct {
	Path  string            `json:"path"`
	Score jscompat.JSNumber `json:"score"`
}

// BuildCoChangeOptions mirrors the TS options bag. A nil MaxCommitFiles is
// both `undefined` and `null` on the TS side — `opts.maxCommitFiles ??
// MAX_COMMIT_FILES` falls back for either.
type BuildCoChangeOptions struct {
	MaxCommitFiles *float64 `json:"maxCommitFiles"`
}

// ---------------------------------------------------------------------------
// regexes

// jsSpace is the JS `\s` class written out; RE2's `\s` is only [\t\n\f\r ].
const jsSpace = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

// jsDot is JS's `.` (no /s flag): any code point except the four JS line
// terminators. Go's `.` would also match \r, U+2028 and U+2029.
const jsDot = `[^\n\r\x{2028}\x{2029}]`

var (
	// /\n\s*\n/ — blank-line block separator.
	blockSplitRe = regexp.MustCompile("\n" + jsSpace + "*\n")
	// /^[0-9a-f]{7,40}$/i — JS non-unicode `i` never folds non-ASCII into
	// a-f, so the explicit A-F range is exact.
	hashRe = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
	// /^(.*)\{([^{}]*)\s*=>\s*([^{}]*)\}(.*)$/
	braceRe = regexp.MustCompile(`^(` + jsDot + `*)\{([^{}]*)` + jsSpace + `*=>` + jsSpace + `*([^{}]*)\}(` + jsDot + `*)$`)
	// /^(.*?)\s*=>\s*(.+)$/
	flatRe = regexp.MustCompile(`^(` + jsDot + `*?)` + jsSpace + `*=>` + jsSpace + `*(` + jsDot + `+)$`)
	// /\/+/g and /^\//
	multiSlashRe   = regexp.MustCompile(`/+`)
	leadingSlashRe = regexp.MustCompile(`^/`)
)

// ---------------------------------------------------------------------------
// ParseGitLog — blank-line-separated commits from
//
//	git log --name-only --pretty=format:%H
//
// Handles merge commits with no files, rename lines, trailing newlines.
func ParseGitLog(raw string) []CoChangeCommit {
	commits := []CoChangeCommit{}
	if raw == "" { // `if (!raw) return []`
		return commits
	}
	// Split on blank lines (one or more). Trailing blank segments are ignored.
	blocks := blockSplitRe.Split(strings.ReplaceAll(raw, "\r\n", "\n"), -1)
	for _, block := range blocks {
		split := strings.Split(block, "\n")
		lines := make([]string, len(split))
		for i, l := range split {
			lines[i] = jsTrimEnd(l)
		}
		// Drop leading/trailing empty lines inside a block (defensive).
		for len(lines) > 0 && jscompat.Trim(lines[0]) == "" {
			lines = lines[1:]
		}
		for len(lines) > 0 && jscompat.Trim(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) == 0 {
			continue
		}
		hash := jscompat.Trim(lines[0])
		if hash == "" || !hashRe.MatchString(hash) {
			continue
		}
		files := []string{}
		seen := make(map[string]struct{})
		for i := 1; i < len(lines); i++ {
			line := jscompat.Trim(lines[i])
			if line == "" {
				continue
			}
			path := normalizeNameOnlyLine(line)
			if path == "" {
				continue
			}
			if _, dup := seen[path]; dup {
				continue
			}
			seen[path] = struct{}{}
			files = append(files, path)
		}
		commits = append(commits, CoChangeCommit{Hash: hash, Files: files})
	}
	return commits
}

// normalizeNameOnlyLine resolves the rename forms `git log --name-only` emits:
//
//	old/path => new/path
//	dir/{old => new}/file.ts
//
// Prefer the destination (right-hand) path — that is the post-commit name.
func normalizeNameOnlyLine(line string) string {
	// Brace rename: prefix/{old => new}/suffix
	if brace := braceRe.FindStringSubmatch(line); brace != nil {
		prefix := brace[1]
		neu := jscompat.Trim(brace[3])
		suffix := brace[4]
		out := multiSlashRe.ReplaceAllString(prefix+neu+suffix, "/")
		out = leadingSlashRe.ReplaceAllString(out, "")
		if out == "" { // `|| line`
			return line
		}
		return out
	}
	// Flat rename: old => new
	if flat := flatRe.FindStringSubmatch(line); flat != nil {
		return jscompat.Trim(flat[2])
	}
	return line
}

// ---------------------------------------------------------------------------
// BuildCoChangeGraph — pair co-occurrence + per-file totals. Bulk commits
// (file count > maxCommitFiles) are skipped entirely.
func BuildCoChangeGraph(commits []CoChangeCommit, opts BuildCoChangeOptions) *CoChangeGraph {
	maxFiles := float64(MaxCommitFiles)
	if opts.MaxCommitFiles != nil {
		maxFiles = *opts.MaxCommitFiles
	}
	totals := jscompat.NewOrderedMap[string, float64]()
	pairs := jscompat.NewOrderedMap[string, float64]()

	for _, commit := range commits {
		// Dedup within a commit (defensive — ParseGitLog already dedups).
		files := uniqueSorted(commit.Files)
		// `files.length > maxFiles` is false when maxFiles is NaN — a NaN
		// override therefore disables bulk filtering rather than dropping
		// everything.
		if len(files) == 0 || float64(len(files)) > maxFiles {
			continue
		}
		for _, f := range files {
			prev, _ := totals.Get(f)
			totals.Set(f, prev+1)
		}
		for i := 0; i < len(files); i++ {
			for j := i + 1; j < len(files); j++ {
				key := PairKey(files[i], files[j])
				prev, _ := pairs.Get(key)
				pairs.Set(key, prev+1)
			}
		}
	}

	return &CoChangeGraph{Totals: totals, Pairs: pairs}
}

// ---------------------------------------------------------------------------
// Coupling — Jaccard over commit sets, in [0,1]. Returns 0 when either file
// is absent or the union is empty.
func Coupling(graph *CoChangeGraph, a, b string) float64 {
	if a == b {
		return 1
	}
	ta, _ := graph.Totals.Get(a) // `?? 0`
	tb, _ := graph.Totals.Get(b)
	if ta == 0 || tb == 0 {
		return 0
	}
	inter, _ := graph.Pairs.Get(PairKey(a, b))
	union := ta + tb - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}

// ---------------------------------------------------------------------------
// RelatedFiles — top-k files coupled to `path`, score desc, path asc ties.
// Pass nil for k to get the TS default parameter (DefaultRelatedK).
func RelatedFiles(graph *CoChangeGraph, path string, k *float64) []RelatedFile {
	kv := float64(DefaultRelatedK)
	if k != nil {
		kv = *k
	}
	if !graph.Totals.Has(path) || kv <= 0 {
		return []RelatedFile{}
	}
	scored := []RelatedFile{}
	for _, other := range graph.Totals.Keys() {
		if other == path {
			continue
		}
		score := Coupling(graph, path, other)
		if score <= 0 { // NaN survives this filter, exactly like TS
			continue
		}
		scored = append(scored, RelatedFile{Path: other, Score: jscompat.JSNumber(score)})
	}
	// JS Array.prototype.sort is stable; a comparator result of NaN is mapped
	// to +0 by ECMA-262 SortCompare, i.e. "treat as equal".
	sort.SliceStable(scored, func(i, j int) bool {
		x, y := scored[i], scored[j]
		xs, ys := float64(x.Score), float64(y.Score)
		if ys != xs { // NaN !== anything, including itself — same in Go
			d := ys - xs
			if math.IsNaN(d) {
				return false
			}
			return d < 0
		}
		return compareUTF16(x.Path, y.Path) < 0
	})
	return sliceTopK(scored, kv)
}

// sliceTopK is `arr.slice(0, k)` for a JS-number k.
//
// jscompat.SliceTo funnels k through int(math.Trunc(k)), and in Go converting
// a float outside the int64 range (±Inf, 1e300, …) is undefined — on amd64 it
// yields math.MinInt64, which SliceTo then reads as a big negative index and
// clamps to 0. ECMA-262 ToIntegerOrInfinity keeps ±∞ and the subsequent
// min(k, len) / max(len+k, 0) clamps it to the ends, so pre-clamp k into
// [-len, len] — a no-op for every in-range value, NaN included (NaN must stay
// NaN so SliceTo maps it to 0, matching slice(0, NaN) → []).
func sliceTopK(s []RelatedFile, k float64) []RelatedFile {
	if !math.IsNaN(k) {
		n := float64(len(s))
		if k > n {
			k = n
		} else if k < -n {
			k = -n
		}
	}
	return jscompat.SliceTo(s, k)
}

// --- internals ---

// PairKey mirrors the exported TS pairKey. `a < b` is a UTF-16 compare.
func PairKey(a, b string) string {
	if compareUTF16(a, b) < 0 {
		return a + "\x00" + b
	}
	return b + "\x00" + a
}

func uniqueSorted(files []string) []string {
	// `typeof f === "string"` is unrepresentable in Go ([]string is already
	// all-strings); only the `f.length > 0` half of the guard survives.
	seen := make(map[string]struct{}, len(files))
	out := make([]string, 0, len(files))
	for _, f := range files {
		if len(f) > 0 {
			if _, dup := seen[f]; !dup {
				seen[f] = struct{}{}
				out = append(out, f)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return compareUTF16(out[i], out[j]) < 0 })
	return out
}

// jsTrimEnd is String.prototype.trimEnd: JS WhiteSpace ∪ LineTerminator,
// which includes U+FEFF — Go's unicode.IsSpace does not.
func jsTrimEnd(s string) string {
	return strings.TrimRightFunc(s, isJSWhitespace)
}

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// compareUTF16 reproduces the JS relational operators on strings: comparison
// is over UTF-16 code units, so U+10000.. (a surrogate pair, 0xD800..0xDFFF)
// sorts BELOW U+E000..U+FFFF — the opposite of UTF-8 byte order.
func compareUTF16(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	if i == n {
		// One is a prefix of the other (or they are equal): shorter sorts
		// first under both orderings.
		switch {
		case len(a) < len(b):
			return -1
		case len(a) > len(b):
			return 1
		default:
			return 0
		}
	}
	// A byte < 0x80 is never a UTF-8 continuation byte, so if both diverging
	// bytes are ASCII, position i is a code-point boundary in both strings and
	// the byte compare is already the code-unit compare.
	if a[i] < 0x80 && b[i] < 0x80 {
		if a[i] < b[i] {
			return -1
		}
		return 1
	}
	au := utf16.Encode([]rune(a))
	bu := utf16.Encode([]rune(b))
	m := len(au)
	if len(bu) < m {
		m = len(bu)
	}
	for k := 0; k < m; k++ {
		if au[k] != bu[k] {
			if au[k] < bu[k] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(au) < len(bu):
		return -1
	case len(au) > len(bu):
		return 1
	default:
		return 0
	}
}
