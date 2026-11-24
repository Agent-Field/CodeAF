package serve

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// ── A FOLDER ON THIS COMPUTER (factory/folder.go) ───────────────────────────

// folderDoor says this factory takes a folder on its own computer as a job's
// place: a Personal (solo) factory whose seam can run a job, since a folder
// job is nothing until it runs. An organization's factory never does: its
// server cannot reach a person's laptop, so the door is absent there.
func (h *Handler) folderDoor() bool {
	return h.mode() == api.FactorySolo && h.config.Store != nil && h.seam.Launch != nil
}

// folderJob turns a new job's folder into its place: the top of the git
// repository the folder is in, written as the job's repository. It refuses,
// in one sentence, a folder on an organization's factory, a folder beside a
// repository, an issue or a workstream (both GitHub's or an organization's),
// and a folder that holds no repository. It answers false once it has
// written the refusal.
func (h *Handler) folderJob(w http.ResponseWriter, r *http.Request, body *api.NewJob) bool {
	if !h.folderDoor() {
		h.fail(w, http.StatusUnprocessableEntity, factory.FolderOnlyPersonalWords)
		return false
	}
	switch {
	case strings.TrimSpace(body.Repo) != "":
		h.fail(w, http.StatusUnprocessableEntity, "say a repository or a folder, not both")
		return false
	case body.Issue:
		h.fail(w, http.StatusUnprocessableEntity, "a job on a folder opens no GitHub issue")
		return false
	case strings.TrimSpace(body.Workstream) != "":
		h.fail(w, http.StatusUnprocessableEntity, factory.FolderOnlyPersonalWords)
		return false
	}
	dir, err := factory.ReadFolder(r.Context(), body.Folder)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return false
	}
	body.Repo, body.Folder = dir, ""
	return true
}

// folderGitHub is the GitHub repository a finished folder job could open a
// pull request on right now: its folder's GitHub remote, and only while
// GitHub is connected on this machine; "" otherwise ([factory.FolderNoGitHubWords]).
func (h *Handler) folderGitHub(ctx context.Context, it factory.Item) string {
	dir := factory.FolderOf(it)
	if dir == "" || !h.folderDoor() || h.config.GitHubToken == nil {
		return ""
	}
	gh := factory.FolderGitHub(ctx, dir)
	if gh == "" || strings.TrimSpace(h.config.GitHubToken(ctx)) == "" {
		return ""
	}
	return gh
}

// folderCan says on a folder job's read whether "Open a pull request" goes
// now ([factory.FolderPROffered]). Only a finished folder job without one
// asks git anything, so a queue of ordinary jobs reads nothing more.
func (h *Handler) folderCan(job *api.Job, it factory.Item) {
	if factory.FolderOf(it) == "" || !factory.FolderPROffered(it, "owner/name") {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	job.Can.OpenPR = factory.FolderPROffered(it, h.folderGitHub(ctx, it))
}

// openFolderPR is POST /v1/jobs/{id}/open_pr, "Open a pull request" on a
// finished folder job: its branch is pushed to the folder's GitHub remote
// with the person's GitHub connection, never with force and without
// changing the folder's own checkout or settings, and a draft pull request
// is opened from it into the default branch, its body the job's proof. The
// job stays done; the pull request is kept on it as a receipt
// ([factory.Item.FolderPR]). PRESSING IT IS THE PERSON'S CONSENT, and it is
// offered only where it can work, so nothing here runs on its own.
func (h *Handler) openFolderPR(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "open_pr", body.Expect) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	gh := h.folderGitHub(ctx, it)
	if !factory.FolderPROffered(it, gh) {
		why := "this job has no finished branch to open a pull request from"
		if factory.FolderOf(it) != "" && gh == "" {
			why = factory.FolderNoGitHubWords
		}
		h.refuse(w, http.StatusConflict, why, "conflict", "", nil, factory.JobState(it))
		return
	}
	token, _ := h.personGitHubToken(r)
	if token == "" {
		h.fail(w, http.StatusForbidden, "Sign in to GitHub before opening a pull request")
		return
	}
	dir, branch := factory.FolderOf(it), factory.WorkBranch(it)
	git := run.ExecGit{Token: func() string { return token }}
	// THE PUSH NAMES BOTH ENDS AND SETS NOTHING: no upstream is recorded in
	// the person's repository, and a remote branch that moved refuses it.
	if out, err := git.Run(dir, "push", "origin", "refs/heads/"+branch+":refs/heads/"+branch); err != nil {
		why := err.Error()
		if strings.TrimSpace(out) != "" {
			why = strings.TrimSpace(out)
		}
		h.fail(w, http.StatusBadGateway, "could not push "+branch+": "+lastLineOf(strings.ReplaceAll(why, token, "…")))
		return
	}
	httpClient := h.githubClient()
	client := forge.NewClient(token).WithHTTPClient(&httpClient)
	source := factorygithub.New(client, []string{gh}, h.clock)
	forgeItem := it
	forgeItem.Repo, forgeItem.Product = gh, ""
	rcpt, err := source.Write(ctx, factory.Action{Verb: "pr", Item: forgeItem, Branch: branch, Body: run.PublishBody(it, ""), Draft: true})
	if err != nil {
		h.fail(w, http.StatusBadGateway, "could not open the pull request: "+err.Error())
		return
	}
	pr := factory.PublishedPR{Number: rcpt.Number, URL: rcpt.URL}
	if h.failed(w, h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		cur.FolderPR = &pr
		cur.Note(h.clock(), "Pull request opened from "+branch+": "+pr.URL)
		return nil
	})) {
		return
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory         `json:"factory"`
		PR      factory.PublishedPR `json:"pr"`
	}{h.factory, pr})
}

// lastLineOf is the last line of s that says something.
func lastLineOf(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}
