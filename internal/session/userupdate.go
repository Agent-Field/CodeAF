package session

import "strings"

// UserUpdatePrefix explicitly addresses an interim response to the person.
// Ordinary tool narration remains operational work; its wording is never used
// to guess the intended audience. The marker is retained in the model journal
// and removed only from the display projection.
const UserUpdatePrefix = "[update]"

// UserFacingUpdate recognizes only a leading protocol marker, never a quoted
// mention or a marker in the middle of an answer. Unmarked content is unchanged.
func UserFacingUpdate(text string) (string, bool) {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if !strings.HasPrefix(trimmed, UserUpdatePrefix) {
		return text, false
	}
	return strings.TrimLeft(strings.TrimPrefix(trimmed, UserUpdatePrefix), " \t\r\n"), true
}
