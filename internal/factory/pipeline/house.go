package pipeline

import (
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ErrHouseRulesUnread marks a read whose factory rules could not be read, as
// against one whose repository file could not: a start may go on with the
// defaults for a broken repository file, never without the factory's rules.
var ErrHouseRulesUnread = errors.New("the factory's own rules could not be read")

// WithHouseRules adds the factory's own rules ([factory.HouseRules]) to one
// repository's read, the way a personal file tightens it: its rules join the
// repository's, its ask-first lines join `start.ask_first` and its never paths
// join `start.never`. NOTHING THE REPOSITORY SAYS IS DROPPED OR LOOSENED, so a
// house rule can only make a job ask sooner or a step follow one more sentence.
//
// The repository's Text is left as it was read; House records what was added.
// The file is copied first, so a cached read is never changed in place.
func WithHouseRules(f File, house factory.HouseRules) File {
	out := cloneFile(f)
	if house.Empty() {
		return out
	}
	out.Rules = unionFold(out.Rules, house.Rules)
	out.Start.AskFirst = unionFold(out.Start.AskFirst, house.AskFirst)
	out.Start.Never = unionExact(out.Start.Never, house.Never)
	if house.Adapt != "" {
		out.Adapt, _ = factory.StricterAdapt(out.Adapt, house.Adapt)
	}
	// THE ONE ADDITION THAT LETS MORE START: the factory's start labels, and
	// only while a person has turned Start on its own on. Every limit and
	// ask-first line still applies to what they let in.
	if house.StartOnItsOwn {
		out.Start.Labels = unionFold(out.Start.Labels, house.StartLabels)
	}
	out.House = factory.HouseRules{
		Rules:    append([]string{}, house.Rules...),
		AskFirst: append([]string{}, house.AskFirst...),
		Never:    append([]string{}, house.Never...),
		Adapt:    house.Adapt,
		// The switch and its labels are the factory's settings, recorded so a
		// client can say where a start label came from.
		StartOnItsOwn: house.StartOnItsOwn,
		StartLabels:   append([]string(nil), house.StartLabels...),
	}
	return out
}

// StartRulesOf is f's ask-first and never lines as a job is stamped with them
// when it starts ([factory.Item.StartRules]): copies, never nil lists.
func StartRulesOf(f File) *factory.StartRules {
	return &factory.StartRules{
		AskFirst: append([]string{}, f.Start.AskFirst...),
		Never:    append([]string{}, f.Start.Never...),
	}
}

// PlanHold is why a finished plan must be approved by a person: the first
// ask-first path rule (`touch auth/`, `touches auth/`, `path: auth/`) or never
// path a planned file matches, by the same matcher a start uses, or "" when
// none does. A sentence rule that names no path is judged before a start, not
// here: a plan's files can only answer a question about paths. A plan that
// names no files, under a path rule, is held too.
func PlanHold(rules *factory.StartRules, files []string) string {
	if rules == nil {
		return ""
	}
	// A PLAN WHOSE FILES ARE UNKNOWN CANNOT CLEAR A PATH RULE: with one in
	// force, a plan that declared no list (or an ambiguous one) is a person's.
	if len(files) == 0 {
		if len(rules.Never) > 0 {
			return "ask first · the plan names no files"
		}
		for _, rule := range rules.AskFirst {
			if askFirstPath(rule) != "" {
				return "ask first · the plan names no files"
			}
		}
		return ""
	}
	for _, rule := range rules.AskFirst {
		glob := underDir(askFirstPath(rule))
		if glob == "" {
			continue
		}
		for _, path := range files {
			if touches(glob, path) {
				return "ask first · " + strings.TrimSpace(rule) + " · " + path
			}
		}
	}
	never := make([]string, 0, len(rules.Never))
	for _, glob := range rules.Never {
		never = append(never, underDir(glob))
	}
	if why := neverWhy(never, files); why != "" {
		return "never starts on its own · " + why
	}
	return ""
}

// askFirstPath is the path an ask-first rule names, or "" for a sentence. Only
// the prefix is read without case: the path keeps the case it was written in,
// because a path is matched as written (`touch src/Auth/` guards src/Auth/).
func askFirstPath(rule string) string {
	words := strings.TrimSpace(rule)
	for _, prefix := range []string{"touches ", "touch ", "path:"} {
		if len(words) >= len(prefix) && strings.EqualFold(words[:len(prefix)], prefix) {
			return strings.TrimSpace(words[len(prefix):])
		}
	}
	return ""
}

// underDir reads a pattern that ends in a slash as everything under that
// folder (`auth/` is `auth/**`), which is what a person writing "touch auth/"
// means, of a plan's files and of the paths an issue names alike. A start
// still matches the pattern as written too, so an issue that names the folder
// itself asks as before.
func underDir(glob string) string {
	glob = strings.TrimSpace(glob)
	if strings.HasSuffix(glob, "/") {
		return glob + "**"
	}
	return glob
}

// underDirs is [underDir] of every pattern, so a folder a never line names
// covers what is under it at a start as it does in a plan.
func underDirs(globs []string) []string {
	out := make([]string, 0, len(globs))
	for _, g := range globs {
		out = append(out, underDir(g))
	}
	return out
}
