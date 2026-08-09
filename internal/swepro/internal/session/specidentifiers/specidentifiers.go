// Package specidentifiers ports src/session/spec-identifiers.ts lines 1-340.
//
// The TypeScript source relies on JavaScript's broader \s class and on
// lookbehind around single-quoted display names. The regexes below spell out
// JavaScript whitespace, while quoted-token extraction is hand-scanned so the
// Go RE2 engine does not change those semantics.
package specidentifiers

import (
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type SpecIdentifierKind string

const (
	KindPath   SpecIdentifierKind = "path"
	KindCode   SpecIdentifierKind = "code"
	KindMarker SpecIdentifierKind = "marker"
	KindOption SpecIdentifierKind = "option"
)

type SpecIdentifierContext string

const (
	ContextRequirement SpecIdentifierContext = "requirement"
	ContextEvidence    SpecIdentifierContext = "evidence"
)

type SpecIdentifier struct {
	Value   string                 `json:"value"`
	Kind    SpecIdentifierKind     `json:"kind"`
	Source  string                 `json:"source"`
	Context *SpecIdentifierContext `json:"context,omitempty"`
}

type LineContext struct {
	Line     string `json:"line"`
	Evidence bool   `json:"evidence"`
}

type CheckIdentifiersInput struct {
	Identifiers  []SpecIdentifier `json:"identifiers"`
	SearchText   string           `json:"searchText"`
	ChangedFiles *[]string        `json:"changedFiles,omitempty"`
}

type CheckIdentifiersResult struct {
	Present []SpecIdentifier `json:"present"`
	Missing []SpecIdentifier `json:"missing"`
}

const IdentifierEnforcementCap = 40

const (
	maxIdentifierLen = 80
	maxSourceLen     = 120
	minIdentifierLen = 3
	jsSpace          = `[\t\n\x0b\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`
)

var (
	stoplist = map[string]bool{
		"the": true, "and": true, "for": true, "with": true, "from": true,
		"this": true, "that": true, "then": true, "else": true, "when": true,
		"true": true, "false": true, "null": true, "none": true, "yes": true,
		"no": true, "not": true, "all": true, "any": true, "one": true,
		"two": true, "via": true, "use": true, "used": true, "uses": true,
		"must": true, "should": true, "will": true, "can": true, "may": true,
		"each": true, "every": true, "both": true, "into": true, "onto": true,
		"over": true, "under": true, "above": true, "below": true,
	}
	enforcedKinds = map[SpecIdentifierKind]bool{
		KindPath: true, KindCode: true, KindMarker: true, KindOption: true,
	}
	fenceToggleRe  = regexp.MustCompile(`^` + jsSpace + `*(?:` + "```" + `|~~~)`)
	evidenceLineRe = regexp.MustCompile(
		`^` + jsSpace + `*(?:` +
			`\$|>|<|//|#|-{1,2}[A-Za-z]|` +
			`(?:curl|wget|git|npm|npx|yarn|pnpm|node|deno|bun|python[0-9.]*|pip[0-9]*|pytest|import|from|export|const|let|var|await|return|print|console|assert|def|class|function|resp?|req|app)\b(?:` + jsSpace + `|[.(])|` +
			`@[A-Za-z0-9_]|[A-Za-z_$][A-Za-z0-9_$.\[\]]*` + jsSpace + `*=[^=])`,
	)
	digitsOnlyRe = regexp.MustCompile(`^[0-9]+$`)
	pathSuffixRe = regexp.MustCompile(`\.[A-Za-z0-9]+$`)
	optionRe     = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*)=([^\n\r\x{2028}\x{2029}]+)$`)
	ellipsisRe   = regexp.MustCompile(`\.{3,}`)
	// A regex character-class escape is the tell that a backticked token is a
	// PATTERN rather than a name. Deliberately narrow: only \d \w \s \b and
	// their negations, so `C:\Users\app` and a printf format keep their
	// backslashes without being mistaken for notation.
	regexClassEscapeRe = regexp.MustCompile(`\\[dDwWsSbB]`)
	regexQuantifierRe  = regexp.MustCompile(`[+*?{}()\[\]|]`)
	// Comparison, logical and arithmetic operators. These occur in code
	// expressions and never in a human-readable display name, which is the
	// only thing the multi-word quoted-phrase branch is meant to catch.
	exprOperatorRe = regexp.MustCompile(`[!<>=]=|&&|\|\||[<>%]|(?:^|` + jsSpace + `)[-+*/](?:` + jsSpace + `|$)`)

	charClassOnlyRe = regexp.MustCompile(`^(?:[A-Za-z0-9]-[A-Za-z0-9]|[-_])+$`)
	charRangeRe     = regexp.MustCompile(`[A-Za-z0-9]-[A-Za-z0-9]`)
	anchorRe        = regexp.MustCompile(`^#[A-Za-z0-9_]+$`)
	jsSpaceRe       = regexp.MustCompile(jsSpace)
	jsSpaceRunRe    = regexp.MustCompile(jsSpace + `+`)
	asciiLetterRe   = regexp.MustCompile(`[A-Za-z]`)
)

