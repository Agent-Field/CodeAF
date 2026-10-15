package session

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/store"
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
	// THE PROFILE DIRECTORY IS HANDED OVER SO THE GATE CAN SEE THE WHOLE BELT.
	// The settings pair is conditional on it (tools_settings.go), and a gate
	// that built the belt without one would let a conditional tool ship with no
	// page — green, and wrong in exactly the way this test exists to catch.
	agent := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}}
	// AND THE SHELF IS REACHED THROUGH A STORE. `use_skill` is gated on a non-nil
	// Memory (tools_skill.go) — without one the verb never lands on the belt and
	// the gate would let it ship without a page. So a store is opened so the belt
	// is the same one a real conversation carries.
	db, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open gate store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	agent.config.Memory = db
	// AND THE AUTOMATIONS STORE IS OPENED, for the shelf's reason. `automation` is
	// on the belt only where a store sits behind it (automation_tool.go), and every
	// conversation door hands one in — so a gate that built the belt without one
	// would let the verb that sets up every reminder ship with no page.
	autos, err := automation.Open(filepath.Join(t.TempDir(), "automations"))
	if err != nil {
		t.Fatalf("open gate automations store: %v", err)
	}
	t.Cleanup(func() { _ = autos.Close() })
	agent.config.Automations = &Automations{Store: autos}
	agent.tools = agent.belt()
	tools := agent.offeredTools()
	if len(tools) == 0 {
		t.Fatal("the belt is empty")
	}
	carriesAutomation := false
	for _, tool := range tools {
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", tool.Name)
		}
		if tool.Name == "automation" {
			carriesAutomation = true
		}
	}
	if !carriesAutomation {
		t.Fatal("a conversation with an automations store was not given the automation verb, so this gate checked nothing about it")
	}
}

// AND A RUN OF AN AUTOMATION CARRIES A VERB NO CONVERSATION HAS.
//
// A piece of scheduled work says how it went with automation_report, which is on
// the belt of a run and of nothing else (automation_run.go) — the conversation's
// `automation` verb is taken away there, because nothing an automation does may
// arm another. The gate above builds a conversation's belt, so it never sees the
// report verb, and a person asking "how does a scheduled run say it is done" is
// asking in the words the tool is named in. So the belt is built the way the
// runner builds it — a task's posture, with the run's report door — and checked
// where the tool actually lives.
func TestTheManualMentionsTheAutomationRunsOwnTool(t *testing.T) {
	agent := &Agent{config: Config{
		Workspace:     t.TempDir(),
		ProfileDir:    t.TempDir(),
		InTask:        true,
		AutomationRun: &AutomationRun{},
	}}
	found := false
	agent.tools = agent.belt()
	for _, tool := range agent.offeredTools() {
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", tool.Name)
		}
		if tool.Name == "automation_report" {
			found = true
		}
	}
	if !found {
		t.Fatal("a run of an automation was not given the verb it reports how it went with")
	}
}

// AND THE BELT A CONVERSATION CARRIES IS NOT THE ONLY BELT IN THIS BINARY.
//
// A sub-harness design's thread is an agent with one tool the conversation does
// not have — revise_design, which is how the person's "make it also run the
// linter" reaches the designer (harness_task.go). It hangs off a door wired from
// the node rather than off anything in a plain Config, so the gate above builds a
// belt without it and would let it ship with no page: green, and wrong in exactly
// the way that gate exists to catch.
//
// It is a question people ask in the words the tool is named in — "can I change a
// design", "how do I revise it" — so the page has to be reachable, and the belt
// has to be checked where the tool actually lives.
func TestTheManualMentionsTheDesignThreadsOwnTool(t *testing.T) {
	agent := &Agent{config: Config{
		Workspace:    t.TempDir(),
		ProfileDir:   t.TempDir(),
		reviseDesign: func(string) error { return nil },
	}}
	found := false
	agent.tools = agent.belt()
	for _, tool := range agent.offeredTools() {
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("no chat manual page mentions the %s tool — add it to internal/manual/chat/", tool.Name)
		}
		if tool.Name == "revise_design" {
			found = true
		}
	}
	if !found {
		t.Fatal("an agent holding a design's revision door was not given the verb for it")
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
