package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// AN AUDIT MUST NOT SPEND THE DEAR TIER ON NAMING OR ON FIGHTING ITS OWN
// COMMAND CONTRACT.
//
// The measured waste (F38/F40): the high-tier judge named tasks, and it
// refused its own verification when the model typed the allowed check with
// a trailing redirect, then paid another turn to type the same shape. Naming
// is a cheap errand; a trailing redirect is not a second command. A task
// whose audit would once have triggered those calls now completes with
// zero auditor-tier calls for either purpose.

func TestAnAuditDoesNotSpendTheAuditorOnNamingOrASelfRefusedCommand(t *testing.T) {
	t.Run("naming stays off the auditor tier", func(t *testing.T) {
		const (
			cheap = "cheap/namer"
			dear  = "dear/auditor"
		)
		var asked []string
		client := &scriptedCompleter{steps: []step{
			func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
				if !isNameCall(messages) {
					t.Fatalf("the call was not the namer's: %q", messageContentText(messages[0]))
				}
				return textResponse("parser recon"), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				t.Fatal("naming fell through onto the auditor tier")
				return nil, errors.New("auditor tier must not name")
			},
		}}
		// Record which model each request named, including a cheap rung that
		// fails: the second request used to be the session floor, and when
		// that floor WAS the high-tier judge the namer billed a verdict model
		// for three words.
		failThenName := &scriptedCompleter{steps: []step{
			func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
				asked = append(asked, "first")
				return nil, errors.New("cheap rung is down")
			},
			func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
				asked = append(asked, "second")
				return textResponse("should not land"), nil
			},
		}}
		agent, _ := newTestAgent(t, client, func(c *Config) {
			c.Model = dear
			c.RolesSource = func(key string) (string, bool) {
				switch key {
				case roles.TierKey(roles.TierLow):
					return cheap, true
				case roles.TierKey(roles.TierHigh):
					return dear, true
				}
				return "", false
			}
		})
		if got := agent.taskName(context.Background(), "read the parser and say what it does"); got != "parser recon" {
			t.Fatalf("name = %q, want the cheap rung's answer", got)
		}
		if client.requests() != 1 || client.model(0) != cheap {
			t.Fatalf("naming rode %q (%d calls), want one call on %q", client.model(0), client.requests(), cheap)
		}

		stubborn, _ := newTestAgent(t, failThenName, func(c *Config) {
			c.Model = dear
			c.RolesSource = func(key string) (string, bool) {
				switch key {
				case roles.TierKey(roles.TierLow):
					return cheap, true
				case roles.TierKey(roles.TierHigh):
					return dear, true
				}
				return "", false
			}
		})
		if got := stubborn.taskName(context.Background(), "read the parser and say what it does"); got != "" {
			t.Fatalf("a failed cheap namer fell through: %q", got)
		}
		if failThenName.requests() != 1 || failThenName.model(0) != cheap {
			t.Fatalf("the failed namer rode %v, want the cheap rung alone", []string{failThenName.model(0)})
		}
		if len(asked) != 1 {
			t.Fatalf("asked %v, want the cheap rung and no auditor-tier retry", asked)
		}
	})

	t.Run("a redirected check runs as its first stage", func(t *testing.T) {
		var ran string
		tool := readingOnlyBash(bare.Tool{
			Name: "bash",
			Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
				var fields struct {
					Command string `json:"command"`
				}
				if err := json.Unmarshal(args, &fields); err != nil {
					t.Fatal(err)
				}
				ran = fields.Command
				return "passed", false, nil
			},
		}, plainDoor([]string{"python3 -m pytest", "go test"}), auditShell)

		for _, typed := range []string{
			"python3 -m pytest tests 2>&1",
			"python3 -m pytest tests > /tmp/out",
			"go test ./... | tail -5",
		} {
			ran = ""
			args, err := json.Marshal(struct {
				Command string `json:"command"`
			}{Command: typed})
			if err != nil {
				t.Fatal(err)
			}
			text, isError, execErr := tool.Execute(context.Background(), args)
			if execErr != nil {
				t.Fatalf("%q: execute: %v", typed, execErr)
			}
			if isError || strings.HasPrefix(text, "refused:") {
				t.Fatalf("%q was refused, which is the self-refusal this test exists to stop:\n%s", typed, text)
			}
			if strings.ContainsAny(ran, shellComposition) {
				t.Fatalf("%q ran a composed command %q", typed, ran)
			}
			if !strings.HasPrefix(ran, "python3 -m pytest") && !strings.HasPrefix(ran, "go test") {
				t.Fatalf("%q ran %q, want the allowed first stage", typed, ran)
			}
		}

		// AND A SECOND COMMAND IS STILL REFUSED. Pre-validation is not a
		// wider door: `&&` is two commands, not a pipeline's first stage.
		args, err := json.Marshal(struct {
			Command string `json:"command"`
		}{Command: "go test ./... && rm -rf ."})
		if err != nil {
			t.Fatal(err)
		}
		text, isError, execErr := tool.Execute(context.Background(), args)
		if execErr != nil {
			t.Fatal(execErr)
		}
		if !isError || !strings.HasPrefix(text, "refused:") {
			t.Fatalf("a chained command ran: %q", text)
		}
	})

	t.Run("an auditor session does not name itself", func(t *testing.T) {
		parent, dir := newTestAgent(t, &scriptedCompleter{}, nil)
		graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
		node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "the greeting"}}
		graph.nodes[1] = node
		auditor, err := parent.newAuditAgent(dir, node, plainDoor(auditReadCommands))
		if err != nil {
			t.Fatalf("newAuditAgent: %v", err)
		}
		t.Cleanup(func() { _ = auditor.Close() })
		auditor.mu.Lock()
		tried := auditor.titleTried
		auditor.mu.Unlock()
		if !tried {
			t.Fatal("the auditor was left free to name its own journal")
		}
		auditor.maybeTitle(context.Background(), nil)
	})
}

func TestPreparedAuditCommandKeepsAChainAndDropsARedirect(t *testing.T) {
	if got := preparedAuditCommand("python3 -m pytest tests 2>&1"); got != "python3 -m pytest tests" {
		t.Fatalf("redirected pytest = %q", got)
	}
	if got := preparedAuditCommand("go test ./... > /tmp/out"); got != "go test ./..." {
		t.Fatalf("redirected go test = %q", got)
	}
	chain := "go test ./... && rm -rf ."
	if got := preparedAuditCommand(chain); got != chain {
		t.Fatalf("a chain was rewritten to %q", got)
	}
}
