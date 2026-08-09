// This file ports review/observer config reads from swe-pro/src/config/*.ts at commit 3b25a1a.
package config

// This file ports swe-pro/src/config/review.ts:1-116 and models.ts:1-48 at
// commit 3b25a1a.

import (
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

var DefaultReviewSkipGlobs = []string{
	"*.md", "*.txt", "*.rst", "*.adoc", ".gitignore", ".gitattributes",
	"LICENSE", "LICENSE.*", "CHANGELOG", "CHANGELOG.*", "docs/**",
}

type Review struct {
	Enabled       bool
	RepairCap     int
	TimeoutMS     int64
	MaxToolCalls  int
	SkipGlobs     []string
	RepairTier    string
	ReviewerAgent string
	RepairAgent   string
	DryRun        bool
}

func jsParseInt10(raw string) (int64, bool) {
	raw = jscompat.Trim(raw)
	if raw == "" {
		return 0, false
	}
	sign := int64(1)
	index := 0
	if raw[0] == '+' || raw[0] == '-' {
		if raw[0] == '-' {
			sign = -1
		}
		index++
	}
	start := index
	for index < len(raw) && raw[index] >= '0' && raw[index] <= '9' {
		index++
	}
	if index == start {
		return 0, false
	}
	value, err := strconv.ParseInt(raw[start:index], 10, 64)
	return sign * value, err == nil
}

func intEnv(lookup Lookup, name string, fallback int64) int64 {
	raw, ok := lookup(name)
	if !ok || raw == "" {
		return fallback
	}
	value, valid := jsParseInt10(raw)
	if !valid || value <= 0 {
		return fallback
	}
	return value
}

func listEnv(lookup Lookup, name string, fallback []string) []string {
	raw, ok := lookup(name)
	if !ok || raw == "" {
		return append([]string(nil), fallback...)
	}
	out := []string{}
	for _, value := range strings.Split(raw, ",") {
		value = jscompat.Trim(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func stringEnv(lookup Lookup, name, fallback string) string {
	value, ok := lookup(name)
	if !ok {
		return fallback
	}
	return value
}

// ReviewConfig reads the review config at call time, making the Go equivalent
// testable without package-load environment state.
func ReviewConfig(lookup Lookup) Review {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	enabled, _ := lookup("CODEAF_REVIEW_ENABLED")
	tier := "high"
	if value, ok := lookup("CODEAF_REVIEW_REPAIR_TIER"); ok && value != "" {
		lower := strings.ToLower(value)
		if lower == "high" || lower == "low" {
			tier = lower
		}
	}
	dry, dryPresent := lookup("CODEAF_REVIEW_DRY_RUN")
	return Review{
		Enabled:       enabled != "0" && enabled != "false",
		RepairCap:     int(intEnv(lookup, "CODEAF_REVIEW_REPAIR_CAP", 3)),
		TimeoutMS:     intEnv(lookup, "CODEAF_REVIEW_TIMEOUT_MS", 1_800_000),
		MaxToolCalls:  int(intEnv(lookup, "CODEAF_REVIEW_MAX_TOOL_CALLS", 50)),
		SkipGlobs:     listEnv(lookup, "CODEAF_REVIEW_SKIP_GLOBS", DefaultReviewSkipGlobs),
		RepairTier:    tier,
		ReviewerAgent: stringEnv(lookup, "CODEAF_REVIEW_AGENT", "superpowers-code-reviewer"),
		RepairAgent:   stringEnv(lookup, "CODEAF_REPAIR_AGENT", "fixer"),
		DryRun:        dryPresent && dry == "1",
	}
}

var DefaultModelSortPriority = []string{"gpt-5", "claude-sonnet-4", "big-pickle", "gemini-3-pro"}
var DefaultSmallModelPriority = []string{
	"claude-haiku-4-5", "claude-haiku-4.5", "3-5-haiku", "3.5-haiku",
	"gemini-3-flash", "gemini-2.5-flash", "gpt-5-nano",
}

type Models struct {
	SortPriority  []string
	SmallPriority []string
}

func ModelConfig(lookup Lookup) Models {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	return Models{
		SortPriority:  listEnv(lookup, "CODEAF_MODEL_PRIORITY", DefaultModelSortPriority),
		SmallPriority: listEnv(lookup, "CODEAF_SMALL_MODEL_PRIORITY", DefaultSmallModelPriority),
	}
}

func globPattern(pattern string) string {
	var out strings.Builder
	for index := 0; index < len(pattern); {
		switch {
		case pattern[index] == '*' && index+1 < len(pattern) && pattern[index+1] == '*':
			out.WriteString(".*")
			index += 2
			if index < len(pattern) && pattern[index] == '/' {
				index++
			}
		case pattern[index] == '*':
			out.WriteString("[^/]*")
			index++
		case strings.ContainsRune(`.+?^${}()|[]\`, rune(pattern[index])):
			out.WriteByte('\\')
			out.WriteByte(pattern[index])
			index++
		default:
			out.WriteByte(pattern[index])
			index++
		}
	}
	return "^" + out.String() + "$"
}

// MatchesAnyGlob supports the deliberately small **/* matcher in review.ts.
func MatchesAnyGlob(file string, globs []string) bool {
	for _, pattern := range globs {
		matched, err := regexpMatch(globPattern(pattern), file)
		if err == nil && matched {
			return true
		}
	}
	return false
}

var regexpMatch = func(pattern, value string) (bool, error) {
	return regexp.MatchString(pattern, value)
}
