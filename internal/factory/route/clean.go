package route

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// MaxRules is the most rules one factory keeps. A table a person cannot read
// to the end is one where nobody can say which line moved a ticket.
const MaxRules = 100

// MaxNameLen is the longest rule name; it is quoted in an ask-first hold.
const MaxNameLen = 64

// MaxAskLen is the longest question a rule may put to the judge
// ([Match.Ask]): one sentence a person can read in the table.
const MaxAskLen = 300

// maxEntryLen bounds every other word a rule holds.
const maxEntryLen = 200

// Clean is THE ONE CHECK of a routing table, used by the store before it
// keeps one and by the wire before it sends one, so a table the factory would
// refuse is refused in the same words everywhere. It trims every word, drops
// blank list entries, lower-cases what is compared that way (priorities,
// sources), and refuses the whole table when any rule is wrong: a table is
// never half-kept.
//
// A rule needs a name, unique without regard to case; a start of auto, ask or
// backlog; a priority, when it names one, of p0..p3; a repository, when it
// names one, of owner/name; and a pipeline, when it names one, spelled as a
// pipeline of a repository's file and not pr or ci, which are not an issue
// job's to run; and a question for the judge, when it asks one, of one line
// of at most [MaxAskLen] characters. Whether the target repository's file
// lists that pipeline is known only when an item is routed there.
func Clean(rules []Rule) ([]Rule, error) {
	if len(rules) > MaxRules {
		return nil, fmt.Errorf("a factory keeps at most %d routing rules", MaxRules)
	}
	out := make([]Rule, 0, len(rules))
	seen := make(map[string]bool, len(rules))
	for i, r := range rules {
		r, err := cleanRule(r)
		if err != nil {
			if r.Name == "" {
				return nil, fmt.Errorf("routing rule %d: %w", i+1, err)
			}
			return nil, fmt.Errorf("routing rule %q: %w", r.Name, err)
		}
		key := strings.ToLower(r.Name)
		if seen[key] {
			return nil, fmt.Errorf("routing rule %q: two rules have this name", r.Name)
		}
		seen[key] = true
		out = append(out, r)
	}
	return out, nil
}

func cleanRule(r Rule) (Rule, error) {
	r.Name = strings.TrimSpace(r.Name)
	switch {
	case r.Name == "":
		return r, fmt.Errorf("a rule needs a name")
	case len(r.Name) > MaxNameLen || strings.ContainsAny(r.Name, "\r\n"):
		return r, fmt.Errorf("a name is one line of at most %d characters", MaxNameLen)
	}
	var err error
	w := &r.When
	if w.Sources, err = cleanList("source", w.Sources, strings.ToLower, nil); err != nil {
		return r, err
	}
	if w.Repos, err = cleanList("repo", w.Repos, nil, repoWrong); err != nil {
		return r, err
	}
	if w.Labels, err = cleanList("label", w.Labels, nil, nil); err != nil {
		return r, err
	}
	if w.Authors, err = cleanList("author", w.Authors, nil, nil); err != nil {
		return r, err
	}
	if w.Text, err = cleanList("text", w.Text, nil, nil); err != nil {
		return r, err
	}
	if w.Priority, err = cleanList("priority", w.Priority, strings.ToLower, priorityWrong); err != nil {
		return r, err
	}
	w.Ask = strings.TrimSpace(w.Ask)
	if len(w.Ask) > MaxAskLen || strings.ContainsAny(w.Ask, "\r\n") {
		return r, fmt.Errorf("when ask is one line of at most %d characters", MaxAskLen)
	}
	t := &r.Then
	t.Repo, t.Pipeline = strings.TrimSpace(t.Repo), strings.TrimSpace(t.Pipeline)
	t.Priority = strings.ToLower(strings.TrimSpace(t.Priority))
	t.Start = Start(strings.ToLower(strings.TrimSpace(string(t.Start))))
	if t.Repo != "" {
		if why := repoWrong(t.Repo); why != "" {
			return r, fmt.Errorf("then repo %s", why)
		}
	}
	if t.Priority != "" {
		if why := priorityWrong(t.Priority); why != "" {
			return r, fmt.Errorf("then priority %s", why)
		}
	}
	if t.Pipeline != "" {
		switch {
		case !pipeline.KindNamed(t.Pipeline):
			return r, fmt.Errorf("then pipeline %q is not spelled as a pipeline of a repository's file", t.Pipeline)
		case t.Pipeline == string(factory.KindPR) || t.Pipeline == string(factory.KindCI):
			return r, fmt.Errorf("then pipeline %q is not an issue job's to run", t.Pipeline)
		}
	}
	switch t.Start {
	case StartAuto, StartAsk, StartBacklog:
	default:
		return r, fmt.Errorf("then start must be auto, ask or backlog")
	}
	return r, nil
}

// cleanList trims and drops blanks, folds each entry with fold when there is
// one, and refuses the first entry wrong names a problem with. An empty list
// answers nil, which matches anything.
func cleanList(what string, in []string, fold func(string) string, wrong func(string) string) ([]string, error) {
	var out []string
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if fold != nil {
			v = fold(v)
		}
		if len(v) > maxEntryLen || strings.ContainsAny(v, "\r\n") {
			return nil, fmt.Errorf("a %s is one line of at most %d characters", what, maxEntryLen)
		}
		if wrong != nil {
			if why := wrong(v); why != "" {
				return nil, fmt.Errorf("when %s %s", what, why)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func repoWrong(repo string) string {
	if factory.RepoName(repo) {
		return ""
	}
	return fmt.Sprintf("%q is not owner/name", repo)
}

func priorityWrong(p string) string {
	if _, ok := factory.PriorityFromShort(p); ok {
		return ""
	}
	return fmt.Sprintf("%q is not one of p0, p1, p2 or p3", p)
}
