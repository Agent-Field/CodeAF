package cellstore

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The manual tells a person how many files a running codeaf remembers as
// checked for secrets; that sentence is this constant, spelled for people.
func TestTheManualStatesTheCleanFileBound(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "manual", "chat", "continuing-a-chat-on-another-computer.md"))
	if err != nil {
		t.Fatal(err)
	}
	digits := strconv.Itoa(cleanFilesMost)
	var spelled strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			spelled.WriteByte(',')
		}
		spelled.WriteRune(d)
	}
	if !strings.Contains(string(raw), "remembers at most "+spelled.String()+" files") {
		t.Fatalf("the manual's move page does not say it remembers at most %s files", spelled.String())
	}
}
