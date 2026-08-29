package plan

import "strings"

// levelled turns what a spine SAYS about its stages into the shape the rest of
// the planner reads.
//
// A spine is written as a list, and a list has an order whether or not the
// work does. Asked for the stages of "seven independent tools", every model
// tried wrote seven stages — one per tool, in the order the tools were named —
// and the fan-out, bind and audit passes below all read that order as a
// sequence, so seventeen nodes ran one after another for fourteen minutes with
// one worker busy and sixteen waiting. The prompt begged for one stage; the
// list won.
//
// So the spine is asked for one more thing per stage, and it is a fact rather
// than a preference: which earlier stages' OUTPUT this one consumes. That list
// is the schedule. A stage's level is one more than the deepest stage it
// names, a stage that names nothing is level one, and the stages of one level
// are one stage — they run at the same time, which is what "independent" was
// always supposed to mean. A stage that only came before, and is not named, is
// not waited for.
//
// Names that point at nothing — a later stage, the stage itself, a number off
// the end — are not needs; they are ignored rather than trusted, because a
// need the planner cannot resolve is not a gate it can honour.
func levelled(stages []Stage) []Stage {
	if len(stages) < 2 || !answeredNeeds(stages) {
		return clearNeeds(stages)
	}
	levels := make([]int, len(stages))
	deepest := 0
	for index := range stages {
		level := 1
		for _, need := range stages[index].Needs {
			// Positions are 1-based and may only reach backwards.
			if need < 1 || need > index {
				continue
			}
			if levels[need-1]+1 > level {
				level = levels[need-1] + 1
			}
		}
		levels[index] = level
		if level > deepest {
			deepest = level
		}
	}
	merged := make([]Stage, 0, deepest)
	for level := 1; level <= deepest; level++ {
		var group []Stage
		for index, stage := range stages {
			if levels[index] == level {
				group = append(group, stage)
			}
		}
		merged = append(merged, oneStage(group))
	}
	return merged
}

// answeredNeeds reports whether the spine answered the question at all. A
// stage that consumes nothing says so with an empty list; a reply with no list
// on any stage — an endpoint that dropped the field, an older shape — did not
// say, and its order is kept as the schedule it always was rather than read as
// seven things that can all start now. The distinction is nil against empty,
// and it is the one place that distinction carries meaning.
func answeredNeeds(stages []Stage) bool {
	for _, stage := range stages {
		if stage.Needs != nil {
			return true
		}
	}
	return false
}

// oneStage folds the stages of one level into the single stage the fan-out
// will split into simultaneous parts. Each stage keeps its own name inside the
// summary, so a reader of the spine block — the fan-out, and a person reading
// the plan — still sees what was drawn.
func oneStage(group []Stage) Stage {
	if len(group) == 1 {
		return clearNeeds(group)[0]
	}
	titles := make([]string, 0, len(group))
	summaries := make([]string, 0, len(group))
	for _, stage := range group {
		titles = append(titles, stage.Title)
		summaries = append(summaries, stage.Title+": "+stage.Summary)
	}
	return Stage{
		Title:   strings.Join(titles, ", "),
		Summary: strings.Join(summaries, "; "),
	}
}

// clearNeeds drops the stated needs from stages that are already levelled:
// after this point the order of the list is the schedule, and a reader of the
// graph file should not find two accounts of it.
func clearNeeds(stages []Stage) []Stage {
	cleared := make([]Stage, len(stages))
	for index, stage := range stages {
		stage.Needs = nil
		cleared[index] = stage
	}
	return cleared
}
