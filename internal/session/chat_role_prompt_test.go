package session

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// THERE IS ONE WAY TO PUT WORK OUT UNDER THE BELT. A conversation whose
// hand-offs are runs in the plan store carries `propose_task` and no
// `quick_task`, and its page says so in one paragraph: several proposals in one
// message are how it works in parallel, each joins the live run, and a question
// about running work is answered from the run's rows. The belt and the page are
// asked together, because a page naming a verb the belt does not carry is the
// defect beltfacts.go exists to prevent.
//
// THE RULES ARE A BELT FACT, NEVER BYTES OF system.md: inside it they sat in
// every request's fixed prefix and broke the frontier page's identity with the
// composed page.
func TestTheConversationUnderTheBeltHasOneWayToPutWorkOut(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

	t.Setenv("CODEAF_TASK_BELT", "bash")
	registerBeltRunEngine(t, newBeltRunDouble("unused"))
	agent, _ := newTestAgent(t, beltRunCompleter{text: "unused"}, func(config *Config) {
		config.Workspace = t.TempDir()
	})
	belt := beltNames(agent)
	if !slices.Contains(belt, "propose_task") {
		t.Fatalf("the conversation under the belt lost propose_task: %v", belt)
	}
	if slices.Contains(belt, quickTaskToolName) {
		t.Errorf("the conversation under the belt still carries %s, a second road outside the plan store", quickTaskToolName)
	}
	page := renderSystemAt(agent.config, now)
	if strings.Contains(page, quickTaskToolName) {
		t.Errorf("the page under the belt names %s, which is not on the belt", quickTaskToolName)
	}
	for _, rule := range []string{
		"ONE way to put more minds on the work",
		"SEVERAL PROPOSALS IN ONE MESSAGE ARE HOW YOU WORK IN\nPARALLEL",
		"joins the same run",
		"`depends_on`",
		"A finished task is asked about with `tasks` and is never redone or rechecked by hand.",
	} {
		if !strings.Contains(page, rule) {
			t.Errorf("the page under the belt does not say %q", rule)
		}
	}
	if !strings.Contains(tasksDescription, "A finished task is asked about with `tasks` and is never redone or rechecked by hand.") {
		t.Errorf("the tasks description lacks the finished-task rule: %q", tasksDescription)
	}
	for _, rule := range []string{"Hand off: launch a task.", "Add to: while one runs"} {
		if strings.Contains(systemPromptSource, rule) {
			t.Errorf("prompts/system.md carries %q; a rule that holds only under the belt is a belt fact", rule)
		}
	}
}

// The flag-off arm pins the conversation that ships today: both verbs, the two
// roads paragraph, and none of the belt's wording.
func TestTheConversationWithoutTheBeltKeepsBothVerbs(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

	t.Setenv("CODEAF_TASK_BELT", "node")
	registerBeltRunEngine(t, newBeltRunDouble("unused"))
	agent, _ := newTestAgent(t, beltRunCompleter{text: "unused"}, func(config *Config) {
		config.Workspace = t.TempDir()
	})
	belt := beltNames(agent)
	for _, verb := range []string{"propose_task", quickTaskToolName} {
		if !slices.Contains(belt, verb) {
			t.Errorf("the conversation without the belt lost %s: %v", verb, belt)
		}
	}
	page := renderSystemAt(agent.config, now)
	for _, existing := range []string{"two ways to put more minds on the work", "## Specialized Tools", "THERE IS NO PLANNER ON YOUR BELT"} {
		if !strings.Contains(page, existing) {
			t.Errorf("the conversation without the belt lost today's page text %q", existing)
		}
	}
	if strings.Contains(page, "ONE way to put more minds on the work") {
		t.Error("the conversation without the belt gained the belt's one-road paragraph")
	}
}

// With the belt asked for and no engine wired a proposal still runs on the
// session tree, so both verbs stay: the verb leaves the belt exactly where a
// proposal starts a run.
func TestTheBeltWithNoEngineKeepsBothVerbs(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	registerBeltRunEngine(t, nil)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "unused"}, func(config *Config) {
		config.Workspace = t.TempDir()
	})
	if belt := beltNames(agent); !slices.Contains(belt, quickTaskToolName) {
		t.Errorf("with no run engine a proposal is a session-tree task, and %s left the belt anyway: %v", quickTaskToolName, belt)
	}
}
