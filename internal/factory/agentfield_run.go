package factory

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// ── A JOB RUN BY AN AGENTFIELD REASONER ─────────────────────────────────────
//
// A job filed with RunOn is run by an AgentField reasoner on some node, not by
// this factory's own steps, and it reads exactly like any other job: the same
// lists, page, Start and Stop. Its steps ARE the run's top-level calls — the
// reasoner the job dispatched called plan, investigate, draft_reply, so those
// are the job's steps, by the reasoner's own name — and a step that ran the
// codeaf harness has a chat (Phase.Chat) that is an ordinary conversation on
// this machine, written from the lines the node streamed. Nothing the
// developer of that node wrote names a step: the tree is AgentField's own.

// AgentFieldRun is the run behind a RunOn job.
type AgentFieldRun struct {
	// Target is the reasoner dispatched, "<node>.<reasoner>".
	Target string `json:"target"`
	// ExecutionID is the run's root execution; RunID its workflow.
	ExecutionID string    `json:"execution_id,omitempty"`
	RunID       string    `json:"run_id,omitempty"`
	Began       time.Time `json:"began,omitzero"`
	Ended       time.Time `json:"ended,omitzero"`
	// Status is the root execution's last known status, in AgentField's words.
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
	// URL is the run's page on the control plane's own UI, when it has one.
	URL string `json:"url,omitempty"`
	// Host is the machine the node said it runs on when the run began.
	Host string `json:"host,omitempty"`
	// EndOwed says this run is an agentfield step's whose end the step has
	// not yet taken as its outcome: set when the step starts the run, cleared
	// when the step reads the run's end. A RUN WHOSE END IS OWED IS THE STEP'S
	// RUN, WHATEVER THE CLOCKS SAY: a runner that takes the job again (a
	// restart, a lease handed back or run out) reads that end instead of
	// starting a second run. False on a record written before it was kept,
	// which is judged by its times as before ([AgentFieldRunOwed]).
	EndOwed bool `json:"end_owed,omitempty"`
	// A harness step's conversation keeps its own relay record beside its
	// transcript (agentfield.json): which execution it is, how much of it is
	// kept, whether it is steerable now. It lives with the conversation,
	// because the conversation is what this factory keeps.

	// Stages and Phases are the run's executions as steps, when the run is
	// one STEP of a pipeline (an agentfield step, [Item.AgentFieldSteps])
	// rather than the whole job: the pipeline's own stages stay the job's,
	// and the job's read places these right after the step they ran under.
	// A RunOn job keeps its run's steps as the job's own instead (nil here).
	Stages []Stage `json:"stages,omitempty"`
	Phases []Phase `json:"phases,omitempty"`
	// Waiting is the run's pause for a person, while one execution of it
	// waits for approval ([AgentFieldWait]); nil otherwise.
	Waiting *AgentFieldWait `json:"waiting,omitempty"`
	// Asks are the pauses for a person the run made, oldest first, one per
	// distinct approval (the runner's to write): the rounds a plan went
	// through, so the person sees what changed after their note.
	Asks []AgentFieldAsk `json:"asks,omitempty"`
	// Decisions are the people's answers this factory sent, oldest first
	// (the factory's own to write; a runner's report never replaces them).
	Decisions []AgentFieldDecision `json:"decisions,omitempty"`
	// Refused is the control plane's refusal of the last answer the runner
	// carried to it (the runner's to write): the answer did not reach the
	// run, so its question stands again with these words beside it.
	Refused *AgentFieldRefusal `json:"refused,omitempty"`
}

// AgentFieldRefusal is an answer the control plane would not take.
type AgentFieldRefusal struct {
	RequestID string    `json:"request_id,omitempty"`
	Execution string    `json:"execution"`
	Words     string    `json:"words"`
	At        time.Time `json:"at,omitzero"`
}

// AgentFieldRejected is the control plane's word for a person's Reject of a
// run's pause: the run is cancelled there, and the job ends as their Stop.
const AgentFieldRejected = "rejected"

// AgentFieldRunLive says step's AgentField run (the job's own run for a
// RunOn job) was started on the control plane and has not ended: a runner
// that takes the job again follows that run rather than starting another.
func AgentFieldRunLive(it Item, step string) bool {
	run := AgentFieldRunOf(it, step)
	return run != nil && run.ExecutionID != "" && run.Ended.IsZero()
}

