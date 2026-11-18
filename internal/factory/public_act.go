package factory

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ── PUBLIC ACTS ─────────────────────────────────────────────────────────────
//
// A public act is anything a job does on its own pull request that leaves
// evidence outside this computer: an edit of its title or description, a
// change of its base, a reply in a review thread, a comment, resolving a
// thread, asking for a review again, running failed checks again, marking it
// ready. The job's manager proposes them in any state of the job; the service
// carries them out, one at a time, with the job's person's GitHub sign-in.
//
// WHO SAYS YES IS DECIDED HERE, IN CODE, NEVER IN A PROMPT. In Manual an act
// the manager proposes waits for the person (state proposed, and the job
// reads "you: approve_acts"). In Auto it is approved at once. An act the
// person asks for themselves is theirs. One yes covers every act that waits.
//
// This file is the rules: what an act may be, who may approve it, what the
// set reads as in a line, and what becomes of an act when the job moves under
// it. It writes nothing outward; internal/factory/serve carries acts out.

// The kinds of public act.
const (
	ActKindEditPR          = "edit_pr"
	ActKindChangeBase      = "change_base"
	ActKindReply           = "reply"
	ActKindComment         = "comment"
	ActKindRerunChecks     = "rerun_checks"
	ActKindResolveThread   = "resolve_thread"
	ActKindRerequestReview = "rerequest_review"
	ActKindMarkReady       = "mark_ready"
	// Push, open the pull request and merge are public acts with a road of
	// their own: the publish approval and the merge card. They are never
	// stored as acts ([PushRoad], [OfferMerge]).
	ActKindPush   = "push"
	ActKindOpenPR = "open_pr"
	ActKindMerge  = "merge"
)

// The states of a stored act.
const (
	// ActProposed waits for the person's yes.
	ActProposed = "proposed"
	// ActApproved is carried out on the service's next pass.
	ActApproved = "approved"
	// ActRunning is being sent now. An act found running by a later pass was
	// cut off mid-send and is never sent again.
	ActRunning  = "running"
	ActDone     = "done"
	ActFailed   = "failed"
	ActDeclined = "declined"
)

// Who proposed an act. The service cannot tell the manager from the person
// on a factory with one credential, so the trusted caller says which.
const (
	ActByMainAgent = "main_agent"
	ActByYou       = "you"
)

// PublicActFooter ends every reply and comment a job posts in its person's
// name, so a reader on GitHub knows an agent wrote it.
const PublicActFooter = "Posted by a WorkAF factory agent."

// The limits of the list a job keeps.
const (
	// ActsAtOnce is how many acts one proposal may carry, and ActsWaiting how
	// many may wait for the person at once.
	ActsAtOnce  = 20
	ActsWaiting = 30
	// ActsKept is how many finished acts stay on the job.
	ActsKept = 20
	// ActTextMost is the longest reply, comment or description, under the
	// forge's own limit; ActTitleMost the longest title.
	ActTextMost  = 60000
	ActTitleMost = 256
	// ActTriesMost is how many times an act is tried while GitHub does not
	// answer before it fails.
	ActTriesMost = 5
)

// The words an act fails with when it could not be sent again safely.
const (
	// ActCutOff is an act found running by a later pass.
	ActCutOff = "The factory restarted while this was being sent; check the pull request before trying again."
	// ActPushGone is an act that waited for a push that never went out.
	ActPushGone = "The push this was meant to follow did not happen, so it was not sent."
)

