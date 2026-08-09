// Package evidenceharvest ports src/session/evidence-harvest.ts:1-191 from
// swe-pro commit 3b25a1a. It deterministically preserves high-energy command
// outcomes, exact error signatures, and paths referenced across messages.
//
// low-judge.ts is outside this bundle. SelectEvidence therefore accepts the
// narrow EvidenceJudge interface: an integration supplies the already-judged
// line list and source, while this package owns the exact prompt construction,
// output budgeting, rendering, and deterministic fallback.
//
// Fidelity notes:
//   - All string lengths, truncation, and budgets count UTF-16 code units.
//   - Ties are stable. Cross-file ties use jscompat.LocaleCompare.
//   - The TS path regex only accepts forward slashes, ASCII path characters,
//     and lowercase extensions. Windows paths are intentionally not repaired.
package evidenceharvest

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

const (
	maxLineChars    = 300
	defaultMaxChars = 4000
	maxCorpusChars  = 60000
)

var outcomePatterns = []*regexp.Regexp{
	regexp.MustCompile(`exit(ed)?( with)?( code)? [0-9]+`),
	regexp.MustCompile(`\b[0-9]+ pass(ed|ing)?\b[^\n\r\x{2028}\x{2029}]*\b[0-9]+ fail`),
	regexp.MustCompile(`\b[0-9]+ fail(ed|ing|ures?)\b`),
	regexp.MustCompile(`\btests? (passed|failed)\b`),
}

var errorPatterns = []struct {
	re   *regexp.Regexp
	fold bool
}{
	{regexp.MustCompile(`\b[A-Z][a-zA-Z]*Error\b:?`), false},
	{regexp.MustCompile(`(^|[\x09-\x0d \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}])Error:[\x09-\x0d \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`), false},
	{regexp.MustCompile(`\bTraceback \(most recent call last\)`), false},
	{regexp.MustCompile(`\bpanic:[\x09-\x0d \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`), false},
	{regexp.MustCompile(`\bFAILED\b`), false},
	{regexp.MustCompile(`\bassertionerror\b|\bassert(ion)? failed\b`), true},
	{regexp.MustCompile(`\bENOENT\b|\bEACCES\b|\bECONNREFUSED\b|\bETIMEDOUT\b`), false},
	{regexp.MustCompile(`\berror TS[0-9]+\b`), false},
	{regexp.MustCompile(`\bnpm error\b|\bnpm ERR!`), false},
}

// Captures are: 1 boundary, 2 path, 3 optional relative prefix, 4 repeated
// directory segment, 5 optional line suffix. Group 2 is PATH_RE's match[1].
var pathRE = regexp.MustCompile(
	`(^|[\x09-\x0d \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}"'` + "`" + `(=])` +
		`((\.{0,2}/)?([A-Za-z0-9_.@-]+/)+[A-Za-z0-9_.@-]+\.[a-z]{1,10})(:[0-9]+)?`,
)

// Message is the minimal model-message surface textBlocksOf reads.
type Message struct {
	Content any `json:"content"`
}

// EvidenceSource reports whether the low-tier judgment or deterministic
// fallback supplied SelectEvidence's content.
type EvidenceSource string

const (
	SourceLLM      EvidenceSource = "llm"
	SourceFallback EvidenceSource = "fallback"
)

// EvidenceJudgment is the narrow result SelectEvidence needs from low-judge.
type EvidenceJudgment struct {
	Lines  []string
	Source EvidenceSource
}

// EvidenceJudge is the out-of-bundle low-judge seam.
type EvidenceJudge interface {
	JudgeEvidence(prompt string, language any) EvidenceJudgment
}

// EvidenceJudgeFunc adapts a function to EvidenceJudge.
type EvidenceJudgeFunc func(prompt string, language any) EvidenceJudgment

// JudgeEvidence implements EvidenceJudge.
func (f EvidenceJudgeFunc) JudgeEvidence(prompt string, language any) EvidenceJudgment {
	return f(prompt, language)
}

// SelectEvidenceOptions mirrors the TS maxChars option and carries the Go
// low-judge seam. Nil MaxChars selects 4000.
type SelectEvidenceOptions struct {
	MaxChars *float64
	Judge    EvidenceJudge
}

