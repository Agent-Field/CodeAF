package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// ── CARRYING OUT A PUBLIC ACT ───────────────────────────────────────────────
//
// [Source.Act] carries out one approved public act ([factory.PublicAct]) on a
// job's own open pull request, with the sign-in the source was made on: the
// job's person's. The rules of who approved it are internal/factory's; this
// is the forge's half, and its one promise is about failure.
//
// EVERY FAILURE SAYS WHETHER ANYTHING WAS SENT ([ActFailure]). The pull
// request is read first, so GitHub being down is found before any write and
// the act is simply tried again later. A write is then sent once. A no from
// GitHub is final and said in plain words. A write whose answer never came is
// neither: it may have gone through, so it is never sent again by itself.

// actsAPI is the writes a client needs beyond [API] to carry out public acts.
// A client without them cannot, and says so.
type actsAPI interface {
	EditPullRequest(ctx context.Context, owner, repo string, number int, edit forge.PullEdit) (forge.Pull, error)
	ReplyToReviewComment(ctx context.Context, owner, repo string, number int, comment int64, body string) (forge.Posted, error)
	CommentOnce(ctx context.Context, owner, repo string, number int, body string) (forge.Posted, error)
	ResolveReviewThread(ctx context.Context, owner, repo, thread string) error
	RequestReviewers(ctx context.Context, owner, repo string, number int, reviewers []string) error
	RerunFailedChecks(ctx context.Context, owner, repo, sha string) (forge.Rerun, error)
	MarkDraftPullReady(ctx context.Context, owner, repo string, number int, head string) error
}

// How an act that was not carried out stands.
const (
	// ActNotSent is GitHub not answering before anything was written: the act
	// can be tried again as it is.
	ActNotSent = "not_sent"
	// ActRefused is a definite no: GitHub's, or the pull request's own state.
	ActRefused = "refused"
	// ActUnknown is a write sent whose answer never came. It may have
	// happened; it is not sent again.
	ActUnknown = "unknown"
)

// ActFailure is an act that was not carried out, or not known to have been.
type ActFailure struct {
	// How is one of [ActNotSent], [ActRefused], [ActUnknown].
	How string
	// Words says it in a line the job's person reads.
	Words string
	Err   error
}

func (e *ActFailure) Error() string { return e.Words }
func (e *ActFailure) Unwrap() error { return e.Err }

// ActDone is an act carried out.
type ActDone struct {
	// URL is where it landed on GitHub, when GitHub named a place.
	URL string
	// Words says what was done, for the job's log.
	Words string
	// Change brings the job's stored pull request up to date with the act;
	// nil when the act changed nothing the job keeps.
	Change func(*factory.PullRequest)
}

