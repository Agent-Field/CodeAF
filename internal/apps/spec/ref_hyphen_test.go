package spec

import (
	"strings"
	"testing"
)

// The object is the one it-asset-register and dev-freelance-clients kept
// failing on: a list id may hold hyphens, so a reference to it may too.
func TestAReferenceToAHyphenatedListIsValid(t *testing.T) {
	files := Files{"screens/people.json": []byte(`{"screen":"people","title":"People","body":{"part":"Button","label":"Mark returned",
		"on":{"click":[{"records.update":{"ids":"$selection.held-devices.ids","set":{"holder":null,"status":"Returned"}}}]}}}`)}
	for _, issue := range Validate(files) {
		if issue.File == "screens/people.json" && strings.Contains(issue.Path, "/on/click") {
			t.Errorf("%s: %s", issue.Path, issue.Message)
		}
	}
}
