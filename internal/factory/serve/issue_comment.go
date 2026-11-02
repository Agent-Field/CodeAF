package serve

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
)

// ── A PERSON'S COMMENT ON THE JOB'S ISSUE ──────────────────────────────────
//
// POST /v1/jobs/{id}/issue_comment posts the person's words on the job's
// GitHub issue AS THAT PERSON: with their own sign-in's token, never the
// host's, exactly as the pipeline pull request is opened (pipeline_pr.go).
// The words go as they were written, with no publication marker, because they
// are the person's and not the factory's.
//
// THE COMMENT IS POSTED ONCE. A transport failure, a 5xx or an unreadable yes
// leaves the outcome unknown, and it is answered as such and never repeated.
// Only a definitive 4xx says nothing was posted.
//
// The posted comment is kept on the job at once, so its next read carries it.
// Its author's standing is not asked here: the poll's read of the comment
// brings it, and only then does the job's manager handle the comment
// ([factory.ReadComments]), so a comment is read by one path whoever posted it.
//
// EVERY FAILURE OF THIS ROUTE'S OWN SAYS WHETHER ANYTHING WAS POSTED, in
// outcome: refused (nothing was) or unknown (GitHub's answer cannot say). A
// DOUBLE CLICK POSTS ONCE: while one comment on a job is being posted, another
// on the same job is refused (409 in_flight). A repository that is off takes
// no comment (offVerbs).

const (
	issueCommentReconnect = "Reconnect GitHub to comment on the issue"
	issueCommentTimeout   = 30 * time.Second
	issueCommentReadLimit = 1 << 20
)

// githubPostedComment is the part of GitHub's receipt this route reads.
type githubPostedComment struct {
	ID      int64     `json:"id"`
	HTMLURL string    `json:"html_url"`
	Body    string    `json:"body"`
	Created time.Time `json:"created_at"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (h *Handler) issueComment(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.IssueCommentRequest
	if !h.decode(w, r, &body, false) {
		return
	}
	words := strings.TrimSpace(body.Body)
	if words == "" || utf8.RuneCountInString(words) > factory.IssueCommentMost {
		h.commentFailed(w, http.StatusUnprocessableEntity, "", factory.IssueCommentBounds)
		return
	}
	owner, repo, ok := issueOf(it)
	if !ok {
		h.commentFailed(w, http.StatusConflict, "", "this job has no GitHub issue to comment on")
		return
	}
	token, viaSession := h.personGitHubToken(r)
	if token == "" {
		h.commentFailed(w, http.StatusUnauthorized, "", issueCommentReconnect)
		return
	}
	if _, busy := h.commentFlights.LoadOrStore(it.ID, struct{}{}); busy {
		h.commentFailed(w, http.StatusConflict, api.ReasonInFlight, "a comment on this job is already being posted; nothing more was posted")
		return
	}
	defer h.commentFlights.Delete(it.ID)
	ctx, cancel := context.WithTimeout(r.Context(), issueCommentTimeout)
	defer cancel()
	var got githubPostedComment
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/issues/" + strconv.Itoa(it.Num) + "/comments"
	status, err := h.githubREST(ctx, token, http.MethodPost, path, map[string]string{"body": words}, &got, issueCommentReadLimit)
	if refusal, code := issueCommentRefusal(status, err, got, owner+"/"+repo); refusal != "" {
		if code == http.StatusUnauthorized && viaSession {
			h.endGitHub(r, token)
		}
		h.commentFailed(w, code, "", refusal)
		return
	}
	posted := factory.Comment{ID: strconv.FormatInt(got.ID, 10), URL: got.HTMLURL, Author: got.User.Login, Body: words, At: got.Created}
	if st := h.config.Store; st != nil {
		if h.failed(w, st.Update(it.ID, func(cur *factory.Item) error {
			cur.Comments = keepPosted(cur.Comments, posted)
			return nil
		})) {
			return
		}
	}
	h.write(w, http.StatusOK, api.IssueCommentPosted{Factory: h.factory, Job: it.ID, Comment: api.FromComment(posted)})
}

// issueOf is the job's issue on GitHub, when it has one.
func issueOf(it factory.Item) (string, string, bool) {
	if it.Origin != factory.OriginForge || it.Num <= 0 || !factory.IsIssueJob(it) {
		return "", "", false
	}
	full := itemRepo(it)
	owner, repo, ok := strings.Cut(full, "/")
	return owner, repo, ok && owner != "" && repo != "" && !strings.Contains(repo, "/")
}

// issueCommentRefusal is the sentence and status for a post that did not
// come back as a whole receipt, or "" for one that did. 401 is the person's
// sign-in; 403, 404 and 410 are one answer, the sign-in may not comment there.
func issueCommentRefusal(status int, err error, got githubPostedComment, repo string) (string, int) {
	const unknown = "GitHub's answer is unknown, so the comment may have posted; check the issue before trying again"
	switch {
	case err != nil || status >= 500 || (status >= 200 && status < 300 && (got.ID <= 0 || got.Created.IsZero())):
		return unknown, http.StatusBadGateway
	case status >= 200 && status < 300:
		return "", 0
	case status == http.StatusUnauthorized:
		return issueCommentReconnect, http.StatusUnauthorized
	case status == http.StatusForbidden || status == http.StatusNotFound || status == http.StatusGone:
		return fmt.Sprintf("Your GitHub sign-in can't comment on %s (HTTP %d); nothing was posted", repo, status), http.StatusForbidden
	case status == http.StatusTooManyRequests:
		return "GitHub is limiting requests; nothing was posted, so try again soon", http.StatusTooManyRequests
	case status >= 400:
		return fmt.Sprintf("GitHub refused the comment (HTTP %d); nothing was posted", status), http.StatusUnprocessableEntity
	}
	return unknown, http.StatusBadGateway
}

// keepPosted adds the posted comment to the job's last comments, as many as
// the floor keeps, unless a read already brought it.
func keepPosted(list []factory.Comment, c factory.Comment) []factory.Comment {
	for _, have := range list {
		if have.ID == c.ID {
			return list
		}
	}
	list = append(append([]factory.Comment{}, list...), c)
	if len(list) > factorygithub.CommentsMost {
		list = list[len(list)-factorygithub.CommentsMost:]
	}
	return list
}

// commentFailed answers a failure of this route with its outcome: a 502 is
// the only answer that cannot say whether GitHub took the comment. An empty
// reason is the status's own.
func (h *Handler) commentFailed(w http.ResponseWriter, status int, reason, words string) {
	if reason == "" {
		reason = reasonFor(status)
	}
	outcome := api.CommentRefused
	if status == http.StatusBadGateway {
		outcome = api.CommentUnknown
	}
	h.writeRefusal(w, status, refusalBody{Error: words, Reason: reason, Outcome: outcome})
}