// AgentFieldRunOwed says step's AgentField run is one a runner still owes
// the job: live ([AgentFieldRunLive]), or ended while the step that started
// it is still running — a quit or a lease let go between the run's end and
// the step's — so its end is on the record and not yet the step's. A runner
// that takes the job again reads that end as the step's outcome; the step was
// never lost, and nothing about it is a retry.
func AgentFieldRunOwed(it Item, step string) bool {
	if AgentFieldRunLive(it, step) {
		return true
	}
	run := AgentFieldRunOf(it, step)
	if run == nil || run.ExecutionID == "" || run.Ended.IsZero() || it.Stream == nil {
		return false
	}
	for _, ph := range it.Stream.Phases {
		if ph.Name == step {
			if ph.State != PhaseRunning {
				return false
			}
			// THE RECORD SAYS SO, when it was written by a runner that keeps
			// [AgentFieldRun.EndOwed]. A step's start can move past its run's
			// (a round entered again after a pause or an admission retry
			// stamps a new start while the same run goes on), so the times
			// alone once disowned a run that was the step's (live e2e round
			// 9, job 306) and a second run was started.
			if run.EndOwed {
				return true
			}
			return !run.Began.IsZero() && (ph.Started.IsZero() || !run.Began.Before(ph.Started))
		}
	}
	return false
}

// AgentFieldRunStopped says run ended because someone stopped it — a
// person's Stop or Reject carried to the control plane, or a cancel there —
// rather than by finishing its work. Such an end is never a later round's
// outcome: Reopen or Run again starts a run of its own.
func AgentFieldRunStopped(run AgentFieldRun) bool {
	switch strings.ToLower(strings.TrimSpace(run.Status)) {
	case "cancelled", "canceled", AgentFieldRejected:
		return true
	}
	return false
}

// AgentFieldAnyRunLive says one of the job's runs (its own, or any agentfield
// step's) was started on the control plane and has not ended. Such a run goes
// on there whether or not a runner here follows it, so a job that has one and
// no runner holding it is owed a runner: to follow it, carry an answer the
// person gave, or carry the person's Stop.
func AgentFieldAnyRunLive(it Item) bool {
	if r := it.AgentField; r != nil && r.ExecutionID != "" && r.Ended.IsZero() {
		return true
	}
	for _, r := range it.AgentFieldSteps {
		if r != nil && r.ExecutionID != "" && r.Ended.IsZero() {
			return true
		}
	}
	return false
}

// AgentFieldRejectCancel is the control plane's reason on a run it
// cancelled because a person rejected its pause: `approval_rejected: <their
// words>`.
const AgentFieldRejectCancel = "approval_rejected"

// AgentFieldRejectedEnd is the person's Reject that ended one of the job's
// runs, ok false when no run of it ended that way.
//
// A RUN THAT ENDED ON A PERSON'S NO IS A REJECTION, NEVER A FAILED STEP. The
// control plane cancels a rejected run at once, so the runner's reading of
// that cancel can reach the factory before (or instead of) the runner's
// acknowledgement of carrying the Reject; whichever arrives first, the job
// ends Rejected with the person's words. IT IS KEYED ON THE DECISION AND THE
// CANCEL REASON, NEVER ON A PIPELINE'S OR A STEP'S NAME: the newest rejected
// decision this factory recorded on one of the ended run's own pauses (with
// who gave it), else the control plane's own reason when the no was given
// elsewhere, such as on its own page. A decision on an earlier run's pause
// is not this run's: a fresh run has fresh pauses.
func AgentFieldRejectedEnd(it Item) (Rejection, bool) {
	runs := []*AgentFieldRun{it.AgentField}
	names := make([]string, 0, len(it.AgentFieldSteps))
	for name := range it.AgentFieldSteps {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		runs = append(runs, it.AgentFieldSteps[name])
	}
	for _, run := range runs {
		if run == nil || run.Ended.IsZero() {
			continue
		}
		for i := len(run.Decisions) - 1; i >= 0; i-- {
			d := run.Decisions[i]
			if d.Decision == AgentFieldRejected && agentFieldPauseOf(run, d.Execution) {
				return Rejection{By: NormalizeHandle(d.By), Words: strings.TrimSpace(d.Words), At: d.At}, true
			}
		}
		if reason := strings.TrimSpace(run.Error); strings.HasPrefix(reason, AgentFieldRejectCancel) {
			words := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(reason, AgentFieldRejectCancel), ":"))
			return Rejection{Words: words, At: run.Ended}, true
		}
	}
	return Rejection{}, false
}

