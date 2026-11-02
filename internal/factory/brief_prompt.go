package factory

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// BriefInputMost bounds the writer's input text, in bytes, under the
// errand's own limit on what one call may carry.
const BriefInputMost = 40 << 10

// BriefSystem is the writer's prompt. Its limits are the palette's own
// constants, so the prompt and the check that reads the answer cannot drift.
func BriefSystem() string {
	return fmt.Sprintf(`You write the brief of one factory job: its state in one screen, for the person who owns it.
You are given the job as JSON: the issue, the plan, each step's result per try, the change's stats and hunk index, the rule checks, the facts row and the brief before.
Answer ONE JSON object and nothing else:
{"verdict": string, "verdict_evidence": [evidence], "blocks": [block]}
Every "evidence" field is a JSON ARRAY of {"step": string, "try": number, "kind": one of %s, "ref": string}. tests, exit, findings, claims and output name a step's latest try; summary names any try; file names a changed file in ref; change is the whole change's stat (files, added, removed); hunk names "file:start" in ref; rule_checks names no step.
The verdict says the outcome, not the activity, in at most %d words ("Fixed. Retries no longer double-send after a worker restart.").
Pick 2 to %d blocks. Every block has "kind" and a short "heading". Kinds and their fields:
- bullets: "bullets": [{"text","evidence"}], at most %d, at most %d words each, a **bold** lead word allowed.
- flow: "flow": [{"label":"Before"|"After","nodes":[{"text","mark"}]}], at most %d nodes a row, one node may be marked; "evidence".
- diagram: "mermaid": a mermaid graph, "nodes": its node labels (at most %d); "evidence".
- table: "table": {"columns":[2-%d],"rows":[[cells]] at most %d}; "evidence".
- where_to_look: "spots": [{"file","symbol","lines": a NUMBER, the lines the spot changes (a hunk's added plus removed),"why","evidence" of kind hunk}] 1-%d from the hunk index, and "rest": one line saying what the rest of the change is.
- proof: "proof": {"command","shown","total"} exactly as the proof step's claims say; "evidence" of kind claims.
- open_question: "questions": [{"text","evidence","action":{"label","step","ask"}}] at most %d, for what the evidence does not cover.
- your_call: "options": 2-%d short answers; only while needs_you is true; "evidence".
- screenshots: "shots": [{"label","ref","evidence"}] at most %d.
EVERY NUMBER YOU WRITE MUST BE ONE ITS EVIDENCE SHOWS; a claim or block that breaks this, or names a step or try the job does not have, is thrown away. Never restate the facts row. Say nothing you cannot point at.`,
		strings.Join(BriefEvidenceKinds, ", "), BriefWordsMost, BriefBlocksMost, BriefBulletsMost, BriefWordsMost,
		BriefFlowNodesMost, BriefDiagramMost, BriefTableColsMost, BriefTableRowsMost, BriefSpotsMost,
		BriefQuestionsMost, BriefOptionsMost, BriefShotsMost)
}

// BriefInputText is the input as the writer reads it, cut to
// [BriefInputMost]: the hunk index first, then the steps' outputs, then the
// issue's body, so what is kept is what the brief rests on most.
func BriefInputText(in BriefInput) string {
	cuts := []func(*BriefInput){
		func(in *BriefInput) { in.Hunks = in.Hunks[:len(in.Hunks)/2] },
		func(in *BriefInput) {
			for i := range in.Steps {
				in.Steps[i].Output = clip(in.Steps[i].Output, len(in.Steps[i].Output)/2)
			}
		},
		func(in *BriefInput) {
			in.Issue.Body, in.Plan = clip(in.Issue.Body, len(in.Issue.Body)/2), clip(in.Plan, len(in.Plan)/2)
		},
		func(in *BriefInput) { in.Previous = nil },
	}
	raw, _ := json.Marshal(in)
	for i := 0; len(raw) > BriefInputMost && i < 8*len(cuts); i++ {
		cuts[i%len(cuts)](&in)
		raw, _ = json.Marshal(in)
	}
	return string(raw)
}

// BriefWritten is how a write ended: the writer's answer and its cost, or
// why there is none.
type BriefWritten struct {
	Hash    string
	Model   string
	CostUSD float64
	Draft   []byte
	Failure string
}

// BeginBrief marks a write on its way after the job's last finished step,
// unless [BriefBeginSkip] says why not, which it answers.
func (it *Item) BeginBrief(trigger, hash string, now time.Time) string {
	if skip := BriefBeginSkip(*it, trigger, hash, now); skip != "" {
		return skip
	}
	it.BriefWriting = &BriefWriting{State: BriefGenerating, AfterStep: briefAfterStep(*it), At: now, Hash: hash}
	return ""
}

// briefAfterStep is the last step of the run that is done.
func briefAfterStep(it Item) string {
	step := ""
	if it.Stream != nil {
		for _, p := range it.Stream.Phases {
			if p.State == PhaseDone {
				step = p.Name
			}
		}
	}
	return step
}

// EndBrief ends a write: the writer's cost is the job's, whatever came of it;
// a draft whose verdict passes is kept with what survived the check; anything
// else leaves the versions as they were and the write failed. It answers the
// version kept (false when none), and the parts dropped.
func (it *Item) EndBrief(w BriefWritten, now time.Time) (BriefVersion, []BriefDrop, bool) {
	AddChatSpend(it, w.CostUSD)
	after := briefAfterStep(*it)
	if it.BriefWriting != nil && it.BriefWriting.AfterStep != "" {
		after = it.BriefWriting.AfterStep
	}
	fail := func(why string) (BriefVersion, []BriefDrop, bool) {
		it.BriefWriting = &BriefWriting{State: BriefFailed, AfterStep: after, Failure: why, At: now, Hash: w.Hash}
		return BriefVersion{}, nil, false
	}
	if w.Failure != "" {
		return fail(w.Failure)
	}
	v, drops, fault := CheckBriefDraft(*it, w.Draft)
	if fault != "" {
		return fail("the brief did not pass its check: " + fault)
	}
	v.AfterStep, v.MadeAt, v.Model, v.CostUSD, v.InputHash, v.Dropped = after, now, w.Model, w.CostUSD, w.Hash, len(drops)
	return it.KeepBrief(v), drops, true
}
