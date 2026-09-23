//go:build !windows

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// SubstituteInput is the input to Substitute: the config text plus where it
// came from, which anchors relative {file:...} references.
type SubstituteInput struct {
	Text   string
	Path   string
	Dir    string
	Source string
	Lookup Lookup
}

var envToken = regexp.MustCompile(`\{env:([^}]+)\}`)
var fileToken = regexp.MustCompile(`\{file:[^}]+\}`)

// Substitute applies {env:VAR} and {file:path} substitutions. A file
// reference on a line that starts with // is left alone; a missing file is an
// error.
func Substitute(input SubstituteInput) (string, error) {
	lookup := input.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	text := envToken.ReplaceAllStringFunc(input.Text, func(token string) string {
		name := token[len("{env:") : len(token)-1]
		value, _ := lookup(name)
		return value
	})
	matches := fileToken.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text, nil
	}
	configDir := input.Dir
	if input.Path != "" {
		configDir = filepath.Dir(input.Path)
	}
	var out strings.Builder
	cursor := 0
	for _, match := range matches {
		token := text[match[0]:match[1]]
		out.WriteString(text[cursor:match[0]])
		lineStart := strings.LastIndex(text[:match[0]], "\n") + 1
		if strings.HasPrefix(strings.TrimLeftFunc(text[lineStart:match[0]], unicode.IsSpace), "//") {
			out.WriteString(token)
			cursor = match[1]
			continue
		}
		file := strings.TrimSuffix(strings.TrimPrefix(token, "{file:"), "}")
		if strings.HasPrefix(file, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				file = filepath.Join(home, file[2:])
			}
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(configDir, file)
		}
		file = filepath.Clean(file)
		data, err := os.ReadFile(file)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf(`bad file reference: %q %s does not exist`, token, file)
			}
			return "", fmt.Errorf(`bad file reference: %q`, token)
		}
		quoted, _ := jsonutil.Marshal(strings.TrimSpace(string(data)))
		if len(quoted) >= 2 {
			out.Write(quoted[1 : len(quoted)-1])
		}
		cursor = match[1]
	}
	out.WriteString(text[cursor:])
	return out.String(), nil
}