// SelectedEvidence mirrors selectEvidence's returned object. Text is nil for
// the TS null value.
type SelectedEvidence struct {
	Text   *string        `json:"text"`
	Source EvidenceSource `json:"source"`
}

func utf16Units(s string) []uint16 { return utf16.Encode([]rune(s)) }
func utf16Len(s string) int        { return len(utf16Units(s)) }

func sliceUTF16(s string, start, end int) string {
	units := utf16Units(s)
	if start < 0 {
		start = 0
	}
	if end > len(units) {
		end = len(units)
	}
	if start > end {
		start = end
	}
	return string(utf16.Decode(units[start:end]))
}

func truncate(line string) string {
	trimmed := jscompat.Trim(line)
	if utf16Len(trimmed) > maxLineChars {
		return sliceUTF16(trimmed, 0, maxLineChars) + "…"
	}
	return trimmed
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

func isOutcome(line string) bool {
	folded := asciiLower(line)
	for _, re := range outcomePatterns {
		if re.MatchString(folded) {
			return true
		}
	}
	return false
}

func isError(line string) bool {
	for _, pattern := range errorPatterns {
		scan := line
		if pattern.fold {
			scan = asciiLower(scan)
		}
		if pattern.re.MatchString(scan) {
			return true
		}
	}
	return false
}

type scoredLine struct {
	line  string
	score int
	seq   int
}

type pathRefs struct {
	path   string
	blocks map[int]struct{}
}

// HarvestEvidence extracts deterministic, verbatim evidence. Nil is TS null.
func HarvestEvidence(blocks []string, maxChars ...float64) *string {
	budget := float64(defaultMaxChars)
	if len(maxChars) > 0 {
		budget = maxChars[0]
	}
	if len(blocks) == 0 || budget <= 0 {
		return nil
	}

	scored := []scoredLine{}
	seen := map[string]struct{}{}
	pathIndex := map[string]int{}
	paths := []pathRefs{}
	seq := 0

	for blockIndex, block := range blocks {
		for _, raw := range strings.Split(block, "\n") {
			line := truncate(raw)
			if utf16Len(line) < 4 {
				continue
			}
			for _, match := range pathRE.FindAllStringSubmatch(raw, -1) {
				path := match[2]
				index, ok := pathIndex[path]
				if !ok {
					index = len(paths)
					pathIndex[path] = index
					paths = append(paths, pathRefs{path: path, blocks: map[int]struct{}{}})
				}
				paths[index].blocks[blockIndex] = struct{}{}
			}
			score := 0
			if isOutcome(line) {
				score += 3
			}
			if isError(line) {
				score += 2
			}
			if score == 0 {
				continue
			}
			if _, exists := seen[line]; exists {
				continue
			}
			seen[line] = struct{}{}
			scored = append(scored, scoredLine{line: line, score: score, seq: seq})
			seq++
		}
	}

	crossFiles := []pathRefs{}
	for _, item := range paths {
		if len(item.blocks) >= 2 {
			crossFiles = append(crossFiles, item)
		}
	}
	sort.SliceStable(crossFiles, func(i, j int) bool {
		if len(crossFiles[i].blocks) != len(crossFiles[j].blocks) {
			return len(crossFiles[i].blocks) > len(crossFiles[j].blocks)
		}
		return jscompat.LocaleCompare(crossFiles[i].path, crossFiles[j].path) < 0
	})
	if len(crossFiles) > 20 {
		crossFiles = crossFiles[:20]
	}

	if len(scored) == 0 && len(crossFiles) == 0 {
		return nil
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].seq < scored[j].seq
	})

	fileBlock := []string{}
	if len(crossFiles) > 0 {
		fileBlock = append(fileBlock, "### Files referenced across multiple steps")
		for _, file := range crossFiles {
			fileBlock = append(fileBlock,
				"- "+file.path+" ("+jscompat.FormatNumber(float64(len(file.blocks)))+" messages)")
		}
	}
	fileChars := 0
	for _, line := range fileBlock {
		fileChars += utf16Len(line) + 1
	}

	lines := []string{}
	used := 0
	evidenceBudget := budget - float64(fileChars)
	for _, entry := range scored {
		if float64(used+utf16Len(entry.line)+3) > evidenceBudget {
			continue
		}
		lines = append(lines, "- "+entry.line)
		used += utf16Len(entry.line) + 3
	}

	sections := []string{"## Preserved evidence (verbatim, harness-extracted)"}
	if len(lines) > 0 {
		sections = append(sections, "### Command outcomes & errors")
		sections = append(sections, lines...)
	}
	sections = append(sections, fileBlock...)
	if len(sections) == 1 {
		return nil
	}
	text := strings.Join(sections, "\n")
	return &text
}

