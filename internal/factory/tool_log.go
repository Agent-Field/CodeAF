package factory

import (
	"os"
	"regexp"
	"strings"
)

// ToolCaptionPrefix distinguishes tool captions from prose inside the runner.
// It is consumed before a line is saved or reported.
const ToolCaptionPrefix = "\x00tool "

var toolHome = regexp.MustCompile(`(^|[\s"'=(:])/(?:Users|home)/[^/\s"';&|)]+`)

// ToolLogText makes a known tool caption readable before limiting its length.
// The home may belong to a remote runner rather than the factory serving it.
func ToolLogText(text, checkout string) string {
	if checkout != "" {
		text = strings.ReplaceAll(text, checkout+"/", "")
		text = replaceLogPath(text, checkout, ".")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/" {
		text = replaceLogPath(text, home, "~")
	}
	text = toolHome.ReplaceAllString(text, "${1}~")
	words := strings.SplitN(text, " ", 3)
	if len(words) >= 2 && words[0] == words[1] {
		text = strings.TrimPrefix(text, words[0]+" ")
	}
	runes := []rune(text)
	if len(runes) > 240 {
		text = string(runes[:239]) + "…"
	}
	return text
}

// LegacyToolLog recognizes only the duplicated tool captions emitted by older
// stages, never a person's words or an arbitrary thought with a path in it.
func LegacyToolLog(line LogLine) bool {
	if line.Tone != "thought" {
		return false
	}
	words := strings.SplitN(line.Text, " ", 3)
	if len(words) < 2 || words[0] != words[1] {
		return false
	}
	switch words[0] {
	case "bash", "read", "write", "edit", "glob", "grep", "web_search", "web_fetch":
		return true
	}
	return false
}

// replaceLogPath preserves adjacent path names that merely share a prefix.
func replaceLogPath(text, path, replacement string) string {
	pattern := regexp.MustCompile(`(^|[\s"'=(:])` + regexp.QuoteMeta(path) + `($|[/\s"';&|)])`)
	return pattern.ReplaceAllString(text, "${1}"+replacement+"${2}")
}