// ClassifyLineContexts tracks fenced code and classifies command/code-shaped
// lines as evidence rather than enforceable requirement prose.
func ClassifyLineContexts(taskText string) []LineContext {
	lines := regexp.MustCompile(`\r?\n`).Split(taskText, -1)
	out := make([]LineContext, 0, len(lines))
	inFence := false
	for _, line := range lines {
		if fenceToggleRe.MatchString(line) {
			out = append(out, LineContext{Line: line, Evidence: true})
			inFence = !inFence
			continue
		}
		out = append(out, LineContext{Line: line, Evidence: inFence || evidenceLineRe.MatchString(line)})
	}
	return out
}

func trimSource(line string) string {
	t := jscompat.Trim(line)
	units := utf16.Encode([]rune(t))
	if len(units) <= maxSourceLen {
		return t
	}
	return string(utf16.Decode(units[:maxSourceLen-1])) + "…"
}

func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func isNoise(value string) bool {
	n := utf16Len(value)
	if n < minIdentifierLen || n > maxIdentifierLen || digitsOnlyRe.MatchString(value) {
		return true
	}
	if !jsSpaceRe.MatchString(value) && stoplist[strings.ToLower(value)] {
		return true
	}
	return false
}

func looksLikeMarker(token string) bool {
	if strings.Contains(token, "<!--") || strings.Contains(token, "-->") {
		return true
	}
	units := utf16.Encode([]rune(token))
	if len(units) < 3 || !strings.ContainsRune("[<{", rune(units[0])) ||
		!strings.ContainsRune("]>}", rune(units[len(units)-1])) {
		return false
	}
	for _, unit := range units[1 : len(units)-1] {
		switch unit {
		case '\n', '\r', 0x2028, 0x2029:
			return false
		}
	}
	return true
}

func looksLikePath(token string) bool {
	return strings.Contains(token, "/") && pathSuffixRe.MatchString(token)
}

// looksLikeCodeName guards the backtick scanner's `code` fallback, which is
// otherwise a bare `else`: every backticked token on a non-evidence line
// becomes an identifier the changed files must contain verbatim.
//
// Deliberate divergence from spec-identifiers.ts:236. A token is an enforceable
// NAME only when it is a single lexical token carrying at least one ASCII
// letter. Command invocations (`mycommand one --help`), inline statements
// (`this._x = source._x`) and letterless runs (`🚀🚀🚀🚀`, `===`) are sample
// output quoted in prose, not names a fix must reproduce — the commander-2342
// benchmark run failed a correct 34-line patch because the gate demanded the
// agent paste an emoji into lib/command.js.
//
// Marker, option and path tokens are classified BEFORE this check, so
// `<!-- toc -->` and `listStyle='dash item'` keep their whitespace; quoted
// multiword display names come from the separate quoted-phrase branch below.
// Neither is affected.
func looksLikeCodeName(token string) bool {
	return !jsSpaceRe.MatchString(token) && asciiLetterRe.MatchString(token)
}

// looksLikeExpression guards the quoted-phrase branch, which exists to catch
// multi-word DISPLAY NAMES ("Auto Table of Contents"). A quoted phrase carrying
// a comparison, logical or arithmetic operator is not a name — it is a code
// EXPRESSION written to show the reader what changes, the same principle
// looksLikeSpecNotation applies to a quoted regex.
//
// A planner issue wrote its acceptance checks as shell:
//
//   - [ ] `grep -n 'x%100 != 12' ordinals.go` exits 0 and matches line 16
//   - Check: `grep -n 'x%100 != 12' ordinals.go && grep -n 'x%100 != 13' …`
//
// The single-quoted grep ARGUMENT is a three-token phrase, so the branch
// enforced `x%100 != 12` as an identifier the changed files must contain. The
// gate then fired on a tree whose ordinals.go literally reads
// `if x%100 != 12 {`, and kept re-dispatching a fix that was already correct
// with `go build ./...` and `go test ./...` both green.
//
// Scoped to this branch on purpose. Markers, options and paths are classified
// earlier and keep their punctuation — `<!-- TOC -->` is a marker, not an
// expression — and a single backticked token never reaches here at all,
// because looksLikeCodeName already rejects anything carrying whitespace.
func looksLikeExpression(phrase string) bool {
	return exprOperatorRe.MatchString(phrase)
}

