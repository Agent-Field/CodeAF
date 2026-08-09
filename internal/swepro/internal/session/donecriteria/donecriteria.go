// Package donecriteria ports src/session/done-criteria.ts lines 1-145.
//
// The source uses lookbehind to split prose sentences and several JavaScript
// regular expressions whose \s class is wider than RE2's. The scanners below
// preserve those semantics without relying on unsupported lookbehind.
package donecriteria

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type DoneCriterionKind string

const (
	KindTest     DoneCriterionKind = "test"
	KindFile     DoneCriterionKind = "file"
	KindBehavior DoneCriterionKind = "behavior"
	KindGeneric  DoneCriterionKind = "generic"
)

type DoneCriterion struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Kind        DoneCriterionKind `json:"kind"`
}

type DoneCheckResult struct {
	Satisfied   []string `json:"satisfied"`
	Unsatisfied []string `json:"unsatisfied"`
	Done        bool     `json:"done"`
}

type DoneEvidence struct {
	TestsPassed  *bool    `json:"testsPassed"`
	ChangedFiles []string `json:"changedFiles"`
	TestOutput   *string  `json:"testOutput"`
}

const jsWS = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var (
	testCriterionRE = regexp.MustCompile(`\b(?:test|tests|testing|test-suite|test suite|typecheck|lint|pytest|bun` + jsWS + `+test|npm` + jsWS + `+(?:run` + jsWS + `+)?test|pnpm` + jsWS + `+(?:run` + jsWS + `+)?test|yarn` + jsWS + `+test)\b`)
	allTestsRE      = regexp.MustCompile(`\b(?:all|every)` + jsWS + `+(?:checks?|tests?)` + jsWS + `+(?:pass|passed|green)\b`)
	behaviorVerbRE  = regexp.MustCompile(`\b(?:supports?|handles?|returns?|prevents?|detects?|enforces?|allows?|rejects?)\b`)
	behaviorStartRE = regexp.MustCompile(`^(?:please` + jsWS + `+)?(?:add|create|implement|build|make|ensure|verify|support|handle|detect|enforce|allow|prevent|reject|return|provide|expose|update|keep|preserve)\b`)
	behaviorModalRE = regexp.MustCompile(`\b(?:must|should|shall|needs?` + jsWS + `+to|is` + jsWS + `+able` + jsWS + `+to|can)\b`)
	fileActionRE    = regexp.MustCompile(`^(?:please` + jsWS + `+)?(?:add|create|edit|modify|update|write|remove|delete)\b`)
	fileTokenRE     = regexp.MustCompile(`^((?:\.\.?[\\/])?[A-Za-z0-9_@.-]+(?:[\\/][A-Za-z0-9_@.-]+)*\.[A-Za-z0-9_-]+)`)
)

// ParseDoneCriteria extracts test, file, and behavior obligations from prose.
// An any parameter preserves the source's runtime typeof guard.
func ParseDoneCriteria(taskDescription any) []DoneCriterion {
	text, _ := taskDescription.(string)
	candidates := extractCandidates(text)
	found := make([]DoneCriterion, 0)
	for _, candidate := range candidates {
		if isTestCriterion(candidate) {
			found = append(found, DoneCriterion{Description: candidate, Kind: KindTest})
		}
		if isFileCriterion(candidate) {
			found = append(found, DoneCriterion{Description: candidate, Kind: KindFile})
		}
		if isBehaviorCriterion(candidate) {
			found = append(found, DoneCriterion{Description: candidate, Kind: KindBehavior})
		}
	}

	deduped := make([]DoneCriterion, 0, len(found))
	seen := make(map[string]bool)
	for _, item := range found {
		key := string(item.Kind) + "\x00" + jsLowerCase(item.Description)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, item)
	}
	if len(deduped) == 0 {
		return []DoneCriterion{{
			ID:          "implementation-complete-and-verified",
			Description: "implementation complete and verified",
			Kind:        KindGeneric,
		}}
	}
	for i := range deduped {
		deduped[i].ID = "criterion-" + jscompat.FormatNumber(float64(i+1))
	}
	return deduped
}

