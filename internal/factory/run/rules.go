package run

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// JobRules are the sentences a job's steps must follow: the repository's own
// rules (its recipe's policy, read from the file the job runs, [jobRecipe])
// and then the factory's own ([factory.Item.House]), each once, ignoring
// case. recipe may be nil, and a job queued before the factory kept rules has
// none of its own; either half alone is still the answer.
func JobRules(recipe func(it factory.Item) factory.Recipe, it factory.Item) []string {
	var repo []string
	if r, ok := jobRecipe(recipe, it); ok {
		repo = r.Policy
	}
	var house []string
	if it.House != nil {
		house = it.House.Rules
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range append(append([]string(nil), repo...), house...) {
		line = strings.TrimSpace(line)
		key := strings.ToLower(line)
		if line == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, line)
	}
	return out
}

// jobRecipe is the recipe a job's rules, a plan's reshaping and a post's
// policy are bounded by: recipe's answer for the job, which reads THE
// FACTORY'S COPY THE JOB CARRIES ([factory.Item.PipelineCopy]) first
// (pipeline.JobFile); with no recipe, that copy alone when the job carries
// one. ok is false when there is neither. A runner on another machine cannot
// read the factory's store, so the job is the only place it learns that the
// factory replaced main's file.
func jobRecipe(recipe func(it factory.Item) factory.Recipe, it factory.Item) (factory.Recipe, bool) {
	if recipe != nil {
		return recipe(it), true
	}
	if it.PipelineCopy == nil {
		return factory.Recipe{}, false
	}
	f, _ := pipeline.JobFile(it.PipelineCopy, "", func() (pipeline.File, error) { return pipeline.Defaults(), nil })
	return f.Recipe(), true
}

// rulesBrief is the brief's paragraph of rules, the job's [Job.Rules] and
// then the ask-first and never lines it started under ([factory.StartRules]),
// so a step knows the paths it must ask about before it touches them, not
// only the approval after the plan:
//
//	rules for this work (follow every one; if the work would break one, stop and ask through ask instead):
//	- don't touch auth without checking with me
//	- no new dependencies without asking
//	- ask before touching: auth/, billing/, infra/
//	- ask before you: change the public API
//
// (infra/ there is a never path: a protected path like the ask-first ones.)
//
// "" when there are none (the emptiness law).
func rulesBrief(job Job) string {
	var lines []string
	for _, r := range job.Rules {
		if r = oneLine(r); r != "" {
			lines = append(lines, "- "+r)
		}
	}
	if sr := job.Item.StartRules; sr != nil {
		// EVERY PROTECTED PATH IS ONE LINE: ask-first path lines and never
		// paths hold a job the same way, so the steps are told the same thing.
		var paths, sentences []string
		for _, p := range pipeline.ProtectedPaths(sr.AskFirst, sr.Never) {
			if p = oneLine(p); p != "" {
				paths = append(paths, p)
			}
		}
		for _, rule := range sr.AskFirst {
			if pipeline.AskFirstPath(rule) == "" {
				if r := oneLine(rule); r != "" {
					sentences = append(sentences, r)
				}
			}
		}
		if len(paths) > 0 {
			lines = append(lines, "- ask before touching: "+strings.Join(paths, ", "))
		}
		if len(sentences) > 0 {
			lines = append(lines, "- ask before you: "+strings.Join(sentences, "; "))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(append([]string{"rules for this work (follow every one; if the work would break one, stop and ask through ask instead):"}, lines...), "\n")
}

// withHouse is a repository's policy lines followed by the job's house rules,
// so code applies a rule it recognises whichever of the two said it.
func withHouse(policy []string, it factory.Item) []string {
	if it.House == nil || len(it.House.Rules) == 0 {
		return policy
	}
	return append(append([]string(nil), policy...), it.House.Rules...)
}
