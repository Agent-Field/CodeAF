package serve

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// prReady is the person's explicit ready-for-review decision. postJob holds
// the consent lock from the guarded read through the confirmed local update.
func (h *Handler) prReady(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "pr-ready", body.Expect) {
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if !api.CanFor(it, who, h.clock()).PRReady {
		h.refuse(w, http.StatusConflict, "this job has no open draft pull request to mark ready", "conflict", "", nil, factory.JobState(it))
		return
	}
	token, _ := h.personGitHubToken(r)
	if token == "" {
		h.fail(w, http.StatusForbidden, "Sign in to GitHub before marking this job ready")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	httpClient := h.githubClient()
	client := forge.NewClient(token).WithHTTPClient(&httpClient)
	source := factorygithub.New(client, []string{factory.RepoOf(it)}, h.clock)
	ready, err := source.MarkReady(ctx, it)
	if err != nil {
		h.prReadyFailed(w, err)
		return
	}
	if h.failed(w, h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		// A worker may have refreshed other PR facts while GitHub was answering.
		// Only the same pull request and head receive this confirmation.
		if cur.PullRequest == nil || cur.PullRequest.Number != ready.Number || cur.PullRequest.HeadSHA != ready.HeadSHA {
			return errors.New("the job changed while marking ready; read the job before trying again")
		}
		pr := *cur.PullRequest
		pr.Draft = false
		cur.PullRequest = &pr
		return nil
	})) {
		return
	}
	event := ""
	if h.config.Record != nil {
		receipt, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.PRReady, Job: it.ID, Handle: who})
		if h.failed(w, err) {
			return
		}
		event = receipt.ID
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		Event   string      `json:"event,omitempty"`
	}{h.factory, event})
}

func (h *Handler) prReadyFailed(w http.ResponseWriter, err error) {
	var refusal *forge.ReadyRefusal
	if errors.As(err, &refusal) {
		status := http.StatusConflict
		if refusal.Permission {
			status = http.StatusForbidden
		}
		h.fail(w, status, refusal.Error())
		return
	}
	var apiErr *forge.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
		if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden || apiErr.StatusCode == http.StatusNotFound {
			h.fail(w, http.StatusForbidden, "Your GitHub sign-in cannot mark this pull request ready")
			return
		}
		h.fail(w, http.StatusConflict, "GitHub refused to mark this pull request ready; read the job before trying again")
		return
	}
	h.fail(w, http.StatusBadGateway, "GitHub did not confirm ready for review; read the job before trying again")
}
