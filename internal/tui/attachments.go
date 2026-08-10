package tui

import (
	"os"
	"path/filepath"
	"strings"
)

func (m *Model) imageInputSupport() (string, bool) {
	role := "talk"
	if m.boost != boostOff {
		role = "boost"
	}
	model := m.currentModel(role)
	if m.commander == nil {
		return model, false
	}
	return m.commander.ImageInputSupportFor(role)
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

// keepAttachment copies an attached file somewhere durable and answers with the
// reference the message will carry. The composer keeps holding the person's own
// path until the message is sent — the chips should say what they attached —
// and the durable record is a copy from that moment on.
//
// It degrades to the path itself. A surface with no commander, and one whose
// commander cannot place a copy, behaves exactly as it did before copies
// existed rather than losing the attachment.
func (m *Model) keepAttachment(path string) string {
	if m.commander == nil {
		return path
	}
	reference, err := m.commander.KeepAttachment(path)
	if err != nil || strings.TrimSpace(reference) == "" {
		return path
	}
	return reference
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
// for drag-and-drop paths and removes existing image and PDF inputs from the
// visible draft. The historical name remains internal compatibility; submit
// keeps documents distinct from image model content.
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
		if err != nil || !isAttachmentExtension(absolute) {
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

func isAttachmentExtension(path string) bool {
	return isImageExtension(path) || isDocumentAttachment(path)
}

// The manual has always promised that documents are read with a cost ladder,
// and named all three; the executor has always handled all three. Only the
// composer disagreed, and it disagreed silently — a .docx dragged in stayed in
// the draft as literal text and went to the head as prose.
func isDocumentAttachment(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf", ".docx", ".pptx":
		return true
	default:
		return false
	}
}

func hasImageAttachments(paths []string) bool {
	for _, path := range paths {
		if isImageExtension(path) {
			return true
		}
	}
	return false
}

func attachmentGlyph(path string) string {
	if isDocumentAttachment(path) {
		return "▤"
	}
	return "⌾"
}
