package session

// THE TWO VERBS A FACTORY STAGE ENDS WITH.
//
// A stage conversation (stage_contract.go) is opened by the factory's runner
// with a brief, and the runner is waiting in code for what it found.
// `stage_result` is how the conversation says it, and `plan_edit` is how the
// plan stage proposes a different set of stages for the rest of the item.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - BOTH ARE ABSENT WITHOUT THE DOOR. [Config.mayStage] is the one
//     predicate: an ordinary conversation, a task node and every door that is
//     not a stage round never carry either verb, because a model told it can
//     report a stage plans around a runner that is not there.
//
//   - NEITHER RAISES A CARD AND NEITHER POSTS. The stage's result is the
//     runner's to read; the person sees it on the floor. Writing to an item's
//     source is a post stage's, and a post stage is not a conversation.
//
//   - THE MODEL FILLS, CODE DECIDES. Nothing here says whether the stage met
//     its `until`; the runner asks [factory.Met] of what was reported. A
//     proposal is applied by the runner through [factory.Adapt], whose bounds
//     a description cannot loosen.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// stageResultToolName is the verb a stage reports with, and the one a turn
// may be made to call ([Agent.FollowUpRequiring]).
const stageResultToolName = "stage_result"

// The sentences the model reads back. The manual quotes them
// (internal/manual/chat/factory-stage-conversations.md).
const (
	stageReported      = "reported · the last report you send before you stop is the one the factory reads"
	stageNotReported   = "not reported: "
	planProposed       = "proposed · the runner applies it within the recipe's bounds"
	planNotProposed    = "not proposed: "
	stageNeedsDone     = "Invalid arguments: stage_result needs done, true when the ask was carried out and false when it was not."
	stageNeedsFindings = "Invalid arguments: findings is a count of problems still open, zero or more."
	stageNeedsClaim    = "Invalid arguments: every claim needs its text."
	planNeedsChange    = "Invalid arguments: plan_edit needs at least one of add, skip or on."
)

const stageResultDescription = "This conversation IS one stage of a factory item, and the ask you were given is its brief. " +
	"Work as in any conversation, handing independent parts to tasks. " +
	"Report this step's result. Call it again if your result changes; the last call before you stop is the one that counts. " +
	"done says whether the ask was carried out; findings counts the problems still open, and a stage that runs until clean needs zero; " +
	"claims are what you say is now true, each with ok, its evidence and the medium that shows it (test, screenshot, transcript, benchmark, policy); " +
	"notes are sentences for the stages after this one; output is what you did and found, which they read. " +
	"Nothing is posted anywhere, by this or by anything in the stage."

const planEditDescription = "Propose a change to this factory item's stages, from inside one of them. " +
	"add is new stage sentences, a place word first when it matters (\"after test, read it for auth holes\"); skip and on are stage names the item already has. " +
	"Nothing changes by calling this: the runner applies it once this stage ends, within the recipe's bounds, and refuses what crosses them; proof and a person's gate are never skipped. " +
	"It never posts anywhere."

func stageResultSchemaJSON() string {
	return `{"type":"object","properties":{` +
		`"done":{"type":"boolean","description":"True when the ask was carried out."},` +
		`"findings":{"type":"integer","minimum":0,"description":"Problems still wrong after your fixes."},` +
		`"claims":{"type":"array","items":{"type":"object","properties":{` +
		`"text":{"type":"string"},"ok":{"type":"boolean"},"evidence":{"type":"string"},` +
		`"medium":{"type":"string","description":"test, screenshot, transcript, benchmark or policy."}` +
		`},"required":["text","ok","evidence"],"additionalProperties":false},"description":"What is now true, each with its evidence."},` +
		`"notes":{"type":"array","items":{"type":"string"},"description":"Sentences for the stages after this one."},` +
		`"output":{"type":"string","description":"What you did and found."}` +
		`},"required":["done"],"additionalProperties":false}`
}

func planEditSchemaJSON() string {
	return `{"type":"object","properties":{` +
		`"add":{"type":"array","items":{"type":"string"},"description":"Stage sentences to add."},` +
		`"skip":{"type":"array","items":{"type":"string"},"description":"Stage names to switch off."},` +
		`"on":{"type":"array","items":{"type":"string"},"description":"Stage names to switch on."},` +
		`"why":{"type":"string","description":"One sentence saying why."}` +
		`},"additionalProperties":false}`
}

// The gloss a person reads beside each call: what the stage did, and why the
// plan wants a change.
func init() {
	glossField[stageResultToolName] = "output"
	glossField["plan_edit"] = "why"
}

// stageTools is the two verbs, or nothing at all when this conversation is not
// a stage ([Config.mayStage]).
func (a *Agent) stageTools() []bare.Tool {
	if !a.config.mayStage() {
		return nil
	}
	return []bare.Tool{a.stageResultTool(), a.planEditTool()}
}

