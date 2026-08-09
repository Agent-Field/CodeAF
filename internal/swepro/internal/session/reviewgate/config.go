// Package reviewgate ports src/session/review-gate.ts:1-1937 and
// src/session/review-synthesizer.ts:1-161 from swe-pro at commit 3b25a1a.
//
// This file contains the narrow review configuration projection consumed by
// the gate. It reproduces src/config/review.ts:12-111 without creating a
// separate out-of-bundle config package.
package reviewgate

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type Config struct {
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

var defaultSkipGlobs = []string{
	"*.md",
	"*.txt",
	"*.rst",
	"*.adoc",
	".gitignore",
	".gitattributes",
	"LICENSE",
	"LICENSE.*",
	"CHANGELOG",
	"CHANGELOG.*",
	"docs/**",
}

var parseIntPrefix = regexp.MustCompile(`^[+-]?[0-9]+`)

func intEnv(name string, fallback int64) int64 {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	prefix := parseIntPrefix.FindString(jscompat.Trim(raw))
	if prefix == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func listEnv(name string, fallback []string) []string {
	raw := os.Getenv(name)
	if raw == "" {
		return append([]string(nil), fallback...)
	}
	out := []string{}
	for _, item := range strings.Split(raw, ",") {
		if trimmed := jscompat.Trim(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func enumEnv(name string, allowed []string, fallback string) string {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	lower := strings.ToLower(raw)
	for _, item := range allowed {
		if lower == item {
			return lower
		}
	}
	return fallback
}

func stringEnv(name, fallback string) string {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback
	}
	return value
}

// LoadConfig reads the same process-level knobs as ReviewConfig. Callers keep
// the resulting value for one gate instance, matching the TS module's
// import-time snapshot.
func LoadConfig() Config {
	enabled := os.Getenv("CODEAF_REVIEW_ENABLED")
	return Config{
		Enabled:       enabled != "0" && enabled != "false",
		RepairCap:     int(intEnv("CODEAF_REVIEW_REPAIR_CAP", 3)),
		TimeoutMS:     intEnv("CODEAF_REVIEW_TIMEOUT_MS", 1_800_000),
		MaxToolCalls:  int(intEnv("CODEAF_REVIEW_MAX_TOOL_CALLS", 50)),
		SkipGlobs:     listEnv("CODEAF_REVIEW_SKIP_GLOBS", defaultSkipGlobs),
		RepairTier:    enumEnv("CODEAF_REVIEW_REPAIR_TIER", []string{"high", "low"}, "high"),
		ReviewerAgent: stringEnv("CODEAF_REVIEW_AGENT", "superpowers-code-reviewer"),
		RepairAgent:   stringEnv("CODEAF_REPAIR_AGENT", "fixer"),
		DryRun:        os.Getenv("CODEAF_REVIEW_DRY_RUN") == "1",
	}
}

// MatchesAnyGlob implements the source's intentionally minimal glob language.
func MatchesAnyGlob(file string, globs []string) bool {
	for _, glob := range globs {
		if globMatch(file, glob) {
			return true
		}
	}
	return false
}

func globMatch(file, glob string) bool {
	var pattern strings.Builder
	pattern.WriteByte('^')
	for index := 0; index < len(glob); {
		switch {
		case glob[index] == '*' && index+1 < len(glob) && glob[index+1] == '*':
			pattern.WriteString(".*")
			index += 2
			if index < len(glob) && glob[index] == '/' {
				index++
			}
		case glob[index] == '*':
			pattern.WriteString("[^/]*")
			index++
		default:
			_, size := utf8.DecodeRuneInString(glob[index:])
			pattern.WriteString(regexp.QuoteMeta(glob[index : index+size]))
			index += size
		}
	}
	pattern.WriteByte('$')
	return regexp.MustCompile(pattern.String()).MatchString(file)
}
