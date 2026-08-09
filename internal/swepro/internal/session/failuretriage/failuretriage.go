// Package failuretriage ports src/session/failure-triage.ts:1-369 from
// swe-pro at commit 3b25a1a.
//
// The classifier is pure and heuristic-first. Regex fidelity is deliberately
// explicit: ECMAScript \s includes more code points than RE2's \s, JavaScript
// non-Unicode /i does not use Go's Unicode simple-fold equivalence classes,
// multiline ^ recognizes CR/U+2028/U+2029 as well as LF, and JavaScript string
// slicing counts UTF-16 code units.
package failuretriage

import (
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// FailureDiagnosis mirrors the TS string union.
type FailureDiagnosis string

const (
	DiagnosisLocalization FailureDiagnosis = "localization"
	DiagnosisSpecMisread  FailureDiagnosis = "spec-misread"
	DiagnosisEnvFlaky     FailureDiagnosis = "env-flaky"
	DiagnosisLoop         FailureDiagnosis = "loop"
	DiagnosisTooHard      FailureDiagnosis = "too-hard"
	DiagnosisUnknown      FailureDiagnosis = "unknown"
)

// FailureRemedy mirrors the TS string union.
type FailureRemedy string

const (
	RemedyFreshContextRetry FailureRemedy = "fresh-context-retry"
	RemedyAddContextRetry   FailureRemedy = "add-context-retry"
	RemedyRerunTest         FailureRemedy = "rerun-test"
	RemedySplit             FailureRemedy = "split"
	RemedyEscalate          FailureRemedy = "escalate"
)

// FailureTriageInput mirrors the TS interface. TestOutput nil is undefined.
// Numeric fields stay float64 because the source performs no integer
// validation and interpolates the original JS numbers into signals.
type FailureTriageInput struct {
	TranscriptTail string  `json:"transcriptTail"`
	TestOutput     *string `json:"testOutput"`
	ToolErrors     float64 `json:"toolErrors"`
	Turns          float64 `json:"turns"`
	RepairRounds   float64 `json:"repairRounds"`
}

// FailureTriageResult mirrors the TS interface and preserves object field
// order for JSON.stringify parity.
type FailureTriageResult struct {
	Diagnosis  FailureDiagnosis  `json:"diagnosis"`
	Confidence jscompat.JSNumber `json:"confidence"`
	Remedy     FailureRemedy     `json:"remedy"`
	Signals    []string          `json:"signals"`
}

// MarshalJSON uses a JS-compatible string quoter so a loop-action signal cut
// between surrogate halves serializes as "\udXXX", exactly like
// JSON.stringify.
func (r FailureTriageResult) MarshalJSON() ([]byte, error) {
	out := []byte(`{"diagnosis":`)
	out = appendJSQuoted(out, string(r.Diagnosis))
	out = append(out, `,"confidence":`...)
	f := float64(r.Confidence)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		out = append(out, "null"...)
	} else {
		out = append(out, jscompat.FormatNumber(f)...)
	}
	out = append(out, `,"remedy":`...)
	out = appendJSQuoted(out, string(r.Remedy))
	out = append(out, `,"signals":[`...)
	for i, signal := range r.Signals {
		if i > 0 {
			out = append(out, ',')
		}
		out = appendJSQuoted(out, signal)
	}
	out = append(out, ']', '}')
	return out, nil
}

// FailureTriageLlmHook is the future enrichment seam; ClassifyFailure never
// invokes it.
type FailureTriageLlmHook func(FailureTriageInput, FailureTriageResult) FailureTriageResult

const (
	loopIdenticalStreak         = 3
	loopSameFileReads           = 4
	tooHardMinTurns             = 12
	tooHardMinEdits             = 4
	tooHardEscalateRepairRounds = 2
	envFlakyConfidence          = 0.9
	loopConfidence              = 0.85
	localizationConfidence      = 0.8
	tooHardConfidence           = 0.75
	specMisreadConfidence       = 0.65
	unknownConfidence           = 0.3
)