// agentFieldPauseOf says execution is one of run's own pauses: the run
// itself, the one it waits on, or one it asked.
func agentFieldPauseOf(run *AgentFieldRun, execution string) bool {
	if execution == "" {
		return false
	}
	if run.ExecutionID == execution || (run.Waiting != nil && run.Waiting.Execution == execution) {
		return true
	}
	for _, a := range run.Asks {
		if a.Execution == execution {
			return true
		}
	}
	return false
}

// AgentFieldAnswerFor is the person's answer to the pause request waits on
// that has not been refused since it was given: the newest decision for that
// request (and execution), nil when there is none or the control plane
// refused it. IT IS WHAT A RUNNER STILL OWES THE CONTROL PLANE, and what
// keeps a runner's older reading of the same pause from asking again.
func AgentFieldAnswerFor(run *AgentFieldRun, execution, request string) *AgentFieldDecision {
	if run == nil || execution == "" {
		return nil
	}
	for i := len(run.Decisions) - 1; i >= 0; i-- {
		d := run.Decisions[i]
		if d.Execution != execution || d.RequestID != request {
			continue
		}
		if f := run.Refused; f != nil && f.Execution == execution && f.RequestID == request && !d.At.After(f.At) {
			return nil
		}
		return &d
	}
	return nil
}

// MayAnswerAgentField says caller may answer the job's AgentField pause.
// ONLY THE JOB'S PERSON ANSWERS IT, or anyone the factory lets act on a job
// that has no person yet: the same rule whatever door the answer comes
// through, so the buttons a person is offered and the answers a factory
// takes never disagree.
func MayAnswerAgentField(it Item, caller string) bool {
	person := it.Person()
	return person == "" || person == NormalizeHandle(caller)
}

// AgentFieldAsk is one pause of the run, as it was first read.
type AgentFieldAsk struct {
	RequestID string    `json:"request_id,omitempty"`
	Execution string    `json:"execution"`
	Body      string    `json:"body,omitempty"`
	At        time.Time `json:"at,omitzero"`
}

// AgentFieldDecision is one person's answer to a pause, as sent.
type AgentFieldDecision struct {
	RequestID string    `json:"request_id,omitempty"`
	Execution string    `json:"execution"`
	Decision  string    `json:"decision"`
	Words     string    `json:"words,omitempty"`
	By        string    `json:"by,omitempty"`
	At        time.Time `json:"at,omitzero"`
}

// AgentFieldAsksMost bounds the pauses a run record keeps.
const AgentFieldAsksMost = 12

// AgentFieldWait is one execution of an AgentField run waiting for a
// person's approval (the reasoner's app.pause / Agent.Pause), as the control
// plane's approval-status said it. THE BODY IS KEPT WHOLE: it is what the
// person approves (a plan, a diff), and approving what you cannot read is no
// approval. A re-plan after "Ask for changes" is a new pause with a new
// RequestID and a new body, which replaces this one.
type AgentFieldWait struct {
	Execution string `json:"execution"`
	// Node is the AgentField node of the waiting execution.
	Node      string    `json:"node,omitempty"`
	Step      string    `json:"step,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	Question  string    `json:"question,omitempty"`
	Body      string    `json:"body,omitempty"`
	Options   []string  `json:"options,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	URL       string    `json:"url,omitempty"`
	At        time.Time `json:"at,omitzero"`
}

// QKindAgentField is a job waiting on an AgentField run's approval
// ([AgentFieldWait]). ONLY A PERSON ANSWERS IT: the manager never approves a
// plan a reasoner paused on, whatever its autonomy.
const QKindAgentField = "agentfield_approval"

// AgentFieldQuestion is the one line a run's approval asks, in the reasoner's
// words when it gave some.
func AgentFieldQuestion(w AgentFieldWait) string {
	if q := strings.TrimSpace(w.Question); q != "" {
		return q
	}
	return "The AgentField run asks for your approval"
}

// AgentFieldRunOf is the run behind a job's step: the step's own run for an
// agentfield step, else the job's (a RunOn job).
func AgentFieldRunOf(it Item, step string) *AgentFieldRun {
	if r := it.AgentFieldSteps[step]; r != nil {
		return r
	}
	return it.AgentField
}

// AgentFieldPause says the job waits in Needs you on an AgentField run's
// approval. ITS REJECT IS THE CONTROL PLANE'S, NOT THIS FACTORY'S PLAN
// REVISION: a factory that serves no plan revision of its own, or a solo
// factory that reads its plans off the stored run, still offers it.
func AgentFieldPause(it Item) bool {
	return it.State == StateNeedsYou && it.QKind == QKindAgentField && AgentFieldWaiting(it) != nil
}

