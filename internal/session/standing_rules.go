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
// a check that guessed at it would hold good reports back on nothing.
const standingRulesPrompt = `You check one report against the rules a person placed on the work that wrote it. You are given the rules, each in the person's own words, and the report exactly as it would be published.

Decide ONE thing: does the report, as written, break any of these rules?

Reply with one JSON object and nothing else:
{"kept": true}
when it breaks none of them, or
{"kept": false, "rule": "<the rule it breaks, as written>", "quote": "<the exact words in the report that break it, copied character for character>", "why": "<one plain sentence>"}

Judge only what the report itself says. A rule about how the work is done that a report cannot show — which files were read, whether some other file was edited — is not broken by the report. Do not judge whether the report is good, complete or accurate; only whether it breaks one of these rules.`

// standingRuleVerdict is what one check came to. answered is false when the
// check gave nothing usable — a failed call, no JSON, or a finding whose quote
// is not in the report — and then kept and the finding mean nothing.
type standingRuleVerdict struct {
	answered bool
	kept     bool
	rule     string
	quote    string
	why      string
	// trouble is why an unanswered check is unanswered, for the record.
	trouble string
}

// finding is the verdict as one line a person reads.
func (v standingRuleVerdict) finding() string {
	line := "“" + oneLine(v.rule) + "”: the report says “" + oneLine(v.quote) + "”"
	if why := oneLine(v.why); why != "" {
		line += " — " + why
	}
	return line
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
	return parseStandingRuleVerdict(response.Text(), report)
}

// standingRulesQuestion is the check as the checker reads it.
func standingRulesQuestion(rules []standing.Item, report string) string {
	var out strings.Builder
	out.WriteString("THE RULES:\n")
	for _, rule := range rules {
		out.WriteString("- " + oneLine(rule.Prompt()) + "\n")
	}
	out.WriteString("\nTHE REPORT:\n<<<\n")
	out.WriteString(strings.TrimSpace(report))
	out.WriteString("\n>>>\n\nDoes the report break any of these rules? Reply with the JSON object only.")
	return out.String()
}

// parseStandingRuleVerdict reads the check's reply.
//
// A FINDING MUST QUOTE THE REPORT. A "broken" whose quote is not in the report,
// word for word once spacing is set aside, is a finding about some other text,
// and holding a report back on it would be the check inventing evidence. It is
// read as no answer, which the caller asks about once more.
func parseStandingRuleVerdict(reply, report string) standingRuleVerdict {
	raw, err := subharness.Salvage(reply)
	if err != nil {
		return standingRuleVerdict{trouble: "the check did not answer in the form it was asked for"}
	}
	var parsed struct {
		Kept  *bool  `json:"kept"`
		Rule  string `json:"rule"`
		Quote string `json:"quote"`
		Why   string `json:"why"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.Kept == nil {
		return standingRuleVerdict{trouble: "the check did not say whether the report keeps the rules"}
	}
	if *parsed.Kept {
		return standingRuleVerdict{answered: true, kept: true}
	}
	quote := strings.TrimSpace(parsed.Quote)
	if quote == "" || !strings.Contains(squeezeSpace(report), squeezeSpace(quote)) {
		return standingRuleVerdict{trouble: "the check named a breach without quoting words that are in the report"}
	}
	return standingRuleVerdict{answered: true, rule: strings.TrimSpace(parsed.Rule), quote: quote, why: strings.TrimSpace(parsed.Why)}
}

// squeezeSpace folds every run of white space to one space, so a quote copied
// across a line break still matches the report it came from.
func squeezeSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// standingCorrection is the one turn a run is sent back with: the rule, the
// words that break it, and the instruction to reply with the whole report
// again. It says nothing about how to fix it — the rule is the person's, and
// the words are the evidence.
func standingCorrection(verdict standingRuleVerdict) string {
	return "RULE CHECK: before your report is published it was checked against the rules placed on this work, and the check found it breaks one.\n" +
		"- rule: " + oneLine(verdict.rule) + "\n" +
		"- the report says: " + oneLine(verdict.quote) + "\n" +
		"- why: " + oneLine(verdict.why) + "\n\n" +
		"Reply with the complete corrected report as your final reply, in Markdown, and nothing else. Keep everything that does not break a rule."
}