// FailureTriageThresholds is FAILURE_TRIAGE_THRESHOLDS.
var FailureTriageThresholds = struct {
	LoopIdenticalStreak         float64 `json:"LOOP_IDENTICAL_STREAK"`
	LoopSameFileReads           float64 `json:"LOOP_SAME_FILE_READS"`
	TooHardMinTurns             float64 `json:"TOO_HARD_MIN_TURNS"`
	TooHardMinEdits             float64 `json:"TOO_HARD_MIN_EDITS"`
	TooHardEscalateRepairRounds float64 `json:"TOO_HARD_ESCALATE_REPAIR_ROUNDS"`
}{
	LoopIdenticalStreak:         loopIdenticalStreak,
	LoopSameFileReads:           loopSameFileReads,
	TooHardMinTurns:             tooHardMinTurns,
	TooHardMinEdits:             tooHardMinEdits,
	TooHardEscalateRepairRounds: tooHardEscalateRepairRounds,
}

type envPattern struct {
	match func(string) bool
	label string
}

var envPatterns = []envPattern{
	{func(s string) bool { return containsBounded(s, "MODULE_NOT_FOUND", true) }, "MODULE_NOT_FOUND in output"},
	{func(s string) bool { return containsBounded(s, "Cannot find module", true) }, "Cannot find module in output"},
	{func(s string) bool { return containsBounded(s, "ENOENT", false) }, "ENOENT in output"},
	{func(s string) bool {
		return containsBounded(s, "EACCES", true) || containsBounded(s, "EPERM", true)
	}, "permission errno in output"},
	{func(s string) bool { return containsBounded(s, "permission denied", true) }, "permission denied in output"},
	{func(s string) bool {
		for _, errno := range []string{"ECONNREFUSED", "ENOTFOUND", "ETIMEDOUT", "ECONNRESET"} {
			if containsBounded(s, errno, true) {
				return true
			}
		}
		return false
	}, "network errno in output"},
	{func(s string) bool {
		for _, phrase := range []string{"network error", "network unreachable", "network failure"} {
			if containsBounded(s, phrase, true) {
				return true
			}
		}
		return false
	}, "network error in output"},
	{func(s string) bool {
		return containsBounded(s, "getaddrinfo ENOTFOUND", true) ||
			containsBounded(s, "getaddrinfo EAI_AGAIN", true)
	}, "DNS lookup failure in output"},
}

func collectEnvSignals(blobs ...string) []string {
	nonempty := []string{}
	for _, blob := range blobs {
		if blob != "" {
			nonempty = append(nonempty, blob)
		}
	}
	joined := strings.Join(nonempty, "\n")
	signals := []string{}
	if joined == "" {
		return signals
	}
	for _, pattern := range envPatterns {
		if pattern.match(joined) {
			signals = append(signals, pattern.label)
		}
	}
	return signals
}

func containsBounded(text, needle string, insensitive bool) bool {
	if needle == "" || len(needle) > len(text) {
		return false
	}
	for i := 0; i+len(needle) <= len(text); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			a, b := text[i+j], needle[j]
			if insensitive {
				a = asciiLower(a)
				b = asciiLower(b)
			}
			if a != b {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		beforeWord := i > 0 && isASCIIWord(text[i-1])
		after := i + len(needle)
		afterWord := after < len(text) && isASCIIWord(text[after])
		if !beforeWord && !afterWord {
			return true
		}
	}
	return false
}

func asciiLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func isASCIIWord(b byte) bool {
	return b >= 'A' && b <= 'Z' ||
		b >= 'a' && b <= 'z' ||
		b >= '0' && b <= '9' ||
		b == '_'
}