func optionMaxChars(opts *SelectEvidenceOptions) float64 {
	if opts == nil || opts.MaxChars == nil {
		return defaultMaxChars
	}
	return *opts.MaxChars
}

func languageTruthy(language any) bool {
	switch value := language.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		return value != ""
	case float64:
		return jscompat.Truthy(value)
	case float32:
		return value != 0
	case int:
		return value != 0
	default:
		return true
	}
}

func evidencePrompt(corpus string) string {
	return strings.Join([]string{
		"The transcript region below is about to be replaced by a summary.",
		"Select the LOAD-BEARING lines that must survive VERBATIM because a",
		"paraphrase would lose their value: commands with their outcomes/exit",
		"codes, exact error messages and signatures, and file paths central to",
		"the work. Copy each selected line EXACTLY as it appears (you may",
		"truncate a line after 300 characters). Skip conversational prose,",
		"reasoning, and anything a summary can safely restate. Max 25 lines;",
		"return an empty list if nothing qualifies.",
		"",
		"--- TRANSCRIPT REGION ---",
		corpus,
	}, "\n")
}

// SelectEvidence renders low-tier-selected evidence or falls back to the
// deterministic regex harvester when the judge is unavailable/fails.
func SelectEvidence(blocks []string, language any, opts *SelectEvidenceOptions) SelectedEvidence {
	maxChars := optionMaxChars(opts)
	if len(blocks) == 0 {
		return SelectedEvidence{Text: nil, Source: SourceFallback}
	}

	corpus := strings.Join(blocks, "\n---\n")
	if utf16Len(corpus) > maxCorpusChars {
		corpus = sliceUTF16(corpus, 0, maxCorpusChars)
	}

	if opts == nil || opts.Judge == nil || !languageTruthy(language) {
		return SelectedEvidence{Text: HarvestEvidence(blocks, maxChars), Source: SourceFallback}
	}
	judged := opts.Judge.JudgeEvidence(evidencePrompt(corpus), language)
	if judged.Source != SourceLLM {
		return SelectedEvidence{Text: HarvestEvidence(blocks, maxChars), Source: SourceFallback}
	}

	lines := []string{}
	used := 0
	for _, raw := range judged.Lines {
		line := truncate(raw)
		if float64(used+utf16Len(line)+3) > maxChars {
			break
		}
		lines = append(lines, "- "+line)
		used += utf16Len(line) + 3
	}
	if len(lines) == 0 {
		return SelectedEvidence{Text: nil, Source: SourceLLM}
	}
	text := strings.Join(
		append([]string{"## Preserved evidence (verbatim, low-tier selected)"}, lines...),
		"\n",
	)
	return SelectedEvidence{Text: &text, Source: SourceLLM}
}

// TextBlocksOf flattens string content and typed text/output parts.
func TextBlocksOf(messages []Message) []string {
	blocks := []string{}
	for _, message := range messages {
		if content, ok := message.Content.(string); ok {
			if jscompat.Trim(content) != "" {
				blocks = append(blocks, content)
			}
			continue
		}
		parts, ok := message.Content.([]any)
		if !ok {
			continue
		}
		texts := []string{}
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok {
				if text != "" {
					texts = append(texts, text)
				}
				continue
			}
			output, exists := part["output"]
			if !exists {
				continue
			}
			if text, ok := output.(string); ok {
				if text != "" {
					texts = append(texts, text)
				}
				continue
			}
			if object, ok := output.(map[string]any); ok {
				if text, ok := object["value"].(string); ok && text != "" {
					texts = append(texts, text)
				}
			}
		}
		text := strings.Join(texts, "\n")
		if jscompat.Trim(text) != "" {
			blocks = append(blocks, text)
		}
	}
	return blocks
}
