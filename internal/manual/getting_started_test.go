package manual

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGettingStartedTopicsFitStandaloneReads(t *testing.T) {
	body, err := os.ReadFile("chat/getting-started.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range strings.Split(string(body), "\n## ")[1:] {
		heading, _, _ := strings.Cut(section, "\n")
		if size := utf8.RuneCountInString(section); size >= 2000 {
			t.Errorf("setup topic %q has %d characters, want under 2000 for a standalone read", heading, size)
		}
	}
}
