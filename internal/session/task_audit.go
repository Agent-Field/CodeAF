package session

// The auditor: the one thing standing between a node's last words and the word
// "done".
//
// ── WHY A NODE MAY NOT MARK ITSELF FINISHED ──
//
// Before this file existed, a task node's done-state was its own self-report:
// the child agent stopped calling tools, said something confident in its final
// message, and the graph wrote that down as TaskDone. Everything downstream —
// the dependents' briefs, the merge onto the person's branch, the note in the
// conversation — was built on a sentence the executor wrote about itself.
//
// That is the failure mode LongHorizon-Harness names in one line
// (harness-research-notes.md §1, arXiv:2608.01964): task state is updated ONLY
// from independent audit evidence, and executor self-reports never flip a
// record to completed. An agent that has spent forty steps on a change is the
// worst available judge of whether the change works — not because it lies, but
// because it has been reasoning about its own intentions for forty steps and
// its intentions are what it will grade.
//
// So the frontier advances on EVIDENCE. When a node's run finishes, a fresh
// auditor — no shared context, no memory of the trajectory, a different agent
// on the HIGH tier — is pointed at the node's worktree, runs the repository's
// own verification, reads the diff, and answers VERIFIED or REFUTED with the
// three lines of evidence it is standing on. VERIFIED is the only thing that
// merges. REFUTED is a TaskFailed carrying the auditor's evidence as the
// report, and the cascade in runFrontier fails its dependents with it, which is
// exactly right: work built on top of work that does not hold is work built on
// nothing.
//
// ── ROLE SEPARATION IS THE SAFETY ARGUMENT ──
//
// The auditor's belt is COMPOSED, not filtered by a flag: the four readers
// (read, grep, find, ls) and a bash that refuses everything outside a named
// allowlist of verification commands. It cannot edit, write, install, fetch or
// paint. That is what makes its verdict worth anything — an auditor that could
// fix what it found would be an executor with a second name, and the first
// thing it would do is repair the thing it was sent to judge and then report
// success. It is also why the belt is built here from bare's tools rather than
// by adding a config flag to the session's belt(): "which hands does an auditor
// have" is a question with one answer, written once, in the file that depends
// on it.
//
// ── WHY THE VERDICT IS TWO WORDS AND THREE LINES ──
//
// The same reason the guardian's contract is one word (guardian.go): a verdict
// with a middle answer has a middle answer nobody has defined, and the first
// thing a model does with an undefined answer is use it. So the auditor is
// still asked for one of two words, and everything that is not VERIFIED leaves
// the work unmerged: the frontier fails CLOSED, and the worst a broken auditor
// can do is keep good work on a branch with an explanation attached.
//
// ── BUT A NON-ANSWER IS NOT A VERDICT ──
//
// Failing closed is about what MERGES. It is not a licence to write down a
// finding nobody made. An auditor that answered with neither word, or that was
// never asked at all because the provider errored, has told us exactly nothing
// about the work — and recording that as REFUTED is the harness inventing
// evidence and then cascading it through every dependent. That is a false
// failure, and it was observed in the wild: a deep-research node landed as
// "REFUTED — the auditor answered neither VERIFIED nor REFUTED".
//
// So a non-verdict is asked ONCE MORE — a fresh auditor, the same evidence
// packet, which is the whole remedy for a truncated reply or a provider blip —
// and if the second attempt is also not a verdict, the node lands UNVERIFIED
// (task_contract.go). Unverified is settled and it is not failed: the branch is
// kept, nothing merges, nothing cascades, and the dependents wait for the one
// thing that can move them, which is a person deciding ([Agent.ResolveUnverified]).
// A REAL REFUTED verdict is untouched by any of this — it is a finding, it
// fails the node, and it takes the dependents with it, exactly as before.
//
// ── THE LADDER UNDER A NON-ANSWER: NUDGE, THEN A FRESH AUDITOR ──
//
// A fresh auditor is the expensive rung. It re-reads the diff, re-runs the
// verification, re-pays the whole investigation — and none of that is what was
// missing when an auditor did all the work and then stopped one word short (the
// one seen in the wild ended on "Let me be targeted:" with no tool call and no
// verdict). That auditor is still sitting there with the evidence in its
// context. Asking IT for the word costs one turn.
//
// So the ladder is NUDGE → FRESH AUDITOR → UNVERIFIED, and the first rung is
// taken only when there is a lane to take it on: a delivered reply that did not
// PARSE into a verdict. A provider error, an auditor that never started, a
// deadline that ran out — none of those has a live auditor behind it, and each
// goes straight to the rung that builds a new one, exactly as before. The nudge
// is one line and it demands the contract, nothing else: an auditor asked to
// "reconsider" is an auditor being led.
//
// ── THE REPAIR LOOP: REFUTED IS NOT ALWAYS THE END ──
//
// A REFUTED verdict used to land the node dead on the spot. What that costs was
// measured in the wild: a deep-research node produced a 138-line report covering
// ten companies, the acceptance asked for eleven, the auditor correctly refuted
// it — and the person re-typed the entire task by hand. The work was 90% there
// and the harness threw all of it away because the last 10% was missing.
//
// So a finding now buys the node a REPAIR ROUND (task.repair_rounds, one by
// default, 0 for the old behaviour): the SAME worktree, a fresh worker, and the
// original brief with the gaps in front of it. Then a fresh auditor judges
// again. Refuted with the rounds spent is the old landing — TaskFailed, branch
// kept, cascade — except that the report now carries the evidence of EVERY
// round, because "it was sent back twice and this is what was still missing" is
// the only version of that story a person can act on.
//
// THE AUDITOR IS NEVER TOLD IT IS JUDGING A REPAIR. Same evidence packet, same
// contract, no round number, nothing about what the last one found. An auditor
// that knows the work has been fixed once already is an auditor with a reason to
// be satisfied, and the whole value of this gate is that it has none. CONVERGENCE
// COMES FROM THE LOOP, NOT FROM A SOFTENED JUDGE: the worker is told what is
// missing, the judge is told nothing.
//
// ── THE VOCABULARY LAW ──
//
// NONE OF THE WORDS IN THIS FILE REACH A PERSON. Not "auditor", not "audit", not
// "verdict", not VERIFIED, REFUTED or "unverified" — not in the outcome written
// to the project's index, not in the report on a landed notice, not in the note
// the chat model reads off the steering lane. The machinery is real and it is
// named honestly HERE, in the code, the comments, the job log and the audit's
// own journal. What lands in front of a person is what HAPPENED:
//
//	verified          the work's own account, with the evidence sentence under it
//	                  — the state already says done
//	refuted out       "incomplete — " and the plain gaps, every round of them
//	nobody could say  "finished, but needs your look — " and what the checker said
//
// The reason is not squeamishness. The person did not ask for an audit; they
// asked for a report on eleven companies. "REFUTED" tells them about the
// harness's internal court, and a chat model reading it will repeat the court to
// them, in its own sentence, as though a trial had happened. "incomplete — the
// report covers ten companies, amp-labs is missing" tells them the thing they
// can act on, which is the same fact with the machinery taken off it.
//
// TWO LITERALS ARE EXEMPT, AND ONLY BECAUSE THEY ARE ADDRESSES. The settings key
// `task.audit` names a switch the person can throw, and `reaudit` is a word the
// model must type back to the `tasks` tool. A handle somebody has to type is not
// a finding about their work, and translating it would leave them holding a name
// that opens nothing.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// The auditor is a ROLE, registered from the file that makes the call, exactly
// as internal/roles' own doc says an auxiliary call should. HIGH, and not as a
// default somebody is expected to tune down: this is the call that decides
// whether work is real.
func init() { roles.Register(roles.RoleAuditor, roles.TierHigh) }

