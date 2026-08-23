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
// (tools_jobs.go, tools_watch.go), remember carries the person's durable memory
// (memory.go) when there is a store to keep it in and search_conversations reads
// the other half of that store — the verbatim words of every earlier
// conversation, which were indexed and unreachable until it existed
// (tools_conversations.go) — track, commit and recall hold
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
// A TASK NODE'S BELT IS THIS BELT MINUS THREE, AND MINUS FIVE AT THE FLOOR OF
// THE TREE. watch comes off because its whole delivery mechanism is a note
// arriving in a conversation, and a node has none. The two settings hands come
// off for the sharpest reason there is: a node runs in a worktree with nobody
// watching it, so a settings change made there is a permanent change to the
// person's machine that no transcript ever showed them — and the node was
// briefed to do one piece of work, not to retune the product around it.
//
// propose_task and tasks STAY, and they are one pair. A node may hand parts of
// its own work further out (task.go's fan-out law) — the proposals join the
// conversation's own graph under the node that made them, so there is no second
// machine and no second id space — and `tasks` is how the node then watches
// those children, reads what they found and says a line into one that is going
// the wrong way. In a node that tool is SCOPED TO ITS OWN FAMILY
// (tools_tasks.go): a node's brief is still its whole world, and a node
// rummaging through the project's history would be a node reading the
// conversation it was deliberately given none of.
//
// Both come off together at the floor, by [Agent.mayProposeTask] rather than by
// a check here: a node standing on taskDepthLimit can have no children, so it is
// given neither the verb nor the window onto them. The three big machines come
// off by their own gates for the same kind of reason: a node is handed no
// registry, no harness runner and no orchestrate runner, so it can neither
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
		tools = append(tools, a.watchTool())
	}
	// tasks rides with propose_task: it is the window onto the work this agent
	// can hand out, so an agent that cannot hand any out is given neither.
	if a.mayProposeTask() {
		tools = append(tools, a.tasksTool())
	}
	tools = append(tools, a.taskTools()...)
	// stand (tools_standing.go) is the ambient side's one verb, and it is
	// CONDITIONAL for the sharpest version of the absence law on this belt: a
	// model told it can set up a reminder will plan a whole reply around one,
	// so a session with no store behind it is not given the verb at all. Every
	// door that is a conversation fills the seam; --once, a task node and a
	// firing's own headless session do not, because nothing unwatched may arm
	// something that spends forever.
	tools = append(tools, a.standingTools()...)
	tools = append(tools, a.harnessTools()...)
	// The saved PROGRAMS, and the list that says which ones there are
	// (tools_subharness.go). They are conditional on the same terms the three
	// machines above are — a registry with something on it, and somebody
	// watching who can answer the card — because a model told it can run a saved
	// program plans around that ability for the rest of the conversation.
	tools = append(tools, a.subharnessTools()...)
	tools = append(tools, a.memoryTools()...)
	// search_conversations (tools_conversations.go) is the other half of memory
	// and is conditional for the same reason `remember` is: what it reads is the
	// FTS index over every message ever posted, which lives in the store, and
	// a session opened with memory off has opened no store. A task node is
	// handed no store either (task_run.go sets no Config.Memory), so it does not
	// get the verb — which is the same wall that already keeps a node from
	// writing memories, read from the other side.
	tools = append(tools, a.conversationTools()...)
	tools = append(tools, a.stateTools()...)
	tools = append(tools, a.searchTools()...)
	tools = append(tools, a.settingsTools()...)
	tools = append(tools, a.connectTools()...)
	// The media verbs are one family and are appended together: generate_image
	// paints, speak talks, generate_music composes, generate_video films,
	// view_image looks. Each is absent-not-broken on its own terms — one media
	// client and one resolver answer for all five, and a machine whose resolver
	// has no model for a modality simply does not have that verb
	// (media_contract.go).
	//
	// THE WHOLE FAMILY TRAVELS. It is built here and nowhere else, so a task
	// node, an adaptive run's node and this conversation all reach for the same
	// five verbs under the same five conditions — the surfaces differ in what
	// they are handed (task_run.go, orchestrate.go pass Media and MediaModel
	// through), never in which tools this function decides to build out of it.
	tools = append(tools, a.imageTools()...)
	tools = append(tools, a.speakTools()...)
	tools = append(tools, a.musicTools()...)
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
