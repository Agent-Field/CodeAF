package factory

import (
	"strings"
	"time"
)

// Needs-you kinds on the wire. ADDING A KIND IS A DELIBERATE CHANGE: add it
// here and to TestNeedsYouKindsAreTheSix in the same commit. The queue groups
// jobs by this list, in this order.
const (
	NeedsApprovePlan   = "approve_plan"
	NeedsApproveProof  = "approve_proof"
	NeedsQuestion      = "question"
	NeedsSpendingLimit = "spending_limit"
	NeedsOutOfAttempts = "out_of_attempts"
	NeedsPublishFailed = "publish_failed"
	QKindOutOfAttempts = NeedsOutOfAttempts
	WhoAnyoneWithRepo  = "anyone_with_repo"
	WhoJobPerson       = "job_person"
	QuestionText       = "text"
	AskYesNo           = "yes_no"
	AskOptions         = "options"
	ChoiceContinue     = "continue"
	ChoiceTryOnceMore  = "try_once_more"
	ChoiceSendBack     = "send_back"
	ChoiceCancel       = "cancel_job"
	CodeToolError      = "tool_error"
	CodeProviderError  = "provider_error"
	CodeCrash          = "crash"
	CodeMachineLost    = "machine_lost"
	FailureRequeued    = "requeued"
	FailureStopped     = "stopped"
	AskApp             = "app"
	AskTUI             = "tui"
	CurrencyUSD        = "USD"
)

// NeedsYouKinds is the whole enum, in the order the queue groups by.
var NeedsYouKinds = []string{
	NeedsApprovePlan,
	NeedsApproveProof,
	NeedsQuestion,
	NeedsSpendingLimit,
	NeedsOutOfAttempts,
	NeedsPublishFailed,
}

// OutOfAttemptOptions are the only answers when a step used its rounds
// without its until holding. try_once_more runs one more round, send_back
// returns to the previous step with a reason, and cancel_job stops the job.
var OutOfAttemptOptions = []string{ChoiceTryOnceMore, ChoiceSendBack, ChoiceCancel}

// JobAsk is the person's own words for a job that did not start from a
// GitHub issue. A job that did keeps the issue on the wire and leaves this
// empty, so the two cannot be stored twice.
type JobAsk struct {
	Text   string    `json:",omitempty"`
	By     string    `json:",omitempty"`
	At     time.Time `json:",omitempty"`
	Source string    `json:",omitempty"` // app or tui
}

// Failure is a step that errored. It is not a question and it does not wait
// for an answer. Next is requeued when the machine was lost, and stopped
// when the step itself errored.
type Failure struct {
	Step   string    `json:",omitempty"`
	Code   string    `json:",omitempty"`
	Detail string    `json:",omitempty"`
	Next   string    `json:",omitempty"`
	At     time.Time `json:",omitempty"`
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
	waiting := it.State == StateNeedsYou || it.State == StateLanded
	if !waiting {
		return ""
	}
	if publishFailed(it) {
		return NeedsPublishFailed
	}
	switch {
	case it.State == StateLanded:
		return NeedsApproveProof
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
	case NeedsApprovePlan, NeedsApproveProof, NeedsSpendingLimit, NeedsOutOfAttempts, NeedsPublishFailed:
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
