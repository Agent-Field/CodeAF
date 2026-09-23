//go:build !windows

// Package evidenceharvest extracts the lines worth preserving from tool
// output before a compaction: command outcomes, exact error signatures and
// paths referenced across messages. A low-tier judge may pick the lines; the
// deterministic harvester is the fallback. All budgets count characters.
package evidenceharvest

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
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
	// Suite-abort signatures that are lower-case or toolchain-specific, so
	// the generic Error patterns above miss them.
	{regexp.MustCompile(`error: could not compile|\[build failed\]`), true},
	{regexp.MustCompile(`error during collection|ERROR collecting|ImportError while loading`), false},
	{regexp.MustCompile(`Transform failed with [0-9]+ error|Exception during run`), false},
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

// EvidenceJudgment is the narrow result SelectEvidence needs from a judge.
type EvidenceJudgment struct {
	Lines  []string
	Source EvidenceSource
}

// EvidenceJudge is the seam for a model-backed evidence selector.
type EvidenceJudge interface {
	JudgeEvidence(prompt string, language any) EvidenceJudgment
}

// EvidenceJudgeFunc adapts a function to EvidenceJudge.
type EvidenceJudgeFunc func(prompt string, language any) EvidenceJudgment

// JudgeEvidence implements EvidenceJudge.
func (f EvidenceJudgeFunc) JudgeEvidence(prompt string, language any) EvidenceJudgment {
	return f(prompt, language)
}

// SelectEvidenceOptions configures SelectEvidence. Nil MaxChars selects 4000.
type SelectEvidenceOptions struct {
	MaxChars *float64
	Judge    EvidenceJudge
}

// SelectedEvidence is SelectEvidence's result. Text is nil when nothing was
// selected.
type SelectedEvidence struct {
	Text   *string        `json:"text"`
	Source EvidenceSource `json:"source"`
}

// charCount is the length of s in characters (runes).
func charCount(s string) int { return utf8.RuneCountInString(s) }

// sliceChars returns the characters of s in [start, end), clamped to s.
func sliceChars(s string, start, end int) string {
	runes := []rune(s)
	start = max(0, min(start, len(runes)))
	end = max(start, min(end, len(runes)))
	return string(runes[start:end])
}

func truncate(line string) string {
	trimmed := strings.TrimSpace(line)
	if charCount(trimmed) > maxLineChars {
		return sliceChars(trimmed, 0, maxLineChars) + "…"
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
	block int
	seq   int
}

type pathRefs struct {
	path   string
	blocks map[int]struct{}
}

// HarvestEvidence extracts deterministic, verbatim evidence. Nil means nothing
// qualified.
func HarvestEvidence(blocks []string, maxChars ...float64) *string {
	budget := float64(defaultMaxChars)
	if len(maxChars) > 0 {
		budget = maxChars[0]
	}
	if len(blocks) == 0 || budget <= 0 {
		return nil
	}

	scored := []scoredLine{}
	seen := map[string]int{}
	pathIndex := map[string]int{}
	paths := []pathRefs{}
	seq := 0

	for blockIndex, block := range blocks {
		for _, raw := range strings.Split(block, "\n") {
			line := truncate(raw)
			if charCount(line) < 4 {
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
			if index, exists := seen[line]; exists {
				// Attribute repeated evidence to its newest occurrence.
				scored[index].block, scored[index].seq = blockIndex, seq
				seq++
				continue
			}
			seen[line] = len(scored)
			scored = append(scored, scoredLine{
				line: line, score: score, block: blockIndex, seq: seq,
			})
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
		return crossFiles[i].path < crossFiles[j].path
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
		// Prefer the newest command, but retain line order within that command.
		if scored[i].block != scored[j].block {
			return scored[i].block > scored[j].block
		}
		return scored[i].seq < scored[j].seq
	})

	fileBlock := []string{}
	if len(crossFiles) > 0 {
		fileBlock = append(fileBlock, "### Files referenced across multiple steps")
		for _, file := range crossFiles {
			fileBlock = append(fileBlock,
				"- "+file.path+" ("+strconv.Itoa(len(file.blocks))+" messages)")
		}
	}
	fileChars := 0
	for _, line := range fileBlock {
		fileChars += charCount(line) + 1
	}

	lines := []string{}
	used := 0
	evidenceBudget := budget - float64(fileChars)
	for _, entry := range scored {
		if float64(used+charCount(entry.line)+3) > evidenceBudget {
			continue
		}
		lines = append(lines, "- "+entry.line)
		used += charCount(entry.line) + 3
	}

	sections := []string{"## Preserved evidence (verbatim, extracted by senior-dev)"}
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
		return value != 0
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
	if charCount(corpus) > maxCorpusChars {
		corpus = sliceChars(corpus, charCount(corpus)-maxCorpusChars, charCount(corpus))
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
		if float64(used+charCount(line)+3) > maxChars {
			break
		}
		lines = append(lines, "- "+line)
		used += charCount(line) + 3
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
			if strings.TrimSpace(content) != "" {
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
		if strings.TrimSpace(text) != "" {
			blocks = append(blocks, text)
		}
	}
	return blocks
}
