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
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// The sentences the model reads back. The manual quotes them
// (internal/manual/chat/factory-stage-conversations.md).
const (
	stageReported      = "reported · the stage ends when you stop"
	stageNotReported   = "not reported: "
	planProposed       = "proposed · the runner applies it within the recipe's bounds"
	planNotProposed    = "not proposed: "
	stageNeedsDone     = "Invalid arguments: stage_result needs done, true when the ask was carried out and false when it was not."
	stageNeedsFindings = "Invalid arguments: findings is a count of problems still open, zero or more."
	stageNeedsClaim    = "Invalid arguments: every claim needs its text."
	planNeedsChange    = "Invalid arguments: plan_edit needs at least one of add, skip or on."
)

const stageResultDescription = "This conversation IS one stage of a factory item, and the ask you were given is its brief. " +
	"Work as in any conversation: read, change, run, and hand parts that do not need each other to tasks so they run in parallel. " +
	"End the stage by calling this ONCE, when the work is finished, with an honest account. " +
	"done says whether the ask was carried out; findings counts the problems still open, and a stage that runs until clean needs zero; " +
	"claims are what you say is now true, each with ok, its evidence and the medium that shows it (test, screenshot, transcript, benchmark, policy); " +
	"notes are sentences for the stages after this one; output is what you did, in a few lines. " +
	"Nothing is posted anywhere, by this or by anything in the stage. The stage ends when you stop after calling it."

const planEditDescription = "Propose a change to this factory item's stages, from inside one of them. " +
	"add is new stage sentences, a place word first when it matters (\"after test, read it for auth holes\"); skip and on are stage names the item already has. " +
	"Nothing changes by calling this: the runner applies it once this stage ends, within the recipe's bounds, and refuses what crosses them; proof and a person's gate are never skipped. " +
	"It never posts anywhere."

func stageResultSchemaJSON() string {
	return `{"type":"object","properties":{` +
		`"done":{"type":"boolean","description":"True when the ask was carried out."},` +
		`"findings":{"type":"integer","minimum":0,"description":"Problems still open."},` +
		`"claims":{"type":"array","items":{"type":"object","properties":{` +
		`"text":{"type":"string"},"ok":{"type":"boolean"},"evidence":{"type":"string"},` +
		`"medium":{"type":"string","description":"test, screenshot, transcript, benchmark or policy."}` +
		`},"required":["text","ok"],"additionalProperties":false},"description":"What is now true, each with its evidence."},` +
		`"notes":{"type":"array","items":{"type":"string"},"description":"Sentences for the stages after this one."},` +
		`"output":{"type":"string","description":"What you did, in a few lines."}` +
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
	glossField["stage_result"] = "output"
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

// stageResultTool hands the round's result to the door.
func (a *Agent) stageResultTool() bare.Tool {
	return bare.Tool{
		Name:        "stage_result",
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