// AgentFieldWaiting is the run approval the job waits on, if any: the job's
// own run's, else any agentfield step's.
func AgentFieldWaiting(it Item) *AgentFieldWait {
	if it.AgentField != nil && it.AgentField.Waiting != nil {
		return it.AgentField.Waiting
	}
	for _, r := range it.AgentFieldSteps {
		if r != nil && r.Waiting != nil {
			return r.Waiting
		}
	}
	return nil
}

// AllPhases is every phase of the job: its own, and the steps of every
// agentfield step's run. A harness step's conversation is on one of them.
func AllPhases(it Item) []Phase {
	var out []Phase
	if it.Stream != nil {
		out = append(out, it.Stream.Phases...)
	}
	for _, name := range agentFieldStepNames(it) {
		out = append(out, it.AgentFieldSteps[name].Phases...)
	}
	return out
}

func agentFieldStepNames(it Item) []string {
	names := make([]string, 0, len(it.AgentFieldSteps))
	for _, st := range it.Stages {
		if r := it.AgentFieldSteps[st.Name]; r != nil {
			names = append(names, st.Name)
		}
	}
	return names
}

// AgentFieldRunner is where an AgentField job runs, in the words the job's
// "runs on" fact shows: the node, and the node's machine when it named one —
// "support · dumb". A RunOn job never runs on this factory's machine, so it
// is never the factory's own machine word.
func AgentFieldRunner(it Item) string {
	node, _, _ := strings.Cut(strings.TrimSpace(it.RunOn), ".")
	if node == "" {
		return ""
	}
	if it.AgentField != nil && strings.TrimSpace(it.AgentField.Host) != "" {
		return node + " · " + strings.TrimSpace(it.AgentField.Host)
	}
	return node
}

// RunOnEnded is when a RunOn job's run ended: the run's own end when it
// reported one, else now.
func RunOnEnded(it Item, now time.Time) time.Time {
	if it.AgentField != nil && !it.AgentField.Ended.IsZero() {
		return it.AgentField.Ended
	}
	return now
}

// RunOnPending says the job is run by an AgentField reasoner ([Item.RunOn])
// whose run has not begun. ITS STEPS ARE THE RUN'S, NOT THE FACTORY'S: until
// the run reports its tree there is no step to show, so the default pipeline
// the job was filed with is neither drawn nor counted — a rail that showed
// plan, approve and write for a job that will never run them would be a lie.
func RunOnPending(it Item) bool {
	return it.RunOn != "" && it.AgentField == nil
}

// ── AN AGENTFIELD STEP: A PIPELINE STEP RUN ON AGENTFIELD ───────────────────
//
// A pipeline step of kind agentfield names a reasoner, "<node>.<reasoner>",
// and an input built from the job. WorkAF starts that run on the control
// plane when the step comes up, and the run's executions fold UNDER the step
// ([AgentFieldRun.Stages]); the step ends with the run, and the pipeline goes
// on to its next step. The job itself is an ordinary job from its own source
// (a GitHub issue, a Jira ticket, New job): AgentField is where one step of
// it runs, never a second intake for it.

// AgentFieldPlaceholders are the job's facts an agentfield step's input may
// name, in the order the manual lists them.
var AgentFieldPlaceholders = []string{"title", "body", "repo", "repo_url", "base", "issue_url", "issue_number", "job", "pr_url"}

// AgentFieldTargetWord reads an agentfield step's reasoner: "<node>.<reasoner>",
// each part a word of letters, digits, `-` and `_`.
func AgentFieldTargetWord(s string) (string, error) {
	s = strings.TrimSpace(s)
	node, reasoner, ok := strings.Cut(s, ".")
	if !ok || !targetPart(node) || !targetPart(reasoner) {
		if s == "" {
			return "", errorString("it names none")
		}
		return "", errorString("\"" + s + "\" is not one")
	}
	return s, nil
}

type errorString string

func (e errorString) Error() string { return string(e) }