// Act carries out one approved public act on it's own open pull request.
func (s *Source) Act(ctx context.Context, it factory.Item, a factory.PublicAct) (ActDone, error) {
	refused := func(format string, args ...any) (ActDone, error) {
		return ActDone{}, &ActFailure{How: ActRefused, Words: fmt.Sprintf(format, args...)}
	}
	if err := factory.CheckPersonStop(it); err != nil {
		return refused("The person stopped this job.")
	}
	if err := factory.OwnOpenPR(it); err != nil {
		return refused("%s", err.Error())
	}
	api, ok := s.api.(actsAPI)
	if !ok {
		return refused("This factory's GitHub connection cannot do that.")
	}
	owner, name, ok := pullRepo(it)
	if !ok || !s.watching(owner+"/"+name) {
		return refused("This factory does not write to that repository.")
	}
	pr := it.PullRequest
	// THE READ COMES FIRST. It tells GitHub being down (nothing sent, try
	// again later) from everything after it, and it is the pull request as it
	// stands now, not as the job last read it.
	live, err := s.api.Pull(ctx, owner, name, pr.Number)
	if err != nil {
		return ActDone{}, readFailure(err)
	}
	if state := pullFollowState(live); state != factory.PROpen {
		return refused("The pull request is %s.", state)
	}
	switch a.Kind {
	case factory.ActKindEditPR:
		var edit forge.PullEdit
		if a.Title != "" {
			edit.Title = &a.Title
		}
		if a.Body != "" {
			edit.Body = &a.Body
		}
		if _, err := api.EditPullRequest(ctx, owner, name, pr.Number, edit); err != nil {
			return ActDone{}, writeFailure(err, "edit this pull request", false)
		}
		done := ActDone{URL: live.URL, Words: "Edited the pull request's " + editedWords(a) + "."}
		if title, body := a.Title, a.Body; title != "" || body != "" {
			done.Change = func(p *factory.PullRequest) {
				if title != "" {
					p.Title = title
				}
				if body != "" {
					p.Body = body
				}
			}
		}
		return done, nil
	case factory.ActKindChangeBase:
		if live.Base == a.Base {
			return ActDone{URL: live.URL, Words: "The pull request's base was already " + a.Base + ".", Change: rebased(live)}, nil
		}
		base := a.Base
		after, err := api.EditPullRequest(ctx, owner, name, pr.Number, forge.PullEdit{Base: &base})
		if err != nil {
			return ActDone{}, writeFailure(err, "change this pull request's base", false)
		}
		if after.Base == "" {
			after.Base = base
		}
		return ActDone{URL: live.URL, Words: fmt.Sprintf("Changed the pull request's base from %s to %s.", firstOf(live.Base, "its old base"), base), Change: rebased(after)}, nil
	case factory.ActKindReply:
		item, ok := feedbackItem(pr, a.Thread)
		if !ok {
			return refused("The pull request no longer has the thread %s.", a.Thread)
		}
		body := forge.PublicationMarker + "\n" + a.Text
		if strings.HasPrefix(item.ID, "inline:") && item.ReplyTo <= 0 {
			// A thread kept before its first comment was: the next read of
			// the pull request brings it, and this is tried again then.
			return ActDone{}, &ActFailure{How: ActNotSent, Words: "This job has not read that thread's id from GitHub yet."}
		}
		if item.ReplyTo <= 0 {
			// A comment on the conversation has no thread of its own on
			// GitHub: the answer goes on the conversation, word for word.
			posted, err := api.CommentOnce(ctx, owner, name, pr.Number, body)
			if err != nil {
				return ActDone{}, writeFailure(err, "comment on this pull request", true)
			}
			return ActDone{URL: posted.URL, Words: "Answered " + a.Thread + " on the pull request's conversation."}, nil
		}
		posted, err := api.ReplyToReviewComment(ctx, owner, name, pr.Number, item.ReplyTo, body)
		if err != nil {
			return ActDone{}, threadFailure(err, "reply in this review thread", true)
		}
		return ActDone{URL: posted.URL, Words: "Replied in " + a.Thread + "."}, nil
	case factory.ActKindComment:
		posted, err := api.CommentOnce(ctx, owner, name, pr.Number, forge.PublicationMarker+"\n"+a.Text)
		if err != nil {
			return ActDone{}, writeFailure(err, "comment on this pull request", true)
		}
		return ActDone{URL: posted.URL, Words: "Commented on the pull request."}, nil
	case factory.ActKindResolveThread:
		item, ok := feedbackItem(pr, a.Thread)
		if !ok {
			return refused("The pull request no longer has the thread %s.", a.Thread)
		}
		if item.Thread == "" {
			return ActDone{}, &ActFailure{How: ActNotSent, Words: "This job has not read that thread's id from GitHub yet."}
		}
		if err := api.ResolveReviewThread(ctx, owner, name, item.Thread); err != nil {
			return ActDone{}, threadFailure(err, "resolve this review thread", false)
		}
		thread := item.Thread
		return ActDone{URL: live.URL, Words: "Resolved " + a.Thread + ".", Change: func(p *factory.PullRequest) {
			items := append([]factory.PRFeedback(nil), p.FeedbackItems...)
			for i := range items {
				if items[i].Thread == thread {
					items[i].Resolved = true
				}
			}
			p.FeedbackItems = items
		}}, nil
	case factory.ActKindRerequestReview:
		if err := api.RequestReviewers(ctx, owner, name, pr.Number, a.Reviewers); err != nil {
			return ActDone{}, writeFailure(err, "ask for a review of this pull request", false)
		}
		return ActDone{URL: live.URL, Words: "Asked " + atList(a.Reviewers) + " to review again."}, nil
	case factory.ActKindRerunChecks:
		ran, err := api.RerunFailedChecks(ctx, owner, name, live.HeadSHA)
		if err != nil {
			return ActDone{}, readFailure(err)
		}
		return ActDone{URL: live.URL, Words: rerunWords(ran)}, nil
	case factory.ActKindMarkReady:
		if !live.Draft {
			return ActDone{URL: live.URL, Words: "The pull request was already ready for review.", Change: func(p *factory.PullRequest) { p.Draft = false }}, nil
		}
		// The yes was to the pull request as the job last read it. Commits
		// that arrived since are read first; the poller does that within a
		// minute, and this is tried again then.
		if pr.HeadSHA != "" && live.HeadSHA != pr.HeadSHA {
			return ActDone{}, &ActFailure{How: ActNotSent, Words: "The pull request has commits this job has not read yet."}
		}
		if err := api.MarkDraftPullReady(ctx, owner, name, pr.Number, live.HeadSHA); err != nil {
			var refusal *forge.ReadyRefusal
			if errors.As(err, &refusal) {
				if refusal.Permission {
					return refused("Your GitHub sign-in may not mark this pull request ready.")
				}
				return refused("%s", sentence(refusal.Message))
			}
			return ActDone{}, writeFailure(err, "mark this pull request ready", false)
		}
		return ActDone{URL: live.URL, Words: "Marked the pull request ready for review.", Change: func(p *factory.PullRequest) { p.Draft = false }}, nil
	}
	return refused("%q is not an act this factory carries out.", a.Kind)
}

