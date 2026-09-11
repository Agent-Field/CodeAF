package session

// standing_rules.go is the check a firing's report passes before aforge
// publishes it: is there anything in it that breaks a rule the person placed on
// this work?
//
// ── WHY PUTTING THE RULE IN FRONT OF THE MODEL WAS NOT ENOUGH ──
//
// A rule placed on a folder rides into every run of the work placed in it: the
// run's instructions carry it and its journal records that it did
// (context_trace.go). That is exposure, and the first live run of the local-work
// journey (2026-09-10) showed exactly how far exposure goes. The Launch folder's
// rule said inbox reports never quote email addresses or phone numbers, the
// journal showed the rule reached the run, and the report quoted both — with
// "[redacted]" written AFTER them. The rule was read and not kept.
//
// So the report is read against the rules once more before it leaves the run,
// by a model with a fresh context and no tools, shown only the rules and the
// report. It is the auditor's role ([roles.RoleAuditor]) and the auditor's law
// (task_audit.go): the work does not grade itself, and a check that could edit
// what it was sent to judge would be an executor with a second name.
//
// ── WHAT HAPPENS ON A FINDING ──
//
// The run is sent back ONCE, in the same session, with the rule and the exact
// words the check quoted, and asked for the whole corrected report. The new
// report is checked again. If it still breaks a rule, or the check could not
// give an answer, NOTHING IS PUBLISHED: the previous report stays where it was,
// the draft is kept in the run folder, and the run ends waiting on the person
// with the finding as its line. One correction is a correction made on
// evidence; asking again until a model says yes would be sampling until green.
//
// ── WHAT IT IS NOT ──
//
// It is A MODEL'S READING, recorded as one ([standing.RuleCheck]), and not a
// proof: a check can miss a breach, and it can see one that is not there. A
// finding has to quote words that are really in the report, or it is not taken
// as a finding. It reads the REPORT aforge publishes and nothing else — not
// what the run did with its tools, and not the text of a note. And it runs only
// when rules reached the run: work with no rules on it pays for no check.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// standingRulesPrompt is the whole instruction the check is given.
//
// IT JUDGES THE REPORT, NOT THE WORK. A rule about how the work is done — which
// files were read, whether a file was edited — cannot be seen in a report, and
// a check that guessed at it would hold good reports back on nothing; it is
// not-checkable, and recorded as that.
//
// AND IT ASKS EACH RULE ITS OWN QUESTION. The first version asked one question
// of every rule — does the report BREAK any of them, quoting the words — and an
// obligation whose report simply omitted it had nothing to quote, so the check
// said "kept" and the record kept saying it (validator S11, S12b). An
// obligation is now kept only with the words that satisfy it quoted.
const standingRulesPrompt = `You check one report against the rules a person placed on the work that wrote it. You are given the rules, numbered, each in the person's own words, and the report exactly as it would be published.

Answer EACH rule by its own kind:
- An OBLIGATION says what a report must do or contain ("start with…", "include…", "cite…"). Does the report satisfy it? "kept" needs "quote": the exact words of the report that satisfy it. "broken" when it does not; say what is missing in "why" — there may be nothing to quote. A rule that both requires and forbids is an obligation, kept only when the report does what it requires and nothing in it does what it forbids.
- A PROHIBITION says only what a report must not do or contain ("never…", "no…"). Does the report break it? "broken" needs "quote": the exact words of the report that break it. "kept" when nothing in the report breaks it.
- A rule about how the work is done that a report cannot show — which files were read, whether some other file was edited — is "not-checkable", with why in one sentence.

Reply with one JSON object and nothing else, one entry per rule, in order:
{"rules": [{"rule": 1, "kind": "obligation", "verdict": "kept", "quote": "<words copied character for character from the report>", "why": "<one plain sentence>"}]}
kind is "obligation" or "prohibition"; verdict is "kept", "broken" or "not-checkable". Judge only what each rule asks of the report — not whether the report is good, complete or accurate.`

// standingRuleVerdict is what one check came to. answered is false when the
// check gave nothing usable — a failed call, no JSON, a rule left unanswered,
// or a quote that is not in the report — and then findings mean nothing.
type standingRuleVerdict struct {
	answered bool
	// findings is one per rule, in the order the rules were given.
	findings []standingRuleFinding
	// trouble is why an unanswered check is unanswered, for the record.
	trouble string
}