const (
	// auditDeadline bounds one verdict. Five minutes is a full `go test ./...`
	// on a real repository plus the reading around it; past that the auditor is
	// not judging, it is stuck, and a node that waits forever for a verdict is
	// worse than a node that is told nobody could give it one.
	auditDeadline = 5 * time.Minute

	// auditEvidenceLines is how much evidence rides the report: what was run,
	// what was seen, and at most one line more. The verdict is read off a card
	// and off a dependent's brief, and an auditor writing paragraphs into both
	// is an auditor spending the person's attention on its own reasoning.
	auditEvidenceLines = 3

	// auditCommandLimit keeps a refused command readable when it is handed back
	// to the auditor as a refusal.
	auditCommandLimit = 200

	// auditReaderHint rides every refusal and the bash description itself.
	//
	// A REFUSAL THAT ONLY SAYS NO COSTS A STEP AND TEACHES NOTHING. The audit
	// that died in the wild had already spent one of its steps on a refused
	// `pwd`, and the shape of that mistake is always the same: the auditor
	// reaches for bash to LOOK at something, because looking is what a shell is
	// for everywhere else. It has four hands for looking. The refusal's job is to
	// point at them in the same breath as the no, so the wrong reach costs one
	// step instead of three.
	auditReaderHint = "For looking around, use read, grep, find and ls — that is what they are for. bash is only for verification commands."

	// auditResultLimit is the most one tool result may weigh when it is handed
	// to the auditor.
	//
	// It exists because of a real audit that died of it: the auditor ran `ls` on
	// a huge home directory, the listing filled its context, and what was left of
	// the reply budget was not enough to reach a verdict. The readers already
	// truncate at pi's own numbers (50KB, internal/exec/bare's truncate.go), and
	// 50KB of directory listing is still a whole investigation's worth of budget
	// spent on one wrong reach. Eight thousand bytes is two screens — enough for
	// a real `go test` failure, enough for a diff hunk — and the cut says how
	// much was left behind so the auditor knows to ask a narrower question rather
	// than believing it has seen everything.
	auditResultLimit = 8000

	// auditSaidLines is how much of a NON-ANSWER is kept as the outcome text.
	// Two lines: enough for a person to see what the auditor actually said —
	// which is the whole basis on which they are being asked to decide — and
	// not so much that a card carries an essay somebody wrote instead of a
	// verdict. The rest is in the audit's own journal.
	auditSaidLines = 2
)

// The two things a verdict can say, and the word for an audit that said
// neither. VERIFIED is the only one that merges.
const (
	auditVerified = "VERIFIED"
	auditRefuted  = "REFUTED"
	// auditUnverified is NOT a third verdict — it is the absence of one, and it
	// is spelled differently from REFUTED for the reason the whole fix exists:
	// "the auditor looked and says no" and "nobody ever answered" are very
	// different news, and the person reading the card is the one who has to
	// tell them apart.
	auditUnverified = "UNVERIFIED"
)

// auditCommands is the allowlist: the repository's own verification, and
// git's read-only reporting. It is a variable rather than a constant because it
// is the one part of the auditor's belt that is meant to be configurable — a
// repository whose verification is `make check` or `npm test` says so by
// changing this list, and nothing else about the auditor moves.
//
// Every entry is a COMMAND PREFIX matched at a word boundary, so "go test"
// admits `go test ./... -run TestX` and does not admit `go testify`. What is
// NOT here is everything else, including `go generate` and `go run`, which
// execute code the node wrote — an auditor that runs the executor's own program
// is an auditor holding the tested thing's hand.
//
// The four ORIENTATION commands at the end are not verification and they are
// here anyway, because refusing them cost a verdict: an auditor that cannot ask
// where it is standing spends its steps finding out the hard way, and the audit
// that died in the wild burned two of them on a refused `pwd`. Every one of the
// four READS — they print, they do not touch — and the safety argument the belt
// rests on is about what can CHANGE the thing under judgement, not about which
// program prints it.
var auditCommands = []string{
	"go test",
	"go build",
	"go vet",
	"git diff",
	"git log",
	"git status",
	"git show",
	"pwd",
	"wc",
	"head",
	"cat",
}

// auditPrompt is the auditor's whole world. It never sees the conversation, it
// never sees the node's trajectory, and it is told in the first line that its
// answer is the only reason the work can be called finished.
const auditPrompt = `You are an AUDITOR. Somebody else did a piece of work and says it is finished. You decide whether that is true, and your verdict is the only reason it can be called finished at all.

You are READ-ONLY. You have read, grep, find and ls, and a bash that runs the repository's own verification and nothing else. You cannot edit, write, install, or fix anything, and you must not try — the work is not yours to repair. Your job is to find out what is true.

Judge the work against its ACCEPTANCE and nothing else: not what you would have written, not what else the code could use, not how the change was made. Run the verification yourself and read the diff. A claim you did not check is a claim you have not verified.

Then answer in AT MOST four lines. The first word is the verdict:

VERIFIED — what you ran, and what you saw
REFUTED — what you ran, and what you saw

VERIFIED means you ran something and it passed. REFUTED means it did not pass, or there was nothing there to have passed, or you could not check. When in doubt, REFUTE. Write nothing except the verdict and your evidence.`

// auditNudge is the whole of the first rung. It is ONE SENTENCE and it demands
// the contract — not "have another think", not "are you sure", nothing that
// tells the auditor which way to go. An auditor that has read the work and
// stopped short of the word is missing the word, and this asks for the word.
const auditNudge = "Answer now with one word on the first line: VERIFIED or REFUTED, then your evidence."

// repairHeading and repairStands are the two things a repair round adds to the
// original brief, and the second matters as much as the first. A worker handed a
// brief and a list of faults in the same worktree will happily start the job
// over — that is what a brief reads like — and starting over is how a repair
// round throws away the ninety percent that was right. So it is told, in one
// sentence, that the work stands and only the gaps are its job.
const (
	repairHeading = "A REVIEW FOUND THESE GAPS:"
	repairStands  = "The work so far stands and is already in this working copy. Do not start it again and do not undo any of it: close the gaps above, and nothing else."
)

// ── the words a person actually reads ───────────────────────────────────────

// The leads for the three landings. They are constants because three different
// readers compare against them — the note, the index row, and the tests that
// hold this file to its own law — and a lead that was spelled twice would be a
// law with two versions.
const (
	// incompleteLead opens a node that was looked at and found short. It does
	// not say who looked, because from the person's chair it does not matter:
	// the news is that the work is not finished and here is what is missing.
	incompleteLead = "incomplete — "
	// needsLookLead opens the node nobody could judge. "Finished, but" is the
	// honest half nobody else says: the work RAN, it is sitting on a branch, and
	// the only thing missing is somebody's eyes.
	needsLookLead = "finished, but needs your look — "
	// repairedAgainLead opens the second and later rounds' gaps, so that a
	// report carrying three sets of evidence reads as three attempts rather than
	// as one auditor repeating itself.
	repairedAgainLead = "still incomplete after another go — "
)

// machineryWords is the vocabulary that must never reach a person, and what to
// say instead. The order is LONGEST-STEM-FIRST and it has to be: "unverified"
// contains "verified", and "auditor" contains "audit", so a pass that took the
// short one first would leave "un-confirmed" and "reviewor" behind.
//
// The replacements are not euphemisms — each is the plain word for the thing.
// The auditor IS a checker, its verdict IS an answer, and REFUTED means the
// checker did not confirm the work.
//
// The STEMS carry their own inflections and are not listed twice: "audit" turns
// "audited" into "checked" and "audits" into "checks" on its own, and a row for
// each ending would be four ways for this table to disagree with itself.
var machineryWords = [][2]string{
	{"auditor", "checker"},
	{"audit", "check"},
	{"unverified", "unchecked"},
	{"verified", "confirmed"},
	{"verify", "confirm"},
	{"refuted", "not confirmed"},
	{"refute", "not confirm"},
	{"verdict", "answer"},
}

// plainWords strips the machinery out of a line that is about to be read by a
// person or by the chat model.
//
// IT IS A NET, NOT THE POLICY. Everything this package writes itself is already
// written in plain words at the source — that is the only way to say a thing
// once and correctly. What this catches is the text this package did NOT write:
// the auditor's own evidence, which is usually plain facts ("go test ./... still
// fails: TestHollow") and is sometimes a model narrating its own role ("the
// audit shows the REFUTED case"). The facts survive untouched; the framing is
// translated rather than dropped, because dropping it would leave a sentence
// with a hole in it.
//
// The match is case-insensitive and the replacement is lower-case, which is
// right for the words as they actually appear: mid-sentence prose, or a SHOUTED
// verdict word that has no business being shouted at somebody who never asked
// for a trial.
func plainWords(text string) string {
	for _, pair := range machineryWords {
		text = replaceFold(text, pair[0], pair[1])
	}
	return text
}

