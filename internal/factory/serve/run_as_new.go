package serve

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// runAsNew copies the request, never the run. It uses the same person's Start
// door; a refusal removes only the provisional copy, leaving the source alone.
func (h *Handler) runAsNew(w http.ResponseWriter, r *http.Request, source factory.Item) {
	var body struct {
		Assignee *string     `json:"assignee,omitempty"`
		Expect   *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, source, "run-as-new", body.Expect) {
		return
	}
	if !factory.CanRunAsNew(source) {
		h.refuse(w, 409, "Run as new requires a job that has ended", "conflict", "", nil, factory.JobState(source))
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	next := factory.Item{
		FromJob: source.ID, Repo: source.Repo, Product: source.Product, Kind: factory.KindIssue,
		Title: source.Title, Body: source.Body, Origin: factory.OriginChat, Tier: factory.TierOwner,
		State: factory.StateNew, Autonomy: source.Autonomy, Manual: source.Manual, Cap: source.Cap, CapChosen: source.CapChosen,
		Stages: factory.CopyStages(source.Stages), Model: source.Model, Starter: factory.NormalizeHandle(who),
		SourceIssue: source.SourceIssue,
	}
	if source.Repo != "" {
		next.Places = []string{source.Repo}
	}
	if factory.GitHubIssue(source) {
		next.SourceIssue = source.URL
	}
	if source.AutomaticModels != nil {
		automatic := *source.AutomaticModels
		next.AutomaticModels = &automatic
	}
	if body.Assignee != nil {
		assignee := factory.NormalizeHandle(*body.Assignee)
		if assignee == "" {
			h.fail(w, 422, "assignee must be a GitHub login")
			return
		}
		next.Assignee = assignee
	}
	if source.Ask != nil {
		ask := *source.Ask
		if ask.FromRef != nil {
			ref := *ask.FromRef
			ask.FromRef = &ref
		}
		next.Ask = &ask
	} else {
		next.Ask = &factory.JobAsk{Text: source.Body, By: who, At: h.clock(), Source: factory.AskApp}
	}
	if source.PipelineIsDecided() {
		next.DecidePipeline(source.PipelineCopy, h.clock())
	}
	// The start is this person's, with its normal queue priority and limits.
	start := r.Clone(r.Context())
	start.URL.Path = "/v1/jobs/start_now"
	if h.startRefused(w, start, next) {
		return
	}
	id, err := h.seam.NewJob(next.Repo, next.Title, next.Body)
	if h.failed(w, err) {
		return
	}
	// Creation uses the separate-title door. The saved request then replaces
	// its repository defaults before the ordinary start can see the copy.
	err = h.config.Store.Update(id, func(cur *factory.Item) error {
		next.ID, next.Created, next.Changed = cur.ID, cur.Created, cur.Changed
		*cur = next
		return nil
	})
	if h.failed(w, err) {
		return
	}
	answer := &replayWriter{header: make(http.Header)}
	started := h.launchJob(answer, start, next, "", nil)
	if !started {
		if h.failed(w, h.config.Store.ForgetUnstartedCopy(next.ID)) {
			return
		}
	}
	if answer.status() >= 300 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.status())
		_, _ = w.Write(answer.body.Bytes())
		return
	}
	if h.config.Record != nil {
		for _, pair := range []struct{ job, other int }{{source.ID, next.ID}, {next.ID, source.ID}} {
			_, err = h.config.Record.Append(r.Context(), record.Event{Kind: record.RunAsNew, Job: pair.job, Handle: who,
				Detail: fmt.Sprintf("from job:%d to job:%d", source.ID, next.ID), Object: "job:" + strconv.Itoa(pair.other)})
			if h.failed(w, err) {
				return
			}
		}
	}
	h.write(w, http.StatusCreated, struct {
		Factory api.Factory `json:"factory"`
		ID      int         `json:"id"`
	}{h.factory, next.ID})
}
