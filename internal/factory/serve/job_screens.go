package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func (h *Handler) jobScreens() {
	h.postJob("assignee", h.config.Store != nil, h.changeAssignee)
	h.postJob("sync", true, h.guarded("sync", h.onlyOwner(h.syncJob)))
	for _, verb := range []string{"stop", "rerun"} {
		h.postJob("steps/{sid}/"+verb, h.config.Store != nil, h.stepControl(verb == "rerun"))
	}
	h.route("/v1/jobs/{id}/step-edits", map[string]http.HandlerFunc{
		http.MethodGet: h.readStepEdits, http.MethodPost: h.editSteps,
	})
	h.read("/v1/jobs/{id}/hand-off/{card}", h.readJobHandOff)
	h.route("/v1/jobs/{id}/hand-off/{card}/not-now", map[string]http.HandlerFunc{http.MethodPost: h.jobHandOffNotNow})
	h.route("/v1/repos/{owner}/{repo}/pipeline/open-pr", map[string]http.HandlerFunc{http.MethodPost: h.pipelinePR})
	h.route("/v1/repos/{owner}/{repo}/pipeline/factory-copy", map[string]http.HandlerFunc{
		http.MethodGet: h.pipelineCopy, http.MethodPut: h.pipelineCopy, http.MethodDelete: h.pipelineCopy,
	})
	h.route("/v1/issues", map[string]http.HandlerFunc{http.MethodGet: h.searchIssues, http.MethodPost: h.createIssueJob})
}

// stepControl is a step's stop or rerun, guarded as every job write is. A
// rerun may carry the person's words for it; a stop is refused any.
func (h *Handler) stepControl(rerun bool) jobAction {
	verb := "step_stop"
	if rerun {
		verb = "step_rerun"
	}
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		var body struct {
			Expect *api.Expect `json:"expect,omitempty"`
			Words  string      `json:"words,omitempty"`
		}
		if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, verb, body.Expect) {
			return
		}
		if !rerun && strings.TrimSpace(body.Words) != "" {
			h.fail(w, http.StatusUnprocessableEntity, "only a rerun takes words")
			return
		}
		var revision *int
		if body.Expect != nil {
			revision = body.Expect.Version
		}
		h.onlyOwner(h.stepAct(rerun, body.Words, revision))(w, r, it)
	}
}

func (h *Handler) stepAct(rerun bool, words string, revision *int) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		if !h.workerReady(w) {
			return
		}
		name := r.PathValue("sid")
		found := false
		if it.Stream != nil {
			for _, p := range it.Stream.Phases {
				if p.Name == name {
					found = true
					break
				}
			}
		}
		if !found {
			h.fail(w, 404, "step not found")
			return
		}
		verb := record.StepStopped
		if rerun {
			verb = record.StepRerun
		}
		h.finish(w, h.recordAction(r, it, verb, name, func() error {
			return lease.NewDoors(h.workers.desk).StepControlExpected(it.ID, name, rerun, words, revision)
		}))
	}
}

func (h *Handler) syncJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if it.Origin == factory.OriginForge && it.Num > 0 {
		if !h.requireVerb(w, h.seam.Refresh != nil) {
			return
		}
		h.finish(w, h.seam.Refresh(r.Context(), it.ID))
		return
	}
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	issue, ok := h.openIssue(w, r, itemRepo(it), it.Title, it.Body, it.Person())
	if !ok {
		return
	}
	h.finish(w, h.keepIssue(it.ID, issue))
}

func (h *Handler) readStepEdits(w http.ResponseWriter, r *http.Request) {
	it, ok := h.findJob(w, r)
	if !ok {
		return
	}
	h.stepEditsReply(w, r, it, nil)
}

// stepEditsReply answers with the job's steps after the same presence and
// machine redaction the job body gets: a step's failure names a machine, and
// FS0/FS15 allow only the caller's own.
func (h *Handler) stepEditsReply(w http.ResponseWriter, r *http.Request, it factory.Item, lines []string) {
	jobs := []api.Job{h.withVersion(api.FromItem(it))}
	if _, _, err := h.applyPresence(jobs); h.failed(w, err) {
		return
	}
	h.redactMachines(r, &jobs[0])
	withholdChats(&jobs[0])
	j := jobs[0]
	if lines == nil {
		lines = []string{}
	}
	h.write(w, 200, api.StepEdits{Factory: h.factory, Steps: j.Steps, Version: j.Version, Lines: lines})
}