// replaceFold replaces every case-insensitive occurrence of old with new.
//
// The scan is over a LOWER-CASED COPY and the cut is made on the ORIGINAL, which
// is only safe while the two agree on byte offsets — so the copy is built with
// [strings.Map] over ASCII case alone rather than with ToLower, whose ﬁ→FI kind
// of folding changes a string's length and would make every offset after it a
// byte in the wrong place.
func replaceFold(text, old, new string) string {
	if old == "" {
		return text
	}
	lower := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, text)
	var out strings.Builder
	for {
		at := strings.Index(lower, old)
		if at < 0 {
			out.WriteString(text)
			return out.String()
		}
		out.WriteString(text[:at])
		out.WriteString(new)
		text, lower = text[at+len(old):], lower[at+len(old):]
	}
}

// plainLines is [plainWords] over a run of evidence lines, dropping the empty
// ones. It is what turns an auditor's answer into an outcome.
func plainLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(plainWords(line))
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// doneOutcome is what a VERIFIED node adds to its card: THE EVIDENCE, ALONE.
//
// No lead word, because there is nothing left for one to say — the state is
// done, the note says "finished", the merge line says the branch came home, and
// a fourth sentence announcing the same fact in the harness's own vocabulary
// would be the machinery taking credit for the work.
//
// IT STANDS UNDER THE WORK'S OWN ACCOUNT AND NEVER OVER IT. The first line of a
// finished report is what the settle card quotes and what the project's index
// keeps as the row's outcome, and that line belongs to what the work found —
// not to the command somebody ran to check it (task_run.go's workTaskNode).
func (v auditVerdict) doneOutcome() string {
	return strings.Join(plainLines(v.evidence), "\n")
}

// gapsOutcome is what a node that ran out of repair rounds says: "incomplete —"
// and the gaps, EVERY ROUND OF THEM, oldest first.
//
// The rounds are kept apart rather than merged into one list because they are
// not one finding. "It was missing amp-labs, then after another go the entry was
// there with no revenue figure" is a story about work converging on the answer
// and running out of turns, and a person reading it knows exactly what one more
// round would have cost them. A flat list of five bullets is not that story.
func gapsOutcome(rounds [][]string) string {
	var out []string
	for _, evidence := range rounds {
		lines := plainLines(evidence)
		if len(lines) == 0 {
			continue
		}
		lead := incompleteLead
		if len(out) > 0 {
			lead = repairedAgainLead
		}
		out = append(out, lead+lines[0])
		out = append(out, lines[1:]...)
	}
	if len(out) == 0 {
		// A finding with no evidence behind it is still a finding, and the person
		// is owed the news even when the checker gave them nothing to hold.
		return incompleteLead + "nothing was said about what is missing"
	}
	return strings.Join(out, "\n")
}

// lookOutcome is what the node nobody could judge says: it FINISHED, and it
// needs eyes. The checker's own words follow, in plain form, because they are
// the whole basis on which somebody is being asked to decide.
func (v auditVerdict) lookOutcome() string {
	lines := plainLines(v.evidence)
	if len(lines) == 0 {
		return needsLookLead + "nobody could say whether it holds"
	}
	return needsLookLead + strings.Join(lines, "\n")
}

// auditVerdict is one audit's answer: the word, and what it is standing on.
//
// THERE ARE THREE OUTCOMES AND TWO BOOLS, and the split is the point. `answered`
// says an auditor reached a verdict AT ALL — it looked at the work and said one
// of the two words. `verified` says WHICH word, and it means nothing unless
// answered is true (verified implies answered; the zero value is the safe one,
// which is "nobody said anything"). The executor branches on both, in that
// order, because the two answers it can get from a failed audit lead to
// different states: unverified waits for a person, refuted fails the graph.
type auditVerdict struct {
	// verified is true for exactly one word.
	verified bool
	// answered is false for a NON-VERDICT: a reply with neither word in it, an
	// empty reply, an audit that could not be started or asked, an audit that
	// ran out of its own deadline. None of them is a finding about the work.
	answered bool
	word     string
	evidence []string
}

// report is how the verdict rides the node's Report: the word, an em dash, and
// the evidence — "VERIFIED — go test ./... ok · 3 files".
func (v auditVerdict) report() string {
	word := v.word
	if word == "" {
		// The zero value is a verdict nobody gave, and it says so rather than
		// borrowing REFUTED's clothes.
		word = auditUnverified
	}
	if len(v.evidence) == 0 {
		return word
	}
	lead := word + " — " + v.evidence[0]
	if len(v.evidence) == 1 {
		return lead
	}
	return lead + "\n" + strings.Join(v.evidence[1:], "\n")
}

// noVerdict is the answer to everything that went wrong before a verdict could
// be reached: the auditor would not start, the turn failed, the reply was not a
// verdict. Every one of them is a REFUSAL to call the work done — nothing
// merges on a non-answer — and NONE of them is a refutation of the work.
//
// It carries two things: WHY there is no verdict, which is the line a person
// reads off the card, and WHAT THE AUDITOR ACTUALLY SAID, which is the evidence
// they are being asked to decide on. An auditor that wrote three paragraphs of
// analysis and forgot the word is not the same object as one that returned an
// empty string, and the person resolving it needs to see which they have.
//
// EVERY `why` HANDED TO THIS IS WRITTEN IN PLAIN WORDS AT ITS CALL SITE — "the
// checker could not start", never "the auditor". They are this package's own
// sentences and this package's own law (see the vocabulary section above); a
// translation layer over text we wrote ourselves would be saying the same thing
// twice and getting to disagree with itself.
func noVerdict(why, said string) auditVerdict {
	verdict := auditVerdict{word: auditUnverified, evidence: []string{why}}
	if said = firstLines(said, auditSaidLines); said != "" {
		verdict.evidence = append(verdict.evidence, strings.Split(said, "\n")...)
	}
	return verdict
}

// twice re-tells a non-verdict as the SECOND one it is. A person reading "the
// auditor could not be asked" wants to know whether that happened once or
// whether the harness tried again and got the same nothing, because only the
// second is worth their attention.
func (v auditVerdict) twice() auditVerdict {
	if len(v.evidence) == 0 {
		return noVerdict("asked twice and got no answer either time", "")
	}
	evidence := make([]string, len(v.evidence))
	copy(evidence, v.evidence)
	evidence[0] = "asked twice and got no answer either time — " + evidence[0]
	v.evidence = evidence
	return v
}

// ── the audit ───────────────────────────────────────────────────────────────

// auditNode is the whole gate: stage the work so the diff is complete, put a
// fresh auditor in the node's worktree, and read its verdict.
//
// It never returns an error. Every way this can go wrong is an ANSWER — that is
// what "self-reports never flip a record to completed" means when the machinery
// itself is what failed: an audit that could not happen is not a pass. What it
// is also not is a refutation, so a non-verdict is asked once more before this
// gives up on it.
//
// ONE RETRY, AND ONLY FOR A NON-VERDICT. A verdict is never re-rolled — asking
// again until the answer changes is not verification, it is shopping — and the
// retry is bounded at one because a second nothing is a broken auditor rather
// than a blip, and a third call would only spend the person's money to write
// down the same absence.
func (a *Agent) auditNode(ctx context.Context, node *TaskNode, tree taskTree, changed []string, claim string, log io.Writer) auditVerdict {
	// STAGED, NOT COMMITTED. `git diff` in a worktree shows changes to tracked
	// files only, so an auditor looking at a node whose whole work was three NEW
	// files would see an empty diff and refute perfectly good work for the wrong
	// reason. Staging puts every file — new ones included — where `git diff
	// --cached` can see it, and comeHome commits from exactly the same index
	// afterwards, so nothing is done twice and nothing is done differently.
	//
	// It is done ONCE, out here, so both attempts judge the same tree: a retry
	// that re-staged would be a second evidence packet, and "the same question
	// asked again" is the only thing a retry is allowed to be.
	if tree.root != "" {
		stageTaskWork(tree.dir)
	}

	verdict, again := a.auditOnce(ctx, node, tree, changed, claim, log)
	switch {
	case verdict.answered, !again:
		return verdict
	case ctx.Err() != nil:
		// The NODE was killed, not the audit. There is nobody to ask again and
		// nothing to ask about; the caller reads ctx itself and tells that story.
		return verdict
	}
	fmt.Fprintf(log, "audit: no verdict — asking a fresh auditor\n")
	retried, _ := a.auditOnce(ctx, node, tree, changed, claim, log)
	if retried.answered {
		return retried
	}
	return retried.twice()
}

