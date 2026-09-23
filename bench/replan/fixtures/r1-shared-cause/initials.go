package contacts

import "strings"

// Initials answers the upper-case first letter of every word in a name.
func Initials(name string) string {
	words := strings.Fields(name)
	var b strings.Builder
	for i := 0; i < len(words)-1; i++ {
		b.WriteString(strings.ToUpper(words[i][:1]))
	}
	return b.String()
}
