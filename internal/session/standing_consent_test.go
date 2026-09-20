package session

// WHAT A FIRING MAY DO WHEN NOBODY IS THERE.
//
// A firing is the person's own work done while they are asleep, and the gate it
// runs behind is the one a task node runs behind: allow everything except
// approval's critical floor. It inherited the conversation's policy instead,
// which on a fresh install prompts a human, while also being told it was inside
// a task where no human exists. Every decision came back as the engine's
// in-task refusal, saying the default needed a person and naming that there was
// nobody to ask; the read-only lift was all that survived, and a real watch
// fired thirty-nine times over four days, spent real money and did nothing.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/standing"
)

func TestAFiringRunsAShellCallItsConversationWouldHaveAskedAbout(t *testing.T) {
	// The parent is a fresh install: it asks a person about everything.
	parent := Config{ApprovalPolicy: promptAll(), AskConsent: true}
	item := standing.Item{ID: "item-morning", Words: "every morning, say what changed", Workspace: t.TempDir()}

	cfg, err := standingRunConfig(parent, item, t.TempDir()+"/run")
	if err != nil {
		t.Fatalf("standingRunConfig: %v", err)
	}
	// THE CALL IS THE PROOF, so it comes first: behind the gate the firing
	// really carries, the call runs rather than coming back as a refusal written
	// for a worker who has no colleague in the room.
	completer := &scriptedCompleter{steps: toolCallTurn("touch")}
	runs := make(chan string, 2)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = cfg.ApprovalPolicy
		config.AskConsent = cfg.AskConsent
		config.InTask = cfg.InTask
	})
	agent.tools = append(agent.tools, countingTool("touch", runs, nil))

	for _, event := range collect(t, mustSubmit(t, agent, "do the morning pass")) {
		if strings.Contains(event.Text, "refused in a task") {
			t.Fatalf("the firing refused its own call: %s", event.Text)
		}
	}
	if len(runs) == 0 {
		t.Fatal("the call never ran: a firing behind its own gate still could not run one command")
	}

	// And it is the node's gate and no wider: the same default, with nobody to
	// ask and the in-task words that make a prompt a refusal the firing reads.
	if cfg.ApprovalPolicy == nil || cfg.ApprovalPolicy.Default != approval.ActionAllow {
		t.Fatalf("a firing's policy is %+v, not the task node's allow", cfg.ApprovalPolicy)
	}
	if cfg.AskConsent || !cfg.InTask {
		t.Fatalf("a firing's AskConsent is %v and InTask is %v", cfg.AskConsent, cfg.InTask)
	}
}
