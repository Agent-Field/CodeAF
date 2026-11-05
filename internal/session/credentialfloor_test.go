package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
)

// The credential floor names the profile variable by hand, because
// internal/approval cannot import internal/config. This is what keeps them one.
func TestTheFloorProtectsTheProfileConfigNames(t *testing.T) {
	if config.ProfileDirEnv != "CODEAF_PROFILE_DIR" {
		t.Fatalf("the profile variable is now %s; internal/approval's credentials.go still reads the old name", config.ProfileDirEnv)
	}
}

func credentialCall(id, tool, args string) ai.ToolCall {
	return ai.ToolCall{ID: id, Type: "function", Function: ai.ToolCallFunction{Name: tool, Arguments: args}}
}

func bashJSON(t *testing.T, command string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The incident's mode and the three others. The tool must not run, and the answer
// has to be the harness's so a worker that meets it is not called a repeater.
func TestAnAgentCannotReadACredentialInAnyMode(t *testing.T) {
	work := t.TempDir()
	mark := filepath.Join(work, "ran")
	home := filepath.Join(work, "home")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".aws"), 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(home, ".aws", "credentials")
	if err := os.WriteFile(secret, []byte("not-a-real-secret-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, configure := range map[string]func(*Config){
		"no policy": func(c *Config) { c.ApprovalPolicy = nil },
		"allow":     func(c *Config) { c.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow} },
		"prompt": func(c *Config) {
			c.ApprovalPolicy = &approval.Policy{Default: approval.ActionPrompt}
			c.AskConsent = true
		},
		"named allow": func(c *Config) {
			c.ApprovalPolicy = &approval.Policy{Tools: map[string]approval.Action{"bash": "allow", "read": "allow"}}
		},
		"in a task": func(c *Config) {
			c.InTask = true
			c.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		},
	} {
		t.Run(name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, configure)
			agent.rememberConsent("bash", true)
			agent.rememberConsent("read", true)
			episode := agent.newEpisode()
			for _, call := range []ai.ToolCall{
				credentialCall("c1", "bash", bashJSON(t, "gh auth token; touch "+mark)),
				credentialCall("c2", "bash", bashJSON(t, "cat ~/.aws/credentials; touch "+mark)),
				credentialCall("c3", "read", `{"path":"`+secret+`"}`),
				credentialCall("c4", "grep", `{"pattern":"not-a-real","path":"`+secret+`"}`),
				// These two run inside the engine and no shell is involved.
				credentialCall("c5", "edit", `{"path":"`+secret+`","oldText":"not","newText":"yes"}`),
				credentialCall("c6", "write", `{"path":"`+secret+`","content":"overwritten"}`),
			} {
				result := agent.executeTool(context.Background(), episode, nil, call, "")
				if !result.isError || !strings.Contains(result.text, "Blocked") {
					t.Fatalf("%s answered %+v, want the refusal", call.Function.Arguments, result)
				}
				if !result.harness {
					t.Fatalf("%s was booked against the model", call.Function.Arguments)
				}
				if strings.Contains(result.text, "not-a-real-secret-value") {
					t.Fatal("the refusal carried the secret")
				}
			}
			if body, err := os.ReadFile(secret); err != nil || string(body) != "not-a-real-secret-value" {
				t.Fatalf("a refused write changed the file: %q %v", body, err)
			}
			if _, err := os.Stat(mark); err == nil {
				t.Fatal("a refused command ran")
			}
		})
	}
}

// A read starts before the turn's batch does, so the same answer has to reach
// the early start, or the secret would be in hand before anyone refused it.
func TestAnEarlyReadOfACredentialDoesNotStart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.ApprovalPolicy = nil })
	decision, governed := agent.decide(credentialCall("c1", "read", `{"path":"~/.netrc"}`))
	if !governed || decision.Action != approval.ActionDeny {
		t.Fatalf("an ungoverned agent answered %+v for a credential read", decision)
	}
}

// A relative path means the agent's own folder, which only the session knows.
func TestTheFloorPlacesARelativePathInTheAgentsFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := filepath.Join(home, "project")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace = work
		c.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	if _, hit := agent.credentialRead(credentialCall("c1", "read", `{"path":"../.npmrc"}`)); !hit {
		t.Fatal("a home .npmrc named from the project folder was not refused")
	}
	if _, hit := agent.credentialRead(credentialCall("c2", "read", `{"path":".npmrc"}`)); hit {
		t.Fatal("the project's own .npmrc was refused")
	}
}

// The person's own `!` command is where the refusal sends them, so it stays open.
func TestThePersonsOwnCommandIsNotTheAgents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	call := credentialCall("user_bash_1", "bash", bashJSON(t, "gh auth token"))
	if refused, allowed := agent.approveUserBash(context.Background(), nil, call); !allowed {
		t.Fatalf("the person's own command was refused: %+v", refused)
	}
	if _, allowed := agent.approve(context.Background(), nil, call); allowed {
		t.Fatal("the agent's road allowed the same command")
	}
}
