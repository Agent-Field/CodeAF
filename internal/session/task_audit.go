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
//	nobody could say  the question the row is asking ([yourCallLead]) and what
//	                  the checker said
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
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/taxonomy"
)

// The auditor is a ROLE, registered from the file that makes the call, exactly
// as internal/roles' own doc says an auxiliary call should. HIGH, and not as a
// default somebody is expected to tune down: this is the call that decides
// whether work is real.
func init() { roles.Register(roles.RoleAuditor, roles.TierHigh) }

const (
	// auditDeadline bounds one verdict. Five minutes is a full run of a real
	// repository's own check plus the reading around it; past that the auditor
	// is not judging, it is stuck, and a node that waits forever for a verdict
	// is worse than a node that is told nobody could give it one.
	//
	// IT IS THE BOUND FOR AN AUDIT THAT HAS SOMETHING TO RUN. An audit holding
	// no runnable check has no slow half and gets the much shorter window
	// instead ([auditReadingDeadline], [auditDoor.window]).
	auditDeadline = 5 * time.Minute

	// auditCallShare is how many equal shares a checking window is cut into, one
	// of which is the most a single call may hold.
	//
	// THE WINDOW BOUNDS THE CHECKING AND NOT ONE PROVIDER STREAM. That was the
	// measured failure: a checker opened its five minutes, its first stream hung,
	// and 183 seconds later the window was gone — no refusal, no error, no second
	// attempt, and a landing that said nobody could check the work in five minutes
	// when nobody had in fact been asked twice (#513). A window spent on one hung
	// call is a window that bought nothing.
	//
	// TWO, BECAUSE THE LADDER ASKS TWICE ([Agent.auditNode]: one attempt, then
	// one fresh checker). A share per attempt is the tightest bound that still
	// lets the two of them spend the whole window between them, so a window that
	// closes has genuinely been spent on checking and [checkerRanOut]'s figure
	// stays a true sentence.
	//
	// AND IT IS NOT TIGHTER THAN THAT ON PURPOSE. The window is five minutes
	// because a real check is slow — a full test run on a real repository plus the
	// reading around it — and a share so small that an honest slow check is cut
	// would turn every real verdict into a non-answer, which is a worse defect
	// than the one this fixes. Half is the most one call can hold without
	// leaving the second attempt nothing to be asked with.
	auditCallShare = 2

	// auditCallFloorShare is the LEAST a call may be given, as the number of
	// shares of the window that is, and below it no call is made at all.
	//
	// A BOUND SMALL ENOUGH TO GUARANTEE A NON-ANSWER IS NOT A BOUND, IT IS A LIE.
	// What is left of the window when the second attempt starts is whatever the
	// first one did not spend, less the time it took to close one checker and
	// build another — so a first call that ran nearly to its share can leave the
	// retry a few hundred milliseconds, which it will certainly not answer in and
	// which would then be written down as a call that STALLED
	// ([checkerStalled]). A tenth of the window is the line under which asking is
	// not worth the sentence it produces: below it the retry is not made, and the
	// landing says the window closed, which is what actually happened.
	auditCallFloorShare = 10

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

	// auditReadContentLimit leaves room under auditResultLimit for bare read's
	// exact line range and continuation footer. It changes only the page size;
	// paths and offsets keep the ordinary read contract.
	auditReadContentLimit = 7500

	// auditRestoreEntries bounds the CLEAN RESTORE a verdict is reached in when
	// the workspace is not a repository and the restore has to be copied by hand
	// ([restoreTaskWork]).
	//
	// Twenty thousand entries is a large source tree and nothing like a built
	// one: a repository's own files are thousands, and the hundreds of thousands
	// under a node_modules or a target/ are exactly what a restore must not be
	// carrying anyway. Past it the copy stops and the audit falls back to the
	// node's own working copy, which is where it has always run — a slower
	// verdict is worth having, and a five-minute file copy inside a five-minute
	// deadline is not.
	auditRestoreEntries = 20000

	// auditReceiptCount and auditReceiptLimit bound WHAT THE WORK ALREADY RAN as
	// it rides the auditor's packet ([lastToolReceipts]).
	//
	// Six results, at twelve hundred bytes each, is a little over 7KB — the same
	// order as one [auditResultLimit] answer, which is the figure this build has
	// already settled on for "one screen of evidence an auditor can afford". The
	// six are the LAST six, because a check is the last thing a worker does
	// before it says it is finished, and the tail is where it lives.
	auditReceiptCount = 6
	auditReceiptLimit = 1200

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

// THE ALLOWLIST IS NOT WRITTEN IN THIS FILE ANY MORE, and that is the whole of
// one fix. It used to be a constant naming three `go` verbs, which made the gate
// a gate for exactly one language and a coincidence everywhere else — measured,
// on a Rust deliverable, in task_checks.go's opening. What one audit may run is
// now read off the WORK: the checks its own document declares, the checks its
// worker ran, and the always-safe reading commands ([auditDoorFor]).
//
// Every entry is still a COMMAND PREFIX matched field by field, so a check
// admits its own flags and does not admit a program that merely starts like it.

// auditPrompt is the auditor's whole world. It never sees the conversation, it
// never sees the node's trajectory, and it is told in the first line that its
// answer is the only reason the work can be called finished.
//
// HOW HARD IT LOOKS FOLLOWS WHAT THE WORK CHANGED. The auditor gathers its own
// evidence, and evidence is bought with the person's time and money, so the
// depth is not a constant: work that rewrote something load-bearing is worth the
// whole ladder, while a deliverable whose answer is that nothing needed doing is
// answered by ONE thing that would have needed doing. Hunting for that one thing
// is both the cheaper check and the only one that could have found the mistake —
// re-deriving a claim that changed nothing spends the money and rules out
// nothing, which is the same ceremony the working-style prompt is written
// against (prompts/system.md's Verify law).
const auditPrompt = `You are an AUDITOR. Somebody else did a piece of work and says it is finished. You decide whether that is true, and your verdict is the only reason it can be called finished at all.

You are READ-ONLY. You have read, grep, find and ls, and a bash that runs the repository's own verification and nothing else. You cannot edit, write, install, or fix anything, and you must not try — the work is not yours to repair. Your job is to find out what is true.

Judge the work against its ACCEPTANCE and nothing else: not what you would have written, not what else the code could use, not how the change was made. Run the verification yourself and read the diff. A claim you did not check is a claim you have not verified.

AND WHAT THE WORK SAYS ABOUT THE WORLD IS PART OF WHAT YOU ARE CHECKING. Anything it asserts — a note it wrote listing what stopped being true, its own account of what it did — is a claim, and a claim is checked by going and looking: what something says is a search, what something now does is a thing to run, what was updated somewhere is a question about which files changed. An assertion that is not so is a finding even when the work itself holds. If you could not settle one, say so by name in your evidence rather than passing over it.

A CHECK THAT PASSES ONLY BECAUSE OF SOMETHING THE WORK DID NOT WRITE HAS NOT PASSED. Installing, building and caching are expected — do them freely, they are what your time is for. What may not carry a verdict is state that does not ship: a file put somewhere by hand, a link made so a path would resolve, a directory created outside the change. If what makes the check pass is not in the files the work wrote, REFUTE and name what is missing.

How deep you look follows the size of what the work CHANGED. Where it rewrote something load-bearing, take the whole ladder: run the verification, read the change through. Where the deliverable's answer is that NOTHING needed doing, do not re-derive the whole claim — go hunting for the one thing that WOULD have needed doing, because that is the only thing that can make the answer wrong, and coming back empty-handed is your evidence.

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

// The third thing a repair round adds: WHERE THE NINETY PERCENT IS.
//
// A worker opening on a brief reads it as a job to start, and the first thing it
// does is go and find out what is in the repository — which is the right instinct
// on a fresh task and pure waste here, because the tree it is standing in was
// filled by the last worker an hour ago. Worse, it is waste bought at the
// escalated tier: the cascade puts this round on the careful model
// (repair_role.go), and a careful model re-exploring a repository from scratch is
// the most expensive way there is to learn something the harness already knew.
//
// So the round is handed the change instead of the world: the files the work has
// written, the shape of the diff, and the one sentence that says where the whole
// of it can be read in a single command.
const (
	repairSawHeading = "WHAT IS ALREADY IN THIS WORKING COPY:"
	// repairDiffPointer is spelled to match the auditor's own orientation line
	// ([auditQuestion]) on purpose: both readers are standing in the same staged
	// tree, and two sentences describing it differently would be two accounts of
	// one fact.
	repairDiffPointer = "Its changes are staged, so `git diff --cached` shows all of them, new files included. Read that before you read anything else in the repository."
	repairNoDiff      = "This workspace is not a repository, so there is no diff to read: the files named above are the change."
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
	// yourCallDash joins a your-call landing's question to the detail under it,
	// and it is the only part of that lead spelled in this file: the question
	// itself is [yourCallLead]'s, read off the projection.
	yourCallDash = " — "
	// repairedAgainLead opens the second and later rounds' gaps, so that a
	// report carrying three sets of evidence reads as three attempts rather than
	// as one auditor repeating itself.
	repairedAgainLead = "still incomplete after another go — "
	// keptWhereItIsLead opens the node that was taken as done and could not be
	// brought home because the TREE would not have it (task_land_unsaved.go's
	// [treeRefused]). It says the two things that are true and nothing else: the
	// decision stands, and the work is still where the sentence after it names.
	//
	// IT IS NOT [yourCallLead]. That lead asks somebody a question, and the
	// whole of this landing is that the question has been answered and asking it
	// again would get the same refusal from the same disk (#513).
	keptWhereItIsLead = "taken as it stands, and it could not be brought home, so the work stays where it is — "
	// takenAsItStandsLead and takenAsItStandsTail are what an UNATTENDED run's
	// landing says when nobody could check the work
	// (task_run.go's workTaskNode). Between them goes the checker's own account
	// of what became of it — [checkerRanOut]'s sentence, most often — so the
	// whole reads "taken as it stands: nobody could check it in 5m0s, and the run
	// is unattended".
	//
	// THE TAIL IS THE HALF THAT MATTERS. A person coming back to this landing has
	// to be able to tell it from work somebody looked at, and the reason it was
	// taken rather than asked about is that there was nobody to ask.
	takenAsItStandsLead = "taken as it stands: "
	takenAsItStandsTail = ", and the run is unattended"
	// checkedOnTheSecondTry rides a verdict the FIRST call did not produce
	// ([auditVerdict.onTheSecondTry]). One call stalling and being abandoned
	// inside the window is not news a person needs a card about, and it is news
	// they should be able to find on the landing they are reading.
	checkedOnTheSecondTry = "checked on the second try"
	// taskCutMidCheck opens the node whose CHECK was cut off from outside — a
	// settle-kill, a quit, a deadline on the session. It is the one landing in
	// this file that is not a reading of the work at all, so it says only what is
	// knowable: the work stopped mid-check, nothing finished looking at it, and
	// what it left is on its branch. It borrows [incompleteLead] for
	// [unverifiedEdits]'s reason — nothing new happened to the work, and inventing
	// a state for it would be the machinery describing itself (task_run.go).
	taskCutMidCheck = incompleteLead +
		"it was stopped while its work was being checked, so nothing finished checking it — " +
		"what it wrote is on its branch"
)

// yourCallLead opens the report of a landing SOMEBODY HAS TO DECIDE, and it is
// the question that landing's own row will be asking — [taskAskOf]'s sentence,
// read off the projection rather than spelled a second time here
// (task_status.go). A report that opened with one question while the card beside
// it asked another would be two accounts of one landing, which is the whole
// defect the three tiers were drawn to end.
//
// `finished, but needs your look — ` WAS THIS LEAD AND IT IS DELETED as
// person-facing text (docs/design/task-states/DESIGN.md). It named a state no
// surface calls that any more, and it said the same sentence for a branch that
// would not merge as for work nobody could check.
//
// It takes the facts rather than a question because the caller is holding the
// facts and not the question: what came of the merge decides which of the six
// this landing is asking, and every road here already knows that much.
func yourCallLead(facts TaskFacts) string {
	return taskAskOf(facts).Reason + yourCallDash
}

// withYourCallLead puts that question in front of what is said under it, AND
// DOES NOT SAY IT TWICE.
//
// Three of the checker's own sentences already open with the question, because
// they were written when the lead was a different sentence entirely and each had
// to carry its own subject: [checkerRanOut] says "nobody could check it in 5m0s",
// [checkerAskedTwice] and [checkerWindowClosedAlone] both open "nobody could
// check it". Where the account already opens with the question, THE ACCOUNT IS
// THE LEAD — anything else is one sentence stuttering, which is what a lead and
// an account written a year apart will always eventually do.
func withYourCallLead(facts TaskFacts, said string) string {
	if reason := taskAskOf(facts).Reason; !strings.HasPrefix(said, reason) {
		return reason + yourCallDash + said
	}
	return said
}

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
	return v.withAlreadyRed(strings.Join(plainLines(v.evidence), "\n"))
}

// withAlreadyRed puts the repository's own old red under the checker's
// evidence on a finished landing. It calls the session's sentence rather than
// giving a task a second spelling of the same fact.
func (v auditVerdict) withAlreadyRed(report string) string {
	if len(v.alreadyRed) == 0 {
		return report
	}
	return withReport(report, alreadyRedSentence(v.alreadyRed))
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

// checkedSoFar is whatever the check had ALREADY SAID when something cut it off,
// in plain words, and "" when it had said nothing.
//
// It is not an outcome and it leads nothing: a cancelled check produced no
// finding, so this rides UNDER the node's own claim as evidence rather than over
// it as a verdict (task_run.go's cancel arm). A verdict nobody reached answers
// empty, which is the honest half of "nothing finished checking it".
func (v auditVerdict) checkedSoFar() string {
	if !v.answered {
		return ""
	}
	return strings.Join(plainLines(v.evidence), "\n")
}

// lookOutcome is what the node nobody could judge says: it FINISHED, and it
// needs eyes. The checker's own words follow, in plain form, because they are
// the whole basis on which somebody is being asked to decide.
func (v auditVerdict) lookOutcome(facts TaskFacts) string {
	lines := plainLines(v.evidence)
	if len(lines) == 0 {
		return yourCallLead(facts) + "the checker never answered"
	}
	return withYourCallLead(facts, strings.Join(lines, "\n"))
}

// takenAsItStands is [auditVerdict.lookOutcome]'s counterpart for a run with
// NOBODY WATCHING: the same non-answer, said as a decision rather than as a
// question (task_run.go's workTaskNode).
//
// THE CHECKER'S OWN FIRST LINE IS THE MIDDLE OF THE SENTENCE, which is what keeps
// the figure honest: "nobody could check it in 5m0s" is [checkerRanOut]'s own
// words, written where the window actually closed, and quoting them here rather
// than re-deriving a duration is what stops the landing and the journal drifting
// apart. Everything the checker said after that first line stands under it, as it
// does on every other landing.
func takenAsItStands(v auditVerdict) string {
	lines := plainLines(v.evidence)
	if len(lines) == 0 {
		// A non-answer with nothing behind it says what is true and invents no
		// reason for it — the emptiness law, on the one field there is.
		return v.withAlreadyRed(takenAsItStandsLead + "nobody could check it" + takenAsItStandsTail)
	}
	return v.withAlreadyRed(withReport(takenAsItStandsLead+lines[0]+takenAsItStandsTail, strings.Join(lines[1:], "\n")))
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
	// alreadyRed is the repository's own red that still stood when this answer
	// was reached. Only finished roads render it; a finding or a request for a
	// person's look keeps its attention on what is missing.
	alreadyRed []string
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

// checkerRanOut is what a person reads when the second look ran out of its
// window, and it is A FACT ABOUT THE CHECKER and nothing else.
//
// It used to read "no answer in 5m0s, so nothing was accepted", and every clause
// of that was doing damage. A person reading it takes the five minutes for THEIR
// five minutes — a window they were given and missed — when in truth nobody ever
// asked them anything; and "nothing was accepted" is a sentence about a
// decision, said by a clock that made none. A timer may expire only into a state
// that says UNANSWERED (pending.go states the law), so this says who could not
// answer and how long they had, and stops there. Whether the work is accepted is
// still entirely open, and still entirely the person's.
//
// AND IT IS SAID ONLY WHERE IT IS TRUE: no call stalled and the window ran out
// on its own — a window too small to bound a call in, or a pair of attempts that
// answered without hanging and left nothing behind them. A call that WAS asked
// and held its whole bound is said to have stalled, with the window's closing as
// a clause on the end of it ([checkerStalled], [auditVerdict.andTheWindowClosed]).
func checkerRanOut(window time.Duration) string {
	return "nobody could check it in " + window.String()
}

// checkerStalled is what a person reads when ONE call was abandoned and the
// check asked again inside the same window ([auditPace]).
//
// It is a fact about a call and never about the work, in the register
// [checkerRanOut] is held to, and it is usually the sentence NOBODY EVER SEES:
// the retry that follows it answers, and the verdict it reaches is what lands.
// What keeps it here is the run where the retry does not answer either, where
// this is the only account there is of where five minutes went.
func checkerStalled(bound time.Duration) string {
	return "one call ran " + bound.String() + " without answering and was abandoned"
}

// checkerWindowClosed is what a person reads BESIDE A CALL'S OWN ACCOUNT when
// the window closed on it — on the call that was cut by the window itself, and
// on a first attempt there was no time left to ask again after.
//
// It is a second clause rather than a second sentence, and it is never a
// replacement: the landing has to say both what was asked and why it was not
// asked again, and a line that only said the window closed would be the harness
// claiming nobody was asked when somebody was ([Agent.auditNode]).
const checkerWindowClosed = "the window closed before a second"

// checkerWindowClosedTail is how it joins the account it goes beside, in the
// separator every other run of evidence on a card already uses.
const checkerWindowClosedTail = " · " + checkerWindowClosed

// checkerWindowClosedAlone is the same fact with nothing beside it: one call
// was made, it left no account of itself, and there was no time to ask again.
// It carries the subject the tail borrows from the sentence it hangs off.
const checkerWindowClosedAlone = "nobody could check it — " + checkerWindowClosed + " call could be made"

// ── the checking window, and one call inside it ─────────────────────────────

// auditPace is ONE node's checking window and the bound on one call inside it.
//
// IT IS COMPUTED ONCE PER NODE AND SHARED BY EVERY ATTEMPT, which is the half of
// this that is not about hung streams. Each attempt used to open a window of its
// own, so the ladder's two attempts were bounded at ten minutes and the sentence
// a person read afterwards named five ([checkerRanOut]). One window, cut into
// shares, is what makes "nobody could check it in 5m0s" a true sentence.
//
// AND IT IS A CLOCK READING RATHER THAN A CONTEXT because [Agent.auditOnce]'s
// other question — was this node KILLED — is answered off the caller's own ctx,
// and a second context in the same variable would make the two indistinguishable.
type auditPace struct {
	// window is the whole of what this node's checking gets, and it is the figure
	// every sentence about running out quotes.
	window time.Duration
	// call is the bound on one attempt inside it ([auditCallShare]).
	call time.Duration
	// floor is the least a call may be given before it is not worth making at
	// all ([auditCallFloorShare]).
	floor time.Duration
	// until is when the window closes, read off the clock at the moment the
	// checking began.
	until time.Time
}

// newAuditPace opens one node's window.
func newAuditPace(window time.Duration, now time.Time) auditPace {
	return auditPace{
		window: window,
		call:   window / auditCallShare,
		floor:  window / auditCallFloorShare,
		until:  now.Add(window),
	}
}

// left is how much of the window is still there, and it goes negative once the
// window has closed.
func (p auditPace) left(now time.Time) time.Duration { return p.until.Sub(now) }

// bound is how long the NEXT call may take, READ AT THE MOMENT IT STARTS: its
// own share, or whatever is left of the window when that is less.
//
// The second answer says whether it is worth making at all. What is left when a
// retry begins has already had the first call, the first checker's close and the
// second one's build taken out of it, so this is asked with the clock in hand
// rather than from the share alone — and a call that would get less than
// [auditPace.floor] is not made, because it could only produce a stall it never
// had the time to avoid.
func (p auditPace) bound(now time.Time) (time.Duration, bool) {
	left := p.left(now)
	if left < p.floor {
		return 0, false
	}
	bound := p.call
	if left < bound {
		bound = left
	}
	// AND A BOUND OF NOTHING IS NOT A CALL. A window small enough for the shares
	// to divide down to zero — a test's window, a door that answered in
	// nanoseconds — would otherwise pass the floor above and submit a request on
	// a context that has already expired, and the person would then read that a
	// call stalled when no call was ever made.
	if bound <= 0 {
		return 0, false
	}
	return bound, true
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

// onTheSecondTry marks a verdict the FIRST call did not produce.
//
// THE LINE GOES UNDER THE EVIDENCE AND NEVER OVER IT. What leads a landing's
// report is what the work found and what it was checked on (task_ledger.go's
// [Agent.landFinished]); which try answered is a fact about the harness's
// evening and belongs last, where somebody looking for it can find it and
// nobody else has to read past it.
//
// AND IT SAYS IT ONCE. The ladder asks at most twice ([Agent.auditNode]), so
// this is reached at most once per node, and the copy is what keeps a verdict
// somebody else is holding from growing a line under them.
// andTheWindowClosed keeps what one attempt found and says that there was no
// time for another.
//
// IT GOES ON THE FIRST LINE rather than under it, which is what [onTheSecondTry]
// does and the difference between the two is who reads them. Which try answered
// is a fact about the harness's evening and belongs last; why the checking
// stopped where it did is part of the reason the work is being taken as it
// stands, and the one line that reason quotes is the first ([takenAsItStands]).
func (v auditVerdict) andTheWindowClosed() auditVerdict {
	if len(v.evidence) == 0 {
		// WITH NOTHING BESIDE IT, THE CLAUSE NEEDS ITS SUBJECT BACK. "the window
		// closed before a second" is a tail on a call's own account and reads as
		// half a sentence on its own — and half a sentence about time running out
		// is one a person takes for a deadline they missed ([checkerAskedTwice]
		// is the same repair on the same family).
		return noVerdict(checkerWindowClosedAlone, "")
	}
	evidence := append([]string{}, v.evidence...)
	evidence[0] += checkerWindowClosedTail
	v.evidence = evidence
	return v
}

func (v auditVerdict) onTheSecondTry() auditVerdict {
	v.evidence = append(append([]string{}, v.evidence...), checkedOnTheSecondTry)
	return v
}

// checkerAskedTwice is what a person reads when both calls were made and
// neither came back, and IT NAMES WHO COULD NOT ANSWER.
//
// It used to read "asked twice and got no answer either time", which has no
// subject in it at all — and a person reading an unattributed clause on their
// own card reads it as being about themselves: asked twice, by whom, and did I
// miss it? Nobody was asked anything. Two checking calls were made and neither
// said a word, which is [checkerRanOut]'s law applied to the one line in this
// family that never got it: say who could not answer, and stop there.
const checkerAskedTwice = "nobody could check it — asked twice, and neither call answered"

// twice re-tells a non-verdict as the SECOND one it is. Whether the harness
// asked once or asked again and got the same nothing is the difference between
// a blip and a checker that is not answering at all, and only the second is
// worth somebody's attention.
//
// IT IS SAID ONLY WHERE BOTH CALLS HAPPENED ([Agent.auditNode]). A window that
// closed before a second call could be made has its own clause and does not come
// through here ([checkerWindowClosed]), so this sentence never claims an attempt
// nobody made.
func (v auditVerdict) twice() auditVerdict {
	if len(v.evidence) == 0 {
		return noVerdict(checkerAskedTwice, "")
	}
	evidence := make([]string, len(v.evidence))
	copy(evidence, v.evidence)
	evidence[0] = checkerAskedTwice + " — " + evidence[0]
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
	claim = checkerConclusion(node, claim)
	// THE NODE'S PULSE SAYS WHICH OF ITS THREE LIVES THIS IS (task_beat.go), AND
	// SO DOES THE CARD ([EventTaskPhase]). A node under check is running —
	// nothing landed, nothing was undone — so a reader watching only the state
	// sees an unbroken "running" across a worker, a check and three repair
	// rounds; the phase is what tells those apart, and a check that ends in an
	// error still puts the word back.
	//
	// A check has no rounds and nothing to report yet, so it carries neither —
	// the emptiness law, one field at a time.
	defer a.enterPhase(node, taskBeatChecking, 0, 0, "")()

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
	//
	// AND IT STAGES WHAT THE NODE WROTE, not the whole directory: the auditor
	// judges exactly the change that would merge, so a virtualenv a test run left
	// behind is neither in the diff it reads nor on the branch it approves
	// (task_run.go's [stageTaskWork]).
	//
	// AND WHAT IS STAGED IS THE WHOLE LANDING, THE PARTS INCLUDED. A node that
	// handed work out merges a tree its own worker never wrote most of, and until
	// this line the check was pointed at one worker's files while the assembled
	// tree went home behind them (task_claims.go's [landingFiles]).
	files := landingFilesFor(node, changed)
	if tree.root != "" {
		stageTaskWork(tree.dir, files.all())
	}

	// AND THE VERDICT IS REACHED SOMEWHERE ELSE. The staged tree above is what the
	// node made AND everything the run left around it; what the auditor is put in
	// front of is a clean restore of the first half alone, so a check that leans on
	// the second half fails on its own (see the section on where a verdict is
	// reached). It is built ONCE, out here, for the same reason the staging is:
	// both attempts must judge one tree.
	ground := auditGroundFor(node, tree, files.all(), log)
	defer ground.drop()

	// ── THE LANDING'S CLAIMS ARE ITS CHECKLIST ──
	//
	// Read before anybody is paid to think, because the two shapes settled here
	// are settled by LOOKING and looking is free (task_claims.go). A landing that
	// says a string is gone while the string is still in the tree it would merge
	// has been answered already: the search is the whole of the evidence, the
	// finding names the sentence that is not so, and there is nothing a model
	// could add to it that the person would rather read.
	//
	// IT IS HUNTED IN THE GROUND AND NOT IN THE WORKING COPY, which is the same
	// argument the restore itself rests on: what a claim is about is what would
	// ship, never what the run happened to leave lying around it.
	checklist := checklistFor(ground.dir, files.all(), claim)
	if broken := checklist.broken(); len(broken) > 0 {
		answer := brokenClaimAnswer(broken)
		fmt.Fprintf(log, "check: the landing says something the work does not do — %s\n",
			strings.Join(answer.evidence, " · "))
		return answer
	}

	// AND THE DOOR IS READ OFF THE WORK, ONCE, FOR BOTH ATTEMPTS. What this audit
	// may run is the current verification contract's declared checks, including
	// declared family checks (task_checks.go); worker receipts grant no permission.
	// The baseline reads this same door. A retry that recomputed it could judge
	// the same tree through a different door, and "the same question asked again"
	// is the only thing a retry is allowed to be.
	door := auditDoorFor(node, ground.dir)
	checks := a.checkGroundFor(ctx, tree, ground, door, log)
	// AND THE WINDOW IS OPENED ONCE, HERE, FOR THE WHOLE OF THIS NODE'S CHECKING.
	// Both attempts below spend the same one ([auditPace]), so the figure a
	// landing quotes is the figure the checking actually had.
	pace := newAuditPace(a.auditWindowFor(door), time.Now())
	if len(door.checks) == 0 {
		fmt.Fprintf(log, "audit: nothing this work declares or ran is a re-runnable check — judging from reading, within %s\n",
			pace.window)
	}

	// A CHECK THAT CAME BACK HOLDING STILL OWES THE CLAIMS NOBODY SETTLED. That is
	// the other half of the law: a claim is a finding, or it holds, or it is said
	// out loud — never silently passed ([withOpenClaims]).
	open := checklist.open()
	verdict, again := a.auditOnce(ctx, node, tree, ground, pace, door, checks, files, claim, open, "", log)
	verdict.alreadyRed = checks.alreadyRed()
	switch {
	case verdict.answered, !again:
		return withOpenClaims(verdict, open)
	case ctx.Err() != nil:
		// The NODE was killed, not the audit. There is nobody to ask again and
		// nothing to ask about; the caller reads ctx itself and tells that story.
		return verdict
	}
	// AND A SECOND CALL THAT CANNOT BE MADE DOES NOT ERASE THE FIRST. What is
	// left of the window is asked HERE, before a fresh checker is built, because
	// what the first attempt found is the only account there is of where the time
	// went: a retry that answered `nobody could check it` in its place would tell
	// a person nobody was asked, when somebody was asked and abandoned.
	if _, worthAsking := pace.bound(time.Now()); !worthAsking {
		fmt.Fprintf(log, "audit: %s\n", checkerWindowClosed)
		return withOpenClaims(verdict.andTheWindowClosed(), open)
	}
	// AND THE SECOND ASK GOES SOMEWHERE ELSE WHERE THERE IS SOMEWHERE ELSE. A
	// model that read the diff, ran the verification and then said neither word
	// is a model this question does not fit, and a fresh checker on the same one
	// is the same question put to the same weights — which is what a person
	// reading `nobody could check it` had paid for twice. So the retry rides the
	// adapter's own fallback chain, exactly as a conversation's turn and a node's
	// run do when a lane stops answering ([Agent.failoverCheckerModel]), and it
	// is the ONE line the journal owes about it.
	elsewhere, moved := a.failoverCheckerModel(a.auditorModel())
	if moved {
		fmt.Fprintf(log, "audit: no verdict — asking %s instead\n", elsewhere)
	} else {
		fmt.Fprintf(log, "audit: no verdict — asking a fresh auditor\n")
	}
	retried, _ := a.auditOnce(ctx, node, tree, ground, pace, door, checks, files, claim, open, elsewhere, log)
	retried.alreadyRed = checks.alreadyRed()
	if retried.answered {
		// AND THE LANDING SAYS WHICH TRY ANSWERED. A verdict the first call did
		// not produce is the same verdict — nothing about the work is different —
		// but a person reading a card wants to know that the first call was
		// abandoned rather than skipped ([checkedOnTheSecondTry]).
		return withOpenClaims(retried.onTheSecondTry(), open)
	}
	return retried.twice()
}

// auditOnce is one attempt: a fresh auditor in the node's worktree, one
// question, one reading of what came back.
//
// The second return says whether ASKING AGAIN COULD HELP. A reply with no
// verdict in it and a provider that errored are both worth one more call — the
// first is a model that wandered, the second is a network — while an audit whose
// WINDOW has closed is not: the checking already had every minute it was going to
// get, and there is nothing left to ask with.
//
// AND A CALL THAT HUNG IS WORTH ONE MORE TOO, which is the rung that did not
// exist. A stalled stream is abandoned at its own share of the window
// ([auditPace]) and the check is asked again inside what is left — where before
// it, one hung call spent the whole five minutes and the node landed on a
// sentence claiming nobody could check it (#513).
func (a *Agent) auditOnce(ctx context.Context, node *TaskNode, tree taskTree, ground auditGround, pace auditPace, door auditDoor, checks checkGround, files landingFiles, claim string, open []claimFinding, on string, log io.Writer) (auditVerdict, bool) {
	auditor, err := a.newAuditAgent(ground.dir, node, door, on)
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
	// audit on the same beat it ends everything else, and the window is a bound
	// on the verdict rather than a second life for a task that has already been
	// stopped.
	//
	// AND THE BOUND IS THIS CALL'S SHARE OF THE NODE'S WINDOW, never the window
	// itself ([auditPace]). The window's own length is the door's — a check with
	// something to run gets the time a run takes, one whose only remaining move is
	// a refused command gets the time reading takes ([auditDoor.window]) — and
	// what is decided here is only how much of it one call may hold.
	//
	// A WINDOW WITH TOO LITTLE LEFT IN IT ASKS NOBODY ANYTHING. It is read HERE,
	// with the first checker closed and this one already built, so the bound is
	// measured against the time this call actually has; a retry that would get
	// less than the floor is not made at all, and what the landing then says is
	// that the window closed rather than that a call it never made stalled.
	bound, worthAsking := pace.bound(time.Now())
	if !worthAsking {
		return noVerdict(checkerRanOut(pace.window), ""), false
	}
	auditCtx, done := context.WithTimeout(ctx, bound)
	defer done()

	fmt.Fprintf(log, "audit: verifying against the acceptance\n")
	events, err := auditor.Submit(auditCtx, auditQuestion(node, tree, ground, door, checks, files, claim, open))
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
	// out of its own window with the node still perfectly alive.
	if auditCtx.Err() != nil && ctx.Err() == nil {
		// AND THE TWO CLOCKS ARE TOLD APART, because they decide different things.
		// A call cut while the window still has room leaves something to ask
		// with, and the check is asked again inside what is left; a window that
		// has closed is the whole of this node's checking over.
		//
		// WHAT NEITHER OF THEM CHANGES IS THE SENTENCE. A CALL THAT STALLED IS
		// SAID TO HAVE STALLED, whichever clock ran out second: this one was
		// asked, held the stream for its whole bound and answered nothing, and
		// "nobody could check it in 5m0s" over the top of that is the harness
		// telling a person nobody was asked. So the window's closing is a clause
		// on the end of the call's own account, exactly as it is one rung up
		// ([auditVerdict.andTheWindowClosed]), and [checkerRanOut] is left for
		// the case it is true of: no call stalled, and the window simply ran out.
		stalled := noVerdict(checkerStalled(bound), said)
		if pace.left(time.Now()) > 0 {
			fmt.Fprintf(log, "audit: %s\n", checkerStalled(bound))
			return stalled, true
		}
		fmt.Fprintf(log, "audit: %s%s\n", checkerStalled(bound), checkerWindowClosedTail)
		return stalled.andTheWindowClosed(), false
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
// It runs inside the CALL'S own bound rather than opening a window of its own:
// the auditor has already done the reading, and a nudge that could outlive the
// deadline would be a second audit wearing a cheap name. That bound is a share
// of the node's window ([auditPace]) rather than the whole of it, which is what
// leaves the fresh checker after it something to be asked with.
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
	// AND THE CHECK'S OWN ANSWER IS WRITTEN ON THE NODE, beside the claim and for
	// the same reason: this is the only moment anybody holds it. By the time the
	// node settles the whole of the gate is a paragraph of prose in a report, and
	// what the ratings store needs is the answer itself — the grade a settled
	// node teaches, at no extra call (taskgrade.go).
	defer func() { node.keepClaim(out.claim); node.checkSaid(auditGrade(out.verdict), len(out.gaps)) }()
	rounds := a.config.TaskRepairRounds
	for round := 1; ; round++ {
		out.verdict = a.auditNode(ctx, node, tree, out.changed, out.claim, log)
		if !out.verdict.answered || out.verdict.verified {
			// Nothing to repair: either the work holds, or nobody said anything
			// about it — and a gap nobody named is not a gap a worker can close.
			//
			// A CHECK THAT PASSED IS ALSO WHERE A LIFT IS HANDED BACK. It is the
			// one moment that says the stronger tier has stopped buying anything,
			// and until the response boundary existed nothing looked for it — so
			// every lift this build ever bought was permanent
			// (taxonomy_boundary.go's [Agent.readPass]).
			if out.verdict.verified {
				a.readPass(node, log)
			} else {
				a.tallyFor(node).Round()
			}
			return out
		}
		// A finding is kept the moment it is made, whether or not there is a round
		// left to spend on it: the report owes the person the evidence of every
		// round, and the last one is the one that lands the node.
		out.gaps = append(out.gaps, out.verdict.evidence)
		// AND THE FINDING GOES TO THE BOUNDARY BEFORE ANYTHING IS BOUGHT WITH IT.
		// A refutation of a round whose calls died on the wire is not evidence
		// about the model; K of them with the wire ruled out is, and past the cap
		// the answer is the work's own (internal/taxonomy). The verdict decides
		// which model the round below runs on and whether there is a round at all.
		lift := a.readFinding(node, log)
		if lift.Class == taxonomy.Work {
			return out
		}
		if round > rounds || ctx.Err() != nil {
			return out
		}
		fmt.Fprintf(log, "repair %d of %d: sent back — %s\n",
			round, rounds, strings.Join(out.verdict.evidence, " · "))
		repaired, said := a.repairNode(ctx, node, tree, out.verdict, out.changed, round, lift, log)
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
// AND IT IS WHERE THE EXPENSIVE MODEL IS BOUGHT — WHEN THE BOUNDARY SAYS SO.
// The worker below resolves its model through [roleRepair], which sits on the
// high tier, and repair_role.go carries that argument. What decides whether the
// tier is bought at all is the `lift` verdict this is handed: a finding with
// four dead calls under it buys nothing, because nothing about who served a
// request is evidence about who was asked (taxonomy_boundary.go). A HOLD still
// runs the round — same worktree, same gaps, fresh worker — just not at the
// careful tier's price.
//
// It never returns an error. A repair round that could not start, or that hit a
// threshold, or that wrote nothing, is not a failure of the node: it is a round
// that closed no gaps, and the auditor that follows will say so in evidence a
// person can read.
func (a *Agent) repairNode(ctx context.Context, node *TaskNode, tree taskTree, verdict auditVerdict, changed []string, round int, lift taxonomy.Verdict, log io.Writer) ([]string, string) {
	// THE SURFACE HEARS "STILL WORKING", AND IT HEARS WHAT IS BEING CLOSED. The
	// node never left TaskRunning — nothing landed, nothing was undone — so what
	// goes out is an ordinary running update with the gap on it, and the machinery
	// that sent the work back is not on the wire (task_contract.go's Mending).
	node.mending(mendingLine(verdict.evidence))
	defer node.mending("")
	// AND THE PULSE AND THE CARD SAY WHICH ROUND THIS IS. A repair round is the
	// node still working, and a reader outside the process is owed the same
	// distinction the card gets (task_beat.go, [EventTaskPhase]) — with the two
	// facts a person watching a second minute of it actually wants: how far
	// through the rounds this is, and what the check said that sent it back.
	defer a.enterPhase(node, taskBeatRepairing, round, a.config.TaskRepairRounds, taskFindingLine(verdict.evidence))()

	child, err := a.newTaskAgentOn(ctx, taskGroundDir(node, tree), node, fmt.Sprintf("-repair%d", round), a.repairTierModel(node, lift))
	if err != nil {
		fmt.Fprintf(log, "repair %d: could not start a worker: %v\n", round, err)
		return nil, ""
	}
	defer func() {
		_ = child.Close()
		a.foldTaskUsage(node, child)
		// AND WHAT A LIFTED ROUND COST GOES AGAINST THE CAP. It is ignored while
		// nothing is lifted, so the ordinary price of the work never counts
		// toward a ceiling on the lift (taxonomy_boundary.go's [Agent.billLift]).
		a.billLift(node, child)
	}()
	// WHERE THE MONEY WENT, written off the worker that actually exists rather
	// than off the id the cascade asked for, and only when the ladder really did
	// move ([TaskNode.repairedOn]). The job log says it in words for whoever is
	// reading a run happen; the project's index says it as a field, which is what
	// a bench can add up afterwards.
	if on := child.Model(); node.repairedOn(on) {
		fmt.Fprintf(log, "repair %d: on %s\n", round, on)
	}

	// The room follows the work: somebody watching this node came to watch the
	// node, and a repair round is the node still working (task_room.go). It is
	// handed BACK when the round ends, because the round's worker is closed on the
	// way out and a room pointing at a closed agent would refuse a line somebody
	// typed while the node is still perfectly alive.
	room := node.openRoom()
	spoke := room.speaker()
	room.speaking(child)
	defer room.speaking(spoke)

	wrote, stopped, runErr := runTaskChild(ctx, child, node, repairInstruction(node, tree, verdict, changed), tree.dir, a.taskLimits(node), room, log)
	// AND THE ROUND'S OWN CHECK REPLACES THE LAST ONE'S. The auditor that judges
	// after this round is judging the tree this worker left, so the evidence it is
	// shown has to be this worker's ([lastToolReceipts]). It is taken before the
	// deferred Close above runs, which is the only moment the transcript exists.
	node.keepReceipts(lastToolReceipts(child, auditReceiptCount))
	switch {
	case stopped != "":
		fmt.Fprintf(log, "repair %d: %s\n", round, stopped)
	case runErr != nil:
		fmt.Fprintf(log, "repair %d: ended with an error: %v\n", round, runErr)
	}
	// This round's answer replaces the last one's: a repair round is the node
	// working again, so what it produced is what the node produced. It is kept
	// here for the receipts' reason — the transcript closes on the way out
	// (task_result.go).
	said := lastSaid(child)
	node.keepWorkerConclusion(said, log)
	return wrote, firstLines(said, taskReportLines)
}

// repairInstruction is what the repairing worker is asked.
//
// It is the node's OWN instruction — the same assembled brief, the same frozen
// acceptance, read from the same fields the first run read (task_run.go's
// [TaskNode.instruction]) — with three things added: what is already in the
// working copy, the gaps VERBATIM, and the sentence that the work stands.
//
// THE ORDER IS ORIENTATION, THEN FINDING, THEN RULE. A worker reads the job, then
// where the job already got to, then what is wrong with it, then what it may
// touch — which is the order somebody handing work back across a desk would say
// it in. The gaps sit next to the rule that bounds them on purpose: they are the
// only two sentences in this document about THIS round.
//
// THE EVIDENCE IS NOT PARAPHRASED. It goes in exactly as the auditor wrote it,
// because it is the most precise description of what is missing that exists
// anywhere in this system, and a harness that summarized it would be a harness
// deciding which half of the finding the worker gets to see. (The plain-words
// law is about what a PERSON reads; a worker being told what to fix is machinery
// talking to machinery, and the heading calls it a review because that is what
// it is.)
func repairInstruction(node *TaskNode, tree taskTree, verdict auditVerdict, changed []string) string {
	var out strings.Builder
	// BOUND TO THE COPY, like the opening request was. A repair round is the
	// second and last moment a worker is spoken to, and a worker handed the
	// parent's addresses on round two would be sent back out at the person's
	// own checkout after round one had it right (task_run.go's
	// [TaskNode.instructionOn]).
	out.WriteString(node.instructionOn(tree))
	if ground := repairGround(tree, changed); ground != "" {
		out.WriteString("\n\n" + repairSawHeading + "\n" + ground)
	}
	out.WriteString("\n\n" + repairHeading + "\n")
	for _, line := range verdict.evidence {
		out.WriteString(line + "\n")
	}
	out.WriteString("\n" + repairStands)
	return out.String()
}

// repairGround is the change as it stands: what the work has written, how much
// of each file moved, and where the whole diff can be read in one command.
//
// IT IS BOUNDED BY THE BRIEF'S OWN LIMIT and not by a figure of its own. A worker
// opens on one document, [briefAskLimit] is what that document already holds a
// verbatim section to, and the reason is the same in both places: a paragraph is
// the ordinary case and the bound is for the other one — a node that rewrote nine
// hundred files, whose `--stat` would otherwise be the whole prompt. A second
// constant here would be a second answer to one question, which is the drift
// CLAUDE.md's one-source-of-truth law names.
func repairGround(tree taskTree, changed []string) string {
	var out strings.Builder
	if len(changed) > 0 {
		out.WriteString("Files the work has written so far: " + strings.Join(changed, ", ") + "\n\n")
	}
	if tree.root == "" {
		out.WriteString(repairNoDiff)
		return clip(strings.TrimSpace(out.String()), briefAskLimit)
	}
	out.WriteString(repairDiffPointer)
	if stat := stagedDiffStat(tree.dir); stat != "" {
		out.WriteString("\n\n" + stat)
	}
	return clip(strings.TrimSpace(out.String()), briefAskLimit)
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

// keepWorkerConclusion replaces the current attempt's answer, including an
// empty answer. Keeping an earlier success when a newer worker says nothing
// would give the checker a claim about the previous attempt's work.
func (n *TaskNode) keepWorkerConclusion(said string, log io.Writer) {
	if strings.TrimSpace(said) != "" {
		n.keepResultNoting(said, log)
		return
	}
	n.graph.mu.Lock()
	n.produced = taskResult{}
	n.graph.mu.Unlock()
}

// checkerConclusion gives every checking path the worker's kept answer instead
// of its display summary. Rechecking does not inherit an earlier checker's
// decision, and older nodes without a kept result still use their supplied claim.
func checkerConclusion(node *TaskNode, fallback string) string {
	kept := node.result()
	if strings.TrimSpace(kept.text) == "" {
		return fallback
	}
	return resultBlock(resultDelivery{
		body: kept.text, where: kept.whereWhole(), cut: kept.bytes > len(kept.text),
	})
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
//
// AND IT NAMES THE DOOR. The auditor's bash will run the checks this work
// declares or ran and nothing else (task_checks.go), so the packet says which
// those are — or says plainly that there are none and that reading is the whole
// of the job. A model that has not been told where the door is spends its
// window looking for one, which is exactly what was measured.
func auditQuestion(node *TaskNode, tree taskTree, ground auditGround, door auditDoor, checks checkGround, files landingFiles, claim string, open []claimFinding) string {
	var out strings.Builder
	out.WriteString("The work: " + node.title() + "\n\n")
	out.WriteString("ACCEPTANCE (this is the contract; judge against this and nothing else):\n")
	out.WriteString(node.acceptance() + "\n\n")
	// AND WHERE THE PERSON MOVED IT, THE PACKET SAYS SO AND SAYS WHICH ONE WINS.
	// A revised task has two done-conditions in its history and exactly one it is
	// judged by (assignment.go); an auditor handed both without being told that
	// would look for work satisfying a requirement the person themselves withdrew.
	// Their own words are here because the acceptance above is a restatement of
	// them, and a restatement that has drifted is a thing a reader can only catch
	// with the original beside it.
	if revised := node.revisionNote(); revised != "" {
		out.WriteString(revised + "\n\n")
	}

	if claim = strings.TrimSpace(claim); claim != "" {
		out.WriteString("What it CLAIMS it did — this is the claim under audit, not evidence:\n")
		out.WriteString(claim + "\n\n")
	}
	if len(files.own) > 0 {
		out.WriteString("Files it wrote: " + strings.Join(files.own, ", ") + "\n")
	}
	// AND A DIVIDER'S PARTS ARE NAMED AS PARTS. The assembled tree is what this
	// node's landing is, so the parts' files are in front of the checker with the
	// rest of it — said in a second sentence rather than folded into the first,
	// because "it wrote" would be a claim about this node that is not true
	// (task_claims.go's [landingFiles]).
	if files.divided() {
		out.WriteString("And the parts it handed out wrote, into the same tree: " +
			strings.Join(files.parts, ", ") + "\n")
	}
	if len(files.own) > 0 || files.divided() {
		out.WriteString("\n")
	}
	out.WriteString(claimsBlock(open))
	out.WriteString(auditReceiptBlock(node.lastReceipts(), ground.restored))

	// The current directory and the worker's old addresses are different facts.
	// Receipt paths name where evidence was produced, never where to check now.
	out.WriteString("CHECK THESE FILES HERE: " + ground.dir + "\n")
	out.WriteString("You are already in this directory. Paths in worker receipts name its earlier copy; use this directory for the files under review.\n")
	if ground.restored {
		out.WriteString("WHERE YOU ARE: a CLEAN RESTORE of the selected work. It starts from the restored repository or folder and overlays the selected files. Committed work may already be in that starting state.\n")
	} else {
		out.WriteString("WHERE YOU ARE: the working copy where the work was done. Files or dependencies left by the worker may still be present; distinguish those from the files this task produced.\n")
	}
	if tree.root != "" {
		out.WriteString("`git diff --cached` shows the currently staged delta, which may be empty when the work is already committed. An empty delta does not say the requested work is absent or complete; read the relevant files.\n")
		if tree.checkBase != "" {
			out.WriteString("The task's starting commit is " + tree.checkBase + "; compare against it when checking changes that are already committed.\n")
		}
	} else {
		out.WriteString("This workspace is not a repository, so there is no diff to read: check the files themselves.\n")
	}
	// AND THE GROUND'S OWN MANIFEST UNDER IT (taskmanifest.go). The sentence
	// above is about the diff, and a diff is a view of the TRACKED half of a
	// tree: the run this was written after turned on files nothing had added, so
	// no diff showed them and the reading judged the whole against the parts that
	// happened to be tracked. The manifest is what is staged, what is changed and
	// not staged, and what is not tracked at all — read from the ground the
	// reader is standing in, and drawing nothing at all where that ground is not
	// a repository.
	if manifest := groundManifest(ground.dir); manifest != "" {
		out.WriteString("\n" + manifest)
	}
	out.WriteString(checkGroundBlock(checks))
	out.WriteString("\n" + door.line())
	if len(door.checks) == 0 {
		out.WriteString("\nRead the change. Then give your verdict.")
	} else {
		out.WriteString("\nRun the verification. Read the change. Then give your verdict.")
	}
	return out.String()
}

// auditReceiptBlock is WHAT THE WORK ALREADY RAN, as the auditor reads it.
//
// THE POINT OF IT IS THE RE-RUN THE AUDITOR DOES NOT HAVE TO DO. An auditor that
// must rediscover the repository's check from scratch, and then run it twice
// because the first attempt told it nothing, spends five minutes and lands the
// node unchecked — which is what was measured. Seeing what the last worker ran
// and what came back tells it which command the check even is, and answers the
// cheap half of the question outright.
//
// AND THE FRAMING IS THE SAFETY. In a restore these results came from ANOTHER
// tree — the work's own, the one carrying everything a verdict may not rest on —
// so they may settle a refutation and never an acceptance. Standing in the work's
// own copy they came from this tree, and the warning that matters is the opposite
// one. Both sentences are written here rather than left to the auditor to work
// out, because a judge reasoning about the provenance of its own evidence is a
// judge that will get it wrong once.
func auditReceiptBlock(receipts []toolReceipt, restored bool) string {
	if len(receipts) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("WHAT THE WORK ALREADY RAN — its last tool results, verbatim and cut. This is a record of what " +
		"came back, not something the work said about itself.\n")
	if restored {
		out.WriteString("They were produced in the work's OWN copy, which is not the copy you are standing in. " +
			"Read them to learn WHICH command the check is and what it said there. A result showing the check " +
			"failing, or showing that no check was ever run, is enough on its own — you do not have to run it again " +
			"to believe it. A result showing it PASSING is the work's account of its own copy: a long check whose " +
			"result is here and is recent does not have to be repeated just to be seen again, but anything that " +
			"could pass there and fail here has to be settled here before you accept it.\n")
	} else {
		out.WriteString("They came from this same copy. A long check whose result is here and is recent does not " +
			"have to be run again just to be seen again; run it yourself when the result is stale, thin, or does not " +
			"match what you can see in the files.\n")
	}
	for _, receipt := range receipts {
		name := strings.TrimSpace(receipt.tool + " " + receipt.args)
		if name == "" {
			name = "a call"
		}
		out.WriteString("\n$ " + name + "\n" + receipt.result + "\n")
	}
	out.WriteString("\n")
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

// ── WHERE THE VERDICT IS REACHED, AND ON WHAT ───────────────────────────────
//
// A PASSING CHECK MAY NOT DEPEND ON STATE THE DELIVERABLE DOES NOT CARRY. That
// is the law, and until this section existed nothing enforced it.
//
// What it cost was measured. A node was asked to make a language server pass a
// scorer; the scorer addressed files as `file:///workspace/test-files/…` and the
// repository kept them somewhere else. The node worked out exactly why the
// scorer failed and then fixed the WORLD instead of the WORK — `ln -sf
// /workspace/java /workspace/test-files` — re-ran the scorer against its own
// symlink, watched it pass, and reported the job done. The auditor then ran in
// the same directory, with the same symlink under it, and saw the same pass. The
// deliverable was a repository that fails the moment it leaves that machine, and
// every party that looked at it agreed it was finished.
//
// SO THE AUDIT DOES NOT JUDGE WHERE THE WORK HAPPENED. It judges a CLEAN
// RESTORE: an untouched copy of the repository with exactly the files the node
// wrote laid over it, and nothing else the run left behind. An environment-only
// fix is absent there and the check fails on its own, with no rule anybody had
// to remember and no prompt anybody had to obey.
//
// THE RESTORE IS BUILT FROM WHAT THE HARNESS ALREADY KNOWS THE NODE WROTE —
// `changed`, the same list [stageTaskWork] stages, [commitTaskWork] commits and
// the card names (task_run.go). There is no second record of the deliverable and
// there must not be one: two lists would be two answers to "what ships".
//
// AND THE BUILD PRODUCTS ARE NOT COPIED. A restore carrying the node's `target/`
// would be carrying the very thing it exists to leave behind. The auditor
// installs and builds in the restore itself — that is what its allowlist is for,
// and what its five minutes are spent on.

// auditGround is the directory an auditor is put in, and whether that directory
// is a restore or the node's own working copy.
//
// The fallback is not a failure: a workspace with no repository behind it and no
// record of when the work started cannot be reconstructed, and a verdict reached
// where the work happened is the verdict this build has always reached. What the
// fallback must never do is claim to be a restore, because the sentence the
// auditor is given about where it stands is the one thing it cannot check.
type auditGround struct {
	// dir is where the auditor stands.
	dir string
	// restored says dir really is a clean restore of what would ship.
	restored bool
	// why says, in the log's words, why it is not. Empty when it is.
	why string
	// drop takes the restore away again. It is never nil.
	drop func()
}

// auditGroundFor builds the ground one audit is reached on, and says in the job
// log which of the two it got. Both attempts of one audit share it, exactly as
// they share the staged tree: a retry that rebuilt the restore would be a second
// evidence packet, and "the same question asked again" is all a retry may be.
func auditGroundFor(node *TaskNode, tree taskTree, changed []string, log io.Writer) auditGround {
	ground, why := restoreTaskWork(node, tree, changed)
	if !ground.restored {
		fmt.Fprintf(log, "audit: judging in the node's own working copy — %s\n", why)
		return auditGround{dir: tree.dir, why: why, drop: func() {}}
	}
	fmt.Fprintf(log, "audit: judging a clean restore of what the work wrote, in %s\n", ground.dir)
	return ground
}

// restoreTaskWork makes the clean restore, by the one road that is available.
//
// A REPOSITORY IS RESTORED FROM ITSELF. The node's branch still points at the
// commit its worktree was cut from — the node's work is staged in its own index
// and not committed until it lands (task_run.go's [taskTree.comeHome]) — so a
// detached checkout of that branch IS the untouched tree, and laying the written
// files over it is the whole restore. That is exact, and it is cheap.
//
// A WORKSPACE WITH NO REPOSITORY HAS TO BE COPIED, and there is no record of
// what it held before the run except the clock: everything that predates the
// node's start is the original tree, everything younger that the node did not
// write is what the run left behind. That is a reading of mtimes and it is
// stated as one — it is the only ground truth a directory keeps about itself.
// AND THE RESTORE IS CUT FROM THE GROUND, which is the whole of what made two
// finished tasks read as incomplete (issue #76). The check stands where the work
// stood: the repository the work was ABOUT, not whichever one the conversation
// happened to be opened in. For a worktree that is the same sentence twice — the
// branch was cut from the ground, so `tree.root` IS the ground — and for every
// other mode it is the road below, which is the reason the modes carry their
// ground at all.
func restoreTaskWork(node *TaskNode, tree taskTree, wrote []string) (auditGround, string) {
	if strings.TrimSpace(tree.dir) == "" {
		return auditGround{}, "there is no working copy to restore"
	}
	if tree.root != "" && strings.TrimSpace(tree.branch) != "" {
		return restoreFromBranch(tree, wrote)
	}
	if ground := strings.TrimSpace(tree.ground); ground != "" && ground != tree.dir {
		// A MIRRORED FOLDER IS RESTORED FROM THE FOLDER ITSELF. The node worked in
		// a copy, so the original is sitting there untouched — which is a better
		// answer than any reading of timestamps, and the only one available for a
		// tree whose every file was copied in after the clock started
		// ([copyOriginal] would have called the whole mirror "left behind").
		if _, isRepo := repositoryRoot(ground); !isRepo {
			return restoreFromFolder(tree, wrote)
		}
	}
	if ground := strings.TrimSpace(tree.ground); ground != "" {
		if root, isRepo := repositoryRoot(ground); isRepo && hasCommit(root) {
			return restoreFromGround(root, tree, wrote)
		}
	}
	return restoreByCopy(node, tree, wrote)
}

// restoreFromGround is the road for work that has no branch of its own but
// stands on a repository anyway: the person's own "here", or a task working
// beside a repository it reads.
//
// It is [restoreFromBranch]'s argument with the one word changed. A detached
// checkout of the GROUND'S HEAD is the tree as it was before the work, and what
// the node wrote laid over it is the restore. The person's uncommitted changes
// are not in it, which is exactly right: they are not part of what ships either.
func restoreFromGround(root string, tree taskTree, wrote []string) (auditGround, string) {
	holder, err := os.MkdirTemp("", "aforge-check-")
	if err != nil {
		return auditGround{}, "a fresh checkout could not be made: " + err.Error()
	}
	dir := filepath.Join(holder, "check")
	drop, problem := detachedWorktree(tree.place, root, root, dir, "HEAD")
	remove := func() {
		drop()
		_ = os.RemoveAll(holder)
	}
	if problem != "" {
		_ = os.RemoveAll(holder)
		return auditGround{}, "a fresh checkout could not be made: " + problem
	}
	if problem := layWork(tree.dir, dir, wrote); problem != "" {
		remove()
		return auditGround{}, problem
	}
	// A CHECKOUT THE WORK CANNOT BE STAGED INTO IS NOT A RESTORE. The sentence the
	// checker is handed says its changes are staged, and an unstaged tree makes a
	// change of new files read as an empty diff — which is a refusal of good work
	// for a reason that has nothing to do with the work (task_run.go's
	// [stageTaskWork]). Falling back to the tree the node worked in says so in the
	// job log instead.
	if problem, _ := stageTaskWork(dir, wrote); problem != "" {
		remove()
		return auditGround{}, "the work could not be staged in a clean copy: " + problem
	}
	return auditGround{dir: dir, restored: true, drop: remove}, ""
}

// restoreFromFolder is the mirror's road: a fresh copy of the ground folder,
// which the node never touched, with what the node wrote laid over it.
func restoreFromFolder(tree taskTree, wrote []string) (auditGround, string) {
	dir, err := os.MkdirTemp("", "aforge-check-")
	if err != nil {
		return auditGround{}, "a clean copy could not be made: " + err.Error()
	}
	remove := func() { _ = os.RemoveAll(dir) }
	if problem := mirrorGround(tree.ground, dir); problem != "" {
		remove()
		return auditGround{}, problem
	}
	if problem := layWork(tree.dir, dir, wrote); problem != "" {
		remove()
		return auditGround{}, problem
	}
	return auditGround{dir: dir, restored: true, drop: remove}, ""
}

// restoreFromBranch is the repository road: a detached checkout of the node's
// own branch, with what the node wrote laid over it and staged.
//
// IT IS STAGED FOR THE REASON THE NODE'S OWN TREE IS ([stageTaskWork]): `git
// diff` shows tracked files only, so a change made of new files reads as an
// empty diff, and the sentence the auditor is given — "its changes are staged,
// so `git diff --cached` shows all of them" — has to be true of the tree it is
// actually standing in.
//
// THE CHECKOUT IS CUT FROM WHICHEVER REPOSITORY HOLDS THE BRANCH, which is the
// ground for a node working in a worktree of it and the node's own copy for one
// working in a universe (groundladder.go's [taskTree.branchHolder]). A branch is
// only reachable where it was cut until the landing carries it home, and asking
// the ground for it before then is asking for a name it has never heard.
func restoreFromBranch(tree taskTree, wrote []string) (auditGround, string) {
	dir := tree.dir + "-check"
	holder := tree.branchHolder()
	// THE CHECKOUT IS CUT FROM THE BRANCH THE WORK IS ACTUALLY ON. A node may
	// rename the branch it is standing on — one renamed its own task branch to
	// the name the brief asked for — and the tree's record then names a ref this
	// repository has never heard of, which came back as `fatal: invalid
	// reference` and dropped every check into the node's own copy (#653). The
	// recorded branch stays authoritative while it exists; otherwise the live
	// checkout names its replacement, with the release record as the last
	// observation available after the copy was given back.
	ref := tree.branch
	if !branchIsThere(holder, ref) {
		if live := currentBranch(tree.dir); branchIsThere(holder, live) {
			ref = live
		} else if mark, released := rememberedRelease(tree.dir); released && branchIsThere(holder, mark.Branch) {
			ref = mark.Branch
		}
	}
	remove, problem := detachedWorktree(tree.place, tree.root, holder, dir, ref)
	if problem != "" {
		return auditGround{}, "a fresh checkout could not be made: " + problem
	}
	if problem := layWork(tree.dir, dir, wrote); problem != "" {
		remove()
		return auditGround{}, problem
	}
	// A CHECKOUT THE WORK CANNOT BE STAGED INTO IS NOT A RESTORE. The sentence the
	// checker is handed says its changes are staged, and an unstaged tree makes a
	// change of new files read as an empty diff — which is a refusal of good work
	// for a reason that has nothing to do with the work (task_run.go's
	// [stageTaskWork]). Falling back to the tree the node worked in says so in the
	// job log instead.
	if problem, _ := stageTaskWork(dir, wrote); problem != "" {
		remove()
		return auditGround{}, "the work could not be staged in a clean copy: " + problem
	}
	return auditGround{dir: dir, restored: true, drop: remove}, ""
}

// restoreByCopy is the road for a workspace that is not a repository: the
// original tree copied out by its own timestamps, with what the node wrote laid
// over it.
//
// IT IS BOUNDED, AND THE BOUND MATTERS MORE THAN THE COPY. A restore that spent
// four of the audit's five minutes copying a built tree would have turned a gate
// into a timeout, which is the other defect this wave is fixing — so past
// [auditRestoreEntries] the copy stops and the audit falls back to where it has
// always run, with the reason in the job log.
func restoreByCopy(node *TaskNode, tree taskTree, wrote []string) (auditGround, string) {
	started := node.startedAt()
	if started.IsZero() {
		return auditGround{}, "nothing records when the work began, so the tree it started from cannot be told from what it left behind"
	}
	dir, err := os.MkdirTemp("", "aforge-check-")
	if err != nil {
		return auditGround{}, "a clean copy could not be made: " + err.Error()
	}
	remove := func() { _ = os.RemoveAll(dir) }
	if problem := copyOriginal(tree.dir, dir, wrote, started); problem != "" {
		remove()
		return auditGround{}, problem
	}
	if problem := layWork(tree.dir, dir, wrote); problem != "" {
		remove()
		return auditGround{}, problem
	}
	return auditGround{dir: dir, restored: true, drop: remove}, ""
}

// layWork puts the deliverable over the restored tree: every path the node
// wrote, copied from the node's working copy, and every path it wrote and then
// DELETED taken away again.
//
// IT IS ALL OF THE LEDGER OR NONE OF IT, and every file arrives whole. What is
// being written over on a folder ground is the person's own folder, so the whole
// ledger is staged beside its targets before anything moves and each staged path
// is renamed into place (task_lay.go says why in full).
func layWork(from, to string, wrote []string) string {
	lay, problem := stageLay(from, to, wrote)
	if problem == "" {
		problem = lay.commit()
	}
	if problem != "" {
		lay.abandon()
		return problem
	}
	return ""
}

// copyOriginal copies out the tree AS IT WAS WHEN THE WORK BEGAN.
//
// The rule is one sentence: an entry older than the node's start is the original
// and is copied; an entry younger than it appeared while the work ran and is
// left out, because what the node actually wrote is laid over the top afterwards
// ([layWork]) and everything else younger is the environment.
//
// AND A DIRECTORY IN WHICH NOTHING PREDATES THE WORK IS A DIRECTORY THE WORK
// MADE. It is not descended into at all, which is what keeps a `target/` or a
// `node_modules/` from costing the whole budget of a restore that was never
// going to carry a byte of it. A directory that existed before — even one the
// run added files to — has at least one entry older than the start, and is read
// through.
func copyOriginal(from, to string, wrote []string, started time.Time) string {
	visited := 0
	var walk func(relative string) string
	walk = func(relative string) string {
		entries, err := os.ReadDir(filepath.Join(from, filepath.FromSlash(relative)))
		if err != nil {
			// A directory that cannot be read is left out rather than fatal: the
			// audit's own build will say so far more usefully than this could.
			return ""
		}
		for _, entry := range entries {
			child := entry.Name()
			if relative != "" {
				child = relative + "/" + entry.Name()
			}
			// The repository's own metadata and the harness's own corner are never
			// part of anybody's deliverable (task_run.go's [stageTaskWork] keeps the
			// second one off a branch for the same reason).
			if entry.Name() == ".git" || child == aforgeDroppings {
				continue
			}
			if visited++; visited > auditRestoreEntries {
				return fmt.Sprintf("the working copy holds more than %d files, which is more than a clean copy is worth inside one check", auditRestoreEntries)
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			source := filepath.Join(from, filepath.FromSlash(child))
			target := filepath.Join(to, filepath.FromSlash(child))
			if entry.IsDir() {
				if !coveredByWrites(wrote, child) && info.ModTime().After(started) && appearedDuringTheRun(source, started) {
					continue
				}
				if err := os.MkdirAll(target, 0o755); err != nil {
					return "a clean copy could not be made: " + err.Error()
				}
				if problem := walk(child); problem != "" {
					return problem
				}
				continue
			}
			if info.ModTime().After(started) {
				continue
			}
			if err := copyPath(source, target); err != nil {
				return "a clean copy could not be made: " + err.Error()
			}
		}
		return ""
	}
	return walk("")
}

// appearedDuringTheRun reports whether a directory holds nothing that predates
// the work. An unreadable or empty directory answers false — the safe direction
// is to keep looking, because leaving out a directory that was already there
// would fail an audit over a file the node never touched.
func appearedDuringTheRun(dir string, started time.Time) bool {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return false
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.ModTime().After(started) {
			return false
		}
	}
	return true
}

// coveredByWrites reports whether one path is, holds, or sits under something
// the node wrote. It is deliberately not [orchestrate.Covers]: the question here
// runs BOTH ways — a directory is kept because the deliverable is somewhere
// underneath it, and a file is kept because it is under a directory the node
// declared.
func coveredByWrites(wrote []string, relative string) bool {
	for _, raw := range wrote {
		path := strings.TrimSpace(filepath.ToSlash(raw))
		if path == "" {
			continue
		}
		if path == relative || strings.HasPrefix(path, relative+"/") || strings.HasPrefix(relative, path+"/") {
			return true
		}
	}
	return false
}

// copyPath copies one file, symlink or directory tree.
//
// A SYMLINK IS COPIED AS A SYMLINK and never followed. The link IS the thing
// somebody wrote down, and a restore that turned one into the file it points at
// would be quietly repairing the exact class of mistake it exists to expose.
func copyPath(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		where, err := os.Readlink(source)
		if err != nil {
			return err
		}
		_ = os.Remove(target)
		return os.Symlink(where, target)
	case info.IsDir():
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	case !info.Mode().IsRegular():
		// A socket, a device node or a fifo is not a deliverable and cannot be
		// copied; skipping it is the honest answer and it is not an error.
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	from, err := os.Open(source)
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(to, from); err != nil {
		_ = to.Close()
		return err
	}
	return to.Close()
}

// ── what the work already ran ───────────────────────────────────────────────

// toolReceipt is ONE of the node's own tool results, as the auditor is shown it:
// what was called, with what, and what came back.
//
// IT IS THE WIRE RESULT AND NOT THE DISPLAY COPY. [Event.Output] and
// [DisplayEntry.Output] are both capped copies carrying a contract that says
// they are for showing a person and nothing else; this is read off the worker's
// own transcript, which is the text the model actually read.
type toolReceipt struct {
	tool   string
	args   string
	result string
}

// lastToolReceipts is the tail of what a worker RAN, read off its transcript.
//
// WHY THE AUDITOR IS GIVEN IT AT ALL: a check is often the most expensive thing
// in the whole task, and an auditor that must re-run every one of them from
// scratch inside [auditDeadline] is an auditor that times out. That was measured
// — a node landed unchecked because its scorer was re-run twice by a judge that
// then ran out of its five minutes — and the remedy is not a longer deadline, it
// is letting the judge see what already happened before deciding what it needs
// to repeat.
//
// IT IS NOT A SHORTCUT TO VERIFIED, and the contract in [auditQuestion] says so:
// these results came out of the work's OWN copy, which is exactly the copy a
// verdict may not rest on. What they are good for is the cheap direction — a
// check that failed, or a check nobody ever ran, needs no second run to be
// believed — and for telling the auditor WHICH command the check even is.
func lastToolReceipts(child *Agent, most int) []toolReceipt {
	if child == nil || most <= 0 {
		return nil
	}
	messages := child.snapshot()
	calls := toolResultCalls(messages)
	var out []toolReceipt
	for index := len(messages) - 1; index >= 0 && len(out) < most; index-- {
		message := messages[index]
		if !strings.EqualFold(message.Role, "tool") {
			continue
		}
		result := strings.TrimSpace(messageContentText(message))
		if result == "" {
			continue
		}
		receipt := toolReceipt{result: clip(result, auditReceiptLimit)}
		if call, ok := calls[index]; ok {
			receipt.tool = call.Function.Name
			// The arguments are JSON and a pretty-printed call would spend six lines
			// of the packet saying what one says (tools_standing.go's [oneLine]).
			receipt.args = clip(oneLine(call.Function.Arguments), taskReportLineLimit)

		}
		out = append(out, receipt)
	}
	// Oldest first, because they are read as a sequence of what happened and the
	// transcript is walked backwards to find them.
	for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	return out
}

// keepReceipts and lastReceipts are how the node carries what its last worker
// ran to the judge that never watched it. Whichever worker spoke last wins, for
// the reason the claim works the same way ([TaskNode.keepClaim]): a repair
// round's check is the one that describes the tree the auditor is about to read.
func (n *TaskNode) keepReceipts(receipts []toolReceipt) {
	n.graph.mu.Lock()
	n.receipts = receipts
	n.graph.mu.Unlock()
}

func (n *TaskNode) lastReceipts() []toolReceipt {
	if n == nil || n.graph == nil {
		return nil
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.receipts
}

// startedAt is when this node's run began, and it is read for one question: what
// in its working copy predates the work ([copyOriginal]).
func (n *TaskNode) startedAt() time.Time {
	if n == nil || n.graph == nil {
		return time.Time{}
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.started
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
	return a.resolveUnverifiedBy(id, resolution, why, TaskAskOwnerPerson)
}

// resolveUnverifiedBy is that door with WHICH DOOR WAS USED carried through it.
//
// THE RECEIPT SAYS WHO DECIDED, AND IT IS A FACT RATHER THAN AN INFERENCE. A
// node settled under `task.settle = auto` and a node the person pressed `[a]` on
// reach the same three answers, and reading the policy afterwards to guess which
// happened is wrong the moment a person answers a card on a node the model was
// holding — which is exactly what `[t] take it back` is for. So the two callers
// name themselves: the surface goes through [Agent.ResolveUnverified] and the
// model's own `tasks … resolve` goes through here (tools_tasks.go), and the
// report leads with the one that spent the verb ([acceptedLine]).
func (a *Agent) resolveUnverifiedBy(id uint64, resolution TaskResolution, why string, by TaskAskOwner) error {
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
		return a.acceptTask(node, why, by)
	case TaskRefute:
		return a.refuteTask(node, why, by)
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
	// AND A SECOND PRESS IS NOT A SECOND HAND-OVER. On 2026-09-09 the card was
	// pressed twice twenty-seven seconds apart and the model was handed the same
	// decision twice, in two identical lines — a second instruction about a
	// question it was already holding, which is an invitation to answer it twice.
	// The honest answer to the second press is what is already true, and the card
	// draws it exactly as it draws every other refusal these doors give.
	if node.wasHandedOver() {
		return fmt.Errorf("task %d is %s: %w", id, handedAlreadyWord, ErrTaskHandedOver)
	}
	// AND THE NODE RECORDS WHO IS HOLDING IT, so that the card in front of the
	// person stops offering them chips they have just handed over and says who is
	// deciding instead. It is the same mark `task.settle = auto` makes at the
	// landing (task_run.go's [Agent.handToModelOnAuto]) and the same floor hands it
	// back when the model's turn ends without an answer (agent.go).
	node.holdsDecision(TaskAskOwnerModel)
	notice := node.notice()
	a.enqueueSteering(handOverLead + "\n" +
		taskNote(notice, taskURI(node.journalPath()), TaskSettleAuto, a.quietAddress()))
	a.emitTaskUpdate(notice)
	return nil
}

// handedAlreadyWord is what a repeated hand-over answers with, in the person's
// own vocabulary for the thing they pressed — the card says aforge is deciding,
// so the refusal says the same word back rather than naming a field.
const handedAlreadyWord = "already handed to aforge"

// ErrTaskHandedOver says the second press changed nothing because the first one
// already did it, and it is a SEPARATE sentinel from [ErrTaskDecided] because
// the two are opposite facts about the card in front of somebody. A decided node
// is over and its card stops asking; a handed-over one is still `your call`,
// still waiting on an answer, and the only thing that moved is whose hands the
// question is in — so a surface that drew "already answered" over it would be
// reporting a decision nobody has made (internal/tui3's tasksettle.go).
var ErrTaskHandedOver = errors.New("session: that task is already handed to aforge")

// wasHandedOver reports that THIS DOOR has already given the model this node's
// decision and nothing has taken it back.
//
// IT IS NOT "THE MODEL IS DECIDING", and the difference is the whole of why it
// is its own fact. Under `task.settle = auto` — and in every headless run, where
// nobody is there to be asked — a landing marks the model as the decider by
// POLICY (task_run.go's [Agent.handToModelOnAuto]), which is not a press and
// carries no note. Refusing the press on that would refuse the first one.
//
// IT IS IN MEMORY AND NEVER ON THE RECORD, for [taskRecord.Decider]'s own
// reason: a hand-over lasts at most one turn — the floor takes it back at the
// end of the model's turn and a resumed session takes it back on load — so a
// receipt that survived either would refuse a press for a turn that is over.
func (n *TaskNode) wasHandedOver() bool {
	if n == nil || n.graph == nil {
		return false
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.handed
}

// decidedBy reads who is holding one node's question, with the graph taken for
// the read the way every other reader of a node's fields takes it.
func (n *TaskNode) decidedBy() TaskAskOwner {
	if n == nil || n.graph == nil {
		return ""
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.decider
}

// givesBackLocked puts one node's question back in the person's hands, with the
// graph held. It is a function because the receipt above has to move with the
// owner wherever the owner moves, and there are three roads that move it: the
// person taking it back, the end-of-turn floor, and a resumed session's load
// (task_run.go). A road that wrote only the owner would leave a receipt behind
// and refuse the next press.
func (n *TaskNode) givesBackLocked() {
	n.decider, n.handed = TaskAskOwnerPerson, false
}

// holdsDecision writes who is holding one node's question. It is the graph's
// lock and one field, and it is here rather than beside the door because both
// doors that move a decision — this one and the settle policy's — have to write
// exactly the same thing.
func (n *TaskNode) holdsDecision(owner TaskAskOwner) {
	if n == nil || n.graph == nil {
		return
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if owner != TaskAskOwnerModel {
		n.givesBackLocked()
		return
	}
	// AND THE RECEIPT IS WRITTEN WITH THE OWNER. A press that gets this far is
	// the one that hands the model the note, and the next press on the same node
	// is answered with what is already true rather than sending a second copy of
	// one decision ([TaskNode.wasHandedOver]).
	n.decider, n.handed = owner, true
}

// TakeBackDecision is [Agent.HandUnverifiedToModel] in reverse: the person
// deciding, after all, to decide. It is what the card's "take it back" presses,
// and it is what the floor does by itself at the end of a turn (task_run.go's
// [Agent.handBackUnsettled]).
//
// IT RESOLVES NOTHING EITHER. The node stays exactly as it is and what changes is
// who is holding the question, so the card stops saying aforge is deciding and
// draws its chips again. A line already on the steering queue is left where it
// is: the model may still say what it thinks, and what it may no longer do is
// have the last word.
func (a *Agent) TakeBackDecision(id uint64) error {
	node := a.taskNode(id)
	if node == nil {
		return fmt.Errorf("no task %d in this session", id)
	}
	if state := node.stateNow(); state != TaskUnverified {
		return settledAlready(id, state)
	}
	node.holdsDecision(TaskAskOwnerPerson)
	a.emitTaskUpdate(node.notice())
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
func (a *Agent) acceptTask(node *TaskNode, why string, by TaskAskOwner) error {
	// THE CLAIM IS TAKEN BEFORE THE WORKING COPY IS LOOKED FOR, and it covers
	// everything down to the resettle. What sits between the two is an os.Stat, a
	// `git rev-parse` and a merge — long enough for a second accept in the same
	// tool batch, or for a re-audit landing REFUTED, to walk straight through a
	// state that was read and not held (task_run.go's [TaskNode.claimSettle]).
	if err := node.claimSettle(claimAccept); err != nil {
		return err
	}
	defer node.releaseSettle()
	tree, err := node.workingCopy(a.familyPlace(node), a.config.Workspace)
	if err != nil {
		return err
	}
	// A PERSON IS THE CHECK HERE, and the store is told so. Nothing was spent to
	// learn it and nobody guessed: somebody read the work and said it holds,
	// which is the same kind of answer the gate gives and belongs in the same
	// record (taskgrade.go).
	node.checkSaid(provider.VerdictVerifiedSuccess, 0)
	report, changed, _, _ := node.leavings()
	// AND IT LANDS THE FAMILY'S LEDGER, THROUGH THE ONE ROAD EVERY LANDING TAKES
	// (task_ledger.go's [landHome]). An accept happens long after the run: the
	// list it reads is whatever the first landing wrote onto the node, and a
	// divider that landed unverified and is accepted in the morning must lay the
	// same whole product a verified one laid at once. The fold is idempotent, so
	// a list that is already complete costs a walk of itself.
	changed, merge, detail, refusal := landHome(node, tree, changed)
	if refusal == refusedByYourFiles {
		// AND THE ROAD IS MARKED HERE TOO. An accept is the second time a node's
		// branch is offered to the ground, and it can be refused by the person's
		// own untracked copies exactly as the first was (groundcarry.go) — so the
		// card that comes back asks the same question with the answer that can
		// actually spend it behind it, rather than falling back to a branch
		// conflict that is not what happened.
		node.heldByYourFiles()
	}
	// AN ACCEPT IS NOT A MERGE, and a branch that would not go is not done
	// however sure the person was about the work. The node stays where it was —
	// needing a look — with the conflicting files named, because what is being
	// asked of them has changed: they said the work was good, and it is; what is
	// left is two versions of the same file (task_run.go's [Agent.landConflicted]).
	if !cameHome(merge) {
		// AND A TREE THAT WOULD NOT TAKE THE WORK IS NOT ASKED AGAIN
		// (task_land_unsaved.go's [landingRefusal]). Somebody has looked at this work
		// and said it holds; what failed is the disk, and re-offering the same
		// question buys the same refusal. So it settles as it stands, with the
		// work where the sentence under it says it is, and the next resolution on
		// this node is answered as already decided ([settledAlready]).
		if refusal == refusedByTheTree {
			node.finish(withReport(keptWhereItIsLead+detail, withReport(acceptedLine(why, by), report)),
				changed, tree.branch, merge)
			node.graph.resettle(node, TaskDone)
			return nil
		}
		node.finish(withReport(withYourCallLead(node.landingFacts(merge), detail), withReport(acceptedLine(why, by), report)),
			changed, tree.branch, merge)
		node.graph.resettle(node, TaskUnverified)
		return nil
	}
	node.finish(withReport(acceptedLine(why, by), withReport(report, detail)), changed, tree.branch, merge)
	node.graph.resettle(node, TaskDone)
	return nil
}

// refuteTask is the person doing the auditor's job in the negative. The node's
// previous report is KEPT under the refusal rather than replaced — unlike a real
// REFUTED verdict, which drops the node's claim because the auditor's evidence
// has already answered it. Here the auditor answered nothing, so what the node
// said is still the only account of the work there is.
func (a *Agent) refuteTask(node *TaskNode, why string, by TaskAskOwner) error {
	if err := node.claimSettle(claimRefute); err != nil {
		return err
	}
	defer node.releaseSettle()
	// The same fact in the negative, and it is evidence of exactly the same
	// weight: a person doing the check's job and finding the work does not hold.
	node.checkSaid(provider.VerdictSemanticFailure, 0)
	report, changed, branch, merge := node.leavings()
	node.end(TaskEndingRefused)
	node.finish(withReport(refutedLine(why, by), report), changed, branch, merge)
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
	tree, err := node.workingCopy(a.familyPlace(node), a.config.Workspace)
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
		// The node supplies its worker's kept conclusion at the common check
		// boundary. No earlier checker's decision is passed as a claim.
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
	// A LATE VERDICT IS STILL THE CHECK'S VERDICT, so the node carries it into
	// the settle below exactly as the gate's own road does.
	node.checkSaid(auditGrade(verdict), 0)
	// AND IT SETTLES ON THE FAMILY'S LEDGER, on all three arms. Two of them do
	// not merge and still write the list onto the node, which is what the accept
	// after them will land (task_ledger.go).
	changed = absorbedLedger(node, changed)
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
		node.finish(withReport(verdict.lookOutcome(TaskFacts{Merge: merge, Conflicts: node.clashes()}), claim), changed, branch, merge)
		node.graph.resettle(node, TaskUnverified)
	case !verdict.verified:
		// A re-audit that finds something is a landing, not a loop. The repair
		// rounds belong to a node's RUN (see [Agent.auditWithRepair]); this node
		// has already landed once and been handed to a person, and starting a
		// worker inside their answer would be the harness spending on a decision
		// they made rather than carrying it out.
		node.end(TaskEndingRefused)
		node.finish(gapsOutcome([][]string{verdict.evidence}), changed, branch, abortedMerge(tree))
		node.graph.resettle(node, TaskFailed)
	default:
		changed, merged, detail, refusal := landHome(node, tree, changed)
		// A VERDICT THAT ARRIVES LATE CANNOT MERGE A BRANCH THAT WILL NOT GO
		// EITHER. The node keeps the one state that is true of it — somebody has
		// to look — with the work committed on its branch and the clashing files
		// named (task_run.go's [Agent.landConflicted] makes the same call on the
		// gate's own road).
		if !cameHome(merged) {
			// A TREE THAT WOULD NOT TAKE THE WORK IS NOT ASKED AGAIN HERE EITHER,
			// for [Agent.acceptTask]'s reason and by the same reading: a late
			// verdict saying the work holds, over a disk that cannot take it, is
			// the same pair of facts a person's accept produces.
			if refusal == refusedByTheTree {
				node.finish(withReport(keptWhereItIsLead+detail, withReport(claim, verdict.doneOutcome())),
					changed, tree.branch, merged)
				node.graph.resettle(node, TaskDone)
				return
			}
			node.finish(withReport(withYourCallLead(TaskFacts{Merge: merged, Conflicts: node.clashes()}, detail), withReport(claim, verdict.doneOutcome())),
				changed, tree.branch, merged)
			node.graph.resettle(node, TaskUnverified)
			return
		}
		// THE CLAIM, NOT THE CARRIED REPORT — and the claim LEADS, exactly as it
		// does on the gate's own landing (task_run.go's workTaskNode). The carried
		// report opens with the line that said nobody could judge this work, and a
		// card stacking a fresh answer over the question the last landing asked
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
func acceptedLine(why string, by TaskAskOwner) string {
	line := acceptedTookLine(by)
	if why != "" {
		line += ": " + clip(firstLine(why), taskReportLineLimit)
	}
	return line
}

// The two voices a resolved landing is written in, spelled once and read by the
// card, the room, the row and the `tasks` reply.
//
// THEY SAY WHO, AND THEY MUST. A node the model settled under `task.settle =
// auto` carried `you looked at this yourself and took it as done` into the
// person's own transcript — a sentence about something they never did, on work
// nobody had read. The person's own press keeps `you`; the model's verb says
// `aforge`, which is what this product is called everywhere a person reads it.
const (
	acceptedByYou    = "you took this as done"
	acceptedByAforge = "aforge took this as done"
	notRightByYou    = "you said it is not finished"
	notRightByAforge = "aforge said it is not finished"
)

// acceptedTookLine and notRightSaidLine pick the voice off the door that spent
// the verb, never off the settle policy: a person answering a card on a node the
// model was holding is the person, and `[t] take it back` exists to make that
// happen.
func acceptedTookLine(by TaskAskOwner) string {
	if by == TaskAskOwnerModel {
		return acceptedByAforge
	}
	return acceptedByYou
}

func notRightSaidLine(by TaskAskOwner) string {
	if by == TaskAskOwnerModel {
		return notRightByAforge
	}
	return notRightByYou
}

func refutedLine(why string, by TaskAskOwner) string {
	// It leads with the same word a node that ran out of repair rounds leads
	// with, because it is the same news: the work is not finished. Who decided is
	// the second half of the sentence, not the headline.
	line := incompleteLead + notRightSaidLine(by)
	if why != "" {
		line += ": " + clip(firstLine(why), taskReportLineLimit)
	}
	return line
}

// ── the auditor's agent ─────────────────────────────────────────────────────

// auditorModel is the model a fresh checker for this session lands on, read the
// way [Agent.newAuditAgent] reads it and for one purpose: the failover has to
// know which model it is moving OFF before it can ask the chain what comes after
// it ([Agent.failoverCheckerModel]).
//
// A role that will not resolve answers with the conversation's own model, which
// is what the checker would have been built on anyway — this is a question about
// where to go next, and a build with nothing to resolve simply has nowhere.
func (a *Agent) auditorModel() string {
	a.mu.Lock()
	source, model := a.config.RolesSource, a.model
	a.mu.Unlock()
	judge, err := roles.Resolve(roles.Source(source), roles.RoleAuditor, model)
	if err != nil {
		return model
	}
	return judge
}

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
//
// THE DOOR COMES IN RATHER THAN BEING DECIDED HERE. What one audit may run is a
// fact about the WORK — the checks its document declares and its worker ran
// (task_checks.go) — and it is read once by the caller so that both attempts at
// one node judge it through the same door.
// AND WHICH MODEL IT JUDGES ON MAY BE HANDED IN. `on` is empty for every
// ordinary check, which resolves the judge off the roles ladder below; it is
// filled only by the one retry that moves a non-answering check onto another
// lane ([Agent.failoverCheckerModel]), and nothing else about the auditor
// changes with it — same prompt, same belt, same door, same window.
func (a *Agent) newAuditAgent(dir string, node *TaskNode, door auditDoor, on string) (*Agent, error) {
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
	// The audit reads and judges rather than works, but it judges THIS person's
	// work, so it thinks as hard as the node it is checking (effort.go). It is
	// the node's rung above the parent's own resolved answer, which is exactly
	// the pair [Agent.newTaskAgent] hands a worker.
	inherited := a.effortLocked(a.model)
	a.mu.Unlock()
	nodeRung := node.effortRung()

	judge, err := roles.Resolve(roles.Source(parent.RolesSource), roles.RoleAuditor, model)
	if err != nil {
		return nil, err
	}
	// THE HANDED-IN MODEL WINS OUTRIGHT, and only the failover hands one in. The
	// ladder has already been asked at that point and answered with a lane this
	// session would move to; resolving the role again over the top of it would be
	// this function deciding that the answer did not count.
	if named := strings.TrimSpace(on); named != "" {
		judge = named
	}
	auditor, err := newAgent(Config{
		// The auditor reads rather than writes, but reading is what makes a
		// dropping: a long file it looks at is stubbed on its way out of the live
		// context (stub.go), and with nothing here those bytes landed in the
		// worktree it was judging (landing.go).
		droppings: parent.droppingsPlace(),
		Workspace: dir,
		Model:     judge,
		APIKey:    parent.APIKey,
		BaseURL:   parent.BaseURL,
		// The window of the model the AUDITOR runs, which the roles ladder has
		// very often made a different one from the node's
		// (loop.go's [Agent.childWindow]).
		ContextWindow:    a.childWindow(judge),
		ContextWindowFor: parent.ContextWindowFor,
		// The audit is bounded at five minutes and reads what it chooses to
		// read; a compaction inside that window is a summary of a judgement in
		// progress, which is the one thing a verdict must not be built on.
		CompactEnabled: false,
		SessionFile:    journal,
		System:         auditPrompt,
		Effort:         nodeRung,
		EffortRole:     effort.RoleWorker,
		DefaultEffort:  inherited,
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
		// Beside the ladder it overrides, for task_run.go's reason.
		OneModel: parent.OneModel,
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
	tools := auditBelt(dir, door, parent.droppingsPlace())
	definitions, err := toolDefinitions(tools)
	if err != nil {
		_ = auditor.Close()
		return nil, err
	}
	auditor.mu.Lock()
	auditor.tools = tools
	auditor.definitions = definitions
	// AN AUDITOR DOES NOT NAME ANYTHING. Its journal is machinery, and a
	// session namer running after the first verdict would be a cheap-looking
	// errand whose floor is THIS agent — the high tier — so a fall-through
	// is a naming call billed on the model that exists to judge work, not to
	// label it (F38). The attempt is marked used so maybeTitle is a no-op.
	auditor.titleTried = true
	auditor.mu.Unlock()
	// The shelf goes with the belt it was built beside, for the reason fork.go
	// states: `auditBelt` is an allowlist, and a narrowing meant to be total has
	// to empty the cupboard as well as the list (tools_capabilities.go).
	auditor.clearShelf()
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
func auditBelt(dir string, door auditDoor, droppings Place) []bare.Tool {
	var belt []bare.Tool
	for _, tool := range bare.AllTools(dir) {
		switch tool.Name {
		case "read":
			belt = append(belt, boundedResult(bare.ReadTool(dir, auditReadContentLimit), droppings, dir))
		case "grep", "find", "ls":
			belt = append(belt, boundedResult(tool, droppings, dir))
		case "bash":
			belt = append(belt, boundedResult(verifyOnlyBash(tool, door), droppings, dir))
		}
	}
	return belt
}

// boundedResult caps what one tool call may hand back.
//
// A cut result is filed through [writeStub], the same content-addressed,
// atomic droppings path used by conversation compaction. The cap therefore
// remains a hard bound on this observation while ordinary read paths and line
// offsets reach the rest. Nothing is written into the work being judged.
//
// The refusals pass through UNCAPPED in every practical case and deliberately go
// through the same cap anyway: a refusal is a result like any other, and a gate
// with an exception in it is a gate with a way around it.
func boundedResult(tool bare.Tool, droppings Place, workspace string) bare.Tool {
	inner := tool.Execute
	tool.Execute = func(ctx context.Context, args json.RawMessage) (string, bool, error) {
		text, isError, err := inner(ctx, args)
		if err != nil || len(text) <= auditResultLimit {
			return text, isError, err
		}
		pointer, fileErr := writeStub(droppings, workspace, text)
		if fileErr != nil || pointer == "" {
			footer := "\n[cut here; full output could not be saved — ask for a narrower path, pattern, or range]"
			return capBytes(text, auditResultLimit-len(footer)-32) + footer, isError, nil
		}
		footer := fmt.Sprintf("\n[cut here; whole output: %s — use read with offset/limit]", pointer)
		return capBytes(text, auditResultLimit-len(footer)-32) + footer, isError, nil
	}
	return tool
}

// shellLeash is WHO is holding a read-only bash, in the words its own refusals
// are written in.
//
// The gate below is one mechanism with two citizens — the auditor, and a fork's
// hand (fork.go) — and the two are doing different jobs, so a refusal that told
// a hand it was an auditor would be a lie in the one sentence the model is
// supposed to act on. What is shared is the DECISION, which is the part that has
// to be right; what differs is three fragments of prose.
type shellLeash struct {
	// who names the agent as it is named to itself: "an auditor", "a hand".
	who string
	// forWhat is what its bash is FOR, as a noun: "verification", "orientation".
	forWhat string
	// hint is the last line of every refusal and says where to go instead. A no
	// that does not say where to go costs another step.
	hint string
}

// auditShell is the auditor's voice, and it is the wording every refusal in this
// file has always carried.
var auditShell = shellLeash{who: "an auditor", forWhat: "verification", hint: auditReaderHint}

// verifyOnlyBash wraps pi's bash so it runs the work's own verification and
// nothing else.
//
// THE DESCRIPTION NAMES THIS AUDIT'S OWN DOOR, not a list somebody wrote once.
// The commands interpolated here are the checks the work declares and ran, so
// the first thing the auditor reads about its shell is the exact command the
// work is checked with (task_checks.go).
func verifyOnlyBash(tool bare.Tool, door auditDoor) bare.Tool {
	tool = readingOnlyBash(tool, door, auditShell)
	tool.Description = "Run one of THIS WORK's own verification commands and read its output: " +
		door.offer() + ". Every other command is refused, including anything that " +
		"edits, installs, fetches, or chains a second command onto one of these. " +
		auditReaderHint + " " + tool.Description
	return tool
}

// readingOnlyBash is the gate itself: pi's bash, allowed to run one command off
// a list and refusing everything else.
//
// The refusal is a RESULT, not an error: the agent reads "I am not allowed to
// run that, here is what I am allowed to run" and gets on with the job, exactly
// as a node reads a refused consent (consent.go). A Go error would end its turn
// and cost a verdict — or a hand's whole errand — over one wrong reach.
//
// The DESCRIPTION is left to the caller, because what a bash is for is the one
// thing the two citizens disagree about and it is the sentence the model reads
// before it ever reaches a refusal.
func readingOnlyBash(tool bare.Tool, door auditDoor, voice shellLeash) bare.Tool {
	inner := tool.Execute
	tool.Execute = func(ctx context.Context, args json.RawMessage) (string, bool, error) {
		var fields struct {
			Command string `json:"command"`
		}
		if err := decodeToolArguments(args, &fields); err != nil {
			return "Invalid arguments: " + err.Error(), true, nil
		}
		command := fields.Command
		if voice == auditShell {
			// PRE-VALIDATE ONLY THE AUDITOR'S OWN BELT SO THE CONTRACT NEVER
			// REFUSES ITS OWN COMMAND. The shared gate also serves fork hands,
			// whose read-only door must continue to refuse every composition.
			command = preparedAuditCommand(fields.Command)
			if command != fields.Command {
				fields.Command = command
				rewritten, err := json.Marshal(fields)
				if err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
				args = rewritten
			}
		}
		if refusal, ok := refuseOutsideDoor(command, door, voice); !ok {
			return refusal, true, nil
		}
		return inner(ctx, args)
	}
	return tool
}

// auditRefusal is [refuseOutsideDoor] in the auditor's own voice, asked about a
// door that is a bare list of commands. It is a door rather than a call site so
// that everything already written against the auditor's gate — this package's
// tests included — asks the same question with the same two arguments it always
// did.
func auditRefusal(command string, allowed []string) (string, bool) {
	return refuseOutsideDoor(command, plainDoor(allowed), auditShell)
}

// refuseOutsideAllowlist is the same question asked about a list rather than a
// whole door, for the citizens that have no tree behind them (fork.go's
// read-only shell).
func refuseOutsideAllowlist(command string, allowed []string, voice shellLeash) (string, bool) {
	return refuseOutsideDoor(command, plainDoor(allowed), voice)
}

// refuseOutsideDoor decides one command, and it decides it in three steps
// because a prefix check on its own is not a gate: `go test ./... && rm -rf .`
// starts with an allowed prefix and is not an allowed command.
//
// So SHELL COMPOSITION IS REFUSED OUTRIGHT — every operator that can start a
// second command, redirect output, or substitute one — and only then is what
// remains matched against the door. That order is the whole safety argument:
// after the first check there is exactly one command in the string, and the
// checks after it are about that command.
//
// THE DOOR IS MATCHED THE TWO WAYS IT IS WRITTEN. A check that named a file is
// matched by WHICH FILE the command names, under any spelling of it
// ([auditDoor.admitsFile]); everything else is matched field by field as the
// prefix it is. A file check is skipped by the prefix walk on purpose: it speaks
// for that file being run and not for arguments the work never declared.
func refuseOutsideDoor(command string, door auditDoor, voice shellLeash) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Sprintf("refused: %s runs %s, and that was an empty command.\n%s",
			voice.who, voice.forWhat, voice.hint), false
	}
	if index := strings.IndexAny(command, shellComposition); index >= 0 {
		return fmt.Sprintf("refused: %s runs ONE %s command with no shell composition, and %q is in %s.\nYou may run: %s\n%s",
			voice.who, voice.forWhat, string(command[index]), clip(command, auditCommandLimit),
			door.offer(), voice.hint), false
	}
	// Whitespace is normalized so "make  check" is the same command as
	// "make check": the door is about which program runs, not about how it
	// was typed.
	normalized := strings.Join(strings.Fields(command), " ")
	fields := strings.Fields(normalized)
	if door.admitsFile(fields) {
		return "", true
	}
	for _, prefix := range door.allowed {
		if door.identified(prefix) {
			continue
		}
		if matchesCommandPrefix(fields, strings.Fields(prefix)) {
			return "", true
		}
	}
	return fmt.Sprintf("refused: %s is not %s, and %s only runs %s.\nYou may run: %s\n%s",
		clip(normalized, auditCommandLimit), voice.forWhat, voice.who, voice.forWhat,
		door.offer(), voice.hint), false
}

// shellComposition is every character that can start a second command, redirect
// output, or substitute one. It is a CONSTANT rather than a literal at the gate
// because two readers now ask the same question of a string — the gate itself,
// and the reader that decides whether a fragment of the work's own text could
// ever be a door (task_checks.go's [commandLike]) — and a composition set spelled
// twice is a safety argument with two versions.
const shellComposition = ";|&<>`$(){}\n\r\\"

// matchesCommandPrefix decides whether one command starts with one allowed
// prefix, FIELD BY FIELD.
//
// The field walk is what makes a prefix a command prefix rather than a string
// prefix: "make check" admits `make check ./...` and does not admit
// `make checkout`, which a byte-wise HasPrefix would wave straight through.
//
// AND A WILDCARD THE WORK WROTE IS HONOURED. A brief that names its check as
// `run_tests.*` is naming one check whose extension it does not want to spell,
// and a door that took the star literally would be a door onto nothing. The
// match is per field, so a star never spans the space between two arguments, and
// a prefix that is nothing but wildcards was refused before it ever reached this
// list ([commandLike]).
func matchesCommandPrefix(fields, prefix []string) bool {
	if len(prefix) == 0 || len(fields) < len(prefix) {
		return false
	}
	for index, want := range prefix {
		if want == fields[index] {
			continue
		}
		if !strings.ContainsAny(want, "*?[") {
			return false
		}
		if ok, err := path.Match(want, fields[index]); err != nil || !ok {
			return false
		}
	}
	return true
}