// PublicAct is one public act on a job's pull request.
type PublicAct struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Title and Body are an edit's new title and description, Base a change
	// of base's new base branch, Thread the feedback item a reply answers or
	// a resolve closes (as the job stores it: `inline:<comment id>`), Text a
	// reply's or a comment's words exactly as they will be posted, and
	// Reviewers who a review is asked of again.
	Title     string   `json:"title,omitempty"`
	Body      string   `json:"body,omitempty"`
	Base      string   `json:"base,omitempty"`
	Thread    string   `json:"thread,omitempty"`
	Text      string   `json:"text,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
	// By is who proposed it ([ActByMainAgent], [ActByYou]), Why their reason
	// and At when.
	By    string    `json:"by"`
	Why   string    `json:"why,omitempty"`
	At    time.Time `json:"at"`
	State string    `json:"state"`
	// ApprovedBy is who said yes: the person's handle, or the manager in Auto.
	ApprovedBy string `json:"approved_by,omitempty"`
	// Error is why a failed act failed; Result what a finished one did, in
	// words, and for a declined one the person's words; URL where it landed.
	Error  string    `json:"error,omitempty"`
	Result string    `json:"result,omitempty"`
	URL    string    `json:"url,omitempty"`
	DoneAt time.Time `json:"done_at,omitzero"`
	// AfterPush is the commit whose push the act waits for: it was approved
	// together with that push, and runs once it has landed. The write that
	// records the publish lets it go ([Item.MarkPublishing]). WithPush says
	// the yes it has is the push approval's own.
	AfterPush string `json:"after_push,omitempty"`
	WithPush  bool   `json:"with_push,omitempty"`
	// Tries is how many times it was tried while GitHub did not answer, and
	// NextAt when it is tried again.
	Tries  int       `json:"tries,omitempty"`
	NextAt time.Time `json:"next_at,omitzero"`
}

// Finished says the act is over: sent, failed or declined.
func (a PublicAct) Finished() bool {
	return a.State == ActDone || a.State == ActFailed || a.State == ActDeclined
}

// ActRefusal is a proposal the job does not take. Invalid says the act itself
// is wrong (its content); otherwise the job's state refuses it.
type ActRefusal struct {
	Words   string
	Invalid bool
}

func (e *ActRefusal) Error() string { return e.Words }

func actRefused(format string, args ...any) *ActRefusal {
	return &ActRefusal{Words: fmt.Sprintf(format, args...)}
}

func actInvalid(format string, args ...any) *ActRefusal {
	return &ActRefusal{Words: fmt.Sprintf(format, args...), Invalid: true}
}

// ActsRefusal says why the job takes no public act now, or nil: it is
// finished or dropped, its person stopped it, or they put a decision away.
// THE STEP THE JOB IS ON IS NEVER A REASON.
func ActsRefusal(it Item) error {
	switch {
	case it.DoneHow == "dropped" || it.State == StateDismissed:
		return actRefused("This job was dropped, so nothing is posted or changed for it.")
	case JobState(it) == "done":
		return actRefused("This job is finished, so nothing more is posted or changed for it.")
	case StoppedByPerson(it):
		return actRefused("The person stopped this job. Nothing is posted or changed until they start it again.")
	case RecoveryPaused(it):
		return actRefused("%s", firstNonEmpty(DecisionPauseDetail(it), "The person put this job's decision away. Resume it first."))
	}
	return nil
}

// ActsBudgetRefusal says the job's budget is reached, or nil. It stops a new
// proposal; an act already approved costs nothing and is still carried out.
func ActsBudgetRefusal(it Item) error {
	if budgetLeft(it) {
		return nil
	}
	return actRefused("The budget is reached. Raise it before anything more is posted or changed.")
}

// OwnOpenPR says why the job has no open pull request of its own to act on,
// or nil. A job that reviews someone else's pull request never acts on it.
func OwnOpenPR(it Item) error {
	pr := it.PullRequest
	switch {
	case it.Kind == KindPR:
		return actRefused("This job did not open this pull request, so it posts and changes nothing on it.")
	case it.Publish != "pr" || pr == nil || pr.Number <= 0:
		return actRefused("This job has no pull request yet.")
	case pr.State == "":
		return actRefused("This job's pull request has not been read from GitHub yet.")
	case pr.State != PROpen:
		return actRefused("This job's pull request is %s.", pr.State)
	}
	return nil
}

// ActAuthority is the state a new act by starts in on the job: approved when
// by may have it carried out now, proposed when it waits for the person.
func ActAuthority(it Item, by string) string {
	if by == ActByYou || Automatic(it) {
		return ActApproved
	}
	return ActProposed
}

// The answers of [PushRoad].
const (
	// RoadPress is the publish approval pressed now.
	RoadPress = "press"
	// RoadWaiting is the approval left where it is, waiting on its person.
	RoadWaiting = "waiting"
)

// PushRoad answers a proposal to push, or to open the pull request, which is
// the same act: the publish approval of checked work. With an approval
// waiting, the person's own ask presses it, and so does the manager's in
// Auto; in Manual it is already waiting on the person, and an approval the
// pipeline or a rule keeps for a named person stays theirs in both modes.
// With none waiting there is nothing to push, and the proposal is refused.
func PushRoad(it Item, by string) (road, words string, err error) {
	if !OffersPublishApproval(it) {
		switch {
		case it.Publishing:
			return "", "", actRefused("A push is already under way.")
		case it.Approval != nil && it.Approval.Kind == "proof" && it.Approval.State == "approving":
			return "", "", actRefused("The push is already approved and goes out when its Undo window closes.")
		}
		return "", "", actRefused("Nothing is checked and ready to push.")
	}
	if by == ActByYou {
		return RoadPress, "", nil
	}
	if !Automatic(it) {
		return RoadWaiting, "The push approval is already waiting on the person.", nil
	}
	if who := WaitingApprover(it); who != "" {
		return RoadWaiting, "This approval waits on " + who + ", whom the pipeline names.", nil
	}
	if it.RuleHold != "" {
		return RoadWaiting, ruleHoldWords(it.RuleHold) + ". The approval is the person's.", nil
	}
	for i := len(it.Stages) - 1; i >= 0; i-- {
		if !it.Stages[i].On {
			continue
		}
		if IsApprove(it.Stages[i]) && it.Stages[i].Fixed {
			return RoadWaiting, "Repository policy keeps this approval for the person.", nil
		}
		break
	}
	// WHAT AUTO ITSELF STOPPED AT IS THE PERSON'S, BY THIS ROAD TOO. The
	// manager's own approve is refused wherever Auto would not approve
	// ([ManagerMay]: a failing finding, proof that is incomplete, an approval
	// that failed, a job with no owner). Its push is the same approval asked
	// for through another tool, and is held to the same rule, so choosing
	// `push` over `approve` gets round nothing.
	if why := AutonomyBlockReason(it); why != "" {
		return RoadWaiting, "Auto stopped here for the person. " + why, nil
	}
	return RoadPress, "", nil
}

// CleanAct checks one act against the job and answers it as it will be kept:
// trimmed, with nothing of this computer in its public words, a reply's and a
// comment's footer on, a thread by the name the job stores. It refuses an act
// that is wrong in itself. Push, open and merge are not stored and are
// refused here; [PushRoad] and [OfferMerge] answer them.
func CleanAct(it Item, a PublicAct) (PublicAct, error) {
	out := PublicAct{Kind: strings.TrimSpace(a.Kind)}
	if err := OwnOpenPR(it); err != nil {
		return out, err
	}
	pr := it.PullRequest
	switch out.Kind {
	case ActKindEditPR:
		out.Title = strings.TrimSpace(PublicText(a.Title))
		out.Body = strings.TrimSpace(PublicText(a.Body))
		switch {
		case out.Title == "" && out.Body == "":
			return out, actInvalid("An edit needs a new title, a new description, or both.")
		case strings.ContainsAny(out.Title, "\r\n"):
			return out, actInvalid("A pull request's title is one line.")
		case len(out.Title) > ActTitleMost:
			return out, actInvalid("That title is too long: at most %d characters.", ActTitleMost)
		case len(out.Body) > ActTextMost:
			return out, actInvalid("That description is too long: at most %d characters.", ActTextMost)
		}
	case ActKindChangeBase:
		out.Base = strings.TrimSpace(a.Base)
		switch {
		case out.Base == "":
			return out, actInvalid("A change of base needs the branch to change to.")
		case !branchName(out.Base):
			return out, actInvalid("%q is not a branch name.", out.Base)
		case pr.BaseBranch != "" && out.Base == pr.BaseBranch:
			return out, actInvalid("The pull request's base is already %s.", out.Base)
		case pr.HeadBranch != "" && out.Base == pr.HeadBranch:
			return out, actInvalid("%s is the pull request's own branch.", out.Base)
		}
	case ActKindReply, ActKindComment:
		text, err := actText(a.Text)
		if err != nil {
			return out, err
		}
		out.Text = text
		if out.Kind == ActKindReply {
			item, ok := feedbackItem(pr, a.Thread)
			if !ok {
				return out, noSuchThread(pr, a.Thread)
			}
			out.Thread = item.ID
		}
	case ActKindResolveThread:
		item, ok := feedbackItem(pr, a.Thread)
		switch {
		case !ok:
			return out, noSuchThread(pr, a.Thread)
		case !strings.HasPrefix(item.ID, "inline:"):
			return out, actInvalid("Only a review thread on the code can be resolved, and %s is not one.", item.ID)
		case item.Resolved:
			return out, actInvalid("That thread is already resolved.")
		}
		out.Thread = item.ID
	case ActKindRerequestReview:
		out.Reviewers = reviewersFor(it, a.Reviewers)
		if len(out.Reviewers) == 0 {
			return out, actInvalid("Name who to ask for a review again: nobody but the job's own person has reviewed this pull request.")
		}
	case ActKindRerunChecks:
		if pr.ChecksRead && pr.Checks.Failed == 0 {
			return out, actInvalid("No check has failed on the pull request's head.")
		}
	case ActKindMarkReady:
		if !pr.Draft {
			return out, actInvalid("The pull request is already ready for review.")
		}
	case ActKindPush, ActKindOpenPR, ActKindMerge:
		return out, actInvalid("%s has its own approval and is not kept as an act.", out.Kind)
	case "":
		return out, actInvalid("An act needs a kind.")
	default:
		return out, actInvalid("%q is not a public act.", out.Kind)
	}
	return out, nil
}

// actText is a reply's or a comment's words as they will be posted: nothing
// of this computer in them, and the footer on.
func actText(text string) (string, error) {
	text = strings.TrimSpace(PublicText(text))
	text = strings.TrimSpace(strings.TrimSuffix(text, PublicActFooter))
	if text == "" {
		return "", actInvalid("A reply or a comment needs some words.")
	}
	if len(text) > ActTextMost {
		return "", actInvalid("That is too long to post: at most %d characters.", ActTextMost)
	}
	return text + "\n\n" + PublicActFooter, nil
}

// branchName says name could be a branch: no spaces or control characters,
// none of git's forbidden marks, and not an option.
func branchName(name string) bool {
	if len(name) > 255 || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") || strings.Contains(name, "..") || strings.Contains(name, "@{") {
		return false
	}
	for _, r := range name {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	return true
}

// feedbackItem finds the feedback item a thread names on the pull request:
// by the name the job stores, or by a bare comment number.
func feedbackItem(pr *PullRequest, thread string) (PRFeedback, bool) {
	thread = strings.TrimSpace(thread)
	if pr == nil || thread == "" {
		return PRFeedback{}, false
	}
	for _, want := range []string{thread, "inline:" + thread, "conversation:" + thread} {
		for _, item := range pr.FeedbackItems {
			if item.ID == want {
				return item, true
			}
		}
	}
	return PRFeedback{}, false
}

func noSuchThread(pr *PullRequest, thread string) error {
	if strings.TrimSpace(thread) == "" {
		return actInvalid("Name the thread: the feedback item it answers, such as inline:<comment id>.")
	}
	if pr != nil && !pr.FeedbackTracked {
		return actInvalid("This job has not read the pull request's threads yet. Try again after its next read of GitHub.")
	}
	return actInvalid("This job's pull request has no thread %s.", strings.TrimSpace(thread))
}

// reviewersFor is who a review is asked of again: the logins named, or
// everyone who has reviewed the pull request when none is. The job's own
// person is left out; GitHub takes no review request for a pull request's
// author.
func reviewersFor(it Item, named []string) []string {
	var out []string
	add := func(login string) {
		login = strings.TrimPrefix(strings.TrimSpace(login), "@")
		if login == "" || strings.EqualFold(login, it.Person()) {
			return
		}
		for _, have := range out {
			if strings.EqualFold(have, login) {
				return
			}
		}
		out = append(out, login)
	}
	for _, login := range named {
		add(login)
	}
	if len(named) == 0 && it.PullRequest != nil {
		for _, r := range it.PullRequest.Reviews {
			if r.State != "pending" {
				add(r.By)
			}
		}
	}
	return out
}

// sameAct says two acts are the same act: the same kind with the same content.
func sameAct(a, b PublicAct) bool {
	return a.Kind == b.Kind && a.Title == b.Title && a.Body == b.Body && a.Base == b.Base && a.Thread == b.Thread && a.Text == b.Text && slices.Equal(a.Reviewers, b.Reviewers)
}

// ActRepeatWithin is how long after a reply or a comment was posted the same
// words to the same place are taken as the same ask made twice, not as
// something to say again.
const ActRepeatWithin = 10 * time.Minute

// repeats says a is already on the job as have: an act that still waits or
// is being sent, or a reply or comment posted a moment ago.
func repeats(have, a PublicAct, now time.Time) bool {
	if !sameAct(have, a) {
		return false
	}
	if !have.Finished() {
		return true
	}
	return have.State == ActDone && (a.Kind == ActKindReply || a.Kind == ActKindComment) && now.Sub(have.DoneAt) < ActRepeatWithin
}

// AddActs puts cleaned acts ([CleanAct]) on the job for by, each proposed or
// approved by [ActAuthority], and answers them as they now stand on it, in
// the order given. AN ACT ALREADY ON THE JOB IS NOT ADDED AGAIN: one that
// still waits or is being sent is answered as it stands, and so is a reply or
// a comment posted within [ActRepeatWithin], so a manager that asks twice
// never posts twice. It refuses, changing nothing, a set that would leave
// more than [ActsWaiting] acts waiting for the person.
func AddActs(it *Item, acts []PublicAct, by, why string, now time.Time) ([]PublicAct, error) {
	state := ActAuthority(*it, by)
	next := append([]PublicAct(nil), it.Acts...)
	out := make([]PublicAct, 0, len(acts))
	for i, a := range acts {
		found := false
		for _, have := range next {
			if repeats(have, a, now) {
				out, found = append(out, have), true
				break
			}
		}
		if found {
			continue
		}
		a.ID = actID(it.ID, now, i, a)
		a.By, a.Why, a.At, a.State = by, strings.TrimSpace(PublicText(why)), now.UTC(), state
		if state == ActApproved {
			a.ApprovedBy = by
			a.AfterPush = pendingPush(*it)
		}
		next = append(next, a)
		out = append(out, a)
	}
	waiting := 0
	for _, a := range next {
		if a.State == ActProposed {
			waiting++
		}
	}
	if state == ActProposed && waiting > ActsWaiting {
		return nil, actRefused("Too many acts already wait for the person's approval (%d). They approve or decline those first.", waiting-len(out))
	}
	it.Acts = pruneActs(next)
	return out, nil
}

// actID names an act: short, and the same for the same act proposed at the
// same moment, so a test reads it and two acts of one proposal never share it.
func actID(job int, now time.Time, i int, a PublicAct) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%d|%s|%s|%s|%s|%s|%s", job, now.UnixNano(), i, a.Kind, a.Title, a.Body, a.Base, a.Thread, a.Text)))
	return fmt.Sprintf("act_%x", sum[:4])
}

// pendingPush is the commit of the push the job has waiting or under way, ""
// for none: a publish approval waiting or inside its Undo window, or the
// publish itself. An act approved meanwhile follows that push.
func pendingPush(it Item) string {
	a := it.Approval
	if OffersPublishApproval(it) || it.Publishing || a != nil && a.Kind == "proof" && a.State == "approving" {
		return it.HeadCommit
	}
	return ""
}

// releaseActsAfterPush lets go every act that waited for the push of commit:
// that push has landed. It is called in the write that records the publish
// ([Item.MarkPublishing]), so whether the push landed is never read back
// later from what the job's publish status says by then.
func releaseActsAfterPush(it *Item, commit string) {
	if commit == "" {
		return
	}
	var acts []PublicAct
	for i, a := range it.Acts {
		if a.AfterPush != commit || a.Finished() {
			continue
		}
		if acts == nil {
			acts = append([]PublicAct(nil), it.Acts...)
		}
		acts[i].AfterPush, acts[i].WithPush = "", false
	}
	if acts != nil {
		it.Acts = acts
	}
}

// pruneActs keeps every act that is not over and the last [ActsKept] that are.
func pruneActs(acts []PublicAct) []PublicAct {
	finished := 0
	for _, a := range acts {
		if a.Finished() {
			finished++
		}
	}
	if finished <= ActsKept {
		return acts
	}
	drop := finished - ActsKept
	out := make([]PublicAct, 0, len(acts)-drop)
	for _, a := range acts {
		if a.Finished() && drop > 0 {
			drop--
			continue
		}
		out = append(out, a)
	}
	return out
}

// ProposedActs is the job's acts that wait for the person's yes, oldest first.
func ProposedActs(it Item) []PublicAct {
	var out []PublicAct
	for _, a := range it.Acts {
		if a.State == ActProposed {
			out = append(out, a)
		}
	}
	return out
}

// ApproveActs approves every act that waits, as who, and answers how many.
// withPush says the yes is the publish approval's own: the acts then follow
// that push, and come back to wait if it is undone ([SettleActs]). AN ACT
// APPROVED ON ITS OWN DOES NOT WAIT FOR A PUSH that waits beside it: that
// push may never be approved, and the act was not approved with it.
func ApproveActs(it *Item, who string, withPush bool, now time.Time) int {
	n := 0
	acts := append([]PublicAct(nil), it.Acts...)
	after := ""
	if withPush {
		after = pendingPush(*it)
	}
	for i := range acts {
		if acts[i].State != ActProposed {
			continue
		}
		acts[i].State, acts[i].ApprovedBy, acts[i].Error = ActApproved, who, ""
		acts[i].AfterPush, acts[i].WithPush = after, after != ""
		acts[i].Tries, acts[i].NextAt = 0, time.Time{}
		n++
	}
	if n > 0 {
		it.Acts = acts
	}
	return n
}

// DeclineActs declines every act that waits, with the person's words, and
// answers how many. A declined act is never sent.
func DeclineActs(it *Item, words string, now time.Time) int {
	n := 0
	acts := append([]PublicAct(nil), it.Acts...)
	for i := range acts {
		if acts[i].State != ActProposed {
			continue
		}
		acts[i].State, acts[i].Result, acts[i].DoneAt = ActDeclined, strings.TrimSpace(words), now.UTC()
		n++
	}
	if n > 0 {
		it.Acts = pruneActs(acts)
	}
	return n
}

// ActsSummary says a set of acts in one line of plain words, the way the
// approval asks about them: "post 2 replies and change the base to dev".
func ActsSummary(acts []PublicAct) string {
	count := map[string]int{}
	var title, body bool
	var bases, reviewers []string
	for _, a := range acts {
		count[a.Kind]++
		switch a.Kind {
		case ActKindEditPR:
			title, body = title || a.Title != "", body || a.Body != ""
		case ActKindChangeBase:
			bases = append(bases, a.Base)
		case ActKindRerequestReview:
			for _, r := range a.Reviewers {
				if !slices.Contains(reviewers, "@"+r) {
					reviewers = append(reviewers, "@"+r)
				}
			}
		}
	}
	var parts []string
	var posts []string
	if n := count[ActKindReply]; n > 0 {
		posts = append(posts, plural(n, "reply", "replies"))
	}
	if n := count[ActKindComment]; n > 0 {
		posts = append(posts, plural(n, "comment", "comments"))
	}
	if len(posts) > 0 {
		parts = append(parts, "post "+strings.Join(posts, " and "))
	}
	if n := count[ActKindResolveThread]; n > 0 {
		parts = append(parts, "resolve "+plural(n, "review thread", "review threads"))
	}
	switch {
	case title && body:
		parts = append(parts, "edit the pull request's title and description")
	case title:
		parts = append(parts, "edit the pull request's title")
	case body:
		parts = append(parts, "edit the pull request's description")
	}
	if len(bases) > 0 {
		parts = append(parts, "change the base to "+bases[len(bases)-1])
	}
	if len(reviewers) > 0 {
		parts = append(parts, "ask "+wordList(reviewers)+" to review again")
	}
	if count[ActKindRerunChecks] > 0 {
		parts = append(parts, "run the failed checks again")
	}
	if count[ActKindMarkReady] > 0 {
		parts = append(parts, "mark the pull request ready for review")
	}
	return wordList(parts)
}

// wordList joins words as a sentence lists them: "a, b and c".
func wordList(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// ActWords says one act in a few plain words, for the job's log.
func ActWords(a PublicAct) string {
	switch a.Kind {
	case ActKindReply:
		return "reply in " + a.Thread
	case ActKindComment:
		return "comment on the pull request"
	case ActKindResolveThread:
		return "resolve " + a.Thread
	}
	return ActsSummary([]PublicAct{a})
}

// ── WHAT THE WAIT READS WITH ACTS WAITING ───────────────────────────────────

// approveActsWords is the question a waiting set asks the person.
func approveActsWords(acts []PublicAct) string { return "Approve: " + ActsSummary(acts) + "?" }

// waitWithActs is the job's wait with its proposed acts read in. w is the
// wait the job's other fields give ([waitUnderActs]).
//
// A SET THAT WAITS FOR THE PERSON IS THE JOB'S WAIT WHENEVER NOTHING ELSE OF
// THEIRS IS: a job that would wait on GitHub, on a step that is running, or on
// a decision nothing narrower names, reads "you: approve_acts". A running
// step does not hide it: the acts are public and theirs to say, and the step
// goes on meanwhile.
//
// IT NEVER TAKES THE PLACE OF A NARROWER ACTION OF THE PERSON'S: a push to
// approve, a merge, the budget, a question, a retry. Those keep the wait and
// name the acts in their words, and approving the push approves the acts with
// it. Nor does it take the place of the service's own act in flight (a
// publish, a merge, an approval inside its Undo window), of a manager that
// holds a question on a clock, or of a job waiting for a computer: the acts
// are offered all the while, and show when that wait ends.
func waitWithActs(it Item, w Wait) Wait {
	acts := ProposedActs(it)
	if len(acts) == 0 {
		return w
	}
	asked := Wait{Who: WaitYou, Action: ActApproveActs, Words: approveActsWords(acts)}
	switch w.Who {
	case WaitGitHub:
		return asked
	case WaitWorking:
		if w.What == "publishing" || w.What == "merging" || w.What == "approving" {
			return w
		}
		return asked
	case WaitYou:
		switch {
		case w.PutAway:
		case w.Action == ActDecide:
			return asked
		case w.Action == ActApprovePublish || w.Action == ActApproveReady:
			w.Words = strings.TrimSpace(w.Words + " Approving also approves: " + ActsSummary(acts) + ".")
		default:
			w.Words = strings.TrimSpace(w.Words + " Also waiting: " + ActsSummary(acts) + ".")
		}
	}
	return w
}

// ── WHAT BECOMES OF ACTS WHEN THE JOB MOVES ─────────────────────────────────

// SettleActs puts the job's unfinished acts right against the job as it now
// stands, and answers whether it changed any. The service runs it on every
// pass before it carries an act out.
//
//   - A job that ended, or that its person stopped, sends nothing more: an
//     act that waited is declined and an approved one fails, each saying why.
//   - AN ACT AUTO APPROVED IS AUTO'S ONLY WHILE THE JOB IS IN AUTO. Once the
//     person switches to Manual or takes the job over, an act the main agent
//     approved and that has not gone out comes back to wait for them.
//   - An act that waits for a push runs once that push has landed: the write
//     that records the publish lets it go ([Item.MarkPublishing]).
//   - An act approved with the push approval comes back to wait when that
//     approval does (the person pressed Undo, or the door refused it).
//   - An act whose push will never happen (the job's checked commit is now
//     another one) fails, and says so: it was written to follow that push.
func SettleActs(it *Item, now time.Time) bool {
	ended := ""
	switch pr := it.PullRequest; {
	case it.DoneHow == "dropped" || it.State == StateDismissed:
		ended = "The job was dropped."
	case pr != nil && (pr.State == PRMerged || pr.State == PRClosed):
		ended = "The pull request is " + pr.State + "."
	case JobState(*it) == "done":
		ended = "The job is finished."
	case StoppedByPerson(*it):
		ended = "The person stopped the job."
	}
	changed := false
	acts := append([]PublicAct(nil), it.Acts...)
	for i := range acts {
		a := &acts[i]
		if a.Finished() || a.State == ActRunning {
			continue
		}
		if ended != "" {
			if a.State == ActProposed {
				a.State, a.Result = ActDeclined, ended+" This was never approved."
			} else {
				a.State, a.Error = ActFailed, ended+" This was not sent."
			}
			a.DoneAt, changed = now.UTC(), true
			continue
		}
		if autoYesLapsed(*it, *a) {
			a.State, a.ApprovedBy, a.AfterPush, a.WithPush, changed = ActProposed, "", "", false, true
			a.Error, a.Tries, a.NextAt = "", 0, time.Time{}
			continue
		}
		if a.State != ActApproved || a.AfterPush == "" {
			continue
		}
		switch {
		case it.HeadCommit != a.AfterPush && !it.Publishing:
			a.State, a.Error, a.DoneAt, changed = ActFailed, ActPushGone, now.UTC(), true
		case a.WithPush && OffersPublishApproval(*it):
			a.State, a.ApprovedBy, a.AfterPush, a.WithPush, changed = ActProposed, "", "", false, true
		}
	}
	if changed {
		it.Acts = pruneActs(acts)
	}
	return changed
}

// autoYesLapsed says a is an approved act whose yes was Auto's, on a job no
// longer in Auto: it is not sent, and waits for the person instead.
func autoYesLapsed(it Item, a PublicAct) bool {
	return a.State == ActApproved && a.ApprovedBy == ActByMainAgent && !Automatic(it)
}

// NextAct is the act the service carries out next on the job at now, and
// false when none is due: the oldest approved act, which the acts behind it
// wait for, once the push it follows has landed and its next try has come.
// A push under way goes first, and a job that takes no act sends none.
func NextAct(it Item, now time.Time) (PublicAct, bool) {
	if ActsRefusal(it) != nil || it.Publishing {
		return PublicAct{}, false
	}
	for _, a := range it.Acts {
		if a.State == ActRunning {
			return PublicAct{}, false
		}
		if a.State != ActApproved {
			continue
		}
		if a.AfterPush != "" || now.Before(a.NextAt) || autoYesLapsed(it, a) {
			return PublicAct{}, false
		}
		return a, true
	}
	return PublicAct{}, false
}

// ActsDueAt is when the service next has the job's acts to look at by the
// clock: now for an act it could send or one cut off mid-send, the next try
// of an act GitHub did not answer, and zero when only a change to the job
// makes one due.
func ActsDueAt(it Item, now time.Time) time.Time {
	if ActsRefusal(it) != nil {
		return time.Time{}
	}
	for _, a := range it.Acts {
		switch {
		case a.State == ActRunning:
			return now
		case a.State != ActApproved:
			continue
		case a.AfterPush != "" || it.Publishing || autoYesLapsed(it, a):
			return time.Time{}
		case now.Before(a.NextAt):
			return a.NextAt
		}
		return now
	}
	return time.Time{}
}

// editAct changes the job's act id in place, when change says it did, and
// answers whether anything changed.
func editAct(it *Item, id string, change func(*PublicAct) bool) bool {
	acts := append([]PublicAct(nil), it.Acts...)
	for i := range acts {
		if acts[i].ID == id {
			if !change(&acts[i]) {
				return false
			}
			it.Acts = pruneActs(acts)
			return true
		}
	}
	return false
}

// BeginAct marks the job's approved act id as being sent, and answers false
// when it is not the act that is due: someone else took it, or the job moved.
func BeginAct(it *Item, id string, now time.Time) bool {
	next, ok := NextAct(*it, now)
	if !ok || next.ID != id {
		return false
	}
	return editAct(it, id, func(a *PublicAct) bool { a.State = ActRunning; return true })
}

// FinishAct marks the running act id as sent: where it landed and what it
// did. It answers false for an act that is no longer running.
func FinishAct(it *Item, id, url, result string, now time.Time) bool {
	return editAct(it, id, func(a *PublicAct) bool {
		if a.State != ActRunning {
			return false
		}
		a.State, a.URL, a.Result, a.Error, a.DoneAt = ActDone, url, result, "", now.UTC()
		return true
	})
}

// FailAct marks the running act id as failed, with why. It is not tried
// again. It answers false for an act that is no longer running.
func FailAct(it *Item, id, why string, now time.Time) bool {
	return editAct(it, id, func(a *PublicAct) bool {
		if a.State != ActRunning {
			return false
		}
		a.State, a.Error, a.DoneAt = ActFailed, why, now.UTC()
		return true
	})
}

// actRetries are the waits before each further try of an act GitHub did not
// answer.
var actRetries = []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute}

// RetryAct puts the running act id back to be tried again later, with why it
// was not sent: nothing of it reached GitHub. After [ActTriesMost] tries it
// fails instead. It answers whether the act was running, and whether it has
// now failed.
func RetryAct(it *Item, id, why string, now time.Time) (was, failed bool) {
	was = editAct(it, id, func(a *PublicAct) bool {
		if a.State != ActRunning {
			return false
		}
		a.Tries++
		if a.Tries >= ActTriesMost {
			a.State, a.Error, a.DoneAt, failed = ActFailed, fmt.Sprintf("%s It was tried %d times.", why, a.Tries), now.UTC(), true
			return true
		}
		a.State, a.Error = ActApproved, why
		a.NextAt = now.UTC().Add(actRetries[min(a.Tries-1, len(actRetries)-1)])
		return true
	})
	return was, failed
}

// CutOffActs fails every act found running: the service that was sending it
// is gone, and what reached GitHub is not known. IT IS NEVER SENT AGAIN. The
// caller is the one sender, so nothing else can be mid-send. It answers the
// acts it failed.
func CutOffActs(it *Item, now time.Time) []PublicAct {
	var cut []PublicAct
	acts := append([]PublicAct(nil), it.Acts...)
	for i := range acts {
		if acts[i].State == ActRunning {
			acts[i].State, acts[i].Error, acts[i].DoneAt = ActFailed, ActCutOff, now.UTC()
			cut = append(cut, acts[i])
		}
	}
	if len(cut) > 0 {
		it.Acts = pruneActs(acts)
	}
	return cut
}

// ActsStatus says the job's acts in the lines its manager reads beside the
// job's wait: what waits for the person, what is on its way, and how the last
// few ended, newest last. It is nil for a job with none. A failed or declined
// act is the manager's to read and act on: nothing sends it again by itself.
func ActsStatus(it Item) []string {
	var out []string
	if waiting := ProposedActs(it); len(waiting) > 0 {
		out = append(out, "Public acts waiting on the person's approval: "+ActsSummary(waiting)+".")
	}
	var over []PublicAct
	for _, a := range it.Acts {
		switch a.State {
		case ActApproved, ActRunning:
			line := "Approved and on its way: " + ActWords(a) + "."
			switch {
			case a.AfterPush != "":
				line = "Approved, and carried out once the push has landed: " + ActWords(a) + "."
			case a.Tries > 0:
				line = "Approved, and being tried again (" + strings.TrimSuffix(a.Error, ".") + "): " + ActWords(a) + "."
			}
			out = append(out, line)
		case ActDone, ActFailed, ActDeclined:
			over = append(over, a)
		}
	}
	if len(over) > 5 {
		over = over[len(over)-5:]
	}
	for _, a := range over {
		switch a.State {
		case ActDone:
			out = append(out, strings.TrimSpace("Done: "+a.Result+" "+a.URL))
		case ActFailed:
			out = append(out, "Not sent: "+ActWords(a)+". "+a.Error)
		case ActDeclined:
			out = append(out, strings.TrimSpace("Declined: "+ActWords(a)+". "+a.Result))
		}
	}
	return out
}
