package serve

import (
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// spendModels answers GET /v1/spend/models?from=&to=: the same jobs, days and
// dollars as spendRepos, split by the model recorded on each priced call.
// Spend with no recorded model is unknown_usd. A job from before that record
// contributes its whole spend there. Nothing is guessed from the job's model
// or a step's model.
func (h *Handler) spendModels(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, h.seam.Load != nil) {
		return
	}
	q := r.URL.Query()
	zone := h.clock().Location()
	start, end, err := factory.SpendRange(q.Get("from"), q.Get("to"), zone)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	snap, err := h.seam.Load()
	if h.failed(w, err) {
		return
	}
	snap, _, _ = h.forViewer(r, snap)
	if h.authFailed(w, r) {
		return
	}
	models, unknown := foldModelSpend(snap.Items, start, end)
	h.write(w, http.StatusOK, api.SpendByModel{
		From: q.Get("from"), To: q.Get("to"), TimeZone: zoneName(zone, os.LookupEnv, os.Readlink),
		Models: models, UnknownUSD: unknown,
	})
}

type modelFold struct {
	order   []string
	rows    map[string]*modelRow
	unknown float64
}

type modelRow struct {
	api.ModelSpend
	jobs map[int]struct{}
}

func foldModelSpend(items []factory.Item, start, end time.Time) ([]api.ModelSpend, float64) {
	fold := modelFold{rows: map[string]*modelRow{}}
	for _, it := range items {
		fold.add(it, start, end)
	}
	return fold.done()
}

func (f *modelFold) add(it factory.Item, start, end time.Time) {
	usd, in := factory.SpentIn(it, start, end)
	if factory.RepoOf(it) == "" || !in {
		return
	}
	known := f.addUses(it)
	if gap := usd - known; gap > 0 {
		f.unknown += gap
	}
}

func (f *modelFold) addUses(it factory.Item) float64 {
	var known float64
	seen := map[string]struct{}{}
	for _, use := range factory.ModelUses(it) {
		if use.Model == "" || use.USD <= 0 {
			continue
		}
		known += use.USD
		f.row(use.Model).add(it.ID, use, seen)
	}
	return known
}

func (f *modelFold) row(model string) *modelRow {
	row := f.rows[model]
	if row != nil {
		return row
	}
	row = &modelRow{jobs: map[int]struct{}{}}
	row.Model = model
	f.rows[model] = row
	f.order = append(f.order, model)
	return row
}

func (row *modelRow) add(job int, use factory.ModelUse, seen map[string]struct{}) {
	row.CostUSD += use.USD
	row.Calls += use.Calls
	row.InputTokens += use.Input
	row.OutputTokens += use.Output
	if _, ok := seen[use.Model]; ok {
		return
	}
	seen[use.Model] = struct{}{}
	if _, ok := row.jobs[job]; ok {
		return
	}
	row.jobs[job] = struct{}{}
	row.Jobs++
}

func (f *modelFold) done() ([]api.ModelSpend, float64) {
	out := make([]api.ModelSpend, 0, len(f.order))
	for _, name := range f.order {
		out = append(out, f.rows[name].ModelSpend)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CostUSD != out[j].CostUSD {
			return out[i].CostUSD > out[j].CostUSD
		}
		return out[i].Model < out[j].Model
	})
	return out, f.unknown
}
