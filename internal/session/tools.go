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
// — the tool grows a sense, the belt does not grow a tool), read_document
// stands BESIDE it as the rung that sentence names — the scanned page, the
// photograph, the docx that read turns into bytes (tools_doc.go: a second hand
// here and not a second sense, because this one costs money and the split is
// where the bill is), jobs is added
// beside it to look
// at what was started and watch beside that to be TOLD instead of looking
// (tools_jobs.go, tools_watch.go), note and forget carry the session's durable memory
// (memory.go) when there is a file to keep it in, track, commit and recall hold
// the working state a compaction must not lose (state.go — the same file's three
// records, unconditional because every session compacts), and web_search and web_fetch
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
// tasks (tools_tasks.go) is propose_task's other end: the project's whole task
// history, searchable, so that work handed off weeks ago is still findable by
// the model that has to build on it.
//
// build_harness, list_harnesses and run_adaptive (tools_harness.go) are the two
// big machines and the list that says whether one of them already exists: a
// saved procedure this project can be offered again, and a planner-and-fleet run
// against a fuel cap. They are the model's to reach for BY DESIGN — the
// judgement "is this a recipe, a run, or just work" is one no cue list can make
// — and each is absent where its machinery is (no registry, no runner, nobody
// watching to answer the card or the fuel gate).
//
// settings and change_setting (tools_settings.go) are the person's own
// configuration: the sheet read back by its registry keys, and one row of it
// written permanently into the profile through the registry's own validated
// write. They are two tools rather than one with actions because the approval
// gate keys on the tool NAME, and reading a person's settings and rewriting them
// must not share one answer. They are absent together where there is no profile
// directory to read.
//
// services and use_service (tools_connect.go) are the accounts the person
// already has somewhere else. They are the one family on this belt that can
// GROW it: what an account brings — a mailbox, a calendar — is appended when the
// account is picked up rather than carried by every conversation that will never
// touch mail. THE APPEND IS THE ONLY MUTATION THIS SLICE EVER SEES, and
// connect.go states why nothing may move.
//
// A TASK NODE'S BELT IS THIS BELT MINUS FIVE. propose_task comes off because
// there is nobody in a node's world to show a proposal to — decomposition, when
// it lands, is edges added to the graph by the conversation that owns it, not a
// second proposal machine inside a worktree — and watch comes off because its
// whole delivery mechanism is a note arriving in a conversation, and a node has
// none. tasks comes off for the contract's own reason: a node's brief is its
// whole world, and a node reading the project's task history is a node reading
// the conversation it was deliberately given none of. The two settings hands
// come off for the sharpest version of the same reason: a node runs in a
// worktree with nobody watching it, so a settings change made there is a
// permanent change to the person's machine that no transcript ever showed them
// — and the node was briefed to do one piece of work, not to retune the product
// around it. The three above come off
// with them, by their own gates rather than by a check here: a node is handed no
// registry, no harness runner and no orchestrate runner, so a node can neither
// commission a procedure nor start a run of its own. Everything else a node has
// is exactly what the conversation has, which is the point: it is the same
// worker, working somewhere quieter.
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
	tools = append(tools, a.documentTool(), a.jobsTool(), a.manualTool())
	if !a.config.InTask {
		tools = append(tools, a.watchTool(), a.tasksTool())
	}
	tools = append(tools, a.taskTools()...)
	tools = append(tools, a.harnessTools()...)
	tools = append(tools, a.memoryTools()...)
	tools = append(tools, a.stateTools()...)
	tools = append(tools, a.searchTools()...)
	tools = append(tools, a.settingsTools()...)
	tools = append(tools, a.connectTools()...)
	// The media verbs are one family and are appended together: generate_image
	// paints, speak talks, generate_video films, view_image looks. Each is
	// absent-not-broken on its own terms — one media client and one resolver
	// answer for all four, and a machine whose resolver has no model for a
	// modality simply does not have that verb (media_contract.go).
	tools = append(tools, a.imageTools()...)
	tools = append(tools, a.speakTools()...)
	tools = append(tools, a.videoTools()...)
	return append(tools, a.viewTools()...)
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