// rebased brings the stored pull request to the base GitHub now has. What
// depended on the old base (whether it merges, what its branch asks of
// reviews) is unknown until the next read, and a fact nobody read is never a
// yes.
func rebased(after forge.Pull) func(*factory.PullRequest) {
	return func(p *factory.PullRequest) {
		p.BaseBranch = after.Base
		if after.BaseSHA != "" {
			p.BaseSHA = after.BaseSHA
		}
		p.Mergeable, p.Conflict = mergeableWord(after.Mergeable), after.Mergeable != nil && !*after.Mergeable
		p.ReviewsRead = false
		p.ReadinessKey, p.ReadinessContext = "", ""
	}
}

// feedbackItem is the stored feedback item with this name.
func feedbackItem(pr *factory.PullRequest, id string) (factory.PRFeedback, bool) {
	for _, item := range pr.FeedbackItems {
		if item.ID == id {
			return item, true
		}
	}
	return factory.PRFeedback{}, false
}

func editedWords(a factory.PublicAct) string {
	switch {
	case a.Title != "" && a.Body != "":
		return "title and description"
	case a.Title != "":
		return "title"
	}
	return "description"
}

func atList(logins []string) string {
	out := make([]string, 0, len(logins))
	for _, login := range logins {
		out = append(out, "@"+login)
	}
	switch len(out) {
	case 0:
		return "nobody"
	case 1:
		return out[0]
	}
	return strings.Join(out[:len(out)-1], ", ") + " and " + out[len(out)-1]
}

// rerunWords says what running the failed checks again came to: which were
// asked to run, and which could not be and why.
func rerunWords(r forge.Rerun) string {
	if len(r.Ran) == 0 && len(r.Skipped) == 0 {
		return "No failed check was found on the pull request's head."
	}
	var parts []string
	if len(r.Ran) > 0 {
		parts = append(parts, "Asked GitHub to run again: "+strings.Join(r.Ran, ", ")+".")
	}
	for _, s := range r.Skipped {
		parts = append(parts, fmt.Sprintf("Could not run %s again: %s.", s.Name, s.Why))
	}
	return strings.Join(parts, " ")
}

func firstOf(words ...string) string {
	for _, w := range words {
		if strings.TrimSpace(w) != "" {
			return strings.TrimSpace(w)
		}
	}
	return ""
}

// sentence is words as one sentence: a capital first, a full stop last.
func sentence(words string) string {
	words = strings.TrimSpace(words)
	if words == "" {
		return ""
	}
	words = strings.ToUpper(words[:1]) + words[1:]
	if !strings.HasSuffix(words, ".") {
		words += "."
	}
	return words
}