// CheckDone checks criteria against externally supplied evidence.
func CheckDone(criteria []DoneCriterion, evidence *DoneEvidence) DoneCheckResult {
	satisfied := []string{}
	unsatisfied := []string{}
	changedFiles := []string{}
	testsPassed := false
	if evidence != nil {
		changedFiles = evidence.ChangedFiles
		testsPassed = evidence.TestsPassed != nil && *evidence.TestsPassed
	}
	hasDiff := false
	for _, file := range changedFiles {
		if jscompat.Trim(file) != "" {
			hasDiff = true
			break
		}
	}
	for _, criterion := range criteria {
		ok := false
		switch criterion.Kind {
		case KindFile:
			ok = fileCriterionSatisfied(criterion.Description, changedFiles)
		case KindTest:
			ok = testsPassed
		default:
			ok = testsPassed && hasDiff
		}
		if ok {
			satisfied = append(satisfied, criterion.ID)
		} else {
			unsatisfied = append(unsatisfied, criterion.ID)
		}
	}
	return DoneCheckResult{
		Satisfied: satisfied, Unsatisfied: unsatisfied, Done: len(unsatisfied) == 0,
	}
}

// BuildDoneCriteriaPrompt builds the stable review-gate checklist.
func BuildDoneCriteriaPrompt(criteria []DoneCriterion) string {
	rows := make([]string, 0, len(criteria))
	for _, criterion := range criteria {
		rows = append(rows, "- [ ] "+criterion.ID+": "+criterion.Description+" ("+string(criterion.Kind)+")")
	}
	if len(rows) == 0 {
		rows = []string{"- [ ] implementation-complete-and-verified: implementation complete and verified (generic)"}
	}
	lines := append([]string{"Completion criteria (all items must be satisfied by evidence):"}, rows...)
	lines = append(lines, "", "Do not accept an agent self-report as evidence; verify each checklist item against the test result and diff.")
	return strings.Join(lines, "\n")
}

func extractCandidates(text string) []string {
	lines := splitCRLF(text)
	candidates := []string{}
	for _, line := range lines {
		trimmed := stripListPrefix(line)
		trimmed = stripHeadingPrefix(trimmed)
		trimmed = jscompat.Trim(trimmed)
		if trimmed == "" {
			continue
		}
		for _, part := range splitSentences(trimmed) {
			candidate := jscompat.Trim(part)
			candidate = stripBulletPrefix(candidate)
			candidate = jscompat.Trim(candidate)
			if candidate != "" {
				candidates = append(candidates, candidate)
			}
		}
	}
	return candidates
}

func splitCRLF(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' {
			continue
		}
		end := i
		if end > start && s[end-1] == '\r' {
			end--
		}
		out = append(out, s[start:end])
		start = i + 1
	}
	return append(out, s[start:])
}

func stripListPrefix(s string) string {
	i := consumeJSWS(s, 0)
	if i >= len(s) {
		return s
	}
	j := i
	switch {
	case s[j] == '-' || s[j] == '*' || s[j] == '+':
		j++
	case isASCIIDigit(s[j]):
		for j < len(s) && isASCIIDigit(s[j]) {
			j++
		}
		if j >= len(s) || (s[j] != '.' && s[j] != ')') {
			return s
		}
		j++
	default:
		return s
	}
	k := consumeJSWS(s, j)
	if k == j {
		return s
	}
	return s[k:]
}

func stripHeadingPrefix(s string) string {
	i := 0
	for i < len(s) && s[i] == '#' {
		i++
	}
	if i == 0 {
		return s
	}
	j := consumeJSWS(s, i)
	if j == i {
		return s
	}
	return s[j:]
}

func stripBulletPrefix(s string) string {
	if len(s) == 0 || (s[0] != '-' && s[0] != '*' && s[0] != '+') {
		return s
	}
	i := consumeJSWS(s, 1)
	if i == 1 {
		return s
	}
	return s[i:]
}

func splitSentences(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); {
		if !isJSWhitespaceAt(s, i) {
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
			continue
		}
		wsStart := i
		i = consumeJSWS(s, i)
		if wsStart == 0 {
			continue
		}
		prev := s[wsStart-1]
		split := prev == '!' || prev == '?'
		if prev == '.' && sentenceStarter(s[i:]) {
			split = true
		}
		if split {
			out = append(out, s[start:wsStart])
			start = i
		}
	}
	return append(out, s[start:])
}

func sentenceStarter(s string) bool {
	if len(s) >= 2 && s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'a' && s[1] <= 'z' {
		return true
	}
	for _, word := range []string{"Run", "Pass", "Create", "Add", "Implement", "Ensure", "Verify", "Update"} {
		if strings.HasPrefix(s, word) && (len(s) == len(word) || !isASCIIWord(s[len(word)])) {
			return true
		}
	}
	return false
}

