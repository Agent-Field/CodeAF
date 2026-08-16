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
var auditCommands = []string{
	"go test",
	"go build",
	"go vet",
	"git diff",
	"git log",
	"git status",
	"git show",
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
		return noVerdict("asked twice and got no verdict either time", "")
	}
	evidence := make([]string, len(v.evidence))
	copy(evidence, v.evidence)
	evidence[0] = "asked twice and got no verdict either time — " + evidence[0]
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
	fmt.Fprintf(log, "audit: no verdict — asking once more\n")
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
		return noVerdict("the auditor could not start: "+err.Error(), ""), true
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
		return noVerdict("the auditor could not be asked: "+err.Error(), ""), true
	}
	for event := range events {
		if event.Kind == EventToolBegin {
			fmt.Fprintf(log, "audit · %s\n", event.Hint)
		}
	}

	said := lastSaid(auditor)
	// A node killed mid-audit is the caller's story to tell, not the auditor's;
	// it reads ctx itself. What is this function's story is the audit that ran
	// out of its own five minutes with the node still perfectly alive.
	if auditCtx.Err() != nil && ctx.Err() == nil {
		return noVerdict(fmt.Sprintf("no verdict in %s, so nothing was accepted", auditDeadline), said), false
	}

	verdict := parseAuditVerdict(said)
	fmt.Fprintf(log, "audit: %s\n", verdict.report())
	return verdict, true
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
	return noVerdict("the auditor answered neither VERIFIED nor REFUTED", text)
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
	if state := node.stateNow(); state != TaskUnverified {
		return fmt.Errorf("task %d is %s, and only an unverified task is waiting on somebody to decide", id, state)
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

// acceptTask takes the work as done on the person's word.
//
// THE BRANCH COMES HOME THE ORDINARY WAY. An accepted node is a node somebody
// verified by hand, so it merges exactly as a VERIFIED one does and its
// dependents unblock on the frontier pass the settle turns. What it does NOT do
// is pretend an auditor said so: the report leads with who accepted it and on
// what grounds, because a card that read "VERIFIED" over a verdict nobody gave
// would be the same lie as the one this file was fixed to stop telling.
func (a *Agent) acceptTask(node *TaskNode, why string) error {
	tree, err := node.workingCopy(a.config.Workspace)
	if err != nil {
		return err
	}
	if node.beingAudited() {
		return fmt.Errorf("task %d is being re-audited: wait for that verdict, or it will land on top of yours", node.id)
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
	if node.beingAudited() {
		return fmt.Errorf("task %d is being re-audited: wait for that verdict, or it will land on top of yours", node.id)
	}
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
	tree, err := node.workingCopy(a.config.Workspace)
	if err != nil {
		return err
	}
	if !node.beginAudit() {
		return fmt.Errorf("task %d is already being re-audited", node.id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	listed, _ := a.jobs.startTask(node.id, "re-audit · "+node.title(), cancel)
	_, changed, _, _ := node.leavings()
	go func() {
		defer cancel()
		defer node.endAudit()
		if listed != nil {
			defer listed.settle(0)
		}
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
	switch {
	case !verdict.answered:
		// STILL NOBODY. The fresh non-answer REPLACES the stale one rather than
		// stacking under it: two auditors failing to answer is one fact, and a
		// report that grew a paragraph per attempt would be a card nobody can
		// read by the third try. Every attempt is in its own audit journal.
		node.finish(verdict.report(), changed, branch, merge)
		node.graph.resettle(node, TaskUnverified)
	case !verdict.verified:
		node.finish(verdict.report(), changed, branch, abortedMerge(tree))
		node.graph.resettle(node, TaskFailed)
	default:
		merged, detail := tree.comeHome(node.title())
		node.finish(withReport(verdict.report(), withReport(report, detail)), changed, tree.branch, merged)
		node.graph.resettle(node, TaskDone)
	}
}

// acceptedLine and refutedLine are the first line of a resolved node's report —
// which is also its row in the project's index (task_index.go's taskOutcome), so
// each says WHO decided and, when they gave one, why.
func acceptedLine(why string) string {
	line := "ACCEPTED by the person — no auditor verdict was ever reached"
	if why != "" {
		line += ": " + clip(firstLine(why), taskReportLineLimit)
	}
	return line
}

func refutedLine(why string) string {
	line := "REFUTED by the person — no auditor verdict was ever reached"
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
	journal := taskJournalPath(a.sessionID(), node.id, "-audit")
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
		RolesSource:    parent.RolesSource,
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

// auditBelt is the read-only belt: the four readers as they are, and a bash
// that runs verification and refuses the rest. edit and write are not filtered
// out of a list — they are never put in one.
func auditBelt(dir string, allowed []string) []bare.Tool {
	var belt []bare.Tool
	for _, tool := range bare.AllTools(dir) {
		switch tool.Name {
		case "read", "grep", "find", "ls":
			belt = append(belt, tool)
		case "bash":
			belt = append(belt, verifyOnlyBash(tool, allowed))
		}
	}
	return belt
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
		"edits, installs, fetches, or chains a second command onto one of these. " + tool.Description
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
		return "refused: an auditor runs verification, and that was an empty command", false
	}
	if index := strings.IndexAny(command, ";|&<>`$(){}\n\r\\"); index >= 0 {
		return fmt.Sprintf("refused: an auditor runs ONE verification command with no shell composition, and %q is in %s.\nYou may run: %s",
			string(command[index]), clip(command, auditCommandLimit), strings.Join(allowed, ", ")), false
	}
	// Whitespace is normalized so "go  test" is the same command as "go test":
	// the allowlist is about which program runs, not about how it was typed.
	normalized := strings.Join(strings.Fields(command), " ")
	for _, prefix := range allowed {
		if normalized == prefix || strings.HasPrefix(normalized, prefix+" ") {
			return "", true
		}
	}
	return fmt.Sprintf("refused: %s is not verification, and an auditor only runs verification.\nYou may run: %s",
		clip(normalized, auditCommandLimit), strings.Join(allowed, ", ")), false
}
