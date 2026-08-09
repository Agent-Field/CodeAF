// Package tampercheck ports src/session/tamper-check.ts:1-208 from swe-pro
// commit 3b25a1a. It classifies unrequested changes to verification
// infrastructure without reading the filesystem or invoking tools.
//
// Fidelity notes:
//   - path normalization strips edge quotes before JavaScript trim;
//   - changed-file deduplication is exact and insertion ordered;
//   - multi-purpose manifests require a supplied hunk whose changed lines
//     touch a verification signature;
//   - the first matching role wins.
package tampercheck

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// TamperFinding is one unrequested verification-config change.
type TamperFinding struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// TamperVerdict is the ordered finding set and convenience clean flag.
type TamperVerdict struct {
	Findings []TamperFinding `json:"findings"`
	Clean    bool            `json:"clean"`
}

// TamperInput is the changed-file set, task text, and optional per-file hunks.
type TamperInput struct {
	ChangedFiles []string          `json:"changedFiles"`
	TaskText     string            `json:"taskText"`
	Hunks        map[string]string `json:"hunks"`
}

// VerificationConfigPattern is one file-role classifier. HunkGuard is non-nil
// for a multi-purpose manifest.
type VerificationConfigPattern struct {
	Label     string
	Match     func(path, base string, segs []string) bool
	HunkGuard *regexp.Regexp
}

// PyVerificationHunkSection mirrors PY_VERIFICATION_HUNK_SECTION.
var PyVerificationHunkSection = regexp.MustCompile(
	`\btool[.:](pytest|coverage|nox)\b|\[coverage[:.\] ]|\[tool:pytest\]|\[pytest\]|\baddopts\b|\btestpaths\b|\bfail_under\b|cov[_-]?fail[_-]?under|--cov\b|\bnox\b`,
)

// PkgVerificationHunkSection mirrors PKG_VERIFICATION_HUNK_SECTION.
var PkgVerificationHunkSection = regexp.MustCompile(
	`"(jest|nyc)"\s*:|"(pretest|test|posttest|lint|coverage|cov|check|ci)[\w:-]*"\s*:|cov[_-]?fail[_-]?under|--coverage\b`,
)

var (
	jestConfigRE   = regexp.MustCompile(`^jest\.config\.[cm]?[jt]sx?$|^jest\.config\.json$`)
	vitestConfigRE = regexp.MustCompile(`^vitest\.config\.[cm]?[jt]sx?$`)
	karmaConfigRE  = regexp.MustCompile(`^karma\.conf\.[cm]?[jt]s$`)
	mochaConfigRE  = regexp.MustCompile(`^\.mocharc\.(json|ya?ml|js|cjs)$`)
)

func hasSegment(segs []string, target string) bool {
	for _, seg := range segs {
		if seg == target {
			return true
		}
	}
	return false
}

// VerificationConfigPatterns is the ordered TS pattern table.
var VerificationConfigPatterns = []VerificationConfigPattern{
	{Label: "tox test/coverage runner config", Match: func(_, base string, _ []string) bool { return base == "tox.ini" }},
	{Label: "pytest config", Match: func(_, base string, _ []string) bool { return base == "pytest.ini" }},
	{Label: "coverage.py config", Match: func(_, base string, _ []string) bool { return base == ".coveragerc" }},
	{Label: "nox session config", Match: func(_, base string, _ []string) bool { return base == "noxfile.py" }},
	{Label: "jest config", Match: func(_, base string, _ []string) bool { return jestConfigRE.MatchString(base) }},
	{Label: "vitest config", Match: func(_, base string, _ []string) bool { return vitestConfigRE.MatchString(base) }},
	{Label: "karma config", Match: func(_, base string, _ []string) bool { return karmaConfigRE.MatchString(base) }},
	{Label: "mocha config", Match: func(_, base string, _ []string) bool { return mochaConfigRE.MatchString(base) }},
	{Label: "make build/test driver", Match: func(_, base string, _ []string) bool {
		return base == "makefile" || base == "gnumakefile"
	}},
	{Label: "GitHub Actions workflow", Match: func(_, _ string, segs []string) bool {
		return hasSegment(segs, ".github") && hasSegment(segs, "workflows")
	}},
	{Label: "GitLab CI pipeline", Match: func(_, base string, _ []string) bool { return base == ".gitlab-ci.yml" }},
	{Label: "CircleCI pipeline", Match: func(_, _ string, segs []string) bool { return hasSegment(segs, ".circleci") }},
	{Label: "codecov gate config", Match: func(_, base string, _ []string) bool {
		return base == "codecov.yml" || base == ".codecov.yml"
	}},
	{Label: "pre-commit hook config", Match: func(_, base string, _ []string) bool {
		return base == ".pre-commit-config.yaml" || base == ".pre-commit-config.yml"
	}},
	{Label: "pyproject verification section", Match: func(_, base string, _ []string) bool {
		return base == "pyproject.toml"
	}, HunkGuard: PyVerificationHunkSection},
	{Label: "setup.cfg verification section", Match: func(_, base string, _ []string) bool {
		return base == "setup.cfg"
	}, HunkGuard: PyVerificationHunkSection},
	{Label: "package.json verification section", Match: func(_, base string, _ []string) bool {
		return base == "package.json"
	}, HunkGuard: PkgVerificationHunkSection},
}

