package session

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// belt is the session's tool inventory: the seven file tools — read, bash,
// edit, write, grep, find, ls — with their verbatim schemas and descriptions.
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
// is untouched — a subharness leaf gets bare's bash exactly as before, and the
// session gets bare's bash plus one argument.
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
// resultCaps is how much of one tool result this conversation's model can
// afford to be handed, and it is the answer the whole belt is built with.
//
// IT FOLLOWS THE WINDOW. bare's flat caps — 2000 lines or 50KB — were measured
// against a 128,000-token window, which is exactly what [Agent.window] answers
// when no model card says otherwise, so a frontier conversation gets them
// unchanged and is byte-identical to what it was. A model with a smaller window
// gets a smaller share of it, because a read that fills 78% of everything the
// model can hold leaves it the file and no room to think about the file.
//
// It reads [Agent.window] rather than [Agent.trustedWindow]: the caps are
// rendered into the descriptions in message[0], and a bound that moved when
// this process learned something about an endpoint would re-price the whole
// conversation cold.
func (a *Agent) resultCaps() bare.Caps { return bare.CapsFor(a.window()) }

// build_harness and list_harnesses (tools_harness.go) are the ONE big machine
// left on this belt and the list that says whether the thing about to be built
// already exists: a saved procedure this project can be offered again. They are
// the model's to reach for BY DESIGN — the judgement "is this a recipe, or just
// work" is one no cue list can make — and both are absent where their machinery
// is (no registry, no runner, nobody watching to answer the card).
//
// THE PLANNER-AND-FLEET RUN WAS A THIRD HAND HERE (`run_adaptive`) AND IS NOT ANY
// MORE. The judgement it asked the model to make — is this wide enough to plan up
// front — is a guess made before anybody opens the material, and the road that
// won makes it from the material instead: one task, admitted wide, handing the
// parts out as it finds them (task_divide.go). The engine still ships, and since
// the cue that read a typed request for a run went too (loop.go) NOTHING in a
// conversation reaches it: the model has no verb for a run, and neither does
// anybody typing — which is what "absent, not refusing" means when the thing
// taken away is a hand.
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
// AND ON THE EXPERIMENT BRANCH THERE IS A THIRD ANSWER: THE SIX FILE TOOLS
// COME OFF ENTIRELY. spark/bash-task-loop hands a task worker ONE bash tool —
// bare's, with the branch's head+tail+path cut and the session's background
// argument — and the kept hands that cannot be a shell command, under a strict
// one-action-per-response envelope (bashbelt.go, bashbelt_envelope.go). It is
// a SECOND composition behind [Config.mayBashBelt], set from CODEAF_TASK_BELT
// at the one place a worker's Config is built (task_run.go's
// [Agent.newTaskAgentOn]); unset, the belt above is byte for byte what it was,
// and internal/exec/bare is untouched — the hands stay its, and the
// experiment is a wire change, never an engine change.
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
	// THE EXPERIMENT'S SECOND BELT, AND THE ONE DELEGATION THIS FUNCTION MAKES.
	// A task worker built with CODEAF_TASK_BELT=bash reads a belt of one shell
	// and the hands that cannot be a shell command, under a strict one-action
	// envelope — the branch named in [bashBelt] — and unset, not one byte of
	// what is composed here moves. The conversation never reads the switch:
	// [Config.mayBashBelt] is the predicate and it answers InTask first.
	if a.config.mayBashBelt() {
		return a.bashBelt()
	}
	tools := bare.AllToolsCapped(a.config.Workspace, a.resultCaps())
	for index, tool := range tools {
		switch tool.Name {
		case "bash":
			tools[index] = a.backgroundBash(tool)
		case "read":
			tools[index] = a.pdfRead(tool)
		case "write":
			// write is WRAPPED so a file can be continued rather than only
			// replaced (tools_write.go): append:true is the one argument the
			// salvage of a cut-off write (salvage.go) asks the model to reach
			// for, and a belt that cannot append is a belt whose only answer
			// to "the output limit cut your file in half" is to pay for the
			// whole file again.
			tools[index] = a.appendableWrite(tool)
		}
	}
	// AND THE THREE HANDS THAT CAN BE AIMED AT A FOLDER THIS CONVERSATION ONLY
	// REFERS TO are wrapped once more, OUTERMOST (standingbelt.go): where you
	// stand you write, and where you refer the work is kept in a copy until
	// somebody lands it. The wrapper is outside the two above so that everything
	// they do — the append's own read, the PDF sense's open — happens to the same
	// file the write did.
	//
	// It is absent where it could not work rather than present and failing: a
	// conversation with no folder of its own has nowhere to put a copy, and a
	// task node stands on a ground of its own with a guard already around it
	// (taskoutside.go), so neither is given the redirect.
	if !a.config.InTask && strings.TrimSpace(a.config.Place.Dir) != "" {
		for index, tool := range tools {
			switch tool.Name {
			case "read", "write", "edit":
				tools[index] = a.placeAimed(tool)
			}
		}
	}
	tools = append(tools, a.documentTool(), a.jobsTool(), a.manualTool())
	tools = append(tools, a.askTool())
	// watch, on the predicate the prompt's own `watch` sentence is composed
	// from (beltfacts.go): a node has no conversation for a delta to arrive in.
	if a.config.mayWatch() {
		tools = append(tools, a.watchTool())
	}
	// tasks rides with propose_task: it is the window onto the work this agent
	// can hand out, so an agent that cannot hand any out is given neither.
	if a.mayProposeTask() {
		tools = append(tools, a.tasksTool())
	}
	tools = append(tools, a.taskTools()...)
	// quick_task rides beside propose_task and on the same predicate: it is the
	// other way work leaves a turn — not handed away into a copy of its own, but
	// started HERE, where the caller works, its last message its answer
	// (task_quick.go). The judge that decides between the two is written once, in
	// its description.
	tools = append(tools, a.quickTools()...)
	// use_skill rides on the same predicate as propose_task, plus a store to read
	// the shelf from: a worker that may hand work out may also look up what this
	// project already knows how to do (tools_skill.go). A floor node is handed no
	// store and no verb either way, so the two gates agree by construction.
	tools = append(tools, a.useSkillTool()...)
	// items is the verb a QUICK WORKER carries and nothing else does: a node with
	// no list has no door behind the tool, so it is absent rather than present
	// and refusing — the law every conditional family on this belt is built on.
	if a.config.mayTickItems() {
		tools = append(tools, a.itemsTool())
	}
	// revise_assignment is a WORKER'S verb and nothing else's (assignment.go): it
	// folds a direction the person gave this node into what the node is judged by.
	// A conversation has no assignment to revise and an auditor is handed no
	// graph, so both are absent by the same gate that decides everything else on
	// this belt — the capability is not there rather than being there and
	// refusing.
	tools = append(tools, a.assignmentTools()...)
	// divide_work rides beside propose_task and is narrower than it: the one
	// names parts a worker could see from the start, this one names parts it
	// only found once it had opened the material. It is absent unless THIS
	// task was armed for division (task_divide.go), which is what makes a
	// narrow task's belt byte-identical to what it was before that road
	// existed.
	tools = append(tools, a.divideTools()...)
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
	// An owned conversation has no project until the person names one. The
	// anchoring hand exists only in that state; after it succeeds the rebuilt
	// belt omits it, because a capability whose job is already done is absent.
	tools = append(tools, a.anchorWorkspaceTools()...)
	// The workspace's own history — restore points over the FILES, forks to try
	// something risky in, and the merge that lands one (tools_workspace.go).
	// They are furrow's verbs and they are absent in a folder nobody has
	// attached to furrow, which is the same absence law `stand` and the memory
	// pair are built on and is stated at length where they are built.
	tools = append(tools, a.workspaceTools()...)
	// Conversation search needs only a history reader. Task workers and forked
	// hands inherit it without gaining the writable memory store.
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
	tools = append(tools, a.viewTools()...)
	// edit_video (tools_editvideo.go) belongs beside them and is gated on
	// something else entirely: ffmpeg on PATH. It buys nothing, so it needs no
	// model, no key and no money — which means it is present on a machine that
	// cannot generate a single frame, where cutting together footage the person
	// already has is the only video work there is. It rides last because it is
	// what the other five's output is assembled WITH.
	tools = append(tools, a.videoEditTools()...)
	// AND THE LAST STEP IS NOT A GATE. Everything above has already decided what
	// this build can offer; this splits what survived into what the model carries
	// from the first turn and what waits one call away on a named shelf, and puts
	// `load_capability` where the shelved groups used to be
	// (tools_capabilities.go). It removes no capability and adds none: a machine
	// with no media models still has no media group, and a shelf with nothing on
	// it puts no verb on this belt at all.
	return a.shelveDeferred(tools)
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
