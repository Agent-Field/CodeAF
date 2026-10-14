package sample

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

var names = []string{"Priya Shah", "Alex Chen", "Mona Patel", "Sam Rivera", "Jordan Lee", "Casey Morgan", "Noah Wilson", "Amara Okafor", "Sofia Garcia", "Theo Martin", "Avery Kim", "Leila Hassan"}

func label(display, name string) string {
	if strings.TrimSpace(display) != "" {
		return display
	}
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	return strings.Join(words, " ")
}

func hasWord(text string, words ...string) bool {
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) }) {
		for _, word := range words {
			if token == word {
				return true
			}
		}
	}
	return false
}

func personLike(text string) bool {
	return hasWord(text, "person", "people", "contact", "candidate", "member", "employee", "staff", "teacher", "instructor", "trainer", "student", "client", "customer", "patient", "volunteer", "attendee", "participant", "author", "assignee", "owner", "manager")
}

func personTitle(text string) bool {
	words := strings.Fields(label("", text))
	return len(words) > 0 && personLike(words[len(words)-1])
}

func personField(name, display string) bool {
	if personTitle(name) || personTitle(display) {
		return true
	}
	semantic := name + " " + display
	return hasWord(semantic, "name") && (personLike(semantic) || hasWord(semantic, "full"))
}

func personName(i int) string {
	parts := strings.Fields(names[i%len(names)])
	// Combining the same vocabulary keeps larger previews distinct without
	// turning people into numbered labels.
	last := strings.Fields(names[(i/len(names)+i)%len(names)])[1]
	if i < len(names)*len(names) {
		return parts[0] + " " + last
	}
	cycle := i/(len(names)*len(names)) - 1
	return fmt.Sprintf("%s %c.%c. %s", parts[0], 'A'+cycle/26, 'A'+cycle%26, last)
}

func textValue(typ, name string, schema *spec.RecordType, f *spec.Field, i int) string {
	semantic := name + " " + f.Label
	person := personName(i)
	if hasWord(semantic, "first", "given") && hasWord(semantic, "name") {
		return strings.Fields(person)[0]
	}
	if hasWord(semantic, "last", "family", "surname") && hasWord(semantic, "name", "surname") {
		return strings.Fields(person)[len(strings.Fields(person))-1]
	}
	if personField(name, f.Label) {
		return person
	}
	if hasWord(semantic, "sku", "code", "serial", "identifier") {
		return fmt.Sprintf("%s-%04d", strings.ToUpper(typ), i+1)
	}
	return fmt.Sprintf("%s %d", label(f.Label, name), i+1)
}