func (h *Handler) editSteps(w http.ResponseWriter, r *http.Request) {
	unlock, err := h.lockConsent()
	if h.failed(w, err) {
		return
	}
	defer unlock()
	it, ok := h.findJob(w, r)
	if !ok {
		return
	}
	var body api.StepEditsRequest
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "step_edits", body.Expect) || !h.owns(w, r, it) || !h.requireVerb(w, h.seam.Edit != nil) {
		return
	}
	if factory.JobState(it) == api.StateDone || it.Publishing {
		h.fail(w, 409, "finished or publishing jobs cannot edit steps")
		return
	}
	edit := factory.RunEdit{Ask: body.Ask, Thinking: body.Thinking, Max: body.Max, On: body.On, Skip: body.Skip, Remove: body.Remove, Why: body.Why, By: factory.ByYou}
	for _, mv := range body.Move {
		// A MOVE SAYS WHERE IT GOES: "" is first, and no place at all is refused
		// rather than guessed.
		if mv.After == nil {
			h.fail(w, 422, "a moved step names the step it goes after, or \"\" for first")
			return
		}
		edit.Move = append(edit.Move, factory.Moved{Stage: mv.Step, After: *mv.After})
	}
	for _, addition := range body.Add {
		d := addition.Step
		// A CHECK MAY SAY ITS ROUNDS; nothing else added here may.
		if (d.Max != 0 && d.Kind != string(factory.StageCheck)) || d.Fixed || d.Off || d.Custom || d.When != "" || d.Fanout != "" || len(d.Proof) > 0 || d.By != "" || d.Why != "" || d.UntilSentence != "" {
			h.fail(w, 422, "new steps accept name, kind, ask, until and effort, and a check its max")
			return
		}
		edit.Add = append(edit.Add, factory.Added{Stage: factory.Stage{Name: d.Name, Kind: factory.StageKind(d.Kind), Ask: d.Ask, Until: d.Until, Effort: d.Effort, Max: d.Max, On: true}, After: addition.After})
	}
	var updated factory.Item
	var lines []string
	if h.config.Store != nil {
		if !h.workerReady(w) {
			return
		}
		recipe := factory.DefaultRecipe()
		if h.seam.RecipeAt != nil {
			var err error
			recipe, _, _, err = h.seam.RecipeAt(itemRepo(it))
			if h.failed(w, err) {
				return
			}
		}
		if h.config.Pipeline != nil || h.config.JobPipeline != nil {
			file, err := h.jobPipeline(r.Context(), it, "")
			if h.failed(w, err) {
				return
			}
			recipe = file.Recipe()
		}
		err = h.recordAction(r, it, record.StepEdited, body.Why, func() error {
			var err error
			updated, lines, err = lease.NewDoors(h.workers.desk).EditSteps(it.ID, edit, recipe)
			return err
		})
	} else {
		updated, lines, err = h.seam.Edit(r.Context(), it.ID, edit)
	}
	if h.failed(w, err) {
		return
	}
	h.stepEditsReply(w, r, updated, lines)
}

// jobPipeline is the pipeline one job is bounded by, for every door that
// starts it, edits its steps or merges it: THE FILE ITS FIRST LAUNCH DECIDED
// ([factory.Item.DecidePipeline]: the factory's copy it carries, else main's
// file), else, before that, this factory's read of its repository now
// ([Config.JobPipeline], pipeline.JobFile), so nothing bounds a job by a copy
// saved or stopped since. dir is its checkout, "" when not known.
func (h *Handler) jobPipeline(ctx context.Context, it factory.Item, dir string) (pipeline.File, error) {
	if h.config.JobPipeline != nil {
		return h.config.JobPipeline(ctx, it, dir)
	}
	return pipeline.JobFile(it, "", func(bool) (pipeline.File, error) {
		if h.config.Pipeline == nil {
			return pipeline.Defaults(), nil
		}
		return h.config.Pipeline(ctx, itemRepo(it), dir)
	})
}

func (h *Handler) readJobHandOff(w http.ResponseWriter, r *http.Request) {
	it, ok := h.findJob(w, r)
	if !ok {
		return
	}
	card, ok := h.jobHandOff(w, r, it)
	if ok {
		h.write(w, 200, card)
	}
}

func (h *Handler) jobHandOff(w http.ResponseWriter, r *http.Request, it factory.Item) (api.JobHandOff, bool) {
	if it.Ask == nil || it.Ask.FromRef == nil || it.Ask.FromRef.ID != r.PathValue("card") {
		h.fail(w, 404, "hand-off card not found")
		return api.JobHandOff{}, false
	}
	card := api.JobHandOff{Factory: h.factory, ID: r.PathValue("card"), Title: it.Title, Brief: it.Ask.Text, Repo: itemRepo(it), FromRef: it.Ask.FromRef}
	if h.config.Record != nil {
		who, err := h.callerHandle(r)
		if h.failed(w, err) {
			return card, false
		}
		events, err := h.config.Record.Since(r.Context(), time.Time{})
		if h.failed(w, err) {
			return card, false
		}
		for _, e := range events {
			if e.Job == it.ID && e.Kind == record.HandOffNotNow && e.Handle == who && e.Object == card.ID {
				card.NotNow = true
			}
		}
	}
	return card, true
}

