package factory

import (
	"strconv"
	"strings"
	"time"
)

// Needs-you kinds on the wire. ADDING A KIND IS A DELIBERATE CHANGE: add it
// here and to TestNeedsYouKindsContractTwo in the same commit. The queue groups
// jobs by this list, in this order.
const (
	NeedsApproveStart  = "approve_start"
	NeedsApprovePlan   = "approve_plan"
	NeedsApproveProof  = "approve_proof"
	NeedsQuestion      = "question"
	NeedsRunnerOffline = "runner_offline"
	NeedsSpendingLimit = "budget"
	NeedsOutOfAttempts = "out_of_rounds"
	NeedsPublishFailed = "publish_failed"
	// Persisted question kinds keep their bytes across wire contracts.
	QKindOutOfAttempts    = "out_of_attempts"
	NeedsFailed           = "failed"
	NeedsChangesRequested = "changes_requested"
	NeedsConflict         = "conflict"
	WhoAnyoneWithRepo     = "anyone_with_repo"
	WhoJobPerson          = "job_person"
	QuestionText          = "text"
	AskYesNo              = "yes_no"
	AskOptions            = "options"
	ChoiceContinue        = "continue"
	ChoiceTryOnceMore     = "try_once_more"
	ChoiceSendBack        = "send_back"
	ChoiceCancel          = "cancel_job"
	CodeToolError         = "tool_error"
	CodeProviderError     = "provider_error"
	CodeCrash             = "crash"
	CodeMachineLost       = "machine_lost"
	// CodeStopped is a person's stop that cut a step, by the step's own Stop
	// or the whole job's: the job and that step both say it was stopped.
	CodeStopped     = "stopped"
	FailureRequeued = "requeued"
	FailureStopped  = "stopped"
	// RoundLinesMost is how many per-round lines one step keeps. A loop's
	// own max is the bound when it is smaller. The list cannot grow with a
	// report that names a huge round.
	RoundLinesMost     = 32
	LinkExpired        = "expired"
	LinkRevoked        = "revoked"
	LinkUnknown        = "unknown"
	ReasonNotJobPerson = "not_job_person"
	AskApp             = "app"
	AskTUI             = "tui"
	CurrencyUSD        = "USD"
)

// NeedsYouKinds is the whole enum, in the order the queue groups by.
var NeedsYouKinds = []string{
	NeedsApproveStart,
	NeedsApprovePlan,
	NeedsApproveProof,
	NeedsQuestion,
	NeedsSpendingLimit,
	NeedsOutOfAttempts,
	NeedsPublishFailed,
	NeedsRunnerOffline,
	NeedsFailed,
	NeedsChangesRequested,
	NeedsConflict,
}

// OutOfAttemptOptions are the only answers when a step used its rounds
// without its until holding. try_once_more runs one more round, send_back
// returns to the previous step with a reason, and cancel_job stops the job.
var OutOfAttemptOptions = []string{ChoiceTryOnceMore, ChoiceSendBack, ChoiceCancel}

// JobAsk is the person's own words for a job that did not start from a
// GitHub issue. A job that did keeps the issue on the wire and leaves this
// empty, so the two cannot be stored twice.
type JobAsk struct {
	Text    string     `json:",omitempty"`
	By      string     `json:",omitempty"`
	At      time.Time  `json:",omitempty"`
	Source  string     `json:",omitempty"` // app, tui or hand-off
	FromRef *SourceRef `json:",omitempty"`
}

// SourceRef links a hand-off back to its originating agent.
type SourceRef struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Failure is a step that errored. It is not a question and it does not wait
// for an answer. Next is requeued when the machine was lost, and stopped
// when the step itself errored. Attempts is how many tries that step had
// already made. Machine is the one that was holding the work.
type Failure struct {
	Step     string    `json:",omitempty"`
	Code     string    `json:",omitempty"`
	Detail   string    `json:",omitempty"`
	Attempts int       `json:",omitempty"`
	Machine  string    `json:",omitempty"`
	Next     string    `json:",omitempty"`
	At       time.Time `json:",omitempty"`
}

