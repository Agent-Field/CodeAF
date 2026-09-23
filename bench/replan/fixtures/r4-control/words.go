// Package words counts the words in a piece of text.
package words

import "strings"

// Count answers how many words the text holds, a word being any run of
// characters between spaces.
func Count(text string) int {
	return len(strings.Split(text, " "))
}