// stageClaim is one claim as the model writes it.
type stageClaim struct {
	Text     string `json:"text"`
	OK       bool   `json:"ok"`
	Evidence string `json:"evidence"`
	Medium   string `json:"medium"`
}

// stageResultArgs is `stage_result`'s arguments. Done is a pointer so a call
// that never said it is told so, rather than read as a stage that failed.
type stageResultArgs struct {
	Done     *bool        `json:"done"`
	Findings int          `json:"findings"`
	Claims   []stageClaim `json:"claims"`
	Notes    []string     `json:"notes"`
	Output   string       `json:"output"`
}

// parseStageResult reads the arguments into the result the door is handed,
// or answers the sentence that says what is missing.
func parseStageResult(args json.RawMessage) (factory.StageResult, string) {
	var parsed stageResultArgs
	if len(args) > 0 {
		if err := decodeToolArguments(args, &parsed); err != nil {
			return factory.StageResult{}, "Invalid arguments: " + err.Error()
		}
	}
	if parsed.Done == nil {
		return factory.StageResult{}, stageNeedsDone
	}
	if parsed.Findings < 0 {
		return factory.StageResult{}, stageNeedsFindings
	}
	result := factory.StageResult{
		Done:     *parsed.Done,
		Findings: parsed.Findings,
		Notes:    cleanNames(parsed.Notes),
		Output:   strings.TrimSpace(parsed.Output),
	}
	for _, c := range parsed.Claims {
		text := oneLine(c.Text)
		if text == "" {
			return factory.StageResult{}, stageNeedsClaim
		}
		result.Claims = append(result.Claims, factory.Claim{
			Text:     text,
			OK:       c.OK,
			Evidence: strings.TrimSpace(c.Evidence),
			Medium:   strings.ToLower(oneLine(c.Medium)),
		})
	}
	return result, ""
}

// stageResultTool hands the round's result to the door. EVERY CALL IS HANDED
// ON, and the door keeps the last one: a step that reported early and went on
// working says so again. The door is also the one place a report is refused,
// and a refusal is read back as `not reported: <why>` in the same turn, so the
// model can put the report right and call again.
func (a *Agent) stageResultTool() bare.Tool {
	return bare.Tool{
		Name:        stageResultToolName,
		Description: stageResultDescription,
		Schema:      json.RawMessage(stageResultSchemaJSON()),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			result, invalid := parseStageResult(args)
			if invalid != "" {
				return invalid, true, nil
			}
			if err := a.config.Stage.Report(ctx, result); err != nil {
				return stageNotReported + oneLine(err.Error()), true, nil
			}
			return stageReported, false, nil
		},
	}
}

// parsePlanEdit reads `plan_edit`'s arguments into the edit the door is
// handed. An added sentence is a conversation stage with that ask; Adapt names
// and places it, as `factory_stages`' edit does ([stagesEdit]).
func parsePlanEdit(args json.RawMessage) (factory.PlanEdit, string) {
	var parsed struct {
		Add  []string `json:"add"`
		Skip []string `json:"skip"`
		On   []string `json:"on"`
		Why  string   `json:"why"`
	}
	if len(args) > 0 {
		if err := decodeToolArguments(args, &parsed); err != nil {
			return factory.PlanEdit{}, "Invalid arguments: " + err.Error()
		}
	}
	edit := factory.PlanEdit{Skip: cleanNames(parsed.Skip), On: cleanNames(parsed.On), Why: oneLine(parsed.Why)}
	for _, add := range cleanNames(parsed.Add) {
		if factory.ParseStage(add).Ask == "" {
			return factory.PlanEdit{}, "Invalid arguments: a stage to add needs to say what it does: " + add + "."
		}
		edit.Add = append(edit.Add, factory.Stage{Ask: add})
	}
	if edit.Empty() {
		return factory.PlanEdit{}, planNeedsChange
	}
	return edit, ""
}

// planEditTool hands a proposal to the door. Nothing is applied here.
func (a *Agent) planEditTool() bare.Tool {
	return bare.Tool{
		Name:        "plan_edit",
		Description: planEditDescription,
		Schema:      json.RawMessage(planEditSchemaJSON()),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			edit, invalid := parsePlanEdit(args)
			if invalid != "" {
				return invalid, true, nil
			}
			if err := a.config.Stage.Edit(ctx, edit); err != nil {
				return planNotProposed + oneLine(err.Error()), true, nil
			}
			return planProposed, false, nil
		},
	}
}

