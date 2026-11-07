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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"

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

// Matches says whether every field of m except Ask holds for it. It is PURE
// — no store, no clock, no network, no model — so the same rules and the same
// item always give the same answer, and a test is a table.
//
// A SOURCE matches the item's Origin or SourceRef, whole or up to a ':' or
// '/': "github" holds for a forge issue, "jira" for Origin `jira`, and both
// "inbox" and "inbox:ops" for an inbox job (Origin `inbox:ops`). A REPO is compared as
// [factory.RepoOf] spells it, a LABEL and an AUTHOR without regard to case
// (an author's leading @ dropped), TEXT as a case-insensitive substring of
// the title or the body, and a PRIORITY as the p0..p3 spelling of
// [factory.PriorityFor].
func Matches(m Match, it factory.Item) bool {
	return anyOf(m.Sources, func(s string) bool { return sourceIs(it, s) }) &&
		anyOf(m.Repos, func(r string) bool { return strings.EqualFold(r, factory.RepoOf(it)) }) &&
		anyOf(m.Labels, func(l string) bool {
			return slices.ContainsFunc(it.Labels, func(have string) bool { return strings.EqualFold(strings.TrimSpace(have), l) })
		}) &&
		anyOf(m.Authors, func(a string) bool { return factory.NormalizeHandle(a) == factory.NormalizeHandle(it.Author) }) &&
		anyOf(m.Text, func(t string) bool { return containsFold(it.Title, t) || containsFold(it.Body, t) }) &&
		anyOf(m.Priority, func(p string) bool { return p == priorityOf(it) })
}

// anyOf is the one shape of every list field: empty holds, else any one entry.
func anyOf(want []string, holds func(string) bool) bool {
	return len(want) == 0 || slices.ContainsFunc(want, holds)
}

// sourceIs says whether source names where it came from: its Origin or its
// SourceRef, either whole or up to a ':' or '/', without regard to case. An
// inbox job has Origin `inbox:ops` and SourceRef `inbox:ops:A1`, so "inbox"
// and "inbox:ops" both hold for it and "inbox:op" does not.
func sourceIs(it factory.Item, source string) bool {
	return namesFront(string(it.Origin), source) || namesFront(it.SourceRef, source)
}

func namesFront(have, source string) bool {
	rest, ok := strings.CutPrefix(strings.ToLower(have), strings.ToLower(source))
	return ok && source != "" && (rest == "" || rest[0] == ':' || rest[0] == '/')
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// priorityOf is the item's priority as a rule spells it, "" for none.
func priorityOf(it factory.Item) string {
	if p := factory.PriorityFor(it); p != nil {
		return factory.PriorityShort(p.Value)
	}
	return ""
}

// AnswerKey ties an answer to the question and to the item's own words, so
// an edited ticket is judged again and an unchanged one never is. It is the
// first 16 hex digits of a SHA-256 over the question, the title, the body and
// the labels sorted without regard to case, each part length-prefixed so no
// two different sets of words can run together into the same input.
func AnswerKey(ask string, it factory.Item) string {
	labels := make([]string, 0, len(it.Labels))
	for _, l := range it.Labels {
		labels = append(labels, strings.ToLower(strings.TrimSpace(l)))
	}
	sort.Strings(labels)
	h := sha256.New()
	for _, part := range append([]string{strings.TrimSpace(ask), it.Title, it.Body}, labels...) {
		fmt.Fprintf(h, "%d:%s;", len(part), part)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// HeldAnswers reads the answers kept on it ([factory.Item.Judged]) as
// Decide looks them up.
func HeldAnswers(it factory.Item) Answers {
	return func(rule string) (Answer, bool) {
		v, ok := it.Judged[rule]
		return Answer{Yes: v.Yes, Why: v.Why, Model: v.Model, Key: v.Key}, ok
	}
}

// RepoFor is the repository the action sends it to: the action's own when it
// names one, else the one it already has.
func (a Action) RepoFor(it factory.Item) string {
	if a.Repo != "" {
		return a.Repo
	}
	return it.Repo
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
