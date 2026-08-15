package session

import (
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// belt is the session's tool inventory: the seven pi tools — read, bash, edit,
// write, grep, find, ls — with their verbatim pi schemas and descriptions.
//
// The hands are bare's, not a second copy: the session works the way a
// subharness leaf works, and a divergence between them would be a divergence
// between what the person watched happen and what the workforce would do with
// the same instruction. The workforce verbs (task/change/stop) are not here
// yet; when the tasker attaches they append to this slice and nothing else
// changes.
//
// There is deliberately no todo tool (docs/CHAT-V3.md Decision 11): a plan
// the person can read beats state only the model can see, and work big enough
// to decompose belongs to the workforce, not to a session-local list.
//
// The rest are the session's own rather than bare's: bash is WRAPPED (not
// replaced) so it can start a background job, read is WRAPPED so a PDF is a
// file it can answer for rather than a file it returns as bytes (tools_pdf.go
// — the tool grows a sense, the belt does not grow a tool), jobs is added
// beside it to look
// at what was started and watch beside that to be TOLD instead of looking
// (tools_jobs.go, tools_watch.go), note and forget carry the session's durable memory
// (memory.go) when there is a file to keep it in, and web_search and web_fetch
// reach outside the machine (tools_search.go) when a back end was wired, and
// generate_image paints (tools_image.go) when an image model was. bare
// is untouched — a subharness leaf gets pi's bash exactly as before, and the
// session gets pi's bash plus one argument.
//
// propose_task (task.go) is the one hand that gives work AWAY: the model grooms
// a self-contained piece, the person gets a countdown to redirect it, and an
// approved node runs as its own agent in its own worktree.
//
// The last groups are CONDITIONAL, and each says why at its own source: a belt
// is what the model has been promised, so a tool with nothing behind it is left
// off rather than added and made to refuse.
//
// A TASK NODE'S BELT IS THIS BELT MINUS TWO. propose_task comes off because
// there is nobody in a node's world to show a proposal to — decomposition, when
// it lands, is edges added to the graph by the conversation that owns it, not a
// second proposal machine inside a worktree — and watch comes off because its
// whole delivery mechanism is a note arriving in a conversation, and a node has
// none. Everything else a node has is exactly what the conversation has, which
// is the point: it is the same worker, working somewhere quieter.
func (a *Agent) belt() []bare.Tool {
	tools := bare.AllTools(a.config.Workspace)
	for index, tool := range tools {
		switch tool.Name {
		case "bash":
			tools[index] = a.backgroundBash(tool)
		case "read":
			tools[index] = a.pdfRead(tool)
		}
	}
	tools = append(tools, a.jobsTool())
	if !a.config.InTask {
		tools = append(tools, a.watchTool())
	}
	tools = append(tools, a.taskTools()...)
	tools = append(tools, a.memoryTools()...)
	tools = append(tools, a.searchTools()...)
	return append(tools, a.imageTools()...)
}

// toolDefinitions builds the wire form of the belt, carrying each tool's
// schema and description verbatim.
//
// A schema that does not parse fails here, at construction. Dropped instead, it
// would ride the wire as Parameters:nil — a tool the model is told takes no
// arguments — and every call it then made would fail as if the model had
// written it wrong. The belt is built from literals in this binary, so a
// malformed schema is a bug in the build, and the build is where it belongs.
func toolDefinitions(tools []bare.Tool) ([]ai.ToolDefinition, error) {
	definitions := make([]ai.ToolDefinition, len(tools))
	for index, tool := range tools {
		var parameters map[string]interface{}
		if err := json.Unmarshal(tool.Schema, &parameters); err != nil {
			return nil, fmt.Errorf("session: tool %q has a malformed schema: %w", tool.Name, err)
		}
		definitions[index] = ai.ToolDefinition{
			Type: "function",
			Function: ai.ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  parameters,
			},
		}
	}
	return definitions, nil
}
