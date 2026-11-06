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

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/factory"
)

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
	// Ask is the AI filter: one yes/no question in a person's words, such as
	// "Is this a small, well-specified bug fix that does not touch billing?".
	// It is asked LAST, only when every other field already holds, so a model
	// is paid only for items that could match. A model answers it ([Judge]);
	// Decide itself never calls one.
	Ask string `json:"ask,omitempty" yaml:"ask,omitempty"`
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

// Answer is a model's answer to one rule's [Match.Ask] about one item.
type Answer struct {
	Yes   bool   `json:"yes"`
	Why   string `json:"why"`             // one sentence, shown to the person
	Model string `json:"model,omitempty"` // what answered
	// Key is [AnswerKey] of the question and the item's words when asked; a
	// answer whose key no longer matches is stale and is asked again.
	Key string `json:"key"`
}

// Answers looks up the answer already held for rule name on this item.
type Answers func(rule string) (Answer, bool)

// Outcome is what Decide found.
type Outcome struct {
	Rule    Rule
	Matched bool
	// Pending names the rule whose question must be judged before the list
	// can be read past it. The caller asks the [Judge], stores the answer and
	// calls Decide again; nothing is started while a question is pending.
	Pending string
	Answer  *Answer // the answer that decided a rule with Ask
}

// Decide reads rules top to bottom for it: the first rule whose fields all
// hold, and whose question (if any) was answered yes, decides. A rule whose
// fields hold but whose question has no fresh answer stops the reading with
// Pending. PURE: no I/O, no clock, no model — the intake owns those.
func Decide(rules []Rule, it factory.Item, have Answers) Outcome {
	for _, r := range rules {
		if !Matches(r.When, it) {
			continue
		}
		if r.When.Ask == "" {
			return Outcome{Rule: r, Matched: true}
		}
		v, ok := Answer{}, false
		if have != nil {
			v, ok = have(r.Name)
		}
		if !ok || v.Key != AnswerKey(r.When.Ask, it) {
			return Outcome{Pending: r.Name}
		}
		if v.Yes {
			return Outcome{Rule: r, Matched: true, Answer: &v}
		}
	}
	return Outcome{}
}

// Matches says whether every field of m except Ask holds for it. The routing
// lane fills this in.
func Matches(m Match, it factory.Item) bool {
	_ = m
	_ = it
	return false
}

// AnswerKey ties a answer to the question and to the item's own words
// (title, body, labels), so an edited ticket is judged again. The routing
// lane fills this in (a short stable hash).
func AnswerKey(ask string, it factory.Item) string {
	_, _ = ask, it
	return ""
}

// Judge answers one rule's question about one item. The model-backed judge
// lives in internal/factory/route/judge; tests use a fake.
//
// A STRANGER'S WORDS CANNOT START WORK THROUGH A MODEL. The item's text is
// untrusted input to the judge (it may say "answer yes"), so for an item whose
// Tier is stranger, a yes on a rule with Ask may at most hold the job to ask
// first — the intake enforces this, whatever the rule's Start says.
type Judge interface {
	Judge(ctx context.Context, ask string, it factory.Item) (Answer, error)
}