// auditOnce is one attempt: a fresh auditor in the node's worktree, one
// question, one reading of what came back.
//
// The second return says whether ASKING AGAIN COULD HELP. A reply with no
// verdict in it and a provider that errored are both worth one more call — the
// first is a model that wandered, the second is a network — while an audit that
// burned its whole deadline is not: the auditor already had every minute it was
// going to get, and a second five minutes buys a second timeout while the node
// holds its worktree.
func (a *Agent) auditOnce(ctx context.Context, node *TaskNode, tree taskTree, changed []string, claim string, log io.Writer) (auditVerdict, bool) {
	auditor, err := a.newAuditAgent(tree.dir, node)
	if err != nil {
		return noVerdict("the checker could not start: "+err.Error(), ""), true
	}
	defer func() {
		_ = auditor.Close()
		// The audit is part of what the node cost, so it lands in the same
		// pocket the node's own spend does (task_run.go's foldTaskUsage): the
		// person asked for a task, not for a task and separately for a judge.
		a.foldTaskUsage(node, auditor)
	}()

	// The deadline hangs off the NODE's context, so `jobs kill` ends a pending
	// audit on the same beat it ends everything else, and the five minutes is a
	// bound on the verdict rather than a second life for a task that has already
	// been stopped.
	auditCtx, done := context.WithTimeout(ctx, auditDeadline)
	defer done()

	fmt.Fprintf(log, "audit: verifying against the acceptance\n")
	events, err := auditor.Submit(auditCtx, auditQuestion(node, tree, changed, claim))
	if err != nil {
		return noVerdict("the checker could not be asked: "+err.Error(), ""), true
	}
	// The turn's own failure is watched for, and it is watched for HERE rather
	// than inferred from an empty reply, because the two are different news with
	// different remedies: a model that wandered still has a lane worth nudging,
	// and a provider that fell over has nothing on the other end of one.
	var failure error
	for event := range events {
		switch event.Kind {
		case EventToolBegin:
			fmt.Fprintf(log, "audit · %s\n", event.Hint)
		case EventError:
			failure = event.Err
		}
	}

	said := lastSaid(auditor)
	// A node killed mid-audit is the caller's story to tell, not the auditor's;
	// it reads ctx itself. What is this function's story is the audit that ran
	// out of its own five minutes with the node still perfectly alive.
	if auditCtx.Err() != nil && ctx.Err() == nil {
		return noVerdict(fmt.Sprintf("no answer in %s, so nothing was accepted", auditDeadline), said), false
	}
	if failure != nil && strings.TrimSpace(said) == "" {
		// NOTHING WAS DELIVERED. There is no reply to have parsed and no auditor
		// left to ask for a word — the call itself did not land — so this goes to
		// the rung that builds a new one, exactly as it did before the nudge
		// existed.
		return noVerdict("the checker could not be asked: "+failure.Error(), ""), true
	}

	verdict := parseAuditVerdict(said)
	// THE FIRST RUNG IS TAKEN HERE, INSIDE THE LANE IT BELONGS TO. A reply that
	// did not parse is the one non-verdict with a live auditor behind it — it has
	// read the diff, run the verification, and stopped a word short — so it is
	// asked for the word before anybody pays for a second investigation. Every
	// other way to get here (a provider error, an auditor that would not start, a
	// deadline that expired) has already returned above, which is exactly the
	// distinction the ladder is drawn on.
	if !verdict.answered && ctx.Err() == nil {
		verdict = a.nudgeAudit(auditCtx, auditor, verdict, log)
	}
	fmt.Fprintf(log, "audit: %s\n", verdict.report())
	return verdict, true
}

// nudgeAudit asks the SAME auditor, once, for the word it did not say.
//
// It runs inside the audit's own five minutes rather than opening a window of
// its own: the auditor has already done the reading, and a nudge that could
// outlive the deadline would be a second audit wearing a cheap name.
//
// EVERY FAILURE KEEPS THE ORIGINAL NON-ANSWER. A nudge that errors, that is cut
// off, or that comes back without the word again has taught us nothing new about
// the work, and the caller's next rung — a fresh auditor — is the same rung it
// was before this one existed. What it must never do is turn a nudge's own
// silence into a finding.
func (a *Agent) nudgeAudit(ctx context.Context, auditor *Agent, missed auditVerdict, log io.Writer) auditVerdict {
	fmt.Fprintf(log, "audit: no verdict — asking the same auditor for the word\n")
	events, err := auditor.Submit(ctx, auditNudge)
	if err != nil {
		return missed
	}
	for event := range events {
		if event.Kind == EventToolBegin {
			fmt.Fprintf(log, "audit · %s\n", event.Hint)
		}
	}
	if ctx.Err() != nil {
		return missed
	}
	if answer := parseAuditVerdict(lastSaid(auditor)); answer.answered {
		return answer
	}
	return missed
}

// ── the repair loop ─────────────────────────────────────────────────────────

// auditOutcome is where a node's whole gate ended: the last verdict, the gaps
// every round found, and the two things a repair round CHANGES about the node —
// the files it wrote and the claim it makes.
//
// The last two are why this is a struct and not a verdict. A repair round is a
// second worker in the same worktree: it writes more files, and it says
// something new about the work. A caller that finished the node on the first
// child's `changed` and `report` would be filing a node under a description that
// stopped being true two rounds ago.
type auditOutcome struct {
	// verdict is the LAST one reached — the one the node lands on.
	verdict auditVerdict
	// gaps is the evidence of every round that found something missing, oldest
	// first, and it is what a failed node's report is built from.
	gaps [][]string
	// changed is every file the node wrote, across the first run and every
	// repair round.
	changed []string
	// claim is the node's own last words, from whichever child spoke last.
	claim string
}

// auditWithRepair is the whole gate: judge, and when the answer is a finding,
// hand the work back with the gaps in front of it and judge again.
//
// THE LOOP IS THE ONLY THING THAT CONVERGES. Each pass builds a FRESH auditor
// through [Agent.auditNode] with the ordinary evidence packet — same shape, same
// contract, no round number, no word about what the last one found — because a
// judge that knows it is looking at a second attempt is a judge with a reason to
// let it through. What moves between rounds is the WORKER's instruction, which
// carries the gaps verbatim, and that is the whole mechanism.
//
// SPEND AND TIME ACCRUE TO THE ONE NODE. Every worker and every auditor is
// folded into the same node's cost as it closes ([Agent.foldTaskUsage]), and the
// node's elapsed keeps running because the node never landed: the person asked
// for one piece of work, and one piece of work is what the row says.
func (a *Agent) auditWithRepair(ctx context.Context, node *TaskNode, tree taskTree, changed []string, claim string, log io.Writer) auditOutcome {
	out := auditOutcome{changed: changed, claim: claim}
	// THE NODE KEEPS THE CLAIM, whichever round produced it. It is the half of
	// the report that a later verdict must carry forward rather than overwrite,
	// and by the time one lands there is nothing left to recover it from
	// (task_run.go's [TaskNode.claim], [Agent.landAudit]).
	defer func() { node.keepClaim(out.claim) }()
	rounds := a.config.TaskRepairRounds
	for round := 1; ; round++ {
		out.verdict = a.auditNode(ctx, node, tree, out.changed, out.claim, log)
		if !out.verdict.answered || out.verdict.verified {
			// Nothing to repair: either the work holds, or nobody said anything
			// about it — and a gap nobody named is not a gap a worker can close.
			return out
		}
		// A finding is kept the moment it is made, whether or not there is a round
		// left to spend on it: the report owes the person the evidence of every
		// round, and the last one is the one that lands the node.
		out.gaps = append(out.gaps, out.verdict.evidence)
		if round > rounds || ctx.Err() != nil {
			return out
		}
		fmt.Fprintf(log, "repair %d of %d: sent back — %s\n",
			round, rounds, strings.Join(out.verdict.evidence, " · "))
		repaired, said := a.repairNode(ctx, node, tree, out.verdict, round, log)
		out.changed = alsoChanged(out.changed, repaired)
		if said = strings.TrimSpace(said); said != "" {
			// The newest account of the work replaces the old one, for the reason
			// the auditor is given a claim at all: the claim is the thing under
			// audit, and the thing under audit is now the repaired tree.
			out.claim = said
		}
		fmt.Fprintf(log, "repair %d of %d: back from the worker — %s\n",
			round, rounds, firstLine(said))
	}
}