// down says err is GitHub not answering rather than answering no: a network
// failure, a server error, a spent rate limit.
func down(err error) bool {
	var apiErr *forge.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500 || apiErr.StatusCode == http.StatusTooManyRequests ||
			apiErr.StatusCode == http.StatusForbidden && (apiErr.RetryAfter > 0 || strings.Contains(strings.ToLower(apiErr.Body), "rate limit"))
	}
	var ne net.Error
	if errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	// A failure that is no answer of GitHub's at all (a dropped connection)
	// reads the same.
	return !errors.As(err, &apiErr)
}

// readFailure is a read that did not come back. Nothing was written.
func readFailure(err error) *ActFailure {
	var apiErr *forge.APIError
	if !down(err) && errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			return &ActFailure{How: ActRefused, Words: "Your GitHub sign-in has expired or was withdrawn. Sign in to GitHub again.", Err: err}
		case http.StatusForbidden, http.StatusNotFound:
			return &ActFailure{How: ActRefused, Words: "Your GitHub sign-in cannot read this pull request.", Err: err}
		}
		return &ActFailure{How: ActRefused, Words: "GitHub refused to read this pull request (HTTP " + fmt.Sprint(apiErr.StatusCode) + ").", Err: err}
	}
	return &ActFailure{How: ActNotSent, Words: "GitHub did not answer.", Err: err}
}

// writeFailure is a write that did not come back as done. what is the act in
// the words "may not <what>". once says the write is one that must never be
// sent twice (a reply, a comment): its lost answer is unknown, where a lost
// answer to a write that says the same thing every time is tried again.
func writeFailure(err error, what string, once bool) *ActFailure {
	var apiErr *forge.APIError
	if errors.As(err, &apiErr) && !down(err) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			return &ActFailure{How: ActRefused, Words: "Your GitHub sign-in has expired or was withdrawn. Sign in to GitHub again.", Err: err}
		case http.StatusForbidden, http.StatusNotFound:
			return &ActFailure{How: ActRefused, Words: "Your GitHub sign-in may not " + what + ".", Err: err}
		}
		words := "GitHub refused to " + what
		if msg := githubWords(apiErr.Body); msg != "" {
			words += ": " + msg
		}
		return &ActFailure{How: ActRefused, Words: sentence(words), Err: err}
	}
	if once {
		return &ActFailure{How: ActUnknown, Words: "GitHub did not confirm this was posted. Check the pull request before trying again.", Err: err}
	}
	return &ActFailure{How: ActNotSent, Words: "GitHub did not answer.", Err: err}
}

// threadFailure is [writeFailure] for a write that names a review thread: a
// thread or comment that is gone is said as that.
func threadFailure(err error, what string, once bool) *ActFailure {
	var refusal *forge.ThreadRefusal
	if errors.As(err, &refusal) {
		switch {
		case refusal.Gone:
			return &ActFailure{How: ActRefused, Words: "That review thread is no longer on the pull request.", Err: err}
		case refusal.Permission:
			return &ActFailure{How: ActRefused, Words: "Your GitHub sign-in may not " + what + ".", Err: err}
		}
		return &ActFailure{How: ActRefused, Words: sentence(refusal.Message), Err: err}
	}
	var apiErr *forge.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return &ActFailure{How: ActRefused, Words: "That review thread is no longer on the pull request, or your GitHub sign-in may not " + what + ".", Err: err}
	}
	return writeFailure(err, what, once)
}

// githubWords is the message GitHub put in a refusal, with the first of its
// field errors, in a line. It is GitHub's own text about the request, never a
// secret: the request carried only what the act says.
func githubWords(body string) string {
	var r struct {
		Message string `json:"message"`
		Errors  []any  `json:"errors"`
	}
	if json.Unmarshal([]byte(body), &r) != nil {
		return ""
	}
	msg := strings.TrimSpace(r.Message)
	for _, e := range r.Errors {
		switch v := e.(type) {
		case string:
			return strings.TrimSuffix(msg, ".") + " (" + clip(v, 200) + ")"
		case map[string]any:
			if m, _ := v["message"].(string); m != "" {
				return strings.TrimSuffix(msg, ".") + " (" + clip(m, 200) + ")"
			}
			field, _ := v["field"].(string)
			code, _ := v["code"].(string)
			if field != "" && code != "" {
				return strings.TrimSuffix(msg, ".") + " (" + field + " is " + code + ")"
			}
		}
	}
	return clip(msg, 300)
}
