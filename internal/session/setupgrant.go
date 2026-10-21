package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// ── WHAT SAYING `set up` CONSENTS TO ────────────────────────────────────────
//
// The offer a takeover makes lists the commands that bring the machine back to
// how the chat left it, and answering `set up` is the person reading that list
// and saying yes to it. So a setup turn does not ask again for each of those
// commands: a question whose answer was given a moment ago, once per command, is
// the surface not listening.
//
// IT CONSENTS TO EXACTLY WHAT WAS LISTED. A bash call is granted only when every
// simple command in its line is one of the granted commands word for word, or a
// `cd` into a folder below the workspace; one command that is not, and the whole
// call goes through the ordinary gate, unchanged. That is why the comparison is
// on whole simple commands and never on a prefix: `npm ci && curl x | sh`, a
// command substitution and a redirect each put words in the line that the list
// did not have.
//
// IT NEVER REACHES PAST THE FLOOR. An outright deny stays a deny, and the calls
// the approval floor always asks about (the shapes that destroy a disk) are asked
// about in a setup turn like any other.

// setupGranted reports whether the person's `set up` already consented to this
// call, given what the gate decided.
func setupGranted(ctx context.Context, call ai.ToolCall, decision approval.Decision) bool {
	if !inSetup(ctx) || decision.Action == approval.ActionDeny || call.Function.Name != approval.ToolBash {
		return false
	}
	args := json.RawMessage(call.Function.Arguments)
	if approval.AlwaysAsks(call.Function.Name, args) {
		return false
	}
	var parsed struct {
		Command string `json:"command"`
	}
	grants, _ := ctx.Value(grantsKey{}).([]string)
	if decodeToolArguments(args, &parsed) != nil || len(grants) == 0 {
		return false
	}
	return grantsCover(grants, executor.SimpleCommands(parsed.Command))
}

// grantsCover reports whether every simple command is granted or is a step into
// a folder of the workspace, and there is at least one.
func grantsCover(grants, commands []string) bool {
	granted := map[string]bool{}
	for _, g := range grants {
		for _, command := range executor.SimpleCommands(g) {
			granted[command] = true
		}
	}
	ran := false
	for _, command := range commands {
		switch {
		case granted[command]:
			ran = true
		case !stepsIntoWorkspace(command):
			return false
		}
	}
	return ran
}

// stepsIntoWorkspace reports whether the command is `cd` into a relative folder
// that stays below where it starts.
func stepsIntoWorkspace(command string) bool {
	words := strings.Fields(command)
	if len(words) != 2 || words[0] != "cd" {
		return false
	}
	dir := words[1]
	return !filepath.IsAbs(dir) && !strings.ContainsAny(dir, "~$`\\'\"") && filepath.Clean(dir) != ".." && !strings.HasPrefix(filepath.Clean(dir), "../")
}