// RoundLine is one finished try of a loop step. The step keeps one line per
// round, bounded by RoundCap, so out of attempts can show what each try did.
//
// Started and Chat are when that try began and the conversation it ran in, so
// a surface can draw and open an earlier round as it does the latest. A line
// written before they were recorded has neither, and Chat is empty for a step
// that is not a conversation (a check).
type RoundLine struct {
	// Rejected means the round ended without meeting its completion rule.
	// Absent on older records, whose outcome must not be inferred from prose.
	Rejected bool      `json:",omitempty"`
	Round    int       `json:",omitempty"`
	Summary  string    `json:",omitempty"`
	At       time.Time `json:",omitempty"`
	Started  time.Time `json:",omitempty"`
	Chat     string    `json:",omitempty"`
}

// Acts is who may use each verb on this job. It is computed for one caller.
// The job's person may approve, run again, cancel the job and retry
// publishing. A plain question and a reply are anyone who can see the repo.
// Spending and out of attempts are the job's person, and that is Answer.
// AssignToMe is an unassigned job. A job with no person yet is open to the
// caller for the job-person verbs.
type Acts struct {
	Approve      bool
	RunAgain     bool
	CancelJob    bool
	RetryPublish bool
	Answer       bool
	AssignToMe   bool
	Reply        bool
}

// NextJobCap is the ceiling a continue answer allows. THE NUMBER COMES FROM
// codeaf, NEVER FROM THE APP: it is this job's cap doubled. The TUI's cap
// ladder is a different control and is not this figure.
func NextJobCap(cap float64) float64 {
	if cap <= 0 {
		return 0
	}
	return cap * 2
}

// ContinueJob raises this job's cap to NextJobCap when the spend has already
// reached it. It assigns the next ceiling, so a second caller that already
// sees the raised cap leaves it alone.
func ContinueJob(it *Item) bool {
	if it == nil || it.Cap <= 0 || it.Stream == nil || it.Stream.Spent < it.Cap {
		return false
	}
	it.Cap = NextJobCap(it.Cap)
	return true
}

// IsContinue is the spending answer: the word continue, or yes with no other
// words. Stop and cancel do not raise the cap.
func IsContinue(yes bool, words string) bool {
	switch strings.ToLower(strings.TrimSpace(words)) {
	case ChoiceContinue, "yes":
		return true
	case "stop", "no", ChoiceCancel:
		return false
	default:
		return yes && strings.TrimSpace(words) == ""
	}
}

// OutOfAttemptsChoice reads a person's answer to a step that used its rounds.
// yes, or the word try_once_more, runs one more round. send_back returns to
// the previous step. Anything else cancels the job.
func OutOfAttemptsChoice(yes bool, words string) string {
	w := strings.ToLower(strings.TrimSpace(words))
	switch {
	case w == ChoiceTryOnceMore || w == "yes" || (yes && (w == "" || w == "yes")):
		return ChoiceTryOnceMore
	case w == ChoiceSendBack || strings.HasPrefix(w, ChoiceSendBack+":"):
		return ChoiceSendBack
	default:
		return ChoiceCancel
	}
}

// SendBackReason is the text after send_back:, or the words themselves when
// they are already only the reason.
func SendBackReason(words string) string {
	w := strings.TrimSpace(words)
	low := strings.ToLower(w)
	if strings.HasPrefix(low, ChoiceSendBack) {
		if i := strings.Index(w, ":"); i >= 0 {
			return strings.TrimSpace(w[i+1:])
		}
		return ""
	}
	return w
}

// NeedsKind is the wire kind for a job that is waiting, or "" when it is not.
// A publish that failed stays landed, and that kind wins over proof approval
// so the same commit is not approved twice.
func NeedsKind(it Item) string {
	if JobState(it) != "needs_you" {
		return ""
	}
	if it.PublishError != "" || (it.PublishStatus != nil && it.PublishStatus.State == "failed") {
		return NeedsPublishFailed
	}
	switch {
	case it.Failure != nil:
		if it.Failure.Code == CodeMachineLost {
			return NeedsRunnerOffline
		}
		return NeedsFailed
	case it.PullRequest != nil && it.PullRequest.Conflict:
		return NeedsConflict
	case it.PullRequest != nil && it.PullRequest.ReviewDecision == "changes_requested":
		return NeedsChangesRequested
	case it.QKind == QKindAskFirst:
		return NeedsApproveStart
	case it.State == StateLanded:
		return NeedsApproveProof
	case it.QKind == NeedsRunnerOffline:
		return NeedsRunnerOffline
	case it.QKind == QKindApprove:
		return NeedsApprovePlan
	case it.QKind == "cap":
		return NeedsSpendingLimit
	case it.QKind == QKindOutOfAttempts:
		return NeedsOutOfAttempts
	case it.Question != "" || it.Asking != nil:
		return NeedsQuestion
	default:
		return ""
	}
}

