package serve

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// repoFacts answers GET /v1/repos/{owner}/{repo}/facts?days=&months=: the
// repository's facts ([api.RepoFactsOf]) over every job document the
// factory keeps, not only the queue's recent jobs, as of now in the factory's
// zone. A caller who cannot read the repository is told it is not found, as
// on the pipeline read.
func (h *Handler) repoFacts(w http.ResponseWriter, r *http.Request) {
	owner, name, ok := h.splitRepo(w, r)
	if !ok || !h.require(w, h.seam.Load != nil) {
		return
	}
	q := r.URL.Query()
	days, err := boundedParam(q.Get("days"), api.RepoFactsDaysDefault, factory.SpendDaysMax)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, "days is a whole number from 1 to "+strconv.Itoa(factory.SpendDaysMax))
		return
	}
	months, err := boundedParam(q.Get("months"), api.RepoFactsMonthsDefault, api.RepoFactsMonthsMost)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, "months is a whole number from 1 to "+strconv.Itoa(api.RepoFactsMonthsMost))
		return
	}
	snap, err := h.seam.Load()
	if h.failed(w, err) {
		return
	}
	repo := owner + "/" + name
	var areas []string
	for _, known := range snap.Repos {
		if strings.EqualFold(known.Name, repo) {
			areas = known.Areas
		}
	}
	now := h.clock()
	out := api.RepoFactsOf(snap.Items, repo, areas, now, days, months)
	out.TimeZone = zoneName(now.Location(), os.LookupEnv, os.Readlink)
	h.write(w, http.StatusOK, out)
}

// boundedParam is a query number from 1 to most, or fallback when absent.
func boundedParam(raw string, fallback, most int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err == nil && (n < 1 || n > most) {
		err = strconv.ErrRange
	}
	return n, err
}
