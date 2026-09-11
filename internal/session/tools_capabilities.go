package session

// THE SHELF: TOOLS THIS BUILD HAS AND THE MODEL IS NOT CARRYING.
//
// Every schema on the belt rides ahead of the whole transcript on every request
// this session makes (prefixbudget_test.go weighs it), so a verb reached for
// twice a week costs exactly what `read` costs. The rarely-reached families are
// therefore built exactly as they always were, by tools.go, out of the same
// gates — and then held back, with one small verb, `load_capability`, put where
// they were.
//
// Three contracts, and they are the whole of this file:
//
//   - A LOADED GROUP IS ARMED THROUGH [Agent.armFamily], the same door a
//     connected account's tools arrive through, bound by the same append law:
//     tools go at the tail, nothing already there moves, nothing retires. The
//     tools are the SAME [bare.Tool] values tools.go built, so approval, hooks,
//     events and billing are unchanged. Loading is not permission.
//   - THE TABLE IS NAMES. It never carries a schema or a gate, and is matched
//     against the belt that was actually built — so a tool this machine cannot
//     offer is missing from the shelf for free, and a group with no surviving
//     member does not exist. Nothing here can promise a capability the build
//     does not have.
//   - NOTHING IS SHELVED WHERE [Config.shelvesCapabilities] is false, which is
//     inside a task: a node is briefed once and lands, the saving is only paid
//     on a prefix re-sent every turn, and the landing belt (withdrawn.go) needs
//     the making verbs directly.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

const loadCapabilityToolName = "load_capability"

// loadedLead opens a successful load's answer, and it is one constant because
// checkpoint.go reads the armed names back off that very line
// ([loadedAndNeverUsed]): a lead spelled twice is a nudge that stops firing the
// day one of them moves.
const loadedLead = "Loaded: "

// capabilityGroup is one named shelf: the word the model asks for it by, and the
// tools it claims BY NAME. A name no belt builds is simply never matched.
type capabilityGroup struct {
	name    string
	members []string
	// prose is the paragraph a model needs THE MOMENT IT LOADS THIS GROUP, and
	// it rides in the load's own answer rather than in prompts/system.md.
	//
	// LONG MECHANICS ARE PAID FOR BY WHOEVER PULLS THEM. Existence belongs in
	// the page — one line in the routing table, so the model can plan around a
	// verb it is not carrying — and the contract belongs with the verb, in its
	// own description. What is left over is the run of prose that only makes
	// sense once you are INSIDE the thing: how to write a prompt that does not
	// come back average, what a saved recipe is against a saved program, what a
	// refused write means. That was 2.5 KB of message[0] on every request of
	// every turn, for verbs the model could not call; here it is bought once,
	// by the turn that reached for the group.
	//
	// A group with no prose simply says nothing extra, which is the right answer
	// for `questions`: `ask`'s own description is the whole of its law.
	prose string
}

