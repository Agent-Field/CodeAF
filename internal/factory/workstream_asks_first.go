package factory

import (
	"regexp"
	"strconv"
	"strings"
)

// THE ASK RULE, AND WHICH TASKS IT WILL STOP (lane L7, v2 boards "Who does
// what" and J6, DELTA §4.1). A workstream's rule says how much an agent may do
// before it asks: cheap, reversible work is done and listed under Assumed, and
// anything that leaves the workstream (published, sent, posted, paid for) is
// an Ask to the owner first. The rule is ON unless the workstream's steward
// turned it off, so it is spelled as its opposite, PublishesWithoutAsking,
// whose zero value is the default every workstream already kept starts with.
//
// A plan and a bulk offload say BEFORE anything runs which tasks will ask
// first ("waits on 1 · asks first"; "replies are public, so Afi asks you
// before posting each one"), so the Ask is no surprise. AsksFirst is that
// prediction, and the task's packet tells the run to keep it: make the thing
// ready, then raise the Ask before the step that leaves the workstream.

// AsksBeforePublishingRule is the rule line a workstream that asks shows,
// spelled once for the decision a change records and every surface's side
// column.
const AsksBeforePublishingRule = "Afi asks before publishing or sending"

// PublishesWithoutAskingRule is the line when the steward turned it off.
const PublishesWithoutAskingRule = "Afi publishes and sends without asking"

// outward is each kind of step that leaves the workstream, with the words a
// task uses for it, in the order they are tried. A noun that only looks like
// the verb ("a blog post", "the launch email", "a reply macro") does not
// count: posting and replying must take an object or a place, and an email
// is a step only as the title's first word ("Email the three pilots").
var outward = []struct {
	act string
	re  *regexp.Regexp
}{
	{"publishing", regexp.MustCompile(`(?i)\b(publish|publishes|publishing|go live)\b`)},
	{"posting", regexp.MustCompile(`(?i)\b(post|posts|posting)\s+(it|them|this|these|the|a|an|to|on|in|each|every|our|your|their)\b|^\s*post\b|\btweet\w*\b`)},
	{"replying", regexp.MustCompile(`(?i)^\s*(reply|respond|answer)\s+(to|on)\b|\b(reply|respond)\s+(publicly|on github|on the issue|in the thread)\b`)},
	{"sending", regexp.MustCompile(`(?i)\b(send|sends|sending)\s+(it|them|this|these|the|a|an|to|out|each|every|our|your|their)\b|^\s*(send|email|mail|message|invite)\b`)},
	{"paying", regexp.MustCompile(`(?i)\b(pay|pays|paying|purchase|purchases|purchasing|buy|buys|buying|order|book)\s+(it|them|this|these|the|a|an|for|our|your|their)\b`)},
	{"submitting", regexp.MustCompile(`(?i)\b(submit|submits|submitting|deploy|deploys|deploying|merge|merges|merging)\s+(it|them|this|these|the|a|an|to|our|your|their)\b`)},
}

// AsksFirst is the step that will make t's agent ask its owner before it goes
// on ("posting", "sending" …), or "" when it will not: the rule is off, a
// person or the factory does it (the factory keeps its own approvals), it is
// finished, or nothing in its title or request leaves the workstream.
func AsksFirst(t WorkstreamTask, r WorkstreamRules) string {
	if r.PublishesWithoutAsking {
		return ""
	}
	switch t.DoneBy.Kind {
	case TaskByPerson, TaskByFactory:
		return ""
	}
	switch t.State {
	case TaskDone, TaskStopped:
		return ""
	}
	if _, job := IsJobTask(t.ID); job {
		return ""
	}
	// The title and the request say what this task DOES; a deliverable says
	// what it makes, and "ready to post to #announcements" is the next task's
	// step, not this one's (live L7 run: the Discord draft was predicted to
	// ask). A step written as someone else's is read out the same way.
	for _, o := range outward {
		for _, s := range []string{t.Title, t.Request} {
			for _, sentence := range splitSentences(s) {
				if o.re.MatchString(laterStep.ReplaceAllString(sentence, " ")) {
					return o.act
				}
			}
		}
	}
	return ""
}

// laterStep is a step written as someone else's, later: "ready to post",
// "for you to send", "to be published".
var laterStep = regexp.MustCompile(`(?i)\b(ready|able|you|someone|owner|them|person)\s+to\s+\w+|\bto\s+be\s+\w+`)

// splitSentences cuts s at sentence ends, so "Write it. Post it to Discord"
// reads its second sentence's first word as a first word.
func splitSentences(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == ';' || r == '\n' || r == '!' || r == '?' })
}

// FillAsksFirst writes each task's prediction in place, at read time: it is
// never kept, because a change of the rule changes it for every task at once.
func FillAsksFirst(r WorkstreamRules, tasks []WorkstreamTask) {
	for i := range tasks {
		tasks[i].AsksFirst = AsksFirst(tasks[i], r)
	}
}

// checkedAgainstPrefix matches a "Checked against the request…:" lead the
// agent wrote itself, so the server's own lead is never doubled.
var checkedAgainstPrefix = regexp.MustCompile(`(?i)^\s*checked against the request( and (\d+|one|two|three|no) decisions?)?\s*:\s*`)

// CheckedAgainst is an agent's check as the task keeps it: its claims after
// what they were checked against, "Checked against the request and 2
// decisions: …", or "Checked against the request: …" with no decision in
// force. The count is the decisions in force when the check was written,
// which its packet carried, so the page never claims a decision nobody
// checked against (v2 screen 6; the desk reads this lead, PR #572).
func CheckedAgainst(decisions int, claims string) string {
	claims = strings.TrimSpace(checkedAgainstPrefix.ReplaceAllString(claims, ""))
	switch {
	case claims == "":
		return ""
	case decisions == 1:
		return "Checked against the request and 1 decision: " + claims
	case decisions > 1:
		return "Checked against the request and " + strconv.Itoa(decisions) + " decisions: " + claims
	}
	return "Checked against the request: " + claims
}