// standingRuleFinding is the check's answer for one rule.
type standingRuleFinding struct {
	rule                      standing.Item
	kind, verdict, quote, why string
}

// broken is every finding that says the report does not keep its rule.
func (v standingRuleVerdict) broken() []standingRuleFinding {
	var out []standingRuleFinding
	for _, finding := range v.findings {
		if finding.verdict == standing.RuleBroken {
			out = append(out, finding)
		}
	}
	return out
}

// summary is the one word over all the rules ([standing.RuleCheck.Verdict]):
// kept only when every rule was kept.
func (v standingRuleVerdict) summary() string {
	switch {
	case !v.answered:
		return "no answer"
	case len(v.broken()) > 0:
		return standing.RuleBroken
	}
	for _, finding := range v.findings {
		if finding.verdict != standing.RuleKept {
			return standing.RuleNotCheckable
		}
	}
	return standing.RuleKept
}

// recorded is the findings as the occurrence keeps them, by rule id.
func (v standingRuleVerdict) recorded() []standing.RuleVerdict {
	out := make([]standing.RuleVerdict, 0, len(v.findings))
	for _, finding := range v.findings {
		out = append(out, standing.RuleVerdict{ID: finding.rule.ID, Kind: finding.kind, Verdict: finding.verdict, Quote: finding.quote, Why: finding.why})
	}
	return out
}

// line is one finding as one line a person reads: the rule, then the words
// that break it, or what the report does not do.
func (f standingRuleFinding) line() string {
	line := "“" + oneLine(f.rule.Prompt()) + "”: "
	if quote := oneLine(f.quote); quote != "" {
		line += "the report says “" + quote + "”"
	} else {
		line += "the report does not do what it asks"
	}
	if why := oneLine(f.why); why != "" {
		line += " — " + why
	}
	return line
}

// finding is the first breach, as the line a person reads.
func (v standingRuleVerdict) finding() string {
	if broken := v.broken(); len(broken) > 0 {
		return broken[0].line()
	}
	return ""
}

// standingRules answers the rules that reached this run's instructions — the
// same records its journal's exposure names (standing_world.go), read back
// rather than chosen a second time.
func (a *Agent) standingRules() []standing.Item {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]standing.Item(nil), a.governingRecords...)
}

// checkStandingReport asks whether report breaks any of rules, and asks ONCE
// more when the first answer was no answer — the auditor's remedy for a
// truncated reply or a provider blip (task_audit.go). A second non-answer is
// returned as one; the caller holds the report back.
func (a *Agent) checkStandingReport(ctx context.Context, rules []standing.Item, report string) standingRuleVerdict {
	verdict := a.checkStandingReportOnce(ctx, rules, report)
	if verdict.answered || ctx.Err() != nil {
		return verdict
	}
	return a.checkStandingReportOnce(ctx, rules, report)
}

func (a *Agent) checkStandingReportOnce(ctx context.Context, rules []standing.Item, report string) standingRuleVerdict {
	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	response, served, err := a.callRole(ctx, roles.RoleAuditor, model, []ai.Message{
		textMessage("system", standingRulesPrompt),
		textMessage("user", standingRulesQuestion(rules, report)),
	})
	if response != nil {
		// THE CHECK IS PART OF WHAT THE RUN COST. It lands in this session's
		// usage, which is the figure the firing's ledger row is written from.
		a.addAuxiliaryUsageAs(response, served, 1, string(roles.RoleAuditor))
	}
	if err != nil {
		return standingRuleVerdict{trouble: "the check could not be made: " + oneLine(err.Error())}
	}
	if response == nil {
		return standingRuleVerdict{trouble: "the check answered nothing"}
	}
	return parseStandingRuleVerdict(response.Text(), report, rules)
}

// standingRulesQuestion is the check as the checker reads it. The rules are
// numbered, because a number is what the answer can name without copying the
// rule's words back.
func standingRulesQuestion(rules []standing.Item, report string) string {
	var out strings.Builder
	out.WriteString("THE RULES:\n")
	for index, rule := range rules {
		fmt.Fprintf(&out, "%d. %s\n", index+1, oneLine(rule.Prompt()))
	}
	out.WriteString("\nTHE REPORT:\n<<<\n")
	out.WriteString(strings.TrimSpace(report))
	out.WriteString("\n>>>\n\nAnswer every rule, in order. Reply with the JSON object only.")
	return out.String()
}

