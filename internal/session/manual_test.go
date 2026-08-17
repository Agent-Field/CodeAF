package session

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/manual"
)

// The belt half of the completeness gate (internal/tui3's manual_test.go is the
// command half, and states the law at length).
//
// A tool the model can call is a capability a person can ask about — "can you
// read a PDF", "can you search the web", "do you remember things" — and the
// answer comes out of the manual. A tool that lands without a page leaves the
// chat unable to say it has an ability it demonstrably has, so the build fails
// here instead.
func TestTheManualMentionsEveryToolOnTheBelt(t *testing.T) {
	agent := &Agent{config: Config{Workspace: t.TempDir()}}
	tools := agent.belt()
	if len(tools) == 0 {
		t.Fatal("the belt is empty")
	}
	for _, tool := range tools {
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", tool.Name)
		}
	}
}

// The manual tool answers out of the CHAT's pages and must never reach the
// resident's. Both corpora ship in this binary, and a chat that answered from
// the wrong one would describe a product the person is not using — fluently,
// which is what makes it dangerous.
func TestTheManualToolReadsTheChatCorpusAndNotTheResidents(t *testing.T) {
	chat := map[string]bool{}
	for _, name := range manual.Chat().Pages() {
		chat[name] = true
	}
	if len(chat) == 0 {
		t.Fatal("the chat corpus has no pages")
	}
	for _, name := range manual.Pages() {
		if chat[name] {
			t.Errorf("page %q is in both corpora; a page belongs to exactly one product", name)
		}
	}
	// And the tool itself: whatever it returns has to come from a chat page.
	for _, section := range manual.Chat().Search("what can you do", 4) {
		if !chat[section.Page] {
			t.Errorf("a chat manual search returned page %q, which is not a chat page", section.Page)
		}
	}
}
