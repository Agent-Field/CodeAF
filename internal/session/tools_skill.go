package session

// The use_skill hand: the shelf of active skills, listed or resolved by name.
//
// A SKILL IS AN EXECUTION-VERIFIED PROCEDURE the distiller saved as a
// store.Fact of kind "skill", kept on a shelf directory its Artifact points at.
// Until now nothing a worker held could reach one: the shelf was written to and
// promoted, and the only reader was a person with the CLI. This is the worker's
// door onto it — mid-run discovery rather than a prompt fact, so a worker that
// finds itself doing something the shelf has a recipe for can fetch it.
//
// TWO MODES, ONE VERB, because the two questions come together: `list` shows
// what is there (name and the one-line doc, never a path or internal field), and
// `get` resolves one name to the shelf path the worker will actually open. It is
// one tool the way `jobs` and `settings` are one tool with an action, not two,
// because the model reaches for the list to decide whether the get is worth it.
//
// IT IS GATED EXACTLY AS propose_task IS ([Agent.mayProposeTask]) and on one
// thing more: a store to read the shelf FROM. A node on the floor of its tree
// already has no kids and is handed no verb to make any; the same shape has no
// business rummaging a shelf either, and an agent with no Memory has no shelf to
// read. So the belt and the page agree by construction: [Config.mayProposeTask]
// AND a non-nil store, which is the predicate the belt fact is composed from
// (beltfacts.go) and the gate this method reads.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/store"
)

const useSkillToolName = "use_skill"

// skillShelfLimit bounds one listing. The shelf is a curated few, not a corpus,
// so a hundred is every skill any machine has ever held; a larger number would
// only change how much of a runaway shelf rides in one answer.
const skillShelfLimit = 100

// useSkillDescription says what the two modes are for in the model's own terms.
// It is bought on every request of every turn on a belt that carries it, so it
// names the gesture and nothing about the store behind it.
const useSkillDescription = "Reach the shelf of active skills — execution-verified procedures this project has saved. `list` shows every skill as a name and a one-line doc; `get` resolves one name to its shelf path and doc, which you then open with `read`."

// useSkillSchemaJSON is the two modes. `name` is required for `get` alone, which
// the mode enum cannot express, so the handler refuses a nameless get in words
// rather than leaning on the schema.
const useSkillSchemaJSON = `{
  "type": "object",
  "properties": {
    "mode": {"type": "string", "enum": ["list", "get"], "description": "list: show doc lines for all active skills. get: show shelf path and doc for one skill."},
    "name": {"type": "string", "description": "Required for get mode. The skill name (directory name on the shelf)."}
  },
  "required": ["mode"],
  "additionalProperties": false
}`

// useSkillTool is the verb, or nothing at all on a belt that may not have it.
//
// ABSENT-NOT-BROKEN, the law every conditional family on this belt is built on
// (tools.go): a model told it can reach a shelf it has no store behind will plan
// a reply around a call that can only refuse, so the verb is simply not there.
func (a *Agent) useSkillTool() []bare.Tool {
	// The belt's gate and the page's predicate are one predicate
	// (beltfacts.go's `use_skill` row holds this same line), so the sentence a
	// shape reads can never promise a verb its belt withheld.
	if !a.mayProposeTask() || a.config.Memory == nil {
		return nil
	}
	return []bare.Tool{{
		Name:        useSkillToolName,
		Description: useSkillDescription,
		Schema:      json.RawMessage(useSkillSchemaJSON),
		Execute:     a.runUseSkill,
	}}
}

// runUseSkill renders the shelf. Every bad call is an ordinary tool result
// rather than a Go error, the way the rest of the belt answers: a mode spelled
// wrongly is a call the model can make again.
func (a *Agent) runUseSkill(_ context.Context, args json.RawMessage) (string, bool, error) {
	var parsed struct {
		Mode string `json:"mode"`
		Name string `json:"name"`
	}
	if err := decodeToolArguments(args, &parsed); err != nil {
		return invalidArgumentsPrefix + err.Error(), true, nil
	}
	switch strings.TrimSpace(parsed.Mode) {
	case "list":
		return a.listSkills()
	case "get":
		name := strings.TrimSpace(parsed.Name)
		if name == "" {
			return invalidArgumentsPrefix + `mode "get" needs a name — the skill's directory name, as the list shows it`, true, nil
		}
		return a.getSkill(name)
	default:
		return invalidArgumentsPrefix + `mode takes "list" or "get"`, true, nil
	}
}

// listSkills is the shelf as discovery rows: one skill per line, name and doc,
// and NOTHING ELSE. The artifact path is deliberately withheld here — a list of
// hundred-byte paths is noise the model has not asked to open yet — and the doc
// is the one-line Body the skill was recorded with.
func (a *Agent) listSkills() (string, bool, error) {
	skills, err := a.config.Memory.SkillFacts(store.FactActive, skillShelfLimit)
	if err != nil {
		return "Could not read the skill shelf: " + err.Error(), true, nil
	}
	if len(skills) == 0 {
		return "No active skills on the shelf.", false, nil
	}
	lines := make([]string, 0, len(skills))
	for _, skill := range skills {
		lines = append(lines, "- "+filepath.Base(skill.Artifact)+": "+skill.Body)
	}
	return strings.Join(lines, "\n"), false, nil
}

// getSkill resolves one name to the shelf path the worker will open and the doc
// that says what it is for.
//
// THE NAME IS MATCHED BY THE DIRECTORY ON THE SHELF, not by the fact's scope or
// id: what a worker has is the name `list` printed, which is filepath.Base of
// the artifact, and matching anything else would answer a name the model cannot
// see.
func (a *Agent) getSkill(name string) (string, bool, error) {
	skills, err := a.config.Memory.SkillFacts(store.FactActive, skillShelfLimit)
	if err != nil {
		return "Could not read the skill shelf: " + err.Error(), true, nil
	}
	for _, skill := range skills {
		if filepath.Base(skill.Artifact) != name {
			continue
		}
		// TODO: call store.SkillServe when it exists to mark consumption for consolidation weighting
		return fmt.Sprintf("%s: %s\nPath: %s", name, skill.Body, skill.Artifact), false, nil
	}
	return "Skill '" + name + "' not found.", false, nil
}