// ── a turn that must report ─────────────────────────────────────────────────
//
// A STAGE THAT FORGOT ITS REPORT IS ASKED ON A TURN THAT CANNOT ANSWER IN
// WORDS. The runner reminds a stage that stopped without `stage_result`
// (internal/factory/run's chatstage.go), and the last of its reminders is a
// turn whose first request names the verb in `tool_choice`, the provider's own
// required-tool setting: a model that honours it answers with the call. It is
// one request and not the whole turn, because a turn that had to call the
// verb on every request could never end; the call is answered, the next
// request is free again, and the turn ends as any other does.
//
// A PROVIDER THAT IGNORES THE SETTING IS NOT AN ERROR. Its model answers as it
// would have, and a turn that still did not report leaves the runner its last
// resort ([Agent.InferStageResult]). A tool the belt does not carry is never
// named: a choice of a function the request does not define is refused by every
// provider that reads it.

// FollowUpRequiring is [Agent.FollowUp] for a turn whose first answer must be
// a call to tool: the requirement rides the message, so a follow-up queued
// behind a running turn carries it into the turn it opens and no other.
func (a *Agent) FollowUpRequiring(text, tool string) (<-chan Event, error) {
	return a.followUpMarked(text, nil, strings.TrimSpace(tool))
}

// turnBelt is the options a turn's request is sent with: the belt, and when the
// turn still owes a call to one tool on it, the choice that requires it. must is
// that tool, "" for none, for [Agent.spendMustCall] once the request is answered.
func (a *Agent) turnBelt() (options []ai.Option, must string) {
	definitions := a.beltDefinitions()
	options = []ai.Option{ai.WithTools(definitions)}
	a.mu.Lock()
	must = a.mustCall
	a.mu.Unlock()
	if must == "" || !definesTool(definitions, must) {
		return options, ""
	}
	return append(options, requireTool(must)), must
}

// spendMustCall lets the turn go free once a request that required must has
// been answered, whatever the answer said.
func (a *Agent) spendMustCall(must string) {
	if must == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mustCall == must {
		a.mustCall = ""
	}
}

// requireTool sets `tool_choice` to the one named function, in the chat
// completions spelling every road out of this package carries or translates
// (internal/codexauth's transport writes the Responses API's form of it). It
// is applied after [ai.WithTools], which sets `auto`.
func requireTool(name string) ai.Option {
	return func(request *ai.Request) error {
		request.ToolChoice = map[string]any{"type": "function", "function": map[string]any{"name": name}}
		return nil
	}
}

// definesTool says whether the definitions carry the named tool.
func definesTool(definitions []ai.ToolDefinition, name string) bool {
	for _, definition := range definitions {
		if definition.Function.Name == name {
			return true
		}
	}
	return false
}

// ── the report the factory makes for a step ─────────────────────────────────

// errNoStageReport is a last-resort call that came back without a report.
var errNoStageReport = errors.New("the model answered without a report")

// errNotAStage is a report asked of a conversation that is not a stage.
var errNotAStage = errors.New("this conversation is not a factory stage")

// InferStageResult makes this stage's report in one short call apart from its
// own turns, on its own model, for a stage that stopped without one however it
// was asked. ask is the whole of what the call reads (the runner writes it: the
// step's brief, its last message and its change), and the call carries
// `stage_result` alone and requires it. NOTHING REACHES THE DOOR: the result
// is handed back for the runner to mark as its own, and a call that answers in
// words, or with a report that does not parse, is an error that says so. What
// it costs is this conversation's, as every errand it runs is.
func (a *Agent) InferStageResult(ctx context.Context, ask string) (factory.StageResult, error) {
	if !a.config.mayStage() {
		return factory.StageResult{}, errNotAStage
	}
	definitions, err := toolDefinitions([]bare.Tool{a.stageResultTool()})
	if err != nil {
		return factory.StageResult{}, err
	}
	messages := []ai.Message{textMessage("user", ask)}
	response, called, err := a.callRole(withDetachedUsage(ctx), roles.RoleWorker, a.Model(), messages,
		ai.WithTools(definitions), requireTool(stageResultToolName))
	if err != nil {
		return factory.StageResult{}, err
	}
	a.addDetachedUsageAs(response, called, 1, string(roles.RoleWorker))
	if response == nil || len(response.Choices) == 0 {
		return factory.StageResult{}, errNoStageReport
	}
	for _, call := range response.Choices[0].Message.ToolCalls {
		if call.Function.Name != stageResultToolName {
			continue
		}
		result, invalid := parseStageResult(json.RawMessage(call.Function.Arguments))
		if invalid != "" {
			return factory.StageResult{}, errors.New(invalid)
		}
		return result, nil
	}
	return factory.StageResult{}, errNoStageReport
}

// LastWords is the last thing this conversation's model said in words, ""
// when it has said nothing yet: what a stage that stopped without reporting
// left as its account of itself.
func (a *Agent) LastWords() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].Role != "assistant" {
			continue
		}
		if text := strings.TrimSpace(messageContentText(a.messages[i])); text != "" {
			return text
		}
	}
	return ""
}