const jsWS = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var (
	toolActionLineRE = regexp.MustCompile(
		`\A[-*]?` + jsWS + `*(?:` + asciiFold("tool") + jsWS + `*[:=]` + jsWS + `*)?(` +
			joinFolded("read", "write", "edit", "strreplace", "search_replace", "grep", "bash", "shell", "glob", "list") +
			`)\b[^\n]*`,
	)
	readPathRE = regexp.MustCompile(
		`\b(?:read|Read|cat|open)` + jsWS + `*(?:\(|` + "`" + `|'|")?` + jsWS +
			`*([A-Za-z0-9_./\\-]+\.[A-Za-z0-9]+)`,
	)
	editActionRE = regexp.MustCompile(
		`\b(?:` + joinFolded("write", "edit", "strreplace", "search_replace", "apply_patch", "ApplyPatch") + `)\b`,
	)
	assertionFailRE = regexp.MustCompile(
		`\b(?:` +
			asciiFold("AssertionError") + `|` +
			asciiFold("assert") + `(?:` + asciiFold("ion") + `)?` + jsWS + `+` + asciiFold("failed") + `|` +
			asciiFold("Expected") + `[:` + jsWS[1:len(jsWS)-1] + `]|` +
			asciiFold("Received") + `[:` + jsWS[1:len(jsWS)-1] + `]|` +
			asciiFold("toEqual") + `|` +
			asciiFold("toBe") + `|` +
			asciiFold("FAIL") + `(?:` + asciiFold("ED") + `)?` +
			`)\b`,
	)
	fileInTextRE = regexp.MustCompile(
		"`([^`\\n]+\\.[A-Za-z0-9]+)`" +
			`|(?:\A|[` + jsWS[1:len(jsWS)-1] + `("'[:\[])((?:[A-Za-z0-9_.-]+/)+[A-Za-z0-9_.-]+\.[A-Za-z0-9]+)`,
	)
	symbolInTextRE = regexp.MustCompile(
		`\b(?:function|class|const|let|var|type|interface|enum)` + jsWS +
			`+([A-Z][A-Za-z0-9_]{2,})\b|\b([A-Z][A-Za-z0-9_]{3,})\b(?:` +
			jsWS + `*\(|` + jsWS + `*is not|` + jsWS + `*does not)`,
	)
)

func asciiFold(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b >= 'A' && b <= 'Z' {
			lower := b + ('a' - 'A')
			out.WriteByte('[')
			out.WriteByte(lower)
			out.WriteByte(b)
			out.WriteByte(']')
		} else if b >= 'a' && b <= 'z' {
			out.WriteByte('[')
			out.WriteByte(b)
			out.WriteByte(b - ('a' - 'A'))
			out.WriteByte(']')
		} else {
			out.WriteString(regexp.QuoteMeta(string(b)))
		}
	}
	return out.String()
}

func joinFolded(values ...string) string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = asciiFold(value)
	}
	return strings.Join(out, "|")
}

func jsLineStarts(text string) []int {
	starts := []int{0}
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 {
			starts = append(starts, i+size)
		}
		i += size
	}
	return starts
}

func extractToolActions(transcript string) []string {
	actions := []string{}
	lastIndex := 0
	for _, start := range jsLineStarts(transcript) {
		if start < lastIndex {
			continue
		}
		match := toolActionLineRE.FindStringSubmatchIndex(transcript[start:])
		if match == nil {
			continue
		}
		raw := transcript[start+match[0] : start+match[1]]
		actions = append(actions, collapseJSWhitespace(jsLowerCase(jscompat.Trim(raw))))
		lastIndex = start + match[1]
	}
	return actions
}