// repairNode runs one repair round: the SAME worktree, a fresh worker, the
// original brief with the gaps under it.
//
// THE WORKTREE IS THE POINT. A repair round in a new checkout would be the whole
// task again at full price, and everything the first run got right would have to
// be got right a second time. Working where the work already is makes the round
// what it claims to be — the last ten percent — and it is also what makes the
// next audit honest: the auditor reads one tree containing one piece of work,
// not a diff between two attempts.
//
// THE WORKER IS FRESH, though. The first child's context is forty steps of
// reasoning about a job it believes it finished, and the thing it is worst at is
// seeing what it left out — the same argument that put an independent auditor on
// the gate in the first place, one layer down.
//
// It never returns an error. A repair round that could not start, or that hit a
// threshold, or that wrote nothing, is not a failure of the node: it is a round
// that closed no gaps, and the auditor that follows will say so in evidence a
// person can read.
func (a *Agent) repairNode(ctx context.Context, node *TaskNode, tree taskTree, verdict auditVerdict, round int, log io.Writer) ([]string, string) {
	// THE SURFACE HEARS "STILL WORKING", AND IT HEARS WHAT IS BEING CLOSED. The
	// node never left TaskRunning — nothing landed, nothing was undone — so what
	// goes out is an ordinary running update with the gap on it, and the machinery
	// that sent the work back is not on the wire (task_contract.go's Mending).
	node.mending(mendingLine(verdict.evidence))
	defer node.mending("")

	child, err := a.newTaskAgent(ctx, tree.dir, node, fmt.Sprintf("-repair%d", round))
	if err != nil {
		fmt.Fprintf(log, "repair %d: could not start a worker: %v\n", round, err)
		return nil, ""
	}
	defer func() {
		_ = child.Close()
		a.foldTaskUsage(node, child)
	}()

	// The room follows the work: somebody watching this node came to watch the
	// node, and a repair round is the node still working (task_room.go). It is
	// handed BACK when the round ends, because the round's worker is closed on the
	// way out and a room pointing at a closed agent would refuse a line somebody
	// typed while the node is still perfectly alive.
	room := node.openRoom()
	spoke := room.speaker()
	room.speaking(child)
	defer room.speaking(spoke)

	changed, stopped, runErr := runTaskChild(ctx, child, node, repairInstruction(node, verdict), tree.dir, a.taskLimits(node), room, log)
	switch {
	case stopped != "":
		fmt.Fprintf(log, "repair %d: %s\n", round, stopped)
	case runErr != nil:
		fmt.Fprintf(log, "repair %d: ended with an error: %v\n", round, runErr)
	}
	return changed, taskReport(child)
}

// repairInstruction is what the repairing worker is asked.
//
// It is the node's OWN instruction — the same assembled brief, the same frozen
// acceptance, read from the same fields the first run read (task_run.go's
// [TaskNode.instruction]) — with two things added: the gaps, VERBATIM, and the
// sentence that the work stands.
//
// THE EVIDENCE IS NOT PARAPHRASED. It goes in exactly as the auditor wrote it,
// because it is the most precise description of what is missing that exists
// anywhere in this system, and a harness that summarized it would be a harness
// deciding which half of the finding the worker gets to see. (The plain-words
// law is about what a PERSON reads; a worker being told what to fix is machinery
// talking to machinery, and the heading calls it a review because that is what
// it is.)
func repairInstruction(node *TaskNode, verdict auditVerdict) string {
	var out strings.Builder
	out.WriteString(node.instruction())
	out.WriteString("\n\n" + repairHeading + "\n")
	for _, line := range verdict.evidence {
		out.WriteString(line + "\n")
	}
	out.WriteString("\n" + repairStands)
	return out.String()
}

// mendingLine is the gap as a surface may draw it: the first line of evidence,
// in plain words, cut to one line.
//
// IT IS DERIVED, NOT WRITTEN. The temptation is to turn "amp-labs is missing"
// into "adding amp-labs to the report" — a nicer sentence — and that would be
// this build putting words in the checker's mouth about work it has not read.
// The first evidence line IS the gap, stated by the only party that looked, and
// the only thing done to it here is taking the machinery vocabulary off.
func mendingLine(evidence []string) string {
	for _, line := range plainLines(evidence) {
		return clip(firstLine(line), taskReportLineLimit)
	}
	return ""
}

// alsoChanged folds a repair round's files into the node's list, keeping the
// order they were first written in and never listing one twice. A file the first
// run wrote and a repair round rewrote is ONE file the node changed.
func alsoChanged(changed, more []string) []string {
	if len(more) == 0 {
		return changed
	}
	seen := make(map[string]bool, len(changed)+len(more))
	for _, path := range changed {
		seen[path] = true
	}
	for _, path := range more {
		if seen[path] {
			continue
		}
		seen[path] = true
		changed = append(changed, path)
	}
	return changed
}

// auditQuestion is what the auditor is asked: the frozen acceptance, the work's
// own claim, and where to look.
//
// The BRIEF IS NOT HERE, and that is deliberate. The brief is the executor's
// instruction — its goal, its constraints, the conventions it was told to
// follow — and an auditor reading it starts grading effort and intention. The
// acceptance is the contract (Argus's two-tier goal contract,
// harness-research-notes.md §1: the objective moves only with authority), and
// it is the SAME frozen text the node was finished against. The node's own last
// words are included as a CLAIM, labelled as one: it is the thing under audit,
// not evidence about it.
func auditQuestion(node *TaskNode, tree taskTree, changed []string, claim string) string {
	var out strings.Builder
	out.WriteString("The work: " + node.title() + "\n\n")
	out.WriteString("ACCEPTANCE (this is the contract; judge against this and nothing else):\n")
	out.WriteString(node.acceptance() + "\n\n")

	if claim = strings.TrimSpace(claim); claim != "" {
		out.WriteString("What it CLAIMS it did — this is the claim under audit, not evidence:\n")
		out.WriteString(claim + "\n\n")
	}
	if len(changed) > 0 {
		out.WriteString("Files it wrote: " + strings.Join(changed, ", ") + "\n\n")
	}

	out.WriteString("You are in the working copy where the work was done.\n")
	if tree.root != "" {
		out.WriteString("Its changes are staged, so `git diff --cached` shows all of them, new files included.\n")
	} else {
		out.WriteString("This workspace is not a repository, so there is no diff to read: check the files themselves.\n")
	}
	out.WriteString("\nRun the verification. Read the change. Then give your verdict.")
	return out.String()
}

