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
// business rummaging a shelf either, and an agent with no shelf store has
// nothing to read. So the belt and the page agree by construction:
// [Config.mayProposeTask] AND a non-nil [Config.skillShelf], which is the
// predicate the belt fact is composed from (beltfacts.go) and the gate this
// method reads. With memory off the live door still hands a shelf, so the verb
// is there whenever the person's skill folders are.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/fuzzy"
	"github.com/Agent-Field/codeaf/internal/store"
)

const useSkillToolName = "use_skill"

// skillShelfLimit bounds one listing, from the one source of truth.
const skillShelfLimit = store.SkillShelfLimit

// useSkillDescription says what the two modes are for in the model's own terms.
// It is bought on every request of every turn on a belt that carries it, so it
// names the gesture and nothing about the store behind it.
const useSkillDescription = "Reach the shelf of active skills — procedures this project saved after watching them run. `list` shows each as a name and one-line doc; `get` resolves one to its shelf path and doc to `read`."

// useSkillSchemaJSON is the two modes. `name` is required for `get` alone, which
// the mode enum cannot express, so the handler refuses a nameless get in words
// rather than leaning on the schema.
const useSkillSchemaJSON = `{
  "type": "object",
  "properties": {
    "mode": {"type": "string", "enum": ["list", "get"], "description": "list: every skill's name and doc line. get: one skill's shelf path and doc."},
    "name": {"type": "string", "description": "Required for get mode. The skill's directory name on the shelf."}
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
	if !a.mayProposeTask() || a.config.skillShelf() == nil {
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
	skills, err := a.config.skillShelf().SkillFacts(store.FactActive, skillShelfLimit)
	if err != nil {
		return "Could not read the skill shelf: " + err.Error(), true, nil
	}
	if len(skills) == 0 {
		return "No active skills on the shelf.", false, nil
	}
	// Sorted by name rather than in shelf order (which is newest first): a
	// listing that holds steady across two calls is what a model comparing one
	// against the other needs, and a reordered list reads as a shelf that moved.
	sorted := make([]store.Fact, len(skills))
	copy(sorted, skills)
	sort.Slice(sorted, func(i, j int) bool {
		return filepath.Base(sorted[i].Artifact) < filepath.Base(sorted[j].Artifact)
	})
	lines := make([]string, 0, len(sorted))
	for _, skill := range sorted {
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
// see. The match is case-folded — a folder called `Release-Notes` is not a
// different skill from `release-notes` — and the hit is answered with the
// shelf's own spelling, so the name a worker reads back is the one that works
// next time.
func (a *Agent) getSkill(name string) (string, bool, error) {
	skills, err := a.config.skillShelf().SkillFacts(store.FactActive, skillShelfLimit)
	if err != nil {
		return "Could not read the skill shelf: " + err.Error(), true, nil
	}
	for _, skill := range skills {
		if !strings.EqualFold(filepath.Base(skill.Artifact), name) {
			continue
		}
		artifact, doc, _, _, err := a.config.skillShelf().SkillFactAccessors(skill.Seq)
		if err != nil {
			return "Could not read skill: " + err.Error(), true, nil
		}
		// An agentskills folder's content is its SKILL.md and the directory
		// around it is what `read` refuses, so the path handed out is the body
		// file when there is one (store.SkillBodyFile, the one convention every
		// door keys on) and the directory otherwise — which for a forged skill is
		// the thing the worker runs. The result's shape does not change.
		path := artifact
		if body, ok := store.SkillBodyFile(artifact); ok {
			path = body
		}
		return fmt.Sprintf("%s: %s\nPath: %s", filepath.Base(skill.Artifact), doc, path), false, nil
	}
	// A MISS IS NOT A DEAD END. The model guessed a name, so the answer tells
	// it what the shelf holds (how many are active) and how close it got: the
	// nearest handful, scored with internal/fuzzy against the name and the doc
	// line, at most five. An empty shelf says so in one plain line.
	if len(skills) == 0 {
		return "No active skills on the shelf.", false, nil
	}
	terms := fuzzy.Terms(name)
	type scored struct {
		score int
		name  string
	}
	near := make([]scored, 0, 5)
	for _, skill := range skills {
		shelfName := filepath.Base(skill.Artifact)
		score, ok := fuzzy.ScoreFields([]string{shelfName, skill.Body}, terms)
		if !ok {
			continue
		}
		if len(near) == 5 && score <= near[4].score {
			continue
		}
		near = append(near, scored{score, shelfName})
		sort.SliceStable(near, func(i, j int) bool { return near[i].score > near[j].score })
		if len(near) > 5 {
			near = near[:5]
		}
	}
	if len(near) == 0 {
		return fmt.Sprintf("Skill %q not found. %d active skills on the shelf; none of them resembles that name.", name, len(skills)), false, nil
	}
	names := make([]string, 0, len(near))
	for _, s := range near {
		names = append(names, s.name)
	}
	return fmt.Sprintf("Skill %q not found. %d active skills on the shelf; nearest: %s.", name, len(skills), strings.Join(names, ", ")), false, nil
}