func detectLoop(transcript string) []string {
	signals := []string{}
	if transcript == "" {
		return signals
	}

	actions := extractToolActions(transcript)
	streak := 1
	for i := 1; i < len(actions); i++ {
		if actions[i] == actions[i-1] {
			streak++
			if streak >= loopIdenticalStreak {
				signals = append(signals,
					"identical consecutive tool action repeated "+
						jscompat.FormatNumber(float64(streak))+"×: "+
						utf16SliceTo(actions[i], 80),
				)
				break
			}
		} else {
			streak = 1
		}
	}

	counts := map[string]int{}
	order := []string{}
	for _, match := range readPathRE.FindAllStringSubmatch(transcript, -1) {
		key := strings.ToLower(strings.ReplaceAll(match[1], `\`, "/"))
		if _, ok := counts[key]; !ok {
			order = append(order, key)
		}
		counts[key]++
	}
	for _, file := range order {
		if count := counts[file]; count >= loopSameFileReads {
			signals = append(signals,
				"repeated reads of the same file ("+jscompat.FormatNumber(float64(count))+"×): "+file,
			)
		}
	}
	return signals
}

func extractFiles(text string) []string {
	out := []string{}
	for _, match := range fileInTextRE.FindAllStringSubmatch(text, -1) {
		raw := match[1]
		if raw == "" {
			raw = match[2]
		}
		raw = jscompat.Trim(raw)
		if raw != "" {
			out = append(out, strings.ReplaceAll(raw, `\`, "/"))
		}
	}
	return out
}

func extractSymbols(text string) []string {
	out := []string{}
	for _, match := range symbolInTextRE.FindAllStringSubmatch(text, -1) {
		symbol := match[1]
		if symbol == "" {
			symbol = match[2]
		}
		if symbol != "" {
			out = append(out, symbol)
		}
	}
	return out
}

func basenameLower(path string) string {
	normalized := strings.ReplaceAll(path, `\`, "/")
	if i := strings.LastIndex(normalized, "/"); i >= 0 {
		normalized = normalized[i+1:]
	}
	return jsLowerCase(normalized)
}

func transcriptMentionsFile(transcript, file string) bool {
	lowerTranscript := jsLowerCase(transcript)
	full := jsLowerCase(file)
	base := basenameLower(file)
	return strings.Contains(lowerTranscript, full) || (base != "" && strings.Contains(lowerTranscript, base))
}

func detectLocalization(transcript, testOutput string) []string {
	signals := []string{}
	if testOutput == "" {
		return signals
	}

	missingFiles := []string{}
	for _, file := range extractFiles(testOutput) {
		if !transcriptMentionsFile(transcript, file) {
			missingFiles = append(missingFiles, file)
		}
	}
	seen := map[string]bool{}
	for _, file := range missingFiles {
		key := basenameLower(file)
		if seen[key] {
			continue
		}
		seen[key] = true
		signals = append(signals, "test output references file never mentioned in transcript: "+file)
	}

	for _, symbol := range extractSymbols(testOutput) {
		if !strings.Contains(transcript, symbol) {
			signals = append(signals, "test output references symbol never mentioned in transcript: "+symbol)
		}
	}
	return signals
}

func countEdits(transcript string) int {
	return len(editActionRE.FindAllStringIndex(transcript, -1))
}

func hasFailingAssertions(testOutput string) bool {
	return testOutput != "" && assertionFailRE.MatchString(testOutput)
}

func detectTooHard(transcript, testOutput string, turns float64) []string {
	signals := []string{}
	if turns < tooHardMinTurns {
		return signals
	}
	edits := countEdits(transcript)
	if edits < tooHardMinEdits || !hasFailingAssertions(testOutput) {
		return signals
	}
	return append(signals,
		"many turns ("+jscompat.FormatNumber(turns)+") with many edits ("+
			jscompat.FormatNumber(float64(edits))+") and still-failing assertions",
	)
}

func detectSpecMisread(transcript, testOutput string) []string {
	signals := []string{}
	if testOutput == "" || !hasFailingAssertions(testOutput) {
		return signals
	}
	files := extractFiles(testOutput)
	symbols := extractSymbols(testOutput)
	if len(files) == 0 && len(symbols) == 0 {
		return signals
	}

	allFilesMentioned := len(files) > 0
	for _, file := range files {
		if !transcriptMentionsFile(transcript, file) {
			allFilesMentioned = false
			break
		}
	}
	allSymbolsMentioned := len(symbols) > 0
	for _, symbol := range symbols {
		if !strings.Contains(transcript, symbol) {
			allSymbolsMentioned = false
			break
		}
	}
	if allFilesMentioned || allSymbolsMentioned {
		signals = append(signals,
			"failing assertions on files/symbols the agent already touched (likely spec misread)",
		)
	}
	return signals
}

// ClassifyFailure deterministically selects the first matching diagnosis in
// env-flaky, loop, too-hard, localization, spec-misread, unknown order.
func ClassifyFailure(input FailureTriageInput) FailureTriageResult {
	transcript := input.TranscriptTail
	testOutput := ""
	if input.TestOutput != nil {
		testOutput = *input.TestOutput
	}

	envSignals := collectEnvSignals(transcript, testOutput)
	if len(envSignals) > 0 {
		return FailureTriageResult{
			Diagnosis: DiagnosisEnvFlaky, Confidence: envFlakyConfidence,
			Remedy: RemedyRerunTest, Signals: envSignals,
		}
	}

	loopSignals := detectLoop(transcript)
	if len(loopSignals) > 0 {
		return FailureTriageResult{
			Diagnosis: DiagnosisLoop, Confidence: loopConfidence,
			Remedy: RemedyFreshContextRetry, Signals: loopSignals,
		}
	}

	tooHardSignals := detectTooHard(transcript, testOutput, input.Turns)
	if len(tooHardSignals) > 0 {
		escalate := input.RepairRounds >= tooHardEscalateRepairRounds
		remedy := RemedySplit
		if escalate {
			remedy = RemedyEscalate
			tooHardSignals = append(tooHardSignals,
				"repairRounds already ≥ "+jscompat.FormatNumber(tooHardEscalateRepairRounds)+" → escalate",
			)
		}
		return FailureTriageResult{
			Diagnosis: DiagnosisTooHard, Confidence: tooHardConfidence,
			Remedy: remedy, Signals: tooHardSignals,
		}
	}

	localizationSignals := detectLocalization(transcript, testOutput)
	if len(localizationSignals) > 0 {
		return FailureTriageResult{
			Diagnosis: DiagnosisLocalization, Confidence: localizationConfidence,
			Remedy: RemedyAddContextRetry, Signals: localizationSignals,
		}
	}

	specSignals := detectSpecMisread(transcript, testOutput)
	if len(specSignals) > 0 {
		return FailureTriageResult{
			Diagnosis: DiagnosisSpecMisread, Confidence: specMisreadConfidence,
			Remedy: RemedyFreshContextRetry, Signals: specSignals,
		}
	}

	signals := []string{"no strong heuristic matched"}
	if input.ToolErrors > 0 {
		signals = append(signals, "toolErrors="+jscompat.FormatNumber(input.ToolErrors)+" (unclassified)")
	}
	if transcript == "" && testOutput == "" {
		signals = append(signals, "empty transcript and test output")
	}
	return FailureTriageResult{
		Diagnosis: DiagnosisUnknown, Confidence: unknownConfidence,
		Remedy: RemedyFreshContextRetry, Signals: signals,
	}
}

func collapseJSWhitespace(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	inWhitespace := false
	for _, r := range s {
		if isJSWhitespace(r) {
			if !inWhitespace {
				out.WriteByte(' ')
			}
			inWhitespace = true
			continue
		}
		inWhitespace = false
		out.WriteRune(r)
	}
	return out.String()
}

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func jsLowerCase(s string) string {
	runes := []rune(s)
	var out strings.Builder
	out.Grow(len(s))
	for i, r := range runes {
		switch {
		case r == 0x0130:
			out.WriteString("i\u0307")
		case r == 0x03a3 && finalSigma(runes, i):
			out.WriteRune(0x03c2)
		default:
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
}

func finalSigma(runes []rune, i int) bool {
	j := i - 1
	for j >= 0 && caseIgnorable(runes[j]) {
		j--
	}
	if j < 0 || !cased(runes[j]) {
		return false
	}
	k := i + 1
	for k < len(runes) && caseIgnorable(runes[k]) {
		k++
	}
	return k >= len(runes) || !cased(runes[k])
}

func cased(r rune) bool {
	return unicode.IsLower(r) || unicode.IsUpper(r) ||
		unicode.Is(unicode.Lt, r) ||
		unicode.Is(unicode.Other_Lowercase, r) ||
		unicode.Is(unicode.Other_Uppercase, r)
}

func caseIgnorable(r rune) bool {
	switch r {
	case 0x0027, 0x002e, 0x003a, 0x00b7, 0x0387, 0x055f, 0x05f4,
		0x2018, 0x2019, 0x2024, 0x2027, 0xfe13, 0xfe52, 0xfe55,
		0xff07, 0xff0e, 0xff1a:
		return true
	}
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) ||
		unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Lm, r) ||
		unicode.Is(unicode.Sk, r)
}

func decodeWTF8(s string, i int) (rune, int) {
	b := s[i]
	switch {
	case b < 0x80:
		return rune(b), 1
	case b&0xe0 == 0xc0:
		if i+1 < len(s) && s[i+1]&0xc0 == 0x80 {
			cp := rune(b&0x1f)<<6 | rune(s[i+1]&0x3f)
			if cp >= 0x80 {
				return cp, 2
			}
		}
	case b&0xf0 == 0xe0:
		if i+2 < len(s) && s[i+1]&0xc0 == 0x80 && s[i+2]&0xc0 == 0x80 {
			cp := rune(b&0x0f)<<12 | rune(s[i+1]&0x3f)<<6 | rune(s[i+2]&0x3f)
			if cp >= 0x800 {
				return cp, 3
			}
		}
	case b&0xf8 == 0xf0:
		if i+3 < len(s) && s[i+1]&0xc0 == 0x80 && s[i+2]&0xc0 == 0x80 && s[i+3]&0xc0 == 0x80 {
			cp := rune(b&0x07)<<18 | rune(s[i+1]&0x3f)<<12 | rune(s[i+2]&0x3f)<<6 | rune(s[i+3]&0x3f)
			if cp >= 0x10000 && cp <= 0x10ffff {
				return cp, 4
			}
		}
	}
	return utf8.RuneError, 1
}

func appendWTF8(dst []byte, cp rune) []byte {
	if cp >= 0xd800 && cp <= 0xdfff {
		return append(dst,
			byte(0xe0|cp>>12),
			byte(0x80|(cp>>6)&0x3f),
			byte(0x80|cp&0x3f),
		)
	}
	return utf8.AppendRune(dst, cp)
}

func utf16SliceTo(s string, n int) string {
	if n <= 0 {
		return ""
	}
	units := 0
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		width := 1
		if cp >= 0x10000 {
			width = 2
		}
		if units+width > n {
			high := rune(0xd800 + ((cp - 0x10000) >> 10))
			return s[:i] + string(appendWTF8(nil, high))
		}
		units += width
		i += size
		if units == n {
			return s[:i]
		}
	}
	return s
}

func appendJSQuoted(dst []byte, s string) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		i += size
		switch {
		case cp == '"':
			dst = append(dst, '\\', '"')
		case cp == '\\':
			dst = append(dst, '\\', '\\')
		case cp == '\b':
			dst = append(dst, '\\', 'b')
		case cp == '\f':
			dst = append(dst, '\\', 'f')
		case cp == '\n':
			dst = append(dst, '\\', 'n')
		case cp == '\r':
			dst = append(dst, '\\', 'r')
		case cp == '\t':
			dst = append(dst, '\\', 't')
		case cp < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hex[cp>>4], hex[cp&0xf])
		case cp >= 0xd800 && cp <= 0xdfff:
			dst = append(dst, '\\', 'u',
				hex[(cp>>12)&0xf],
				hex[(cp>>8)&0xf],
				hex[(cp>>4)&0xf],
				hex[cp&0xf],
			)
		default:
			dst = append(dst, s[i-size:i]...)
		}
	}
	return append(dst, '"')
}
