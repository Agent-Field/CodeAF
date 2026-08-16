// Leftover-file hygiene classifier — port of src/session/hygiene.ts (swe-pro
// 3b25a1a). Classifies changed files into "scratch" (agent scaffolding that
// should not ship) vs "clean" so a consumer can RE-PROMPT for removal.
//
// Pure by design, exactly like the TS: no filesystem/network I/O, no clock
// reads, no RNG. Nothing here needs an injectable clock.
//
// Fidelity notes (deliberate, do not "fix"):
//   - Rule ORDER inside classifyLeftovers is load-bearing: backup → junk
//     basename → junk suffix → junk path segment → scaffold prefix → scaffold
//     pattern → location-scoped stem. The first rule that fires wins, so e.g.
//     "src/temp_x.bak" reports a backup reason, never a scaffolding one.
//   - normPath strips quotes BEFORE trimming, so ` "a.py" ` keeps its quotes
//     (the leading space defeats the `^['"]+` anchor). Preserved verbatim.
//   - backupBase tests the suffix against the LOWERCASED path but slices the
//     ORIGINAL — a mismatch in UTF-16 length between the two (e.g. U+212A
//     KELVIN SIGN lowercases to "k", U+0130 lowercases to two units) is
//     reproduced by doing the length math in UTF-16 code units, like JS.
//   - The two `RegExp` tables are hand-expanded to ASCII-only character
//     classes. JS `i` without the `u` flag canonicalizes via toUpperCase and
//     refuses ASCII↔non-ASCII folds, so /^scratch\./i does NOT match "ſcratch."
//     while Go's `(?i)s` would. `.` is expanded to [^\n\r  ] because
//     JS `.` excludes all four line terminators while RE2's excludes only \n.
//   - Scratch/clean slices start non-nil so JSON.stringify parity holds ([] not
//     null).
package hygiene

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// HygieneFinding mirrors the TS interface of the same name.
type HygieneFinding struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// HygieneVerdict mirrors the TS interface of the same name.
type HygieneVerdict struct {
	Scratch []HygieneFinding `json:"scratch"`
	Clean   []string         `json:"clean"`
}

// ClassifyInput mirrors the TS interface of the same name. Allow is optional in
// TS (`allow?: string[]`); a nil slice here stands in for `undefined`.
type ClassifyInput struct {
	ChangedFiles []string `json:"changedFiles"`
	// glob-lite allowlist: exact path or `prefix/` (also accepts trailing `**`).
	Allow []string `json:"allow"`
}

// ── Rule tables ─────────────────────────────────────────────────────────────
// Each rule is an exported named variable so it can be tuned/extended later.
// (Go has no `as const`; these are package-level vars, treat them as read-only.)

// BackupSuffixes are backup / editor artifacts by extension or suffix. Matched
// against the normalized path tail. A trailing `~` is handled separately (it is
// a suffix, not a dotted extension).
// W7-TODO(knobs)
var BackupSuffixes = []string{
	".bak",
	".orig",
	".rej",
	".tmp",
	".swp",
	".old",
	".save",
}

// ScaffoldBasenamePrefixes are scaffolding name prefixes on the path BASENAME
// (a stem at the start of the filename), anywhere in the tree. These are
// throwaway names by shape.
// W7-TODO(knobs)
var ScaffoldBasenamePrefixes = []string{
	"probe_",
	"scratch_",
	"debug_",
	"temp_",
	"tmp_",
	"__grader",
}

// ScaffoldBasenamePatterns are scaffolding name patterns on the basename (more
// than a bare prefix).
// W7-TODO(knobs)
//
// TS: [/^verify_.*_done_criteria/i]
var ScaffoldBasenamePatterns = []*regexp.Regexp{
	// one-off "did I finish?" graders: verify_<something>_done_criteria(.ext)
	regexp.MustCompile(`^[vV][eE][rR][iI][fF][yY]_` + jsDot + `*_[dD][oO][nN][eE]_[cC][rR][iI][tT][eE][rR][iI][aA]`),
}

// LocationScopedStems are bare stems that are ONLY scratch when they sit under
// a source/test tree. A root-level NOTES.md in a docs change is legitimate —
// the signal is shape + LOCATION, not the stem alone. Matched against the
// basename.
// W7-TODO(knobs)
//
// TS: [/^scratch\./i, /^notes\.md$/i]
var LocationScopedStems = []*regexp.Regexp{
	regexp.MustCompile(`^[sS][cC][rR][aA][tT][cC][hH]\.`),  // scratch.py, scratch.ts, scratch.md, …
	regexp.MustCompile(`^[nN][oO][tT][eE][sS]\.[mM][dD]$`), // notes.md (but NOT a root-level docs NOTES.md — see scoping)
}