func isTestCriterion(candidate string) bool {
	lower := asciiLower(candidate)
	return testCriterionRE.MatchString(lower) || allTestsRE.MatchString(lower)
}

func isFileCriterion(candidate string) bool {
	hasFileToken := len(extractFileTokens(candidate)) > 0
	lower := asciiLower(candidate)
	namesAFile := strings.Contains(lower, "file") || strings.Contains(lower, "module") || strings.Contains(lower, "path")
	return (hasFileToken || namesAFile) && (namesAFile || fileActionRE.MatchString(lower))
}

func isBehaviorCriterion(candidate string) bool {
	lower := asciiLower(candidate)
	if isTestCriterion(candidate) && !behaviorVerbRE.MatchString(lower) {
		return false
	}
	return behaviorStartRE.MatchString(lower) || behaviorModalRE.MatchString(lower) || behaviorVerbRE.MatchString(lower)
}

func fileCriterionSatisfied(description string, changedFiles []string) bool {
	targets := extractFileTokens(description)
	if len(targets) == 0 {
		return false
	}
	changed := make([]string, 0, len(changedFiles))
	for _, file := range changedFiles {
		if jscompat.Trim(file) != "" {
			changed = append(changed, normalizePath(file))
		}
	}
	for _, target := range targets {
		target = normalizePath(target)
		for _, file := range changed {
			if file == target || basename(file) == basename(target) {
				return true
			}
		}
	}
	return false
}

func extractFileTokens(text string) []string {
	tokens := []string{}
	for i := 0; i < len(text); {
		if i != 0 && !isFileLeadingDelimiterBefore(text, i) {
			_, size := utf8.DecodeRuneInString(text[i:])
			i += size
			continue
		}
		match := fileTokenRE.FindStringSubmatch(text[i:])
		if match == nil {
			_, size := utf8.DecodeRuneInString(text[i:])
			i += size
			continue
		}
		token := match[1]
		end := i + len(token)
		if end == len(text) || isFileTrailingDelimiterAt(text, end) {
			lower := asciiLower(token)
			if !strings.HasPrefix(lower, "http.") && !strings.HasPrefix(lower, "https.") {
				tokens = append(tokens, token)
			}
			i = end
			continue
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		i += size
	}
	return tokens
}

func normalizePath(value string) string {
	value = strings.ReplaceAll(value, `\`, "/")
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimSuffix(value, "/")
	return jsLowerCase(value)
}

func basename(value string) string {
	if i := strings.LastIndexByte(value, '/'); i >= 0 {
		return value[i+1:]
	}
	return value
}

func consumeJSWS(s string, i int) int {
	for i < len(s) && isJSWhitespaceAt(s, i) {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return i
}

func isJSWhitespaceAt(s string, i int) bool {
	r, _ := utf8.DecodeRuneInString(s[i:])
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029,
		0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func isFileLeadingDelimiterBefore(s string, i int) bool {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	if isJSWhitespaceRune(r) {
		return true
	}
	return strings.ContainsRune("`\"'([<{", r)
}

func isFileTrailingDelimiterAt(s string, i int) bool {
	if isJSWhitespaceAt(s, i) {
		return true
	}
	return strings.ContainsRune("`\"'])}>:;,!?", rune(s[i]))
}

func isJSWhitespaceRune(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029,
		0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func asciiLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }
func isASCIIWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func jsLowerCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range runes {
		switch {
		case r == 0x0130:
			b.WriteRune('i')
			b.WriteRune(0x0307)
		case r == 0x03a3 && isFinalSigma(runes, i):
			b.WriteRune(0x03c2)
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
	return k >= len(runes) || !isCased(runes[k])
}

func isCased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

func isCaseIgnorable(r rune) bool {
	if caseIgnorablePunct[r] {
		return true
	}
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) ||
		unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Lm, r) ||
		unicode.Is(unicode.Sk, r)
}

var caseIgnorablePunct = map[rune]bool{
	0x0027: true, 0x002e: true, 0x003a: true, 0x00b7: true, 0x0387: true,
	0x055f: true, 0x05f4: true, 0x2018: true, 0x2019: true, 0x2024: true,
	0x2027: true, 0xfe13: true, 0xfe52: true, 0xfe55: true, 0xff07: true,
	0xff0e: true, 0xff1a: true,
}