// capabilityGroups is the whole partition, in the order the verb lists them.
//
// A TOOL IS SHELVED WHEN IT IS REACHED FOR RARELY AND ITS SCHEMA IS LARGE.
// Everything a conversation does most days stays carried, because a verb needed
// on the turn it is needed must not cost a round trip to discover. `view_image`
// stays carried and the other making verbs do not: looking at something the
// person has just put in front of you is ordinary work, and making a film is not.
var capabilityGroups = []capabilityGroup{{
	name:    questionsGroup,
	members: []string{"ask"},
}, {
	name:    "media",
	members: []string{"generate_image", "speak", "generate_music", "generate_video", "edit_video"},
	// THE PROMPT IS THE QUALITY, and this is where that law now lives. It was
	// 853 bytes of prompts/system.md's Tool Policy on a belt whose every making
	// verb is on this shelf; the page keeps the one line that says a prompt
	// decides the result, and the reasoning arrives with the verbs.
	prose: "When you MAKE media the prompt decides the quality, on every path — a generation tool, or a request a " +
		"script of yours sends. What a prompt leaves open the model fills with its average, and a prompt built from " +
		"the genre's own clichés (adjective piles included) asks for that average outright. Two things escape it: " +
		"ANCHOR IN A REAL MEDIUM — a named print process, photographic setup or drafting tradition, which carries " +
		"its own physics and its own different average — and SPECIFY POSITIVELY, since these models barely read " +
		"negation (\"no glow\" glows; matte ink on cream paper cannot). Judge what came back against the brief AND " +
		"against its genre; a first render is a draft, so read it back before you call it done. `manual` teaches the rest.",
}, {
	name:    "settings",
	members: []string{"settings", "change_setting"},
	// The pair's own descriptions carry the contract — what reads, what writes,
	// that a list row is replaced whole, that a row restraining this session
	// refuses on purpose. What is left is what to do with the refusal in front
	// of the person, which is not a fact about the schema.
	prose: "A refusal is the person's own machine answering and not an error to work around: relay it exactly as " +
		"written, and point them at `/settings`, where they can make the change themselves. Never reach for `edit` " +
		"or `write` on a config file instead — a hand-edited file is not what aforge reads.",
}, {
	name:    "harnesses",
	members: []string{"build_harness", "list_harnesses", "propose_subharness", "list_subharnesses"},
	// TWO DIFFERENT THINGS ARRIVE TOGETHER, which is the one fact none of the
	// four descriptions can state on its own, and it is what prompts/system.md's
	// PROGRAM_FACTS paragraphs were paying for on every request of a belt that
	// carried neither verb.
	prose: "Two different things arrive together. A **sub-harness** is a reusable RECIPE: a named, versioned " +
		"procedure saved on this machine, which the turn offers by itself whenever somebody's words match it. " +
		"`list_harnesses` shows them and `build_harness` designs one, as a task the person can open, watch and " +
		"stop, ending in a card that saves it or discards it. A **subharness** is a saved PROGRAM rather than a " +
		"recipe: typed input, a typed answer, only the tools it declared. `list_subharnesses` shows them and " +
		"`propose_subharness` offers one with your line about why it matched. NOTHING RUNS BECAUSE YOU PROPOSED " +
		"IT — the person answers that card and there is no clock that says yes for them. Build one only for a " +
		"shape of work that will recur; propose one only when the work in front of you IS what that program is for.",
}}

// shelveDeferred is the last step of [Agent.belt]: it splits what the belt built
// into what is carried and what waits, and hands back the carried half with the
// loading verb on the end of it.
//
// THE ORDER OF WHAT IS CARRIED IS THE ORDER tools.go BUILT IT — two builds of
// the same belt must agree, or the prompt cache is paid for twice — so this
// filters in place and the loading verb goes where an appended family would.
func (a *Agent) shelveDeferred(built []bare.Tool) []bare.Tool {
	// [Config.shelvesCapabilities] is the one reading of this question, and
	// beltfacts.go composes the page's load-this-group sentences from the same
	// predicate — so a shape carrying these tools directly can never be told to
	// go and fetch them.
	if !a.config.shelvesCapabilities() {
		return built
	}
	// THE PARTITION IS THIS SHAPE'S AND NOT THE PACKAGE'S. A lean prefix shelves
	// four more groups than a full one (promptprofile.go's
	// [Config.capabilityShelf]), and it is asked of the config for the reason
	// every other question on this road is: the page was composed from the same
	// answer a moment ago, and the two must not be able to differ.
	partition := a.config.capabilityShelf()
	// Where each shelved name belongs, resolved once. A name in two groups would
	// land in the first, which capabilities_test.go proves cannot happen.
	group := make(map[string]string, 16)
	for _, candidate := range partition {
		for _, member := range candidate.members {
			if _, taken := group[member]; !taken {
				group[member] = candidate.name
			}
		}
	}
	// AND THE GROUPS THIS SHAPE IS HANDED RATHER THAN ASKED TO FETCH. They are
	// partitioned off here — not carried, not shelved — so that the loading
	// verb's catalog never offers a group already on its way onto the belt, and
	// armed at construction through the one arming door (agent.go's newAgent,
	// [Agent.armPrearmed]).
	prearmed := make(map[string]bool, 2)
	for _, name := range a.config.prearmedGroups() {
		prearmed[name] = true
	}

	shelf := make(map[string][]bare.Tool, len(partition))
	carried := make([]bare.Tool, 0, len(built))
	var waiting []bare.Tool
	for _, tool := range built {
		name, shelved := group[tool.Name]
		if !shelved {
			carried = append(carried, tool)
			continue
		}
		if prearmed[name] {
			waiting = append(waiting, tool)
			continue
		}
		shelf[name] = append(shelf[name], tool)
	}

	// Only the groups that actually have something on them, in the table's
	// order: a group whose every member was gated off does not exist.
	order := make([]string, 0, len(partition))
	for _, candidate := range partition {
		if len(shelf[candidate.name]) > 0 {
			order = append(order, candidate.name)
		}
	}

	a.armMu.Lock()
	a.shelf, a.shelfOrder = shelf, order
	a.prearm = waiting
	a.armMu.Unlock()

	// NOTHING SHELVED IS NO VERB: a model handed a loader over an empty shelf
	// would spend a call finding out there is nothing to load.
	if len(order) == 0 {
		return carried
	}
	return append(carried, a.loadCapabilityTool(shelf, order))
}

