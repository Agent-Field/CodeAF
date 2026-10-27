package run

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// JobRules are the sentences a job's steps must follow: the repository's own
// rules (its recipe's policy, read from its `.codeaf/factory.yaml`) and then
// the factory's own ([factory.Item.House]), each once, ignoring case. recipe
// may be nil, and a job queued before the factory kept rules has none of its
// own; either half alone is still the answer.
func JobRules(recipe func(repo string) factory.Recipe, it factory.Item) []string {
	var repo []string
	if recipe != nil {
		repo = recipe(it.Repo).Policy
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

// rulesBrief is the brief's paragraph of rules, the job's [Job.Rules] and
// then the ask-first and never lines it started under ([factory.StartRules]),
// so a step knows the paths it must ask about before it touches them, not
// only the approval after the plan:
//
//	rules for this work (follow every one; if the work would break one, stop and ask through ask instead):
//	- don't touch auth without checking with me
//	- no new dependencies without asking
//	- ask before touching: auth/, billing/
//	- ask before you: change the public API
//	- never touch: infra/
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
		var paths, sentences, never []string
		for _, rule := range sr.AskFirst {
			if p := oneLine(pipeline.AskFirstPath(rule)); p != "" {
				paths = append(paths, p)
			} else if r := oneLine(rule); r != "" {
				sentences = append(sentences, r)
			}
		}
		for _, p := range sr.Never {
			if p = oneLine(p); p != "" {
				never = append(never, p)
			}
		}
		if len(paths) > 0 {
			lines = append(lines, "- ask before touching: "+strings.Join(paths, ", "))
		}
		if len(sentences) > 0 {
			lines = append(lines, "- ask before you: "+strings.Join(sentences, "; "))
		}
		if len(never) > 0 {
			lines = append(lines, "- never touch: "+strings.Join(never, ", "))
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