// parseAuditVerdict reads the answer.
//
// It looks for the first line that BEGINS with a verdict word, the way
// guardianSaysAllow reads one word: a model that has decided to be helpful in
// prose and mentions the word VERIFIED inside a sentence has not answered a
// binary contract. An unanswered contract is NOT a refutation, though — it is
// nothing at all, and it says so, carrying whatever was said instead.
func parseAuditVerdict(text string) auditVerdict {
	for _, raw := range strings.Split(text, "\n") {
		line := strings.Trim(strings.TrimSpace(raw), "`*\"'“”#> ")
		if line == "" {
			continue
		}
		word, rest, ok := auditWord(line)
		if !ok {
			continue
		}
		evidence := []string{}
		if rest != "" {
			evidence = append(evidence, clip(rest, taskReportLineLimit))
		}
		return auditVerdict{
			verified: word == auditVerified,
			answered: true,
			word:     word,
			evidence: auditEvidence(evidence, text, raw),
		}
	}
	return noVerdict("the checker answered neither way", text)
}

// auditWord splits a verdict line into the word and whatever follows it, and
// reports false for a line that does not start with one. The separator is
// whatever the model reached for — an em dash, a colon, a hyphen — because the
// contract is about the first word, not about punctuation.
func auditWord(line string) (string, string, bool) {
	for _, word := range []string{auditVerified, auditRefuted} {
		// The uppercasing is done on the SLICE, not on the line: ToUpper can
		// change a string's byte length (ﬁ becomes FI), and an index taken from
		// a converted string and used on the original is an index that can be
		// wrong by a byte.
		if len(line) < len(word) || !strings.EqualFold(line[:len(word)], word) {
			continue
		}
		rest := strings.TrimSpace(line[len(word):])
		rest = strings.TrimLeft(rest, "—–-:· ")
		return word, strings.TrimSpace(rest), true
	}
	return "", "", false
}