// jsDot is the JS non-unicode `.`: any code unit except a LineTerminator.
// RE2's `.` excludes only \n, so the class is spelled out.
const jsDot = `[^\n\r\x{2028}\x{2029}]`

// SourceTreeSegments are directory segments that make a LocationScopedStems
// entry count as scratch.
// W7-TODO(knobs)
var SourceTreeSegments = []string{"src", "test", "tests", "__tests__", "spec"}

// JunkBasenames is well-known junk that never belongs in a diff.
// W7-TODO(knobs)
var JunkBasenames = []string{".ds_store", "thumbs.db"}

// JunkPathSegments is well-known junk that never belongs in a diff.
// W7-TODO(knobs)
var JunkPathSegments = []string{"__pycache__", "node_modules"}

// JunkSuffixes is well-known junk that never belongs in a diff.
// W7-TODO(knobs)
var JunkSuffixes = []string{".pyc"}

// BuiltinAllow is the built-in allowlist that always wins — the harness's own
// durable state.
// W7-TODO(knobs)
var BuiltinAllow = []string{".codeaf/"}

// ── Helpers ─────────────────────────────────────────────────────────────────

// normPath normalizes Windows separators and strips surrounding quotes; no
// case-fold.
//
// TS: p.replace(/\\/g, "/").replace(/^['"]+|['"]+$/g, "").trim()
func normPath(p string) string {
	s := strings.ReplaceAll(p, `\`, "/")
	s = stripEdgeQuotes(s)
	return jscompat.Trim(s)
}

// stripEdgeQuotes reproduces .replace(/^['"]+|['"]+$/g, ""): the maximal run of
// ' / " anchored at the start plus the maximal run anchored at the end. When
// the whole string is quotes the leading (greedy) alternative eats all of it.
func stripEdgeQuotes(s string) string {
	i := 0
	for i < len(s) && (s[i] == '\'' || s[i] == '"') {
		i++
	}
	s = s[i:]
	j := len(s)
	for j > 0 && (s[j-1] == '\'' || s[j-1] == '"') {
		j--
	}
	return s[:j]
}

// stripTrailingStars reproduces .replace(/\*+$/g, "").
func stripTrailingStars(s string) string {
	j := len(s)
	for j > 0 && s[j-1] == '*' {
		j--
	}
	return s[:j]
}

func basename(p string) string {
	i := strings.LastIndex(p, "/")
	if i >= 0 {
		return p[i+1:]
	}
	return p
}

// segmentsLower returns the path segments (directories + file), lowercased for
// membership tests.
func segmentsLower(p string) []string {
	parts := strings.Split(jsLowerCase(p), "/")
	out := []string{}
	for _, s := range parts {
		if s != "" { // .filter(Boolean)
			out = append(out, s)
		}
	}
	return out
}

// matchesAllowEntry is the glob-lite allow match: exact path, or a `prefix/`
// (or `prefix/**`) directory prefix. A bare `prefix` also allows everything
// under `prefix/`.
func matchesAllowEntry(path, entry string) bool {
	p := normPath(path)
	e := stripTrailingStars(normPath(entry)) // drop trailing * / **
	if e == "" {
		return false
	}
	if strings.HasSuffix(e, "/") {
		dir := e[:len(e)-1]
		return p == dir || strings.HasPrefix(p, e)
	}
	return p == e || strings.HasPrefix(p, e+"/")
}

func isAllowed(path string, allow []string) bool {
	p := normPath(path)
	for _, entry := range BuiltinAllow {
		if matchesAllowEntry(p, entry) {
			return true
		}
	}
	// Also honor a nested `.codeaf/` anywhere in the path (worktrees, subdirs).
	for _, seg := range segmentsLower(p) {
		if seg == ".codeaf" {
			return true
		}
	}
	for _, entry := range allow {
		if matchesAllowEntry(p, entry) {
			return true
		}
	}
	return false
}

type backupMatch struct {
	suffix string
	base   string
}

// backupBase matches the trailing backup suffix / `~`; returns the base path it
// shadows, or nil. Length math is in UTF-16 code units because `.length` and
// `.slice` in the TS are.
func backupBase(path string) *backupMatch {
	units := utf16.Encode([]rune(path))
	if strings.HasSuffix(path, "~") && len(units) > 1 {
		return &backupMatch{suffix: "~", base: sliceUnits(units, len(units)-1)}
	}
	lower := jsLowerCase(path)
	for _, suf := range BackupSuffixes {
		if strings.HasSuffix(lower, suf) && len(units) > len(suf) {
			return &backupMatch{suffix: suf, base: sliceUnits(units, len(units)-len(suf))}
		}
	}
	return nil
}

func sliceUnits(units []uint16, end int) string {
	if end < 0 {
		end = 0
	}
	if end > len(units) {
		end = len(units)
	}
	return string(utf16.Decode(units[:end]))
}

// ── Core classifier ─────────────────────────────────────────────────────────

// ClassifyLeftovers classifies each changed file as agent scratch or clean.
// General and repo-agnostic: decisions come from filename shape, location, and
// relationship to the rest of the changed set — never file content.
// Conservative: anything not clearly scratch is reported clean.
func ClassifyLeftovers(input ClassifyInput) HygieneVerdict {
	allow := input.Allow
	if allow == nil {
		allow = []string{} // input.allow ?? []
	}
	// changed = (input.changedFiles ?? []).map(normPath).filter(Boolean)
	changedSet := make(map[string]bool, len(input.ChangedFiles))
	for _, f := range input.ChangedFiles {
		n := normPath(f)
		if n == "" {
			continue
		}
		changedSet[jsLowerCase(n)] = true
	}

	scratch := []HygieneFinding{}
	clean := []string{}
	seen := make(map[string]bool, len(input.ChangedFiles))

	for _, raw := range input.ChangedFiles {
		path := normPath(raw)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true

		// (d) Allowlist always wins.
		if isAllowed(path, allow) {
			clean = append(clean, path)
			continue
		}

		base := basename(path)
		baseLower := jsLowerCase(base)
		segs := segmentsLower(path)
		reason := ""
		hasReason := false

		// (a) Backup / editor artifacts, incl. compound <changed-file>.bak shadow.
		if backup := backupBase(path); backup != nil {
			if changedSet[jsLowerCase(backup.base)] {
				reason = "backup artifact (" + backup.suffix + ") shadowing tracked file " + backup.base
			} else {
				reason = "backup/editor artifact (" + backup.suffix + ")"
			}
			hasReason = true
		}

		// (c) Well-known junk.
		if !hasReason && includes(JunkBasenames, baseLower) {
			reason = "well-known junk file (" + base + ")"
			hasReason = true
		}
		if !hasReason && someHasSuffix(JunkSuffixes, baseLower) {
			reason = "compiled/cache artifact (" + base + ")"
			hasReason = true
		}
		if !hasReason {
			if junkSeg, ok := findIncluded(JunkPathSegments, segs); ok {
				reason = "path under " + junkSeg + "/ (should not be tracked)"
				hasReason = true
			}
		}

		// (b) Scaffolding name prefixes / patterns on the basename.
		if !hasReason {
			if pfx, ok := findPrefixOf(ScaffoldBasenamePrefixes, baseLower); ok {
				reason = "scaffolding name (" + pfx + "* prefix)"
				hasReason = true
			}
		}
		if !hasReason && someMatch(ScaffoldBasenamePatterns, base) {
			reason = "one-off scaffolding name (" + base + ")"
			hasReason = true
		}

		// (b) Location-scoped stems: only scratch under a source/test tree.
		if !hasReason && someMatch(LocationScopedStems, base) {
			underSource := false
			for _, s := range jscompat.SliceTo(segs, -1) {
				if includes(SourceTreeSegments, s) {
					underSource = true
					break
				}
			}
			if underSource {
				reason = "scratch stem (" + base + ") under a source/test tree"
				hasReason = true
			}
		}

		if hasReason {
			scratch = append(scratch, HygieneFinding{File: path, Reason: reason})
		} else {
			clean = append(clean, path)
		}
	}

	return HygieneVerdict{Scratch: scratch, Clean: clean}
}

// HygienePromptBlock builds a concise, numbered removal instruction block for a
// re-prompt. Returns nil (TS: null) when there is nothing to clean up.
func HygienePromptBlock(verdict HygieneVerdict) *string {
	if len(verdict.Scratch) == 0 {
		return nil
	}
	parts := make([]string, 0, len(verdict.Scratch)+4)
	parts = append(parts,
		"The final tree still contains scaffolding you created while working.",
		"Remove exactly these files and make NO other changes:",
	)
	for i, f := range verdict.Scratch {
		parts = append(parts, "  "+jscompat.FormatNumber(float64(i+1))+". "+f.File+" — "+f.Reason)
	}
	parts = append(parts,
		"Do not touch any other file, and do not add new files. If a listed file is",
		"actually required by the solution, keep it and briefly say why.",
	)
	s := strings.Join(parts, "\n")
	return &s
}

// IsTreeClean is the convenience boolean: true when no scratch remains in the
// changed set. A nil `allow` stands in for the TS optional argument being
// omitted (the TS spreads `{ allow }` only when `allow` is truthy — an empty
// array is truthy but behaves identically to omitting it).
func IsTreeClean(changedFiles []string, allow []string) bool {
	in := ClassifyInput{ChangedFiles: changedFiles}
	if allow != nil {
		in.Allow = allow
	}
	return len(ClassifyLeftovers(in).Scratch) == 0
}

// ── small Array.prototype shims ─────────────────────────────────────────────

func includes(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func someHasSuffix(suffixes []string, s string) bool {
	for _, suf := range suffixes {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}

// findIncluded is `list.find((s) => segs.includes(s))`.
func findIncluded(list, segs []string) (string, bool) {
	for _, cand := range list {
		if includes(segs, cand) {
			return cand, true
		}
	}
	return "", false
}

// findPrefixOf is `list.find((p) => s.startsWith(p))`.
func findPrefixOf(list []string, s string) (string, bool) {
	for _, cand := range list {
		if strings.HasPrefix(s, cand) {
			return cand, true
		}
	}
	return "", false
}

func someMatch(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// ── String.prototype.toLowerCase ────────────────────────────────────────────

// jsLowerCase mirrors String.prototype.toLowerCase (locale-independent, FULL
// Unicode lowercase). Go's strings.ToLower implements the SIMPLE lowercase
// mapping; the default (non-language-sensitive) parts of SpecialCasing.txt add
// exactly two rules on top of it, both handled here:
//
//   - U+0130 LATIN CAPITAL LETTER I WITH DOT ABOVE → "i" + U+0307 (two units,
//     which is why backupBase does its length math in UTF-16).
//   - Final_Sigma: U+03A3 → U+03C2 when preceded by a cased letter and not
//     followed by one, skipping case-ignorable characters on both sides.
func jsLowerCase(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range runes {
		switch {
		case r == 0x0130:
			b.WriteRune('i')
			b.WriteRune(0x0307)
		case r == 0x03A3 && isFinalSigma(runes, i):
			b.WriteRune(0x03C2)
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func isFinalSigma(runes []rune, i int) bool {
	j := i - 1
	for j >= 0 && isCaseIgnorable(runes[j]) {
		j--
	}
	if j < 0 || !isCased(runes[j]) {
		return false
	}
	k := i + 1
	for k < len(runes) && isCaseIgnorable(runes[k]) {
		k++
	}
	if k < len(runes) && isCased(runes[k]) {
		return false
	}
	return true
}

// isCased approximates the Unicode `Cased` property (Lowercase ∪ Uppercase ∪ Lt).
func isCased(r rune) bool {
	return unicode.IsLower(r) || unicode.IsUpper(r) ||
		unicode.Is(unicode.Lt, r) ||
		unicode.Is(unicode.Other_Lowercase, r) ||
		unicode.Is(unicode.Other_Uppercase, r)
}

// caseIgnorablePunct is the Word_Break ∈ {MidLetter, MidNumLet, Single_Quote}
// half of the Unicode `Case_Ignorable` property.
var caseIgnorablePunct = map[rune]bool{
	0x0027: true, 0x002E: true, 0x003A: true, 0x00B7: true, 0x0387: true,
	0x055F: true, 0x05F4: true, 0x2018: true, 0x2019: true, 0x2024: true,
	0x2027: true, 0xFE13: true, 0xFE52: true, 0xFE55: true, 0xFF07: true,
	0xFF0E: true, 0xFF1A: true,
}

func isCaseIgnorable(r rune) bool {
	if caseIgnorablePunct[r] {
		return true
	}
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) ||
		unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Lm, r) ||
		unicode.Is(unicode.Sk, r)
}
