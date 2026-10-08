package factory

import "strings"

// DefaultRecipe is the recipe a repo starts from before anybody banks one of
// its own: stages for an issue, and the fixed shapes for the two kinds an
// issue's stages do not describe. A pull request is read and checked; a red
// main is bisected and fixed. Every call answers fresh slices, so a caller may
// toggle its copy without reaching anybody else's.
//
// SECURITY IS BANKED OFF and switched on per item, so an item that asks for a
// security pass gains the stage without the recipe changing. THIS IS THE ONE
// PLACE THE DEFAULT STAGES ARE SPELLED: the mock's generated world and the
// local seam both start from it, so the two floors cannot disagree about what
// a fresh repo runs.
func DefaultRecipe() Recipe {
	return Recipe{
		Stages: []Stage{
			{Name: "plan", Ask: "read the issue and say how", Until: "done", On: true},
			{Name: "write", Ask: "do it in a worktree", Fanout: "per-file", Until: "done", On: true},
			{Name: "test", Ask: "run what the change implies", Until: "green", Max: 2, On: true},
			{Name: "review", Ask: "read it as a stranger would", Fanout: "per-finding", Until: "clean", Max: 1, On: true},
			{Name: "security", Ask: "secrets, injection and authz", Until: "clean", On: false},
			{Name: "proof", Ask: "show each claim in its own medium", Until: "proven", Gate: GateShip, On: true},
		},
		ByKind: map[Kind][]Stage{
			KindPR: {
				{Name: "read", Ask: "the diff and its claims", Until: "done", On: true},
				{Name: "checks", Ask: "run what the claims imply", Fanout: "per-claim", Until: "done", On: true},
				{Name: "review", Ask: "findings as a comment", Fanout: "per-finding", Until: "clean", Max: 1, On: true},
				{Name: "security", Ask: "secrets, injection and authz", Until: "clean", On: false},
				{Name: "proof", Ask: "the sheet", Until: "proven", Gate: GateShip, On: true},
			},
			KindCI: {
				{Name: "bisect", Ask: "the three red runs", Until: "done", On: true},
				{Name: "fix", Ask: "the smallest change that turns them green", Until: "done", On: true},
				{Name: "test", Ask: "the red test and its neighbours", Until: "green", Max: 2, On: true},
				{Name: "proof", Ask: "the sheet", Until: "proven", Gate: GateShip, On: true},
			},
		},
	}
}

// CopyStages is a stage list a caller may change without reaching the one it
// was copied from, the Proof lists included.
func CopyStages(in []Stage) []Stage {
	if in == nil {
		return nil
	}
	out := make([]Stage, len(in))
	copy(out, in)
	for i := range out {
		out[i].Proof = append([]string(nil), in[i].Proof...)
	}
	return out
}

// StageIndex is the first stage with that name, or -1.
func StageIndex(stages []Stage, name string) int {
	for i, s := range stages {
		if s.Name == name {
			return i
		}
	}
	return -1
}

// stageTimeWords are where a sentence may place a stage, in recipe order. With
// no time word a stage runs after review, which is where most of them belong.
var stageTimeWords = []string{"after plan", "after write", "after test", "after review", "before proof"}

// StageWhen is the time word a stage sentence starts with, or "after review".
func StageWhen(words string) string {
	low := strings.ToLower(strings.TrimSpace(words))
	for _, w := range stageTimeWords {
		if strings.HasPrefix(low, w) {
			return w
		}
	}
	return "after review"
}

// ParseStage reads "after review, make it neater" into a stage: a
// conversation whose ask is the words with the time word taken off, and whose
// name is the first two words of that ask. Its place comes from [StageWhen]
// and [PlaceStage], because A STAGE CARRIES NO TIME WORD: its place in the
// list is its time. An empty Ask is a sentence that said only when.
func ParseStage(words string) Stage {
	ask := strings.TrimSpace(words)
	low := strings.ToLower(ask)
	for _, w := range stageTimeWords {
		if strings.HasPrefix(low, w) {
			ask = ask[len(w):]
			break
		}
	}
	ask = strings.Trim(strings.TrimSpace(ask), " ,:;.")
	f := strings.Fields(strings.ToLower(ask))
	if len(f) > 2 {
		f = f[:2]
	}
	return Stage{Name: strings.Join(f, " "), Kind: StageChat, Ask: ask, Until: "done", On: true}
}

// stageAnchors are the stages a time word names. A stage placed "after X" goes
// after X and after whatever already sits between X and the next anchor, so
// stages banked at the same moment keep the order they were said in.
var stageAnchors = map[string]bool{"plan": true, "write": true, "test": true, "review": true, "proof": true}

// PlaceStage inserts st at the moment when names. A recipe without the named
// stage (a pull request has no plan) takes it just before proof, and a recipe
// without proof takes it at the end.
func PlaceStage(stages []Stage, st Stage, when string) []Stage {
	at := len(stages)
	if p := StageIndex(stages, "proof"); p >= 0 {
		at = p
	}
	if after, ok := strings.CutPrefix(when, "after "); ok {
		if i := StageIndex(stages, after); i >= 0 {
			at = i + 1
			for at < len(stages) && !stageAnchors[stages[at].Name] {
				at++
			}
		}
	}
	out := make([]Stage, 0, len(stages)+1)
	out = append(out, stages[:at]...)
	out = append(out, st)
	return append(out, stages[at:]...)
}

// AddStageWords is [ParseStage] and [PlaceStage] together: what the AddStage
// door does to an item's own copy of the recipe.
func AddStageWords(stages []Stage, words string) []Stage {
	return PlaceStage(stages, ParseStage(words), StageWhen(words))
}