// auditEvidence collects the lines after the verdict, up to the limit. The
// evidence is what makes a verdict answerable by a person — "REFUTED" alone is
// an opinion; "REFUTED — TestParse still fails: want 3, got 0" is a fact
// somebody can go and check.
func auditEvidence(evidence []string, text, verdictLine string) []string {
	after := false
	for _, raw := range strings.Split(text, "\n") {
		if raw == verdictLine {
			after = true
			continue
		}
		if !after {
			continue
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if len(evidence) >= auditEvidenceLines {
			break
		}
		evidence = append(evidence, clip(line, taskReportLineLimit))
	}
	if len(evidence) > auditEvidenceLines {
		evidence = evidence[:auditEvidenceLines]
	}
	return evidence
}

// ── when nobody could decide ────────────────────────────────────────────────

// ResolveUnverified is the person's answer to a node no auditor could judge.
//
// It is NOT [Agent.ResolveTask], which answers a PROPOSAL — should this work
// start — and the two are spelled apart on purpose: one is a decision about
// work that has not happened, this is a decision about work that has.
//
// An unverified node is the one state in this graph that WAITS ON A HUMAN. It
// is not stuck by accident and it is not going to resolve itself: the auditor
// was asked twice and said nothing both times, so the only remaining source of
// a verdict is somebody who can read the diff. Until they do, the branch sits
// where it was kept and the dependents sit queued — which is the honest
// position, because an unverified claim is not evidence, and failing them on
// the strength of an audit that never happened is the exact defect this whole
// path exists to remove.
//
// THE THREE ANSWERS GO THROUGH THE GATE'S OWN SETTLE. Accepting merges the
// branch with [taskTree.comeHome], the same call a VERIFIED verdict makes;
// refuting fails the node and lets the frontier cascade; re-auditing runs the
// audit again and lands whatever it says. Nothing here is a second way to
// finish a node — it is the same finish, reached by a different judge.
//
// It is exported because two callers need it: a surface with a person in front
// of it, and the model through the `tasks` tool (tools_tasks.go).
func (a *Agent) ResolveUnverified(id uint64, resolution TaskResolution, why string) error {
	node := a.taskNode(id)
	if node == nil {
		return fmt.Errorf("no task %d in this session", id)
	}
	// The state is read here for the ERROR, and claimed again inside each of the
	// three answers for the SETTLE. The check is not the guard — it cannot be,
	// with a re-audit able to land between this line and the merge — and the
	// claim below is (task_run.go's [TaskNode.claimSettle]). What this one buys
	// is the right sentence: a done node asked to re-audit hears that it is done,
	// rather than that there is no auditor configured.
	if state := node.stateNow(); state != TaskUnverified {
		return settledAlready(id, state)
	}
	why = strings.TrimSpace(why)
	switch resolution {
	case TaskAccept:
		return a.acceptTask(node, why)
	case TaskRefute:
		return a.refuteTask(node, why)
	case TaskReaudit:
		return a.reauditTask(node)
	}
	return fmt.Errorf("%q is not a resolution: say %s, %s or %s", resolution, TaskAccept, TaskReaudit, TaskRefute)
}

// ErrTaskDecided says the answer arrived after the question had gone: somebody
// else settled this node — the model's own `tasks … resolve`, a re-check that
// finally answered, another window — between the surface drawing the choices and
// somebody pressing one.
//
// IT IS A SENTINEL BECAUSE THE SURFACE HAS TO TELL IT APART FROM TROUBLE. Every
// other refusal these doors give means the question is STILL STANDING and the
// person should try another answer — no working copy, no checker to ask — and a
// card that answered both by quietly saying "already answered" would be
// reporting a decision nobody made (internal/tui3's tasksettle.go).
var ErrTaskDecided = errors.New("session: that task has already been settled")

// settledAlready is the refusal both doors give for a node that has moved on. It
// names the state in the person's own words and wraps the sentinel above.
func settledAlready(id uint64, state TaskState) error {
	return fmt.Errorf("task %d is %s, and only a task that needs a look is waiting on somebody to decide: %w",
		id, state, ErrTaskDecided)
}

// HandUnverifiedToModel gives ONE node's decision to the model instead of
// taking it: the surface's "decide these yourself from now on", pressed on the
// card that is asking right now (internal/tui3's taskdone.go).
//
// IT DOES NOT RESOLVE ANYTHING. The node stays exactly as it is — unverified,
// branch kept, dependents waiting — and what changes is who is holding the
// question: a line lands on the steering queue, the session wakes if it is
// idle, and the model reads the work and calls `tasks … resolve` itself. That
// is the same path a landing under `task.settle = auto` takes, said about a
// node that already landed, so the two doors cannot disagree about what the
// model is being asked to do.
//
// The error is the one [Agent.ResolveUnverified] gives for the same node,
// because a surface pressing this on work that somebody else has already
// decided needs the same sentence either way.
func (a *Agent) HandUnverifiedToModel(id uint64) error {
	node := a.taskNode(id)
	if node == nil {
		return fmt.Errorf("no task %d in this session", id)
	}
	if state := node.stateNow(); state != TaskUnverified {
		return settledAlready(id, state)
	}
	notice := node.notice()
	a.enqueueSteering(handOverLead + "\n" +
		taskNote(notice, taskURI(node.journalPath()), TaskSettleAuto))
	return nil
}

// handOverLead is what the model reads first when a person hands one of these
// over. It says who asked, because the sentence under it is written as an
// instruction and an instruction with no author is one the model has to guess
// the standing of.
const handOverLead = "the person has asked you to make this decision rather than making it themselves."

// The three claims, spelled as the thing a person is waiting on rather than as
// the function that took it: whoever loses the race reads this word back inside
// the refusal, and "acceptTask" is not a sentence (see the vocabulary law at the
// top of this file).
const (
	claimAccept  = "your accept"
	claimRefute  = "your refute"
	claimReaudit = "a re-audit"
)

// acceptTask takes the work as done on the person's word.
//
// THE BRANCH COMES HOME THE ORDINARY WAY. An accepted node is a node somebody
// verified by hand, so it merges exactly as a VERIFIED one does and its
// dependents unblock on the frontier pass the settle turns. What it does NOT do
// is pretend an auditor said so: the report leads with who accepted it and on
// what grounds, because a card that read "VERIFIED" over a verdict nobody gave
// would be the same lie as the one this file was fixed to stop telling.
func (a *Agent) acceptTask(node *TaskNode, why string) error {
	// THE CLAIM IS TAKEN BEFORE THE WORKING COPY IS LOOKED FOR, and it covers
	// everything down to the resettle. What sits between the two is an os.Stat, a
	// `git rev-parse` and a merge — long enough for a second accept in the same
	// tool batch, or for a re-audit landing REFUTED, to walk straight through a
	// state that was read and not held (task_run.go's [TaskNode.claimSettle]).
	if err := node.claimSettle(claimAccept); err != nil {
		return err
	}
	defer node.releaseSettle()
	tree, err := node.workingCopy(a.config.Place, a.config.Workspace)
	if err != nil {
		return err
	}
	report, changed, _, _ := node.leavings()
	merge, detail := tree.comeHome(node.title())
	node.finish(withReport(acceptedLine(why), withReport(report, detail)), changed, tree.branch, merge)
	node.graph.resettle(node, TaskDone)
	return nil
}

// refuteTask is the person doing the auditor's job in the negative. The node's
// previous report is KEPT under the refusal rather than replaced — unlike a real
// REFUTED verdict, which drops the node's claim because the auditor's evidence
// has already answered it. Here the auditor answered nothing, so what the node
// said is still the only account of the work there is.
func (a *Agent) refuteTask(node *TaskNode, why string) error {
	if err := node.claimSettle(claimRefute); err != nil {
		return err
	}
	defer node.releaseSettle()
	report, changed, branch, merge := node.leavings()
	node.finish(withReport(refutedLine(why), report), changed, branch, merge)
	node.graph.resettle(node, TaskFailed)
	return nil
}

// reauditTask sends a fresh auditor at the same working copy.
//
// IT RETURNS BEFORE THE VERDICT DOES, and that is the whole shape of it. An
// audit is bounded at five minutes, and a tool call or a keypress that blocked
// for five minutes would be a wedged surface — so the re-audit runs as its own
// goroutine and the node stays UNVERIFIED, which is exactly what it is until
// somebody answers. When the verdict lands it settles the node through
// [Agent.landAudit], and the person and the model hear about it on the same
// lane every other landing rides.
//
// IT IS A JOB, from the same registry the node's own run came from (jobs.go).
// That is not decoration: a piece of work that outlives the call which asked
// for it needs a row in `jobs list`, a `jobs kill`, a log somebody can read,
// and — the one that matters here — a death at [Agent.Close]. A goroutine on
// context.Background would keep auditing a session that has gone, and settle a
// node into a checkpoint nobody is writing any more.
func (a *Agent) reauditTask(node *TaskNode) error {
	if !a.config.TaskAudit {
		return errors.New("task.audit is off, so there is no auditor to ask — accept it or refute it")
	}
	tree, err := node.workingCopy(a.config.Place, a.config.Workspace)
	if err != nil {
		return err
	}
	if err := node.claimSettle(claimReaudit); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	// NO JOB ROW, NO RE-AUDIT. The registry is where the cancel is registered, so
	// a goroutine started without one would run on a bare context: no `jobs kill`,
	// no death at [Agent.Close], and a landing that finishes a node into a session
	// that has gone — the exact thing the block above says the job exists to
	// prevent. A workspace that cannot take a log file is a reason to refuse the
	// re-audit, in words that leave the person their other two answers.
	listed, err := a.jobs.startTask(node.id, "re-audit · "+node.title(), cancel)
	if err != nil {
		cancel()
		node.releaseSettle()
		return fmt.Errorf("the re-audit could not be started: %w — accept it or refute it instead", err)
	}
	_, changed, _, _ := node.leavings()
	go func() {
		defer cancel()
		defer node.releaseSettle()
		defer listed.settle(0)
		// NO CLAIM IS PASSED. The first audit was given the node's own last
		// words as the thing under audit; this one is given the acceptance and
		// the diff, and nothing about the answer that was not an answer — a
		// fresh auditor primed with "the last one could not decide" is a fresh
		// auditor that has been told what to conclude.
		verdict := a.auditNode(ctx, node, tree, changed, "", taskLog(listed))
		if ctx.Err() != nil {
			// KILLED IS NOT A VERDICT. The node is left exactly as it was —
			// unverified, waiting on somebody — because a re-audit that was
			// stopped is a re-audit that never happened.
			return
		}
		a.landAudit(node, tree, verdict, changed)
	}()
	return nil
}

// landAudit settles a node on a verdict that arrived after it had already
// landed. It reads the same three answers the gate reads (task_run.go's
// workTaskNode), and reaches the same three states — the only difference is
// that this one re-settles a node instead of completing a run.
func (a *Agent) landAudit(node *TaskNode, tree taskTree, verdict auditVerdict, changed []string) {
	report, _, branch, merge := node.leavings()
	// THE TWO HALVES OF THE CARD, PULLED APART BEFORE EITHER IS REWRITTEN. The
	// report a landed unverified node carries is the last audit's line with the
	// WORK'S OWN account under it, and the two branches below want opposite
	// things from it: the non-answer replaces the audit half, the verdict
	// replaces it with a verdict. Both keep the work's half, which is why it is
	// kept apart (task_run.go's [TaskNode.claim]).
	claim := node.workClaim(report)
	switch {
	case !verdict.answered:
		// STILL NOBODY. The fresh non-answer REPLACES the stale one rather than
		// stacking under it: two auditors failing to answer is one fact, and a
		// report that grew a paragraph per attempt would be a card nobody can
		// read by the third try. Every attempt is in its own audit journal.
		//
		// WHAT IS NOT REPLACED IS THE WORK'S CLAIM. It was never a non-answer, it
		// is the only description of what was done that exists, and whoever is
		// asked to resolve this node needs both halves (task_contract.go's
		// TaskUnverified) — the index row, the brief a dependent is handed, and
		// the accept that carries it into TaskDone all read this string.
		node.finish(withReport(verdict.lookOutcome(), claim), changed, branch, merge)
		node.graph.resettle(node, TaskUnverified)
	case !verdict.verified:
		// A re-audit that finds something is a landing, not a loop. The repair
		// rounds belong to a node's RUN (see [Agent.auditWithRepair]); this node
		// has already landed once and been handed to a person, and starting a
		// worker inside their answer would be the harness spending on a decision
		// they made rather than carrying it out.
		node.finish(gapsOutcome([][]string{verdict.evidence}), changed, branch, abortedMerge(tree))
		node.graph.resettle(node, TaskFailed)
	default:
		merged, detail := tree.comeHome(node.title())
		// THE CLAIM, NOT THE CARRIED REPORT — and the claim LEADS, exactly as it
		// does on the gate's own landing (task_run.go's workTaskNode). The carried
		// report opens with the line that said nobody could judge this work, and a
		// card stacking a fresh answer over "finished, but needs your look — …"
		// contradicts itself in two lines. What the person and the model want first
		// is what the work found; what it was checked on follows.
		node.finish(withReport(claim, withReport(verdict.doneOutcome(), detail)), changed, tree.branch, merged)
		node.graph.resettle(node, TaskDone)
	}
}

// acceptedLine and refutedLine are the first line of a resolved node's report —
// which is also its row in the project's index (task_index.go's taskOutcome), so
// each says WHO decided and, when they gave one, why.
//
// THEY SAY "YOU", AND THEY SAY IT IN PLAIN WORDS. These two lines land in front
// of the person who wrote them and in front of the model that will describe the
// work back to them, so the vocabulary law holds here exactly as it holds on
// every other landing: what happened is that a person looked and made a call,
// and no part of that is worth spelling in the harness's own courtroom.
func acceptedLine(why string) string {
	line := "you looked at this yourself and took it as done"
	if why != "" {
		line += ": " + clip(firstLine(why), taskReportLineLimit)
	}
	return line
}

func refutedLine(why string) string {
	// It leads with the same word a node that ran out of repair rounds leads
	// with, because it is the same news: the work is not finished. Who decided is
	// the second half of the sentence, not the headline.
	line := incompleteLead + "you looked at this yourself and said so"
	if why != "" {
		line += ": " + clip(firstLine(why), taskReportLineLimit)
	}
	return line
}

// ── the auditor's agent ─────────────────────────────────────────────────────

// newAuditAgent builds the judge: the same loop and the same package as the
// node it audits, on the high tier, with a read-only belt and a system prompt
// that is nothing but the audit contract.
//
// It inherits NEITHER the node's context nor the conversation's: a fresh
// context per round is the other half of MEA's law (the executor's raw
// trajectory is discarded, only its report survives to audit), and an auditor
// that had watched the work happen would be grading a story it had already been
// told. What it inherits is the provider — same key, same base URL — and
// nothing that reaches outside the machine: no search, no fetch, no image
// model. An auditor that can browse is an auditor that can be told a story from
// somewhere else.
func (a *Agent) newAuditAgent(dir string, node *TaskNode) (*Agent, error) {
	a.mu.Lock()
	parent := a.config
	model := a.model
	client := unwrapCompleter(a.client)
	// EVERY ATTEMPT GETS ITS OWN JOURNAL, AND THE NONCE IS WHAT MAKES THE NEXT
	// AUDITOR FRESH. The path carries a timestamp to the second, and two audits of
	// one node — the retry after a non-answer, the check after a repair round —
	// land inside the same second all the time. Sharing a path is not a cosmetic
	// clash: [newAgent] RESUMES a session file that already exists, so the
	// "fresh" auditor would open with the previous one's whole transcript in
	// front of it, including its verdict. That is the one thing this gate must
	// never be — an auditor that has already been told what to think.
	journal := taskJournalPath(parent.Place, a.sessionID(), node.id, "-audit-"+shortID())
	a.mu.Unlock()

	judge, err := roles.Resolve(roles.Source(parent.RolesSource), roles.RoleAuditor, model)
	if err != nil {
		return nil, err
	}
	auditor, err := newAgent(Config{
		Workspace:     dir,
		Model:         judge,
		APIKey:        parent.APIKey,
		BaseURL:       parent.BaseURL,
		ContextWindow: parent.ContextWindow,
		// The audit is bounded at five minutes and reads what it chooses to
		// read; a compaction inside that window is a summary of a judgement in
		// progress, which is the one thing a verdict must not be built on.
		CompactEnabled: false,
		SessionFile:    journal,
		System:         auditPrompt,
		// The floor is still the floor (approval's critical table), but the
		// belt is what actually constrains this agent: there is no hand here
		// that writes. AskConsent is off and InTask is on for the node's own
		// reason — there is nobody in a worktree to ask.
		ApprovalPolicy: &approval.Policy{Default: approval.ActionAllow},
		AskConsent:     false,
		InTask:         true,
		// The auditor is the node too, as far as anybody watching is concerned:
		// it runs on the node's clock, in the node's worktree, and a card whose
		// audit is parked on a provider's pacing is a card whose task is not
		// moving (task_run.go's [TaskNode.pacing]).
		pacing:      node.pacing,
		RolesSource: parent.RolesSource,
	}, client)
	if err != nil {
		return nil, err
	}

	// THE BELT IS REPLACED, and this is the only place in the surface that does
	// it. The alternative — a config flag threaded into belt() — would put
	// "what an auditor may touch" in a file that is about what a conversation
	// may touch, and every later hand added to the session would silently join
	// the auditor's belt unless somebody remembered this rule. Composed here,
	// a new tool reaches the auditor only when this list names it.
	tools := auditBelt(dir, auditCommands)
	definitions, err := toolDefinitions(tools)
	if err != nil {
		_ = auditor.Close()
		return nil, err
	}
	auditor.mu.Lock()
	auditor.tools = tools
	auditor.definitions = definitions
	auditor.mu.Unlock()
	return auditor, nil
}

// auditBelt is the read-only belt: the four readers, and a bash that runs
// verification and refuses the rest. edit and write are not filtered out of a
// list — they are never put in one.
//
// EVERY HAND ON IT IS CAPPED, and the cap is the belt's own rather than each
// tool's, because the failure it exists for was not any one tool misbehaving: an
// auditor ran `ls` on a directory that was not a repository, the listing filled
// the context it was supposed to reach a verdict in, and the audit died. What
// bounds an investigation is what ONE ANSWER may weigh, whichever hand returned
// it, so it is applied here — where the hands are chosen — and not five times
// over in five wrappers.
func auditBelt(dir string, allowed []string) []bare.Tool {
	var belt []bare.Tool
	for _, tool := range bare.AllTools(dir) {
		switch tool.Name {
		case "read", "grep", "find", "ls":
			belt = append(belt, boundedResult(tool))
		case "bash":
			belt = append(belt, boundedResult(verifyOnlyBash(tool, allowed)))
		}
	}
	return belt
}

// boundedResult caps what one tool call may hand back.
//
// It reuses [capBytes], which is the package's own truncation — the same one
// [capOutput] bounds a tool result for a person's screen with — so a cut result
// carries the count of what was left behind rather than an ellipsis: an auditor
// that cannot tell whether it is missing a line or a megabyte cannot tell
// whether it has seen enough to judge. The sentence after it says what to do
// about it, because the answer is never "give up", it is "ask something
// narrower".
//
// The refusals pass through UNCAPPED in every practical case and deliberately go
// through the same cap anyway: a refusal is a result like any other, and a gate
// with an exception in it is a gate with a way around it.
func boundedResult(tool bare.Tool) bare.Tool {
	inner := tool.Execute
	tool.Execute = func(ctx context.Context, args json.RawMessage) (string, bool, error) {
		text, isError, err := inner(ctx, args)
		if err != nil || len(text) <= auditResultLimit {
			return text, isError, err
		}
		return capBytes(text, auditResultLimit) +
			"\n[cut here: ask something narrower — a path, a pattern, a specific file]", isError, nil
	}
	return tool
}

// verifyOnlyBash wraps pi's bash so it runs the repository's own verification
// and nothing else.
//
// The refusal is a RESULT, not an error: the auditor reads "I am not allowed to
// run that, here is what I am allowed to run" and gets on with the job, exactly
// as a node reads a refused consent (consent.go). A Go error would end its turn
// and cost a verdict over one wrong reach.
func verifyOnlyBash(tool bare.Tool, allowed []string) bare.Tool {
	inner := tool.Execute
	tool.Description = "Run one of the repository's own verification commands and read its output: " +
		strings.Join(allowed, ", ") + ". Every other command is refused, including anything that " +
		"edits, installs, fetches, or chains a second command onto one of these. " +
		auditReaderHint + " " + tool.Description
	tool.Execute = func(ctx context.Context, args json.RawMessage) (string, bool, error) {
		var fields struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &fields); err != nil {
			return "Invalid arguments: " + err.Error(), true, nil
		}
		if refusal, ok := auditRefusal(fields.Command, allowed); !ok {
			return refusal, true, nil
		}
		return inner(ctx, args)
	}
	return tool
}