// publishFailed is a landed publish the person can retry. Either the first
// line on the job or publishing.state failed is enough; both name the same wait.
func publishFailed(it Item) bool {
	if it.State != StateLanded && it.State != StateNeedsYou {
		return false
	}
	if it.PublishError != "" {
		return true
	}
	return it.PublishStatus != nil && it.PublishStatus.State == "failed"
}

// JobPersonOnly is true for every needs-you kind except a free question.
// A free question is anyone who can see the repository.
func JobPersonOnly(kind string) bool {
	switch kind {
	case NeedsApproveStart, NeedsApprovePlan, NeedsApproveProof, NeedsSpendingLimit, NeedsOutOfAttempts, NeedsPublishFailed, NeedsRunnerOffline, NeedsFailed, NeedsChangesRequested, NeedsConflict:
		return true
	default:
		return false
	}
}

// WhoCanAnswer is the wire's who_can_answer for a needs-you kind.
func WhoCanAnswer(kind string) string {
	if JobPersonOnly(kind) {
		return WhoJobPerson
	}
	if kind == NeedsQuestion {
		return WhoAnyoneWithRepo
	}
	return ""
}

// QuestionForm is the question object for a free question or an
// out-of-attempts ask. Other needs-you kinds have no question object.
func QuestionForm(it Item) (kind string, options []string, who string) {
	switch NeedsKind(it) {
	case NeedsRunnerOffline:
		return AskOptions, []string{ChoiceTryOnceMore, ChoiceCancel}, WhoJobPerson
	case NeedsOutOfAttempts:
		return AskOptions, append([]string(nil), OutOfAttemptOptions...), WhoJobPerson
	case NeedsQuestion:
		who = WhoAnyoneWithRepo
		var opts []string
		if it.Asking != nil {
			opts = it.Asking.Options
		}
		if len(opts) > 0 {
			if yesNoOptions(opts) {
				return AskYesNo, append([]string(nil), opts...), who
			}
			return AskOptions, append([]string(nil), opts...), who
		}
		if it.QKind == QKindAskFirst {
			return AskYesNo, []string{"yes", "no"}, who
		}
		return QuestionText, nil, who
	default:
		return "", nil, ""
	}
}

func yesNoOptions(options []string) bool {
	if len(options) != 2 {
		return false
	}
	a := strings.ToLower(strings.TrimSpace(options[0]))
	b := strings.ToLower(strings.TrimSpace(options[1]))
	return (a == "yes" && b == "no") || (a == "no" && b == "yes")
}

// NeedsSince is when this wait started: the landed stream's end, the ask's
// clock, the current step's clock, then the job's own stamps.
func NeedsSince(it Item) time.Time {
	if it.Failure != nil && !it.Failure.At.IsZero() {
		return it.Failure.At
	}
	if it.State == StateLanded && it.Stream != nil && !it.Stream.Ended.IsZero() {
		return it.Stream.Ended
	}
	if it.Asking != nil && !it.Asking.At.IsZero() {
		return it.Asking.At
	}
	if it.Stream != nil && it.Stream.Cur >= 0 && it.Stream.Cur < len(it.Stream.Phases) && !it.Stream.Phases[it.Stream.Cur].At.IsZero() {
		return it.Stream.Phases[it.Stream.Cur].At
	}
	if !it.Changed.IsZero() {
		return it.Changed
	}
	return it.Created
}

// GitHubIssue is a job that started from a GitHub issue. A number alone is
// not enough: work typed on the floor can carry a number and still have no
// issue page.
func GitHubIssue(it Item) bool {
	return it.Kind == KindIssue && it.Num > 0 && strings.TrimSpace(it.URL) != "" && it.Origin == OriginForge
}

// AskText is the person's words for the pull request body: the stored ask,
// or the issue body when the job came from GitHub.
func AskText(it Item) string {
	if it.Ask != nil {
		if text := strings.TrimSpace(it.Ask.Text); text != "" {
			return text
		}
	}
	if GitHubIssue(it) {
		return strings.TrimSpace(it.Body)
	}
	return ""
}