func (h *Handler) jobHandOffNotNow(w http.ResponseWriter, r *http.Request) {
	it, ok := h.findJob(w, r)
	if !ok {
		return
	}
	var body api.JobAction
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "hand_off_not_now", body.Expect) || !h.requireVerb(w, h.config.Record != nil) {
		return
	}
	card, ok := h.jobHandOff(w, r, it)
	if !ok {
		return
	}
	if card.NotNow {
		h.finish(w, nil)
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	_, err = h.config.Record.Append(r.Context(), record.Event{Kind: record.HandOffNotNow, Job: it.ID, Object: card.ID, Handle: who})
	h.finish(w, err)
}

func (h *Handler) createIssueJob(w http.ResponseWriter, r *http.Request) {
	var body api.NewJob
	if !h.decode(w, r, &body, false) {
		return
	}
	body.Issue = true
	raw, _ := json.Marshal(body)
	r.Body = ioBody(raw)
	h.newJob(w, r)
}

func (h *Handler) searchIssues(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if !store.RepoName(repo) {
		h.fail(w, 422, "repo must be owner/name")
		return
	}
	if !h.repoVisible(r, repo) {
		if !h.authFailed(w, r) {
			h.fail(w, 404, "repository not found")
		}
		return
	}
	query := "repo:" + repo + " is:issue is:open"
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		query += " " + q
	}
	var result struct {
		Items []struct {
			Number        int    `json:"number"`
			Title         string `json:"title"`
			URL           string `json:"html_url"`
			Body          string `json:"body"`
			RepositoryURL string `json:"repository_url"`
		} `json:"items"`
	}
	if !h.githubJobJSON(w, r, http.MethodGet, "/search/issues?q="+url.QueryEscape(query)+"&per_page=50", nil, &result) {
		return
	}
	issues := []api.Issue{}
	for _, i := range result.Items {
		// GitHub search qualifiers supplied in q must never widen repository visibility.
		if !strings.EqualFold(i.RepositoryURL, "https://api.github.com/repos/"+repo) {
			continue
		}
		issues = append(issues, api.Issue{Number: i.Number, Title: i.Title, URL: i.URL, Body: i.Body})
	}
	h.write(w, 200, api.Issues{Factory: h.factory, Issues: issues})
}

// githubJobJSON uses the signed-in person's credential. Only a solo host may
// use its local profile; a team's read grant is never used for a write.
func (h *Handler) githubJobJSON(w http.ResponseWriter, r *http.Request, method, path string, body any, out any) bool {
	session, _ := h.credential(r)
	token := session.githubToken
	if token == "" && h.mode() == api.FactorySolo && h.config.GitHubToken != nil {
		token = h.config.GitHubToken(r.Context())
	}
	if token == "" {
		h.fail(w, 401, "Reconnect GitHub to change this job")
		return false
	}
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if h.failed(w, err) {
			return false
		}
	}
	req, err := http.NewRequestWithContext(r.Context(), method, "https://api.github.com"+path, ioBody(raw))
	if h.failed(w, err) {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codeaf-factory")
	client := h.githubClient()
	response, err := client.Do(req)
	if err != nil {
		h.fail(w, 502, "GitHub isn't answering; retry this job change")
		return false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		h.fail(w, 502, fmt.Sprintf("GitHub refused this job change (HTTP %d)", response.StatusCode))
		return false
	}
	if err := decodeGitHub(response.Body, out); err != nil {
		h.fail(w, 502, "GitHub returned an invalid job response")
		return false
	}
	return true
}

func (h *Handler) openIssue(w http.ResponseWriter, r *http.Request, repo, title, text, assignee string) (api.Issue, bool) {
	if !store.RepoName(repo) {
		h.fail(w, 422, "repo must be owner/name")
		return api.Issue{}, false
	}
	var out struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"html_url"`
	}
	owner, name, _ := strings.Cut(repo, "/")
	people := []string{}
	if assignee != "" {
		people = append(people, factory.NormalizeHandle(assignee))
	}
	body := struct {
		Title     string   `json:"title"`
		Body      string   `json:"body"`
		Assignees []string `json:"assignees"`
	}{title, text, people}
	ok := h.githubJobJSON(w, r, http.MethodPost, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name)+"/issues", body, &out)
	if ok && (out.Number <= 0 || out.URL == "") {
		h.fail(w, 502, "GitHub returned no issue receipt")
		ok = false
	}
	return api.Issue{Number: out.Number, Title: out.Title, URL: out.URL}, ok
}

func (h *Handler) keepIssue(id int, issue api.Issue) error {
	return h.config.Store.Update(id, func(it *factory.Item) error {
		it.Num, it.URL, it.Origin, it.Synced = issue.Number, issue.URL, factory.OriginForge, true
		return nil
	})
}

func ioBody(raw []byte) io.ReadCloser { return io.NopCloser(bytes.NewReader(raw)) }
