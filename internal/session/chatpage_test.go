package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// toolsFor is a catalog that says "chat/model" takes no tools and every other
// model does.
func toolsFor(model, parameter string) (bool, bool) {
	if parameter != "tools" {
		return true, true
	}
	return model != "chat/model", true
}

// A MODEL WITH NO TOOLS READS A PAGE WITHOUT THEM: no tool policy, no belt, no
// memory, and a plain sentence about what it cannot do.
func TestAModelWithNoToolsReadsTheChatPage(t *testing.T) {
	t.Setenv(promptProfileEnv, "")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		buildShippedConversation(t, c)
		c.System = ""
		c.Model = "chat/model"
		c.SupportsParameter = toolsFor
	})
	if !agent.config.promptProfile().chat() {
		t.Fatal("a model with no tools did not get the chat profile")
	}
	page := systemOf(agent)
	for _, want := range []string{"This model has no tools in codeaf", "# Messages from codeaf", "# Project"} {
		if !strings.Contains(page, want) {
			t.Fatalf("chat page is missing %q", want)
		}
	}
	for _, gone := range []string{"# Tool Policy", "# Putting more hands on the work", "# Session facts"} {
		if strings.Contains(page, gone) {
			t.Fatalf("chat page still carries %q", gone)
		}
	}
	if len(agent.beltDefinitions()) != 0 || agent.remembers() {
		t.Fatalf("belt %d tools, memory %v: want none and off", len(agent.beltDefinitions()), agent.remembers())
	}
	if full := len(renderSystem(Config{Workspace: agent.config.Workspace, Model: "tool/model"})); len(page)*3 > full {
		t.Fatalf("chat page is %d bytes against a working page of %d", len(page), full)
	}
}

// THE PAGE FOLLOWS THE MODEL: onto a model with no tools and back again, with
// an explicitly loaded capability returning with the tools.
func TestSwitchingToAModelWithNoToolsSwapsThePageAndBack(t *testing.T) {
	t.Setenv(promptProfileEnv, "")
	agent := leanShapedAgent(t)
	agent.config.SupportsParameter = toolsFor
	if _, failed := agent.loadCapability("tasks"); failed {
		t.Fatal("load tasks")
	}
	if err := agent.preparePromptProfile("chat/model"); err != nil {
		t.Fatal(err)
	}
	if !agent.config.promptProfile().chat() || len(agent.beltDefinitions()) != 0 {
		t.Fatalf("after moving to a model with no tools: profile %q, %d tools", agent.config.promptProfile(), len(agent.beltDefinitions()))
	}
	if !strings.Contains(systemOf(agent), "This model has no tools in codeaf") {
		t.Fatal("the page did not change with the model")
	}
	if err := agent.preparePromptProfile(agent.Model()); err != nil {
		t.Fatal(err)
	}
	if agent.config.promptProfile().chat() || !agent.hasTool("propose_task") {
		t.Fatal("coming back did not restore the working page and the loaded tasks group")
	}
	if strings.Contains(systemOf(agent), "This model has no tools in codeaf") {
		t.Fatal("the chat page stayed after the model changed back")
	}
}

// A PINNED PROFILE GIVES WAY TO A MODEL WITH NO TOOLS and comes back after it.
func TestAPinnedProfileGivesWayToAModelWithNoTools(t *testing.T) {
	t.Setenv(promptProfileEnv, "full")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		buildShippedConversation(t, c)
		c.System = ""
		c.Model = "tool/model"
		c.SupportsParameter = toolsFor
	})
	if agent.config.promptProfile() != profileFull {
		t.Fatalf("pinned profile = %q", agent.config.promptProfile())
	}
	if err := agent.preparePromptProfile("chat/model"); err != nil {
		t.Fatal(err)
	}
	if !agent.config.promptProfile().chat() {
		t.Fatal("a pin kept a page about tools on a model with none")
	}
	if err := agent.preparePromptProfile("tool/model"); err != nil {
		t.Fatal(err)
	}
	if agent.config.promptProfile() != profileFull {
		t.Fatalf("after leaving the model with no tools: %q, want the pinned full", agent.config.promptProfile())
	}
}

// A MODEL THE CATALOG DOES NOT KNOW KEEPS THE WORKING PAGE.
func TestAnUnknownModelKeepsTheWorkingPage(t *testing.T) {
	t.Setenv(promptProfileEnv, "")
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("ok"), nil
	}}}, func(c *Config) {
		buildShippedConversation(t, c)
		c.System = ""
		c.Model = "unknown/model"
		c.SupportsParameter = func(string, string) (bool, bool) { return false, false }
	})
	if agent.config.promptProfile().chat() || len(agent.beltDefinitions()) == 0 {
		t.Fatal("an undescribed model lost its tools")
	}
}

// systemOf is the system message the next request would carry.
func systemOf(agent *Agent) string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return messageContentText(agent.messages[0])
}