// auditRefusal decides one command, and it decides it in two steps because a
// prefix check on its own is not a gate: `go test ./... && rm -rf .` starts with
// an allowed prefix and is not an allowed command.
//
// So SHELL COMPOSITION IS REFUSED OUTRIGHT — every operator that can start a
// second command, redirect output, or substitute one — and only then is what
// remains matched against the allowlist. That order is the whole safety
// argument: after the first check there is exactly one command in the string,
// and the second check is about that command.
func auditRefusal(command string, allowed []string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "refused: an auditor runs verification, and that was an empty command.\n" + auditReaderHint, false
	}
	if index := strings.IndexAny(command, ";|&<>`$(){}\n\r\\"); index >= 0 {
		return fmt.Sprintf("refused: an auditor runs ONE verification command with no shell composition, and %q is in %s.\nYou may run: %s\n%s",
			string(command[index]), clip(command, auditCommandLimit), strings.Join(allowed, ", "), auditReaderHint), false
	}
	// Whitespace is normalized so "go  test" is the same command as "go test":
	// the allowlist is about which program runs, not about how it was typed.
	normalized := strings.Join(strings.Fields(command), " ")
	for _, prefix := range allowed {
		if normalized == prefix || strings.HasPrefix(normalized, prefix+" ") {
			return "", true
		}
	}
	return fmt.Sprintf("refused: %s is not verification, and an auditor only runs verification.\nYou may run: %s\n%s",
		clip(normalized, auditCommandLimit), strings.Join(allowed, ", "), auditReaderHint), false
}
