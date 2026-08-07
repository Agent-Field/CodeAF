package tui

import (
	"os"
	"path/filepath"
	"strings"
)

type imageInputSupporter interface {
	ImageInputSupport() (model string, supported bool)
}

type roleImageInputSupporter interface {
	ImageInputSupportFor(role string) (model string, supported bool)
}

func (m *Model) imageInputSupport() (string, bool) {
	role := "talk"
	if m.boost != boostOff {
		role = "boost"
	}
	model := m.currentModel(role)
	if support, ok := m.commander.(roleImageInputSupporter); ok {
		return support.ImageInputSupportFor(role)
	}
	if support, ok := m.commander.(imageInputSupporter); ok {
		return support.ImageInputSupport()
	}
	return model, false
}

func (m *Model) captureImageAttachments() {
	cleaned, found := detectImageAttachments(m.input.Value())
	if len(found) == 0 {
		return
	}
	seen := make(map[string]bool, len(m.attachments)+len(found))
	for _, path := range m.attachments {
		seen[path] = true
	}
	for _, path := range found {
		if !seen[path] {
			m.attachments = append(m.attachments, path)
			seen[path] = true
		}
	}
	m.input.SetValue(cleaned)
}

func (m *Model) removeAttachment(index int) {
	if index < 0 || index >= len(m.attachments) {
		return
	}
	m.attachments = append(m.attachments[:index], m.attachments[index+1:]...)
	m.setSize(m.width, m.height)
}

type draftToken struct {
	start int
	end   int
	value string
}

// detectImageAttachments understands the shell-style quoting terminals use
// for drag-and-drop paths and removes only existing image files from the
// visible draft.
func detectImageAttachments(draft string) (string, []string) {
	tokens := draftTokens(draft)
	remove := make([]bool, len(tokens))
	var paths []string
	for index, token := range tokens {
		path := token.value
		if strings.HasPrefix(path, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
			}
		}
		absolute, err := filepath.Abs(path)
		if err != nil || !isImageExtension(absolute) {
			continue
		}
		info, err := os.Stat(absolute)
		if err != nil || info.IsDir() {
			continue
		}
		remove[index] = true
		paths = append(paths, absolute)
	}
	if len(paths) == 0 {
		return draft, nil
	}
	var kept []string
	for index, token := range tokens {
		if !remove[index] {
			kept = append(kept, draft[token.start:token.end])
		}
	}
	return strings.Join(kept, " "), paths
}

func draftTokens(draft string) []draftToken {
	var tokens []draftToken
	for index := 0; index < len(draft); {
		for index < len(draft) && (draft[index] == ' ' || draft[index] == '\t' || draft[index] == '\n') {
			index++
		}
		if index >= len(draft) {
			break
		}
		start := index
		quote := byte(0)
		var value strings.Builder
		for index < len(draft) {
			character := draft[index]
			if quote == 0 && (character == ' ' || character == '\t' || character == '\n') {
				break
			}
			if character == '\\' && index+1 < len(draft) {
				index++
				value.WriteByte(draft[index])
				index++
				continue
			}
			if character == '\'' || character == '"' {
				if quote == 0 {
					quote = character
					index++
					continue
				}
				if quote == character {
					quote = 0
					index++
					continue
				}
			}
			value.WriteByte(character)
			index++
		}
		tokens = append(tokens, draftToken{start: start, end: index, value: value.String()})
	}
	return tokens
}

func isImageExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return true
	default:
		return false
	}
}