// parseStandingRuleVerdict reads the check's reply.
//
// EVERY QUOTE MUST BE THE REPORT'S OWN WORDS. A breach whose quote is not in the
// report is a finding about some other text, and an obligation "kept" on words
// the report does not contain is the blanket "kept" this check was rebuilt to
// end — so either is read as no answer, which the caller asks about once more.
// And every rule must be answered: a check that skipped one has not checked it.
func parseStandingRuleVerdict(reply, report string, rules []standing.Item) standingRuleVerdict {
	raw, err := subharness.Salvage(reply)
	if err != nil {
		return standingRuleVerdict{trouble: "the check did not answer in the form it was asked for"}
	}
	var parsed struct {
		Rules []struct {
			Rule    int    `json:"rule"`
			Kind    string `json:"kind"`
			Verdict string `json:"verdict"`
			Quote   string `json:"quote"`
			Why     string `json:"why"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return standingRuleVerdict{trouble: "the check did not answer in the form it was asked for"}
	}
	findings := make([]standingRuleFinding, len(rules))
	answered := make([]bool, len(rules))
	for _, entry := range parsed.Rules {
		index := entry.Rule - 1
		if index < 0 || index >= len(rules) || answered[index] {
			return standingRuleVerdict{trouble: "the check answered a rule it was not given, or one rule twice"}
		}
		kind, verdict := strings.TrimSpace(entry.Kind), strings.TrimSpace(entry.Verdict)
		quote := strings.TrimSpace(entry.Quote)
		if kind != standing.RuleObligation && kind != standing.RuleProhibition {
			return standingRuleVerdict{trouble: fmt.Sprintf("the check did not say what kind of rule rule %d is", entry.Rule)}
		}
		switch verdict {
		case standing.RuleKept, standing.RuleBroken, standing.RuleNotCheckable:
		default:
			return standingRuleVerdict{trouble: fmt.Sprintf("the check gave rule %d no verdict it was asked for", entry.Rule)}
		}
		cites := (kind == standing.RuleObligation && verdict == standing.RuleKept) || (kind == standing.RuleProhibition && verdict == standing.RuleBroken)
		if cites && !quotedFrom(report, quote) {
			return standingRuleVerdict{trouble: fmt.Sprintf("the check answered rule %d without quoting words that are in the report", entry.Rule)}
		}
		answered[index] = true
		findings[index] = standingRuleFinding{rule: rules[index], kind: kind, verdict: verdict, quote: quote, why: strings.TrimSpace(entry.Why)}
	}
	for index, done := range answered {
		if !done {
			return standingRuleVerdict{trouble: fmt.Sprintf("the check left rule %d unanswered", index+1)}
		}
	}
	return standingRuleVerdict{answered: true, findings: findings}
}

// quotedFrom answers whether quote is words that are really in report, once
// spacing and Markdown's emphasis marks are set aside on both sides — a quote
// copied across a line break, or without the bold around it, is still the
// report's own words.
func quotedFrom(report, quote string) bool {
	quote = quotable(quote)
	return quote != "" && strings.Contains(quotable(report), quote)
}

// quotable folds every run of white space to one space and drops the marks
// Markdown puts around words.
func quotable(text string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("*", "", "`", "", "_", "").Replace(text)), " ")
}

// standingCorrection is the one turn a run is sent back with: every rule the
// report does not keep, with the words that break it or what it leaves undone,
// and the instruction to reply with the whole report again. It says nothing
// about how to fix them — the rules are the person's, and the findings are the
// evidence.
func standingCorrection(verdict standingRuleVerdict) string {
	var out strings.Builder
	out.WriteString("RULE CHECK: before your report is published it was checked against the rules placed on this work, and it does not keep these:\n")
	for _, finding := range verdict.broken() {
		out.WriteString("- rule: " + oneLine(finding.rule.Prompt()) + "\n")
		if quote := oneLine(finding.quote); quote != "" {
			out.WriteString("  the report says: " + quote + "\n")
		} else {
			out.WriteString("  the report does not do what it asks\n")
		}
		if why := oneLine(finding.why); why != "" {
			out.WriteString("  why: " + why + "\n")
		}
	}
	out.WriteString("\nReply with the complete corrected report, in Markdown, between a line " + standingReportOpen + " and a line " + standingReportClose + ". Keep everything that does not break a rule.")
	return out.String()
}