func parseOption(token string) (key, value string, ok bool) {
	match := optionRe.FindStringSubmatch(token)
	if match == nil {
		return "", "", false
	}
	return match[1], jscompat.Trim(match[2]), true
}

func oneWrapperInterior(value string) (string, bool) {
	units := utf16.Encode([]rune(value))
	if len(units) < 3 {
		return "", false
	}
	if len(units) >= 5 && units[0] == '[' && units[1] == '[' &&
		units[len(units)-2] == ']' && units[len(units)-1] == ']' {
		return string(utf16.Decode(units[2 : len(units)-2])), true
	}
	pairs := map[uint16]uint16{'[': ']', '{': '}', '<': '>'}
	if close, ok := pairs[units[0]]; ok && units[len(units)-1] == close {
		return string(utf16.Decode(units[1 : len(units)-1])), true
	}
	return "", false
}

func looksLikeSpecNotation(value string) bool {
	if ellipsisRe.MatchString(value) {
		return true
	}
	// A regex quoted in an issue is notation the reader is meant to understand,
	// not a string the fix must contain. werkzeug-3146's reporter wrote "There
	// are multiple ways to fix this issue. One could for example adjust the
	// regex ... (`\d+\.\d+(e[+-]/d+)?` instead of `\d+\.\d+`). Another option
	// would be to suppress scientific notation" — and upstream took the OTHER
	// option, changing to_url and never touching the regex. Enforcing the
	// pattern verbatim (typo `/d+` and all) pushes the agent away from the fix
	// that was actually correct.
	if regexClassEscapeRe.MatchString(value) && regexQuantifierRe.MatchString(value) {
		return true
	}
	if charClassOnlyRe.MatchString(value) && charRangeRe.MatchString(value) {
		return true
	}
	interior := value
	if wrapped, ok := oneWrapperInterior(value); ok {
		interior = wrapped
	}
	return anchorRe.MatchString(interior)
}

func scanDelimited(line string, delimiter uint16, minUnits int, openingOK, closingOK func([]uint16, int) bool) []string {
	units := utf16.Encode([]rune(line))
	out := []string{}
	for opening := 0; opening < len(units); {
		for opening < len(units) && (units[opening] != delimiter || !openingOK(units, opening)) {
			opening++
		}
		if opening >= len(units) {
			break
		}
		closing := opening + 1
		for closing < len(units) && units[closing] != delimiter {
			closing++
		}
		if closing < len(units) && closing-opening-1 >= minUnits && closingOK(units, closing) {
			out = append(out, string(utf16.Decode(units[opening+1:closing])))
			opening = closing + 1
		} else {
			opening++
		}
	}
	return out
}

func alwaysDelimiter([]uint16, int) bool { return true }

func isASCIIAlpha(unit uint16) bool {
	return unit >= 'A' && unit <= 'Z' || unit >= 'a' && unit <= 'z'
}

func singleQuoteOpening(units []uint16, at int) bool {
	return at == 0 || !isASCIIAlpha(units[at-1])
}

func singleQuoteClosing(units []uint16, at int) bool {
	return at+1 == len(units) || !isASCIIAlpha(units[at+1])
}

func stripOneSurroundingQuote(value string) string {
	units := utf16.Encode([]rune(value))
	if len(units) > 0 && (units[0] == '\'' || units[0] == '"') {
		units = units[1:]
	}
	if len(units) > 0 && (units[len(units)-1] == '\'' || units[len(units)-1] == '"') {
		units = units[:len(units)-1]
	}
	return string(utf16.Decode(units))
}

func wordCount(value string) int {
	parts := jsSpaceRunRe.Split(value, -1)
	count := 0
	for _, part := range parts {
		if part != "" {
			count++
		}
	}
	return count
}

