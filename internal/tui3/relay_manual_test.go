package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
)

func TestRelayWaitAndChoiceWordsAreInTheChatManual(t *testing.T) {
	page, ok := manual.Chat().Page("continuing-a-chat-on-another-computer")
	if !ok {
		t.Fatal("missing continue manual")
	}
	for _, words := range []string{
		"continuing <Y> next; <X> is still on its way here",
		"<holder> picked this chat up again; press enter to continue it here",
		"waiting for <host>; still checking",
		"<host> did not answer in " + machinesAskTimeout.String() + "; still trying",
		"<host> is unreachable; still trying",
		messageKept("<host>"),
		"your message is still in the box; press enter to send it",
	} {
		if !strings.Contains(page, words) {
			t.Errorf("manual is missing %q", words)
		}
	}
}
