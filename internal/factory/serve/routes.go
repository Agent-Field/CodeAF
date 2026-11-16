package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/route"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// routesBody is the whole PUT /v1/routes body. RULES IS REQUIRED, so a client
// that sent nothing cannot wipe the table; an empty table is said with `[]`.
type routesBody struct {
	Rules *[]route.Rule `json:"rules"`
	// Expect is the table as the writer last read it. When it is sent and the
	// factory keeps anything else, NOTHING IS WRITTEN.
	Expect *[]route.Rule `json:"expect"`
}

// routes answers and replaces the factory's routing table ([route.Rule]): GET
// for anyone who may read this factory, PUT for its operator only, because a
// rule decides where every source's work goes and whether it starts.
//
// A PUT replaces the whole table, checked whole by [route.Clean], and answers
// what was kept with changed_by and changed_at. One that sends `expect` is
// refused with 409 when the table changed after the writer read it. Every
// refusal before the save carries the phase marker (phase.go).
func (h *Handler) routes(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	if r.Method == http.MethodGet {
		kept, err := h.config.Store.KeptRoutes()
		if h.failed(w, err) {
			return
		}
		h.write(w, 200, kept)
		return
	}
	if !h.requireOperator(w, r) {
		return
	}
	var body routesBody
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Rules == nil {
		h.fail(w, http.StatusUnprocessableEntity, "rules is required; send [] for no routing rules")
		return
	}
	clean, err := route.Clean(*body.Rules)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if why := unlisted(clean, h.pipelineListed(r.Context())); why != "" {
		h.fail(w, http.StatusUnprocessableEntity, why)
		return
	}
	if body.Expect != nil {
		if _, err := route.Clean(*body.Expect); err != nil {
			h.fail(w, http.StatusUnprocessableEntity, "expect: "+err.Error())
			return
		}
	}
	clearPhase(w)
	by := h.rulesChange(r)
	saved, err := h.config.Store.SetRoutes(*body.Rules, by, body.Expect)
	if errors.Is(err, store.ErrRoutesMoved) {
		// Refused before anything was written: again a proven no-write.
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
		h.fail(w, http.StatusConflict, err.Error()+"; read them again, then make the change")
		return
	}
	if h.failed(w, err) {
		return
	}
	h.recordOrg(r, record.RoutesChanged, "changed routing", orgObjectFactory, fmt.Sprintf("%d rules", len(saved)))
	h.write(w, 200, store.KeptRoutes{Rules: saved, RulesChange: by})
}

// routesTestBody is the POST /v1/routes/test body: a table to try, which
// need not be the one kept, and how many of the newest jobs to try it on.
type routesTestBody struct {
	Rules *[]route.Rule `json:"rules"`
	Limit int           `json:"limit"`
}

// routesTried is the POST /v1/routes/test answer, newest job first.
type routesTried struct {
	Jobs []route.Tried `json:"jobs"`
}

// routesTest answers what a table would do with the newest jobs the caller
// may see ([route.Preview]): per job, the rule that matched, the rule whose
// question has no answer yet, or none. IT WRITES NOTHING AND ASKS NO MODEL:
// only the answers already kept on each job are read. Anyone who may read
// the factory may try a table; only the operator may keep one.
func (h *Handler) routesTest(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	var body routesTestBody
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Rules == nil {
		h.fail(w, http.StatusUnprocessableEntity, "rules is required; send [] to try no routing rules")
		return
	}
	rules, err := route.Clean(*body.Rules)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	items, err := h.config.Store.List()
	if h.failed(w, err) {
		return
	}
	seen := items[:0]
	sight := h.jobSight(r)
	for _, it := range items {
		if factory.IsIssueJob(it) && h.repoVisible(r, itemRepo(it)) && sight(it) {
			seen = append(seen, it)
		}
	}
	h.write(w, 200, routesTried{Jobs: route.PreviewListed(rules, seen, body.Limit, h.pipelineListed(r.Context()))})
}

// unlisted is the refusal of the first rule in rules that sends work to a
// repository whose file can be read and lists no pipeline of the name the
// rule gives ([route.PipelineRefusal]), "" when none does. A rule that names
// no repository of its own is not checked here, because which repository it
// sends a job to is known only when a job is routed; neither is one whose
// repository's file cannot be read now, which is kept as before.
func unlisted(rules []route.Rule, listed route.Listed) string {
	if listed == nil {
		return ""
	}
	for _, r := range rules {
		if r.Then.Repo == "" || r.Then.Pipeline == "" {
			continue
		}
		if ok, known := listed(r.Then.Repo, r.Then.Pipeline); known && !ok {
			return route.PipelineRefusal(r.Name, r.Then.Repo, r.Then.Pipeline)
		}
	}
	return ""
}

// pipelineListed reads, once per repository per call, whether a
// repository's pipeline file lists a pipeline: THE SAME READ A ROUTED JOB'S
// START MAKES ([Config.Pipeline], which keeps the factory's copy in place of
// main's file), on the checkout the factory knows. A factory that cannot
// read files answers nil, and a file that cannot be read now is unknown;
// neither refuses anything.
func (h *Handler) pipelineListed(ctx context.Context) route.Listed {
	if h.config.Pipeline == nil {
		return nil
	}
	type read struct {
		recipe factory.Recipe
		ok     bool
	}
	seen := map[string]read{}
	return func(repo, name string) (bool, bool) {
		key := strings.ToLower(strings.TrimSpace(repo))
		got, done := seen[key]
		if !done {
			dir := ""
			if h.seam.RecipeAt != nil {
				if _, _, d, err := h.seam.RecipeAt(repo); err == nil {
					dir = d
				}
			}
			if f, err := h.config.Pipeline(ctx, repo, dir); err == nil {
				got = read{recipe: f.Recipe(), ok: true}
			}
			seen[key] = got
		}
		if !got.ok {
			return false, false
		}
		return len(got.recipe.Named(name)) > 0, true
	}
}