// clearShelf empties it, for the doors that REPLACE the belt wholesale rather
// than growing it — a hand (fork.go) and an auditor (task_audit.go). Neither
// allowlist names `load_capability`, so neither could reach the shelf anyway;
// this makes the narrowing total rather than total-in-the-list-only.
func (a *Agent) clearShelf() {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	a.shelf, a.shelfOrder = nil, nil
	// AND WHAT WAS WAITING TO BE HANDED OVER GOES WITH IT. A belt replaced
	// wholesale is a belt somebody narrowed on purpose; arming a group into it
	// afterwards would widen it again behind their back.
	a.prearm = nil
}

// armPrearmed puts the groups this shape is HANDED onto the belt, and it is
// called wherever a live conversation's belt is built or rebuilt from
// [Agent.belt] — construction (agent.go's newAgent) and the anchoring rebuild
// (tools_anchor_workspace.go). Anywhere else the belt is replaced wholesale by a
// narrowing that must not be widened again ([Agent.clearShelf]).
//
// It is [Agent.armFamily] and nothing else: the same dedupe, the same append law,
// the same refusal of a schema that will not parse. A group already carried
// costs nothing here, which is what makes calling it after a rebuild safe.
func (a *Agent) armPrearmed() error {
	a.armMu.Lock()
	waiting := append([]bare.Tool(nil), a.prearm...)
	a.armMu.Unlock()
	if len(waiting) == 0 {
		return nil
	}
	_, err := a.armFamily(waiting)
	return err
}

// shelvedNames and shelvedTools are the construction-time partition: what this
// build held back, whether or not a group has since been loaded. The gates that
// walk "every tool this build can offer" read [Agent.offeredTools], which is
// where the two halves are joined and deduplicated.
func (a *Agent) shelvedNames() []string {
	names := make([]string, 0, 8)
	for _, tool := range a.shelvedTools() {
		names = append(names, tool.Name)
	}
	return names
}

func (a *Agent) shelvedTools() []bare.Tool {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	tools := make([]bare.Tool, 0, 8)
	for _, name := range a.shelfOrder {
		tools = append(tools, a.shelf[name]...)
	}
	return tools
}

// ── the verb ────────────────────────────────────────────────────────────────

