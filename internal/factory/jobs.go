package factory

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// DefaultJobBudget is the pipeline default when no file supplies one.
const DefaultJobBudget = 5.0

// ApprovalUndo is the interval during which consent has no outward effect.
const ApprovalUndo = 5 * time.Second

// JobPriority records a person's choice; defaults are derived from labels.
type JobPriority struct {
	Value string `json:"value"`
	SetBy string `json:"set_by,omitempty"`
}

// Approval preserves consent across server restarts without storing credentials.
type Approval struct {
	Automatic bool      `json:"automatic,omitempty"`
	Kind      string    `json:"kind"`
	State     string    `json:"state"`
	Matched   string    `json:"matched,omitempty"`
	DecidedBy string    `json:"decided_by,omitempty"`
	At        time.Time `json:"at"`
	UndoUntil time.Time `json:"undo_until"`
	Commit    string    `json:"commit,omitempty"`
	Words     string    `json:"words,omitempty"`
	Edited    bool      `json:"edited,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// StopUndo retains the run that Stop interrupted; a resumed run gets a new lease.
type StopUndo struct {
	Failure  *Failure  `json:"failure,omitempty"`
	By       string    `json:"by"`
	Until    time.Time `json:"until"`
	State    State     `json:"state"`
	Question string    `json:"question,omitempty"`
	QKind    string    `json:"qkind,omitempty"`
	Stream   *Stream   `json:"stream,omitempty"`
}

// JobRef is display text. A local job id is never a GitHub issue number.
func JobRef(it Item) string {
	if it.Num == 0 {
		return fmt.Sprintf("job %d", it.ID)
	}
	return JobKey(it)
}

// JobKey preserves the reference accepted by existing clients and saved links.
// It is an opaque identity, never display text or a GitHub issue number.
func JobKey(it Item) string {
	repo := it.Repo
	if n := strings.LastIndex(repo, "/"); n >= 0 {
		repo = repo[n+1:]
	}
	number := it.Num
	if number == 0 {
		number = it.ID
	}
	return fmt.Sprintf("%s#%d", repo, number)
}

func ValidPriority(value string) bool { return PriorityShort(value) != "" }

// priorityLevels is THE ONE TABLE of priority spellings, most urgent first:
// the value a person's mark keeps, the priority:* label and the p0..p3 short
// form. A level's position is its place in the queue ([PriorityRank]).
var priorityLevels = []struct{ value, label, short string }{
	{"urgent", "priority:urgent", "p0"}, {"high", "priority:high", "p1"},
	{"medium", "priority:medium", "p2"}, {"low", "priority:low", "p3"},
}

// PriorityFor never consults AI ranking: a person's persisted mark wins over
// priority:* and p0..p3 issue labels. A "bug" label is a type, not a priority,
// so it yields no mark: priority is only ever something a person chose, or a
// routing rule the operator wrote chose for them.
func PriorityFor(it Item) *JobPriority {
	if it.Priority != nil {
		p := *it.Priority
		return &p
	}
	labels := make(map[string]bool)
	for _, label := range it.Labels {
		labels[strings.ToLower(strings.TrimSpace(label))] = true
	}
	for _, p := range priorityLevels {
		if labels[p.label] || labels[p.short] {
			return &JobPriority{Value: p.value}
		}
	}
	return nil
}

// PriorityShort is the p0..p3 spelling of a priority value, "" for none.
func PriorityShort(value string) string {
	for _, p := range priorityLevels {
		if p.value == value {
			return p.short
		}
	}
	return ""
}

// PriorityFromShort is the value a p0..p3 spelling names, false for anything
// else.
func PriorityFromShort(short string) (string, bool) {
	for _, p := range priorityLevels {
		if p.short == short {
			return p.value, true
		}
	}
	return "", false
}

// PriorityRank is the job's place in the queue by priority alone: 0 for urgent
// (p0) through 3 for low (p3), and 4 for a job with no priority, which runs
// after every job that has one.
func PriorityRank(it Item) int {
	if p := PriorityFor(it); p != nil {
		for i, level := range priorityLevels {
			if level.value == p.Value {
				return i
			}
		}
	}
	return len(priorityLevels)
}

// JobSize comes only from the issue's explicit size labels: "size:m" or
// "size/m", any case. The largest wins when several are present, so a second
// label can never make a job look smaller than one a person already put on it.
// XS..XL are all accepted; this is the ONE resolver every start decision and
// the wire share. A model's estimate or the body's length is never read here.
func JobSize(it Item) string {
	best, got := 0, ""
	for _, label := range it.Labels {
		label = strings.ToLower(strings.TrimSpace(label))
		rest, ok := strings.CutPrefix(label, "size:")
		if !ok {
			rest, ok = strings.CutPrefix(label, "size/")
		}
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rank := slices.Index(jobSizes, rest) + 1; rank > best {
			best, got = rank, strings.ToUpper(rest)
		}
	}
	return got
}

var jobSizes = []string{"xs", "s", "m", "l", "xl"}

// The words ShownSize reports for where a size came from.
const (
	ShownLabel = "label"
	ShownRead  = "read"
	ShownDiff  = "diff"
)

// ShownSize is the ONE resolver every DISPLAY or FILTER of an item's size
// reads (`size:`, `risky`, `when: large`, the manager's turn, the chat floor),
// and it answers (letter, source). A size label a person set wins; else a
// forge's measured diff (SizeFrom "diff"); else a completed read's size
// (SizeFrom "read" with the item triaged). Anything else, a legacy document
// with no provenance, a chat guess, a body's length, is UNKNOWN and comes back
// as ("", "") so the surface draws nothing (the emptiness law).
//
// IT NEVER STARTS ANYTHING. [JobSize] stays the only size a start decision,
// the dry run or the wire's job.size reads; no read can grant a start.
func ShownSize(it Item) (letter, from string) {
	if s := JobSize(it); s != "" {
		return s, ShownLabel
	}
	s := strings.ToUpper(strings.TrimSpace(it.Triage.Size))
	if s != "S" && s != "M" && s != "L" {
		return "", ""
	}
	switch it.Triage.SizeFrom {
	case SizeFromDiff:
		if it.Kind == KindPR {
			return s, ShownDiff
		}
	case SizeFromRead:
		if !it.Triage.TriagedAt.IsZero() {
			return s, ShownRead
		}
	}
	return "", ""
}

func IsDismissed(it Item, person string) bool {
	person = NormalizeHandle(person)
	return person != "" && slices.Contains(it.DismissedBy, person)
}

// JobState projects historical engine states into the five canvas states.
func JobState(it Item) string {
	if it.PullRequest != nil && (it.PullRequest.State == PRMerged || it.PullRequest.State == PRClosed) {
		return "done"
	}
	if it.DoneHow != "" || it.State == StateDismissed {
		return "done"
	}
	// A JOB A PERSON STOPPED ASKS NOTHING OF ANYONE. It used to read
	// needs_you, so every Stop (and every Reject that ended as one) added a
	// row and a count to Needs you for a decision the person had already
	// made. It waits in Backlog, with its stop on it, for that person's
	// Carry on, Run again or Close.
	if StoppedByPerson(it) {
		return "backlog"
	}
	if it.FollowUp != nil && it.FollowUp.State == "needs_you" && it.PullRequest != nil && it.PullRequest.State == PROpen {
		return "needs_you"
	}
	if it.PublishError != "" || it.Failure != nil || (it.PublishStatus != nil && it.PublishStatus.State == "failed") {
		return "needs_you"
	}
	// A MERGE THAT FAILED IS YOURS TO LOOK AT: the gate or the forge said no,
	// and nothing tries again on its own.
	if m := it.Merging; m != nil && m.State == MergeFailed && (it.PullRequest == nil || it.PullRequest.State == PROpen) {
		return "needs_you"
	}
	if it.State == StateShipped {
		if pr := it.PullRequest; pr != nil {
			if pr.State == PRMerged || pr.State == PRClosed {
				return "done"
			}
			if it.FollowUp != nil && (it.FollowUp.State == "watching" || it.FollowUp.State == "healthy") {
				return "running"
			}
			if pr.ReviewDecision == "changes_requested" || pr.Conflict {
				return "needs_you"
			}
			return "running"
		}
		return "done"
	}
	if it.Publishing {
		return "running"
	}
	switch it.State {
	case StateNeedsYou, StateLanded:
		return "needs_you"
	case StateRunning:
		return "running"
	case StateQueued:
		return "queued"
	default:
		return "backlog"
	}
}

// DroppedRefusal is the end of the sentence every door that would start or
// change a dropped job is refused with, after the job's reference. The
// factory's HTTP door reads it to answer that refusal as a conflict, so it is
// spelled once, here.
const DroppedRefusal = "was dropped · Bring back returns it to Backlog"

// RefuseDropped is that refusal for a dropped job, and nil for any other.
//
// A DROPPED JOB IS REFUSED WITH ITS WAY BACK. Bring back is the one door out
// of a drop, so the refusal names it rather than calling the drop final: an
// agent that relays this sentence to a person must not tell them the job can
// never run again. AND NO DOOR CLAIMS A CHANGE THAT CANNOT TAKE EFFECT: the
// drop deleted the job's conversations, nothing of it runs, and Bring back
// starts it again from its repository's recipe, so a step edit, a note or a
// budget let through here would be a change nobody ever reads.
func RefuseDropped(it Item) error {
	if it.DoneHow == "closed_locally" {
		return fmt.Errorf("%s is closed locally · Reopen keeps its work and spending", it.Ref())
	}
	if it.DoneHow != "dropped" {
		return nil
	}
	return fmt.Errorf("%s %s", it.Ref(), DroppedRefusal)
}

// EverRan says the job has been started and something of that run is still
// on it: a step that began, a run that ended, a failure, or a step changed
// while it ran. A person's Run again or Bring back clears the run first, so a
// job they restarted reads as never run until it starts once more.
//
// AN INTAKE RULE STARTS ONLY A JOB THAT NEVER RAN. A run that failed or was
// stopped leaves the job new, which is the state a rule's start: auto picks
// up; read on state alone, the rule started the whole run again by itself,
// seconds after it failed and once more at the next engine start. Such a job
// waits in Needs you for a person's Run again, whatever its steps' kind and
// whatever rule matched it.
func EverRan(it Item) bool {
	if it.Failure != nil || len(it.Adapted) > 0 {
		return true
	}
	s := it.Stream
	if s == nil {
		return false
	}
	if !s.Ended.IsZero() {
		return true
	}
	for _, ph := range s.Phases {
		if ph.Round > 0 || ph.State != PhasePending {
			return true
		}
	}
	return false
}

// RunEnded says the job's last run is over and says why, a step's failure or
// a person's stop, and nothing has started it again. Such a job has nothing
// to stop: it waits for a person to start it, run it again or drop it.
func RunEnded(it Item) bool { return StoppedByPerson(it) || it.State == StateNew && it.Failure != nil }

func JobDoneHow(it Item) string {
	if it.DoneHow != "" {
		return it.DoneHow
	}
	if it.State == StateDismissed {
		return "dropped"
	}
	if it.State == StateShipped {
		if it.PullRequest == nil {
			return "answered"
		}
		switch it.PullRequest.State {
		case PRMerged:
			return "merged"
		case PRClosed:
			return "closed"
		}
	}
	return ""
}

// JobReason is a finite vocabulary for Needs you and Running.
func JobReason(it Item) string {
	if PausedDecision(it) {
		return "decision_paused"
	}
	if JobState(it) == "needs_you" {
		if RecoveryPaused(it) {
			return "paused"
		}
		if it.PublishError != "" || (it.PublishStatus != nil && it.PublishStatus.State == "failed") {
			return "publish_failed"
		}
		if m := it.Merging; m != nil && m.State == MergeFailed {
			return "merge_failed"
		}
		if it.Failure != nil {
			if it.Failure.Code == CodeMachineLost {
				return "runner_offline"
			}
			return "failed"
		}
		if it.PullRequest != nil && it.PullRequest.Conflict {
			return "conflict"
		}
		if it.PullRequest != nil && it.PullRequest.ReviewDecision == "changes_requested" {
			return "changes_requested"
		}
		if it.QKind == QKindAskFirst {
			return "approval_start"
		}
		switch NeedsKind(it) {
		case NeedsRunnerOffline:
			return "runner_offline"
		case NeedsApprovePlan:
			return "approval_plan"
		case NeedsApproveStep:
			return "approval_step"
		case NeedsApproveProof:
			return "approval_proof"
		case NeedsSpendingLimit:
			return "budget"
		case NeedsOutOfAttempts:
			return "out_of_rounds"
		case NeedsPublishFailed:
			return "publish_failed"
		default:
			return "question"
		}
	}
	if JobState(it) == "running" {
		if it.Publishing {
			return "publishing"
		}
		if m := it.Merging; m != nil && (m.State == MergeQueued || m.State == MergeMerging) {
			return "merging"
		}
		if it.PullRequest != nil {
			return "github"
		}
		return "step"
	}
	return ""
}

// JobApprovalSummary keeps the latest settled decision visible while a later
// gate waits. JobApproval separately decides whether that gate needs consent.
func JobApprovalSummary(it Item) *Approval {
	if it.Approval != nil && it.Approval.State == "approved" {
		a := *it.Approval
		return &a
	}
	return JobApproval(it)
}

func JobApproval(it Item) *Approval {
	if StoppedByPerson(it) {
		return nil
	}

	kind := ""
	switch JobReason(it) {
	case "approval_start":
		kind = "start"
	case "approval_plan":
		kind = "plan"
	case "approval_step":
		kind = "step"
	case "approval_proof":
		kind = "proof"
	}
	// A plan approval that settled before the question now asked is the old
	// consent for an old question; the new ask needs its own approval.
	settledBefore := (kind == "plan" || kind == "step") && it.Approval != nil && it.Approval.Kind == kind && it.Approval.State == "approved" && it.Asking != nil && it.Asking.At.After(it.Approval.At)
	// An approval still open on a step hold from before the hold had its own
	// kind was written as a plan approval; it is still that hold's. A SETTLED
	// plan approval is an earlier gate's consent and never answers this hold.
	sameKind := it.Approval != nil && (it.Approval.Kind == kind || kind == "step" && it.Approval.Kind == "plan" && it.Approval.State != "approved")
	if it.Approval != nil && !settledBefore && (kind == "" || sameKind) {
		a := *it.Approval
		return &a
	}
	if kind == "" {
		return nil
	}
	return &Approval{Kind: kind, State: "waiting", Matched: it.AskedFirstReason}
}

// ReaskStart puts a job its start rules held back into the backlog, so intake
// decides it again: its labels changed, and the hold judged the old ones. Only
// a hold nobody has answered goes back; a job a person declined, approved or
// ran stays where they put it. It reports whether it changed the job.
func ReaskStart(it *Item) bool {
	if it.State != StateNeedsYou || it.QKind != QKindAskFirst || it.AskedFirstReason == "" || it.Stream != nil || PausedDecision(*it) {
		return false
	}
	if it.Approval != nil && it.Approval.State != "waiting" {
		return false
	}
	it.State, it.QKind, it.Question, it.AskedFirstReason = StateNew, "", "", ""
	if it.RuleChecks != nil {
		it.RuleChecks.Start = ""
	}
	return true
}

// Overlaps is derived from actual branch changes, never from planned paths.
func Overlaps(a, b Item) bool {
	if a.ID == b.ID || a.Repo != b.Repo || a.Product != b.Product || JobState(a) != "running" || JobState(b) != "running" {
		return false
	}
	files := map[string]bool{}
	for _, f := range a.Files {
		files[f.Path] = true
	}
	if a.Change != nil {
		for _, f := range a.Change.Files {
			files[f.Path] = true
		}
	}
	for _, f := range b.Files {
		if files[f.Path] {
			return true
		}
	}
	if b.Change != nil {
		for _, f := range b.Change.Files {
			if files[f.Path] {
				return true
			}
		}
	}
	return false
}

// CanRunAsNew says the work has ended, so a person may start its request anew.
func CanRunAsNew(it Item) bool {
	return RunEnded(it) || JobState(it) == "done" || runCut(it)
}

// runCut says a change of assignee or an unassign stopped the job's run and
// nothing has started it again: the job is new, its run ended, and the step
// the stop cut says it was stopped. The job carries no failure of its own
// (the reset hands it on as nobody's failure), so [RunEnded] does not say it.
//
// A HANDED-ON JOB STILL OFFERS RUN AS NEW. It did while the reset left its
// stop on the job as a failure; when the reset stopped doing that, Run as new
// went with it, for a run that had ended all the same.
func runCut(it Item) bool {
	s := it.Stream
	if it.State != StateNew || s == nil || s.Ended.IsZero() || s.Cur < 0 || s.Cur >= len(s.Phases) {
		return false
	}
	f := s.Phases[s.Cur].Failure
	return f != nil && f.Code == CodeStopped
}
