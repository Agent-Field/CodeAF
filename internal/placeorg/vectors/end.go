package vectors

import (
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

const (
	endUserRunes  = 400
	endReplyRunes = 300
	endBodyRunes  = 600
)

// EndText prepares the v2 tail from user messages in chronological order.
// The first message belongs to START, and a chat with no later user message
// has no END anchor even when its first reply has settled.
func EndText(title string, userMessages []string, lastAssistant string) string {
	if len(userMessages) <= 1 {
		return ""
	}
	later := userMessages[1:]
	later = later[max(0, len(later)-2):]
	body := tail(strings.Join(later, " "), endUserRunes)
	if lastAssistant != "" {
		body = tail(body+" "+tail(lastAssistant, endReplyRunes), endBodyRunes)
	}
	if strings.TrimSpace(body) == "" {
		return ""
	}
	return strings.TrimSpace(title + "\n" + body)
}

// tail counts characters rather than bytes so a multilingual message keeps
// valid text at the same bounds as the reference's Python slices.
func tail(text string, limit int) string {
	runes := []rune(text)
	return string(runes[max(0, len(runes)-limit):])
}

// OpenEnd opens the independent END cache. Consumers read by chat ID and
// HashText(chat.EndText), in the space returned by Vectorize, without fetching
// again. START retains its original filename and existing cache records.
func OpenEnd(dir string, space placeorg.SpaceID, dims int) (*Store, error) {
	return Open(filepath.Join(dir, "end"), space, dims)
}
