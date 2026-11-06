// Package route decides, for one item arriving from any source, where it goes:
// which repository it is worked on, which named pipeline runs it, what
// priority it carries in the queue, and whether it starts by itself, asks
// first, or waits in Backlog. It is the factory's one router, read for a
// GitHub issue and a Jira ticket alike, so a source never decides policy.
//
// THE FIRST RULE THAT MATCHES DECIDES. Rules are an ordered list a person
// writes; there is no scoring and no merging of two rules, because a person
// reading the list must be able to say which line moved a ticket.
//
// docs/design/factory-general/DESIGN.md is the contract this package fills.
package route

import "github.com/Agent-Field/codeaf/internal/factory"

// Start is what a matched rule does with an item once it is routed.
type Start string

const (
	// StartAuto starts the job by itself, inside the repository's start rules
	// and the factory's spending limits, exactly as a labelled issue does today.
	StartAuto Start = "auto"
	// StartAsk holds the job with "Approval needed: start" until a person says yes.
	StartAsk Start = "ask"
	// StartBacklog files the job in Backlog and starts nothing.
	StartBacklog Start = "backlog"
)

// Match is what a rule looks at. Every non-empty field must hold; an empty
// field matches anything. Lists match when any one entry does. Text is a
// case-insensitive substring of the title or body.
type Match struct {
	Sources  []string `json:"sources,omitempty" yaml:"sources,omitempty"`   // "github", "jira", "inbox:<name>"
	Repos    []string `json:"repos,omitempty" yaml:"repos,omitempty"`       // owner/name, for items that already have one
	Labels   []string `json:"labels,omitempty" yaml:"labels,omitempty"`     // any of
	Authors  []string `json:"authors,omitempty" yaml:"authors,omitempty"`   // any of, handles or emails
	Text     []string `json:"text,omitempty" yaml:"text,omitempty"`         // any of
	Priority []string `json:"priority,omitempty" yaml:"priority,omitempty"` // any of p0..p3
}

// Action is what a matched rule decides. An empty field keeps what the item
// already has (an issue's own repository, the kind's pipeline, its priority).
type Action struct {
	Repo     string `json:"repo,omitempty" yaml:"repo,omitempty"`         // owner/name the job is worked on
	Pipeline string `json:"pipeline,omitempty" yaml:"pipeline,omitempty"` // a named pipeline in that repo's factory.yaml
	Priority string `json:"priority,omitempty" yaml:"priority,omitempty"` // p0..p3
	Start    Start  `json:"start,omitempty" yaml:"start,omitempty"`
}

// Rule is one line of the routing table.
type Rule struct {
	Name string `json:"name" yaml:"name"`
	When Match  `json:"when" yaml:"when"`
	Then Action `json:"then" yaml:"then"`
}

// Decide returns the first rule in rules that matches it. ok is false when no
// rule matches, and the caller then keeps today's behaviour for that item.
func Decide(rules []Rule, it factory.Item) (Rule, bool) {
	for _, r := range rules {
		if Matches(r.When, it) {
			return r, true
		}
	}
	return Rule{}, false
}

// Matches says whether m holds for it. The routing lane fills this in.
func Matches(m Match, it factory.Item) bool {
	_ = m
	_ = it
	return false
}