var taskConfigIntent = regexp.MustCompile(
	`\b(ci|workflow|coverage config|test config|tox|pytest config)\b`,
)

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

func normPath(path string) string {
	path = strings.ReplaceAll(path, `\`, "/")
	path = stripEdgeQuotes(path)
	return jscompat.Trim(path)
}

func basenameLower(path string) string {
	i := strings.LastIndex(path, "/")
	if i >= 0 {
		path = path[i+1:]
	}
	return jsLowerCase(path)
}

func segmentsLower(path string) []string {
	parts := strings.Split(jsLowerCase(path), "/")
	out := []string{}
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isASCIIWord(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '_'
}

func leftBasenameForbidden(c byte) bool {
	return isASCIIWord(c) || c == '.' || c == '-'
}

func rightBasenameForbidden(c byte) bool {
	return isASCIIWord(c) || c == '-'
}

// TaskTargetsConfig reports whether taskText explicitly authorizes this file
// or verification-config work generally.
func TaskTargetsConfig(taskText, base string) bool {
	foldedText := asciiLower(taskText)
	if taskConfigIntent.MatchString(foldedText) {
		return true
	}
	needle := asciiLower(base)
	for start := 0; start <= len(foldedText)-len(needle); {
		offset := strings.Index(foldedText[start:], needle)
		if offset < 0 {
			return false
		}
		i := start + offset
		j := i + len(needle)
		leftOK := i == 0 || !leftBasenameForbidden(foldedText[i-1])
		rightOK := j == len(foldedText) || !rightBasenameForbidden(foldedText[j])
		if leftOK && rightOK {
			return true
		}
		start = i + 1
	}
	return false
}

// HunkTouchesVerification applies guard to changed +/- lines when the text
// looks like a unified diff; otherwise it scans the complete hunk.
func HunkTouchesVerification(hunk string, guard *regexp.Regexp) bool {
	if hunk == "" {
		return false
	}
	lines := strings.Split(strings.ReplaceAll(hunk, "\r\n", "\n"), "\n")
	changed := []string{}
	for _, line := range lines {
		if (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")) &&
			!strings.HasPrefix(line, "+++") && !strings.HasPrefix(line, "---") {
			changed = append(changed, line)
		}
	}
	scan := hunk
	if len(changed) > 0 {
		scan = strings.Join(changed, "\n")
	}
	if guard == PyVerificationHunkSection || guard == PkgVerificationHunkSection {
		// Both exported TS guards carry /i and contain only ASCII literals.
		// ASCII-only folding reproduces non-u JS RegExp canonicalization;
		// Go's (?i) would additionally fold ſ↔s and K↔k.
		scan = asciiLower(scan)
	}
	return guard.MatchString(scan)
}

// CheckTamper classifies unrequested verification-infrastructure changes.
func CheckTamper(input TamperInput) TamperVerdict {
	hunks := input.Hunks
	if hunks == nil {
		hunks = map[string]string{}
	}
	findings := []TamperFinding{}
	seen := map[string]struct{}{}

	for _, raw := range input.ChangedFiles {
		path := normPath(raw)
		if path == "" {
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		base := basenameLower(path)
		segs := segmentsLower(path)

		var matched *VerificationConfigPattern
		for i := range VerificationConfigPatterns {
			if VerificationConfigPatterns[i].Match(path, base, segs) {
				matched = &VerificationConfigPatterns[i]
				break
			}
		}
		if matched == nil || TaskTargetsConfig(input.TaskText, base) {
			continue
		}
		if matched.HunkGuard != nil {
			hunk, rawExists := hunks[raw]
			if !rawExists {
				hunk = hunks[path]
			}
			if !HunkTouchesVerification(hunk, matched.HunkGuard) {
				continue
			}
		}
		findings = append(findings, TamperFinding{File: path, Reason: matched.Label})
	}
	return TamperVerdict{Findings: findings, Clean: len(findings) == 0}
}

// TamperBlockerDetails renders one blocker-ready sentence per finding.
func TamperBlockerDetails(verdict TamperVerdict) []string {
	out := []string{}
	for _, finding := range verdict.Findings {
		out = append(out,
			"verification-config file "+finding.File+
				" was modified but the task does not call for it — "+
				"revert or justify explicitly (weakening test/CI gates to make verification pass is never acceptable).",
		)
	}
	return out
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

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
	var out strings.Builder
	out.Grow(len(s))
	for i, r := range runes {
		switch {
		case r == 0x0130:
			out.WriteRune('i')
			out.WriteRune(0x0307)
		case r == 0x03A3 && isFinalSigma(runes, i):
			out.WriteRune(0x03C2)
		default:
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
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
	return k >= len(runes) || !isCased(runes[k])
}

func isCased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

func isCaseIgnorable(r rune) bool {
	switch r {
	case '\'', 0x2019, 0x00AD, 0x02B9, 0x0385, 0x1FBF, 0x1FC1, 0x1FCD, 0x1FCE,
		0x1FCF, 0x1FDD, 0x1FDE, 0x1FDF, 0x1FED, 0x1FEE, 0x1FEF, 0x1FFD, 0x1FFE, 0x2027:
		return true
	}
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) ||
		unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Lm, r) || unicode.Is(unicode.Sk, r)
}