// ClassifyFailure reads a step error. A lost machine is not this function:
// the lease writes that code when a hold expires.
func ClassifyFailure(msg string) (code, detail string) {
	detail = firstLine(msg)
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "panic") || strings.Contains(low, "crash"):
		code = CodeCrash
	case strings.Contains(low, "provider") || strings.Contains(low, "rate limit"):
		code = CodeProviderError
	default:
		code = CodeToolError
	}
	return code, detail
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

// RoundSummary is the one line a finished round leaves on the step, which is
// what the Timeline shows of it: `reported for the step · …` when the factory
// made the report ([StageResult.Inferred]), and `… · updated 2 times` when the
// step reported again after its first report, so a person can see that the
// step corrected itself and that what they read is its last word.
func RoundSummary(res StageResult) string {
	line := roundWords(res)
	switch {
	case res.Inferred:
		return ReportedForTheStep + " · " + line
	case res.Reports == 2:
		return line + " · updated 1 time"
	case res.Reports > 2:
		return line + " · updated " + strconv.Itoa(res.Reports-1) + " times"
	}
	return line
}

// ReportedForTheStep is how a report the factory made for a step is marked
// wherever a person reads it.
const ReportedForTheStep = "reported for the step"

// roundWords is what a round said, in one line.
func roundWords(res StageResult) string {
	if line := firstLine(res.Output); line != "" {
		return line
	}
	switch {
	case res.Findings == 1:
		return "1 finding"
	case res.Findings > 1:
		return strconv.Itoa(res.Findings) + " findings"
	case res.Exit != 0:
		return "exit " + strconv.Itoa(res.Exit)
	case res.Done:
		return "done"
	default:
		return "no result"
	}
}

// RoundCap is how many round lines a step may keep: its max, or the round
// it has been allowed to reach when that is higher, and never more than
// RoundLinesMost.
func RoundCap(max, round int) int {
	if max < 1 {
		max = 1
	}
	if round > max {
		max = round
	}
	if max > RoundLinesMost {
		return RoundLinesMost
	}
	return max
}

// NoteRound records one finished try. A repeat of the same round replaces
// the earlier line. The list stays within cap.
func NoteRound(ph *Phase, cap int, line RoundLine) {
	if ph == nil || line.Round < 1 {
		return
	}
	if cap < 1 {
		cap = 1
	}
	replaced := false
	for i := range ph.Rounds {
		if ph.Rounds[i].Round == line.Round {
			ph.Rounds[i] = line
			replaced = true
			break
		}
	}
	if !replaced {
		ph.Rounds = append(ph.Rounds, line)
	}
	if len(ph.Rounds) > cap {
		ph.Rounds = append([]RoundLine(nil), ph.Rounds[len(ph.Rounds)-cap:]...)
	}
}

// NewFailure is the failure a step just had. attempts is at least 1.
func NewFailure(step, code, detail, machine, next string, attempts int, at time.Time) Failure {
	if attempts < 1 {
		attempts = 1
	}
	return Failure{Step: step, Code: code, Detail: detail, Attempts: attempts, Machine: machine, Next: next, At: at}
}

// StampFailure writes the failure on the job and on the named step, when
// that step is in the run. The step's state is left as it was: a requeued
// step is still the one a worker continues, and marking it failed would
// skip it.
func StampFailure(it *Item, f Failure) {
	if it == nil {
		return
	}
	cp := f
	it.Failure = &cp
	if it.Stream == nil {
		return
	}
	for i := range it.Stream.Phases {
		if it.Stream.Phases[i].Name != f.Step {
			continue
		}
		step := f
		it.Stream.Phases[i].Failure = &step
		return
	}
}

// MayAct is the who-may-act table for one caller. caller is a forge handle.
func MayAct(it Item, caller string) Acts {
	caller = NormalizeHandle(caller)
	person := it.Person()
	owner := person == "" || caller == person
	var answer bool
	switch NeedsKind(it) {
	case NeedsQuestion:
		answer = true
	case NeedsApproveStart, NeedsApprovePlan, NeedsApproveProof, NeedsSpendingLimit, NeedsOutOfAttempts, NeedsRunnerOffline:
		answer = owner
	}
	assign := person == ""
	return Acts{
		Approve: owner, RunAgain: owner, CancelJob: owner, RetryPublish: owner,
		Answer: answer, AssignToMe: assign, Reply: true,
	}
}
