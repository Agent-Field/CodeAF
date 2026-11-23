package manual

import (
	"strings"
	"testing"
)

func TestMemoriesQuestionFindsTheMemoryTab(t *testing.T) {
	for _, s := range Chat().Search("how do I see my memories tab", DefaultResults) {
		if s.Page == "what-i-remember" && strings.Contains(s.Body, "**Memory** word on the tab bar") {
			return
		}
	}
	t.Fatal("memories question does not reach the Memory tab")
}