// loadCapabilityTool is the one hand this file puts on the belt, built from the
// LIVE shelf: the groups it names and the `group` enum are exactly what this
// machine has.
//
// THE CATALOG IS THE DESCRIPTION, and each group line is its own surviving tool
// NAMES — never a claim about what the group can do. A blurb promising pictures
// and music on a machine whose media group is `edit_video` alone would be the
// one lie this design can tell, and naming the tools also lets a model that
// meets an unfamiliar name in its own history find the group it is in.
func (a *Agent) loadCapabilityTool(shelf map[string][]bare.Tool, order []string) bare.Tool {
	return bare.Tool{
		Name:        loadCapabilityToolName,
		Description: loadCapabilityDescription(shelf, order),
		Schema:      json.RawMessage(loadCapabilitySchema(order)),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Group string `json:"group"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			text, failed := a.loadCapability(parsed.Group)
			return text, failed, nil
		},
	}
}

// loadCapabilityDescription is bought on every request of every turn, so every
// sentence in it is a law the model cannot get elsewhere: what the verb does,
// WHEN the tools become usable, how long they last, and that it is not
// permission.
func loadCapabilityDescription(shelf map[string][]bare.Tool, order []string) string {
	var lines strings.Builder
	for _, name := range order {
		lines.WriteString(" " + name + ": " + strings.Join(toolNames(shelf[name]), ", ") + ".")
	}
	return "Load one additional tool group. Available groups:" + lines.String() +
		" Full schemas arrive on the next model request; continue in this same turn. " +
		"Tools stay loaded while this engine runs. After reopening, load any missing group again. " +
		"Existing permissions still apply. Do not reload tools already listed."
}

func loadCapabilitySchema(order []string) string {
	enum, err := json.Marshal(order)
	if err != nil {
		// The input is a slice of literals from this file's own table, so this
		// cannot fail; an empty enum would be a schema the model cannot satisfy.
		panic("session: capability group names are not encodable: " + err.Error())
	}
	return fmt.Sprintf(`{"type":"object","properties":{"group":{"type":"string","enum":%s,`+
		`"description":"Which group to load."}},"required":["group"],"additionalProperties":false}`, enum)
}

// loadCapability arms one group and says what happened. The second answer is
// whether this was a TOOL ERROR: a group this build does not have, and an
// arming that failed, are failures and are reported as failures, so the model
// reads them the way it reads every other refused call rather than as a success
// whose tools never turn up.
//
// THE GROUP NAME IS MATCHED EXACTLY. The schema constrains it to an enum, so a
// near miss is a mistake to be told about and not one to be guessed at.
func (a *Agent) loadCapability(want string) (string, bool) {
	a.armMu.Lock()
	tools := a.shelf[want]
	known := false
	for _, have := range a.shelfOrder {
		if have == want {
			known = true
			break
		}
	}
	offered := append([]string(nil), a.shelfOrder...)
	a.armMu.Unlock()

	if !known || len(tools) == 0 {
		if len(offered) == 0 {
			return "There is no group to load: every tool this build has is already in your tool list.", true
		}
		// A MISTYPED GROUP IS ANSWERED WITH THE REAL ONES, the way a good CLI
		// answers a mistyped flag, so the next call is right rather than a
		// second guess.
		return fmt.Sprintf("There is no group called %q. The groups here are: %s.",
			want, strings.Join(offered, ", ")), true
	}

	// AND THE ARMING IS armFamily's, unchanged: it dedupes against what is held,
	// appends at the tail under armMu and rebuilds nothing, so a group loaded
	// twice costs one cheap line at the END of the transcript (connect.go).
	armed, err := a.armFamily(tools)
	if err != nil {
		return "The " + want + " tools could not be loaded: " + err.Error(), true
	}
	if len(armed) == 0 {
		return "Already loaded — " + strings.Join(toolNames(tools), ", ") +
			" are in your tool list now. Use them; do not ask again.", false
	}
	// AND THE `Loaded: ` LINE STAYS FIRST AND UNCHANGED. checkpoint.go's
	// [loadedAndNeverUsed] reads the armed names back off it, up to its first
	// full stop, so the group's prose goes UNDER it — parted by a blank line,
	// the way the page it came out of parted its own paragraphs.
	return loadedLead + strings.Join(armed, ", ") +
		". Full schemas arrive on your next model request. Continue in this same turn. " +
		"These tools remain loaded while this engine runs." + capabilityProse(want), false
}

// capabilityProse is one group's paragraph, ready to hang under a load's answer,
// and the empty string where the group has none.
func capabilityProse(name string) string {
	for _, group := range capabilityGroups {
		if group.name == name && group.prose != "" {
			return "\n\n" + group.prose
		}
	}
	return ""
}

// rearmLoadedCapabilities puts back the groups an EARLIER PROCESS of this
// conversation loaded, when those calls remain in the saved transcript.
//
// A reopened session rebuilds its belt from scratch (agent.go's New), so
// without this the replayed transcript would still carry "Loaded: settings,
// change_setting" while the tool block carried neither — the model would reach
// for what its own history says it holds and be answered `Unknown tool`, which
// is the exact defect beltfacts.go exists to prevent. The record is the
// journal's own tool calls and nothing is stored beside them.
//
// THE LIMIT IS THE TRANSCRIPT'S. A load that has since been compacted away is
// not replayed, and neither is the line claiming it — so the two stay in step,
// and the model simply loads again.
func (a *Agent) rearmLoadedCapabilities(restored []ai.Message) {
	for _, message := range restored {
		for _, call := range message.ToolCalls {
			if call.Function.Name != loadCapabilityToolName {
				continue
			}
			var parsed struct {
				Group string `json:"group"`
			}
			if err := json.Unmarshal([]byte(call.Function.Arguments), &parsed); err != nil {
				continue
			}
			// A group this build no longer has answers with its refusal, which
			// nobody reads here: a machine that lost its media models must not
			// arm media because yesterday's transcript asked for it.
			_, _ = a.loadCapability(parsed.Group)
		}
	}
}

// capabilityGroupOf answers which group a tool name belongs to, for the tests
// and gates that need the mapping without building a belt. Empty means the name
// is in no group at all and is therefore carried by every shape.
//
// IT WALKS BOTH TABLES because the mapping is a fact about the name and not
// about the shape: `propose_task` is in the `tasks` group whether or not this
// shape shelves that group. Whether it is actually held back is
// [Config.shelvesTool], which is the question with a config behind it. The two
// tables may not claim the same name, which promptprofile_test.go refuses.
func capabilityGroupOf(tool string) string {
	for _, group := range capabilityGroups {
		for _, member := range group.members {
			if member == tool {
				return group.name
			}
		}
	}
	for _, group := range leanCapabilityGroups {
		for _, member := range group.members {
			if member == tool {
				return group.name
			}
		}
	}
	return ""
}

// ── what this build can offer ───────────────────────────────────────────────

// offeredTools is the belt PLUS the shelf, each tool ONCE: every tool this
// build has, whether the model is carrying it yet or not.
//
// IT IS THE QUESTION EVERY LAW ABOUT CAPABILITY ASKS. "Does the manual mention
// every tool", "does a media machine have the making verbs" are questions about
// what the build can DO, and shelving changes only when a schema is sent — so
// the gates ask this, and a shelved tool goes on owing a page. [Agent.hasTool]
// remains the narrower question the TURN asks: what the model may call now.
//
// ONE SNAPSHOT UNDER ONE HOLD. A loaded group is on the belt AND still on the
// construction-time shelf, so the two halves are joined here, deduplicated by
// name with the belt winning, and read under a single armMu so a load landing
// mid-walk cannot make this list disagree with itself.
func (a *Agent) offeredTools() []bare.Tool {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	offered := make([]bare.Tool, 0, len(a.tools)+8)
	held := make(map[string]bool, len(a.tools)+8)
	for _, tool := range a.tools {
		offered = append(offered, tool)
		held[tool.Name] = true
	}
	for _, name := range a.shelfOrder {
		for _, tool := range a.shelf[name] {
			if held[tool.Name] {
				continue
			}
			held[tool.Name] = true
			offered = append(offered, tool)
		}
	}
	return offered
}

// offers says whether this build has the verb at all — carried now, or one
// `load_capability` call away.
func (a *Agent) offers(name string) bool {
	for _, tool := range a.offeredTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}
