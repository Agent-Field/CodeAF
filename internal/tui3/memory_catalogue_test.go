package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

func TestMemoryCatalogueDescribesTheBarePlaceAndQueriedTranscript(t *testing.T) {
	text, ok := manual.Chat().Page("commands")
	if !ok {
		t.Fatal("command catalogue missing")
	}
	if strings.Contains(text, "prints every memory into the conversation") {
		t.Fatal("catalogue still promises the retired bare print posture")
	}
	if !strings.Contains(text, "| `/memories` | — | — | opens the memory place |") || !strings.Contains(text, "prints matching memories into the conversation") {
		t.Fatal("catalogue does not explain both memory doors")
	}
}