func targetPart(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// AgentFieldInputProblem is why an input value cannot be built, "" when it
// can: every {placeholder} it names must be one of [AgentFieldPlaceholders].
func AgentFieldInputProblem(v string) string {
	for rest := v; ; {
		i := strings.IndexByte(rest, '{')
		if i < 0 {
			return ""
		}
		j := strings.IndexByte(rest[i:], '}')
		if j < 0 {
			return ""
		}
		name := rest[i+1 : i+j]
		// ONLY A WORD IN BRACES IS A PLACEHOLDER: JSON text for an object
		// field ({"check_ci": false}) is braces too, and is the reasoner's.
		if placeholderWord(name) && !oneOf(name, AgentFieldPlaceholders) {
			return "{" + name + "} is not a fact of the job; the job's facts are {" + strings.Join(AgentFieldPlaceholders, "}, {") + "}"
		}
		rest = rest[i+j+1:]
	}
}

// AgentFieldInputReady validates result bindings only when a step is about to
// dispatch. Library validation cannot know what earlier work will produce.
func AgentFieldInputReady(it Item, st Stage) error {
	requested := false
	for _, v := range st.Input {
		requested = requested || strings.Contains(v, "{pr_url}")
	}
	if !requested {
		return nil
	}
	if it.PullRequest != nil && it.Stream != nil {
		link, _ := PullRequestIn([]byte(it.PullRequest.URL))
		work := false
		for _, ph := range it.Stream.Phases {
			if ph.Name == st.Name {
				if work && link != "" && link == strings.TrimSpace(it.PullRequest.URL) {
					return nil
				}
				break
			}
			if ph.State != PhaseDone && ph.State != PhaseSkipped {
				break
			}
			work = work || ph.State == PhaseDone && ph.Kind != StageGate
		}
	}
	return errorString("this step needs {pr_url}: finish an earlier work step that opens a pull request first")
}

// agentFieldFacts are the job's facts by placeholder.
func agentFieldFacts(it Item) map[string]string {
	issue, number := "", ""
	if it.Num > 0 {
		number = strconv.Itoa(it.Num)
		if u := strings.TrimSpace(it.URL); strings.Contains(u, "/issues/") {
			issue = u
		} else if r := RepoURL(it); r != "" {
			issue = r + "/issues/" + number
		}
	}
	base := strings.TrimSpace(it.Base)
	if base == "" {
		base = strings.TrimSpace(it.ChangeBase)
	}
	pr := ""
	if it.PullRequest != nil {
		pr = strings.TrimSpace(it.PullRequest.URL)
	}
	return map[string]string{
		"title": it.Title, "body": it.Body, "repo": RepoOf(it), "repo_url": RepoURL(it), "base": base,
		"issue_url": issue, "issue_number": number, "job": strconv.Itoa(it.ID), "pr_url": pr,
	}
}

// placeholderWord says whether s could name a placeholder: lowercase letters,
// digits and underscores.
func placeholderWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// AgentFieldInput is the input an agentfield step dispatches its reasoner
// with: the step's mapping with the job's facts put in, or, when the step
// names none, every fact under the reasoner-neutral names
// {job, title, body, repo, repo_url, base_branch, issue_url, issue_number}.
func AgentFieldInput(it Item, st Stage) map[string]any {
	facts := agentFieldFacts(it)
	if len(st.Input) == 0 {
		out := map[string]any{"job": it.ID}
		for k, v := range facts {
			// Result bindings are opt-in so existing reasoners keep their default input.
			if k == "job" || k == "pr_url" || v == "" {
				continue
			}
			if k == "base" {
				k = "base_branch"
			}
			out[k] = agentFieldValue(k, v)
		}
		return out
	}
	out := make(map[string]any, len(st.Input))
	for k, v := range st.Input {
		// A VALUE THAT IS ONE FACT AND NOTHING ELSE IS SENT AS THAT FACT'S
		// TYPE, and left out when the job has no such fact: a reasoner's input
		// is typed, and neither "12" nor "" decodes into an int field
		// (SWE-AF's issue_number).
		if inner, ok := strings.CutPrefix(v, "{"); ok {
			if name, ok := strings.CutSuffix(inner, "}"); ok {
				if fact, known := facts[name]; known {
					if fact != "" {
						out[k] = agentFieldValue(name, fact)
					}
					continue
				}
			}
		}
		for name, fact := range facts {
			v = strings.ReplaceAll(v, "{"+name+"}", fact)
		}
		out[k] = v
	}
	return out
}

// agentFieldValue is a fact as it is sent: the job's and the issue's numbers
// as numbers, every other fact as its words.
func agentFieldValue(name, fact string) any {
	if name == "job" || name == "issue_number" {
		if n, err := strconv.Atoi(fact); err == nil {
			return n
		}
	}
	return fact
}

// AgentFieldPipeline says the job's pipeline runs only on AgentField: at
// least one agentfield step, and every other step it runs a person's
// approval. Such a job ends as a RunOn job does — done when its runs
// succeeded — since it made no change on this factory for a person to
// approve (a pull request a run opened is the job's own).
func AgentFieldPipeline(it Item) bool {
	return agentFieldOnly(it)
}

// RunsElsewhere reports whether every step of the job runs on another
// machine — a RunOn job, or an [AgentFieldPipeline] — so this machine only
// follows the run and needs no checkout of the job's repository: it never
// clones one, and its page never says it is cloning.
func RunsElsewhere(it Item) bool {
	return strings.TrimSpace(it.RunOn) != "" || AgentFieldPipeline(it)
}

func agentFieldOnly(it Item) bool {
	if it.RunOn != "" {
		return false
	}
	any := false
	for _, st := range it.Stages {
		if !st.On {
			continue
		}
		switch st.Kind {
		case StageAgentField:
			any = true
		case StageGate:
		default:
			return false
		}
	}
	return any
}

// AgentFieldEnded is when the job's AgentField work ended: the latest run end
// any of its agentfield steps reported, else now.
func AgentFieldEnded(it Item, now time.Time) time.Time {
	var end time.Time
	for _, r := range it.AgentFieldSteps {
		if r != nil && r.Ended.After(end) {
			end = r.Ended
		}
	}
	if end.IsZero() {
		return RunOnEnded(it, now)
	}
	return end
}

// PullRequestIn is the first GitHub pull request link anywhere in a run's
// result, and its number; "" when it names none. A reasoner that opened one
// says so in whatever field it likes (pr_url, pull_request.html_url, a line of
// its summary), so the link is looked for rather than expected at one key.
func PullRequestIn(result []byte) (string, int) {
	s := string(result)
	for i := strings.Index(s, "https://github.com/"); i >= 0; {
		rest := s[i:]
		end := strings.IndexAny(rest, "\"' )\n\\<>`]")
		if end < 0 {
			end = len(rest)
		}
		link := rest[:end]
		parts := strings.Split(strings.TrimPrefix(link, "https://github.com/"), "/")
		if len(parts) >= 4 && parts[2] == "pull" {
			if n, err := strconv.Atoi(parts[3]); err == nil && n > 0 {
				return "https://github.com/" + parts[0] + "/" + parts[1] + "/pull/" + parts[3], n
			}
		}
		next := strings.Index(s[i+1:], "https://github.com/")
		if next < 0 {
			break
		}
		i += 1 + next
	}
	return "", 0
}

// RunDeliveredPR says the job's pull request was opened by a run on another
// machine ([RunsElsewhere]): an AgentField pipeline or a RunOn job. No
// worktree on this factory ever held its commits, so no proof gate here
// recorded the commit a person approved, and [PinDeliveredHead] records the
// equivalent from the pull request itself.
//
// The factory's own Follow PR step ([IsCIRepairStage]) is not a step of the
// job's pipeline and does not make the pull request this machine's: a job
// shipped before the r10 fix carries it, and must still record the head its
// run delivered rather than refuse Merge forever.
func RunDeliveredPR(it Item) bool {
	return it.Kind != KindPR && it.Publish == "pr" && it.PullRequest != nil && it.PullRequest.Number > 0 && RunsElsewhere(withoutCIRepair(it))
}

// withoutCIRepair is the job with the factory's CI-repair steps left out of
// its stages, so what remains is the pipeline the job was given.
func withoutCIRepair(it Item) Item {
	stages := make([]Stage, 0, len(it.Stages))
	for _, st := range it.Stages {
		if !IsCIRepairStage(st) {
			stages = append(stages, st)
		}
	}
	it.Stages = stages
	return it
}

// PinDeliveredHead records head as the job's approved commit when the job
// shipped on a run that delivered its pull request and none is recorded yet.
// THE RUN'S ENDING IS ITS APPROVAL: the person approved the run's plan and the
// run, not a local proof, decided the change was done, so the head the pull
// request carries when the run has ended is what the merge gate holds the
// merge to, exactly as a native publish holds it to the head the person
// approved. A push after this is a commit nobody approved, and the gate says
// so. It never touches a job that recorded its own commit, and never a native
// job, whose approved commit is its proof's alone.
func PinDeliveredHead(it *Item, head string) bool {
	head = strings.TrimSpace(head)
	if head == "" || strings.TrimSpace(it.HeadCommit) != "" || it.State != StateShipped || !RunDeliveredPR(*it) || it.PullRequest.State != PROpen {
		return false
	}
	it.HeadCommit = head
	return true
}
