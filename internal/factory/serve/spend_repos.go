package serve

import (
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// spendRepos answers GET /v1/spend/repos?from=&to=: each readable
// repository's job spend over the factory's own calendar days
// ([api.SpendByRepo] says how a job's spend is placed). It reads the job
// documents, which the factory keeps for every job it has run, so it sees the
// spend of every runner, not only this machine's. The caller's visibility is
// the queue's ([Handler.forViewer]); a repository that is off still counts,
// because what it spent was spent.
func (h *Handler) spendRepos(w http.ResponseWriter, r *http.Request) {
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
	out := api.SpendByRepo{From: q.Get("from"), To: q.Get("to"), TimeZone: zoneName(zone, os.LookupEnv, os.Readlink), Repos: []api.RepoSpend{}}
	index := map[string]int{}
	for _, it := range snap.Items {
		repo := factory.RepoOf(it)
		usd, in := factory.SpentIn(it, start, end)
		if repo == "" || !in {
			continue
		}
		i, ok := index[repo]
		if !ok {
			i = len(out.Repos)
			index[repo] = i
			out.Repos = append(out.Repos, api.RepoSpend{Repo: repo})
		}
		out.Repos[i].SpentUSD += usd
		out.Repos[i].Jobs++
		out.TotalUSD += usd
	}
	sort.Slice(out.Repos, func(i, j int) bool { return out.Repos[i].Repo < out.Repos[j].Repo })
	h.write(w, http.StatusOK, out)
}

// zoneName is loc's IANA name. "Local" names no zone, so it is resolved the
// way the Go runtime chose it: TZ when it is set (empty is UTC), else the
// target of /etc/localtime under a zoneinfo directory. A zone nothing resolves is "",
// and the answer then leaves time_zone off.
func zoneName(loc *time.Location, env func(string) (string, bool), link func(string) (string, error)) string {
	if name := loc.String(); name != "Local" {
		return name
	}
	if tz, set := env("TZ"); set {
		tz = strings.TrimPrefix(tz, ":")
		if tz == "" {
			return "UTC"
		}
		if _, err := time.LoadLocation(tz); err == nil && tz != "Local" {
			return tz
		}
		return ""
	}
	target, err := link("/etc/localtime")
	if _, name, ok := strings.Cut(target, "zoneinfo/"); err == nil && ok {
		return name
	}
	return ""
}