// ExtractSpecIdentifiers extracts, classifies, filters, and insertion-order
// deduplicates identifiers quoted by the task text.
func ExtractSpecIdentifiers(taskText string) []SpecIdentifier {
	out := []SpecIdentifier{}
	byKey := make(map[string]int)
	currentEvidence := false

	push := func(value string, kind SpecIdentifierKind, source string) {
		v := jscompat.Trim(value)
		if isNoise(v) || looksLikeSpecNotation(v) {
			return
		}
		key := string(kind) + " " + v
		context := ContextRequirement
		if currentEvidence {
			context = ContextEvidence
		}
		if index, ok := byKey[key]; ok {
			if out[index].Context != nil && *out[index].Context == ContextEvidence && context == ContextRequirement {
				out[index].Context = &context
				out[index].Source = trimSource(source)
			}
			return
		}
		byKey[key] = len(out)
		out = append(out, SpecIdentifier{
			Value: v, Kind: kind, Source: trimSource(source), Context: &context,
		})
	}

	for _, classified := range ClassifyLineContexts(taskText) {
		line := classified.Line
		currentEvidence = classified.Evidence
		for _, raw := range scanDelimited(line, '`', 1, alwaysDelimiter, alwaysDelimiter) {
			token := jscompat.Trim(raw)
			if token == "" {
				continue
			}
			if looksLikeMarker(token) {
				push(token, KindMarker, line)
				continue
			}
			if key, value, ok := parseOption(token); ok {
				push(key, KindOption, line)
				push(stripOneSurroundingQuote(value), KindOption, line)
				continue
			}
			if looksLikePath(token) {
				push(token, KindPath, line)
				continue
			}
			if looksLikeCodeName(token) {
				push(token, KindCode, line)
			}
		}

		phrases := scanDelimited(line, '"', 2, alwaysDelimiter, alwaysDelimiter)
		phrases = append(phrases, scanDelimited(line, '\'', 2, singleQuoteOpening, singleQuoteClosing)...)
		for _, raw := range phrases {
			phrase := jscompat.Trim(raw)
			if wordCount(phrase) >= 2 && !looksLikeExpression(phrase) {
				push(phrase, KindCode, line)
			}
		}
	}
	return out
}

// EnforceableIdentifiers keeps requirement-context identifiers of enforced
// kinds, unless their count exceeds the all-or-nothing enforcement cap.
func EnforceableIdentifiers(identifiers []SpecIdentifier) []SpecIdentifier {
	enforced := []SpecIdentifier{}
	for _, id := range identifiers {
		context := ContextRequirement
		if id.Context != nil {
			context = *id.Context
		}
		if enforcedKinds[id.Kind] && context == ContextRequirement {
			enforced = append(enforced, id)
		}
	}
	if len(enforced) > IdentifierEnforcementCap {
		return []SpecIdentifier{}
	}
	return enforced
}

func normalizePath(path string) string {
	path = jscompat.Trim(path)
	if strings.HasPrefix(path, "./") {
		return path[2:]
	}
	return strings.TrimPrefix(path, "/")
}

func pathInChangedList(value string, changedFiles []string) bool {
	v := normalizePath(value)
	if v == "" {
		return false
	}
	for _, raw := range changedFiles {
		c := normalizePath(raw)
		if c == v || strings.HasSuffix(c, "/"+v) || strings.HasSuffix(v, "/"+c) {
			return true
		}
	}
	return false
}

// CheckIdentifiersInTree partitions identifiers by exact occurrence in changed
// file text, with path identifiers optionally checked against the file list.
func CheckIdentifiersInTree(input CheckIdentifiersInput) CheckIdentifiersResult {
	text := input.SearchText
	lower := strings.ToLower(text)
	present := []SpecIdentifier{}
	missing := []SpecIdentifier{}
	for _, id := range input.Identifiers {
		hit := false
		if id.Kind == KindPath && input.ChangedFiles != nil {
			hit = pathInChangedList(id.Value, *input.ChangedFiles)
		} else {
			hit = strings.Contains(text, id.Value) ||
				id.Kind == KindMarker && strings.Contains(lower, strings.ToLower(id.Value))
			// A file NAMED by the spec and actually changed satisfies the spec,
			// whatever kind it was classified as. looksLikePath requires a "/",
			// so a repo-root basename like `ordinals.go` is classified KindCode
			// and falls to the content check above — which demands the filename
			// appear INSIDE the changed files' text. A Go source file does not
			// contain its own name, so that identifier could never be
			// satisfied: the gate reported `ordinals.go` missing while
			// ordinals.go was the one file the fix changed, and re-dispatched
			// against an otherwise-green tree.
			//
			// Widening looksLikePath instead would misfile selectors — a
			// lowercase `foo.bar` is indistinguishable from a basename by shape
			// — and turn today's content hits into false misses. Checking the
			// changed-file list only as a FALLBACK is strictly additive: it can
			// promote a miss to a hit, never the reverse, so an identifier that
			// names an unchanged file still fires the gate.
			if !hit && input.ChangedFiles != nil {
				hit = pathInChangedList(id.Value, *input.ChangedFiles)
			}
		}
		if hit {
			present = append(present, id)
		} else {
			missing = append(missing, id)
		}
	}
	return CheckIdentifiersResult{Present: present, Missing: missing}
}

// IdentifierBlockerDetails turns missing identifiers into model-visible audit
// blocker sentences.
func IdentifierBlockerDetails(missing []SpecIdentifier) []string {
	out := make([]string, 0, len(missing))
	for _, id := range missing {
		out = append(out,
			"spec names the exact identifier `"+id.Value+"` (from: "+id.Source+") — it does not "+
				"appear anywhere in the changed files; use spec identifiers verbatim, never paraphrase.",
		)
	}
	return out
}
