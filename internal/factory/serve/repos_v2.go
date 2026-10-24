package serve

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// repoPower changes one watched repository through the same operator door as the whole list.
// Every refusal before SetRepos carries api.PhaseHeader (serve.go marks the route);
// the marker is cleared immediately before the first change (a stop, or the save),
// so a failure of the save or of the event after it reads as uncertain to a client.
// Turning a repository off stops the factory working on it ([factory.RepoSwitch]),
// and is refused while any of its jobs is under way unless the body says "stop"
// ([Handler.changeRepos]). Nothing of its jobs or checkouts is deleted.
func (h *Handler) repoPower(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperator(w, r) || !h.requireVerb(w, h.config.Store != nil && h.seam.SetRepos != nil) {
		return
	}
	repo := r.PathValue("owner") + "/" + r.PathValue("repo")
	if !store.RepoName(repo) {
		h.fail(w, http.StatusUnprocessableEntity, "repo must be owner/name")
		return
	}
	if !h.repoVisible(r, repo) {
		h.fail(w, http.StatusNotFound, "repository not found")
		return
	}
	var body struct {
		On   *bool `json:"on"`
		Stop bool  `json:"stop,omitempty"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.On == nil {
		h.fail(w, http.StatusUnprocessableEntity, "on must be true or false")
		return
	}
	if *body.On && body.Stop {
		h.fail(w, http.StatusUnprocessableEntity, "stop is only for turning a repository off")
		return
	}
	detail := "turned off " + repo
	if *body.On {
		detail = "connected repo " + repo
	}
	if !h.changeRepos(w, r, body.Stop, func(repos []string) []string {
		next := []string{}
		for _, name := range repos {
			if !strings.EqualFold(name, repo) {
				next = append(next, name)
			}
		}
		if *body.On {
			next = append(next, repo)
		}
		return next
	}) {
		return
	}
	h.finish(w, h.operatorEvent(r, record.ReposChanged, "repo:"+repo, detail))
}

// setRepos is PUT /v1/repos, the whole watched list at once. Every repository
// it would leave off obeys the same rule as one turned off by repoPower, with
// ONE refusal naming the work under way on all of them, and "stop" on the body
// is the same yes.
func (h *Handler) setRepos(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.seam.SetRepos != nil) {
		return
	}
	if !h.requireOperator(w, r) {
		return
	}
	var body struct {
		Watched *[]string `json:"watched"`
		Stop    bool      `json:"stop,omitempty"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Watched == nil {
		h.fail(w, http.StatusUnprocessableEntity, "watched must be an array of repository names")
		return
	}
	if !h.changeRepos(w, r, body.Stop, func([]string) []string { return *body.Watched }) {
		return
	}
	h.finish(w, h.operatorEvent(r, record.ReposChanged, "factory", strings.Join(*body.Watched, ", ")))
}

// changeRepos is the one write of the watched list a person makes: edit turns
// the list as it stands into the list to save. It answers whether it saved;
// on false it has answered the request.
//
// A REPOSITORY IS NEVER TURNED OFF UNDER WORK UNASKED. When any job of a
// repository the edit leaves off is under way ([factory.UnderWay]: running,
// queued or waiting on a person), the write is refused with 409 has_running
// and a sentence naming those jobs, and nothing changes. With stop, each of
// them is first stopped the way the person's own Stop stops it ([Handler.stopAs]:
// the work is kept and the stop recorded); a stop that fails leaves every
// repository as it was and is refused naming the job. The check itself is made
// once, under the list's lock, just before the save, so a job started while
// the stops ran still keeps its repository on.
//
// A factory whose store keeps no list has nothing to turn off, so nothing is
// checked there.
func (h *Handler) changeRepos(w http.ResponseWriter, r *http.Request, stop bool, edit func(repos []string) []string) bool {
	if stop {
		repos, err := h.watchedNow()
		if h.failed(w, err) {
			return false
		}
		if !h.stopUnderWay(w, r, turnedOff(repos, edit(repos))) {
			return false
		}
	}
	// The read, the edit and the save are one step: a second operator toggling a
	// different repository must read this one's result, not the list before it.
	// Authority was judged above, per request, before any wait here.
	h.reposMu.Lock()
	repos, err := h.watchedNow()
	if err != nil {
		h.reposMu.Unlock()
		h.failed(w, err)
		return false
	}
	next := edit(repos)
	off := turnedOff(repos, next)
	busy, err := h.underWay(off)
	if err != nil || len(busy) > 0 {
		h.reposMu.Unlock()
		if !h.failed(w, err) {
			h.refuse(w, http.StatusConflict, runningWords(off, busy), api.ReasonHasRunning, "", nil, "")
		}
		return false
	}
	// THE FIRST REQUESTED CHANGE when nothing was stopped: from here a failure
	// may follow a stored write (the list is saved, then the event is
	// recorded), so it carries no marker.
	clearPhase(w)
	saved := h.seam.SetRepos(next)
	h.reposMu.Unlock()
	return !h.failed(w, saved)
}

// watchedNow is the watched list the store keeps, nil for a factory with no
// store of its own.
func (h *Handler) watchedNow() ([]string, error) {
	if h.config.Store == nil {
		return nil, nil
	}
	return h.config.Store.Repos()
}

// turnedOff is each repository on repos that next leaves out, spelled as
// repos spells it, without regard to case.
func turnedOff(repos, next []string) []string {
	kept := make(map[string]bool, len(next))
	for _, name := range next {
		kept[strings.ToLower(strings.TrimSpace(name))] = true
	}
	var off []string
	for _, name := range repos {
		if !kept[strings.ToLower(name)] {
			off = append(off, name)
		}
	}
	return off
}

// underWay is every job under way on the repositories in off, by repository
// in off's order and then by id, so a refusal and the stops read the same.
func (h *Handler) underWay(off []string) ([]factory.Item, error) {
	if len(off) == 0 || h.config.Store == nil {
		return nil, nil
	}
	items, err := h.config.Store.List()
	if err != nil {
		return nil, err
	}
	order := make(map[string]int, len(off))
	for i, name := range off {
		order[strings.ToLower(name)] = i
	}
	var busy []factory.Item
	for _, it := range items {
		if _, ok := order[strings.ToLower(factory.RepoOf(it))]; ok && factory.UnderWay(it) {
			busy = append(busy, it)
		}
	}
	sort.SliceStable(busy, func(i, j int) bool {
		a, b := order[strings.ToLower(factory.RepoOf(busy[i]))], order[strings.ToLower(factory.RepoOf(busy[j]))]
		if a != b {
			return a < b
		}
		return busy[i].ID < busy[j].ID
	})
	return busy, nil
}

// stopUnderWay stops every job under way on the repositories in off, as the
// person's own Stop would, and answers whether all of them stopped; on false
// it has answered the request, naming the job that would not stop. The jobs
// stopped before it stay stopped, with their work kept: each was asked for.
func (h *Handler) stopUnderWay(w http.ResponseWriter, r *http.Request, off []string) bool {
	busy, err := h.underWay(off)
	if h.failed(w, err) {
		return false
	}
	if len(busy) == 0 {
		return true
	}
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return false
	}
	// THE FIRST REQUESTED CHANGE: a stop is a stored write, so nothing after it
	// may claim that nothing changed.
	clearPhase(w)
	for _, it := range busy {
		if err := h.stopAs(r, it, handle); err != nil {
			kept := "nothing was turned off"
			if len(off) == 1 {
				kept = off[0] + " is still on"
			}
			why := strings.TrimSuffix(strings.TrimSpace(err.Error()), ".")
			h.refuse(w, http.StatusConflict, factory.JobRef(it)+" could not be stopped, so "+kept+": "+why+".", api.ReasonHasRunning, "", nil, factory.JobState(it))
			return false
		}
	}
	return true
}

// runningWords names the work under way on each repository being turned off,
// one sentence a repository: `acme/api has 1 job running (api#12) and 1
// waiting on you (api#7).`
func runningWords(off []string, busy []factory.Item) string {
	var sentences []string
	for _, repo := range off {
		groups := []struct {
			state, words string
			refs         []string
		}{{state: "running", words: "running"}, {state: "queued", words: "queued"}, {state: "needs_you", words: "waiting on you"}}
		for _, it := range busy {
			if !strings.EqualFold(factory.RepoOf(it), repo) {
				continue
			}
			for i := range groups {
				if groups[i].state == factory.JobState(it) {
					groups[i].refs = append(groups[i].refs, factory.JobRef(it))
				}
			}
		}
		var parts []string
		for _, g := range groups {
			if len(g.refs) == 0 {
				continue
			}
			// The first group carries the noun: "1 job running", then "1 queued".
			count := strconv.Itoa(len(g.refs))
			switch {
			case len(parts) > 0:
			case len(g.refs) == 1:
				count += " job"
			default:
				count += " jobs"
			}
			parts = append(parts, count+" "+g.words+" ("+strings.Join(g.refs, ", ")+")")
		}
		if len(parts) == 0 {
			continue
		}
		list := parts[0]
		if n := len(parts); n > 1 {
			list = strings.Join(parts[:n-1], ", ") + " and " + parts[n-1]
		}
		sentences = append(sentences, repo+" has "+list+".")
	}
	return strings.Join(sentences, " ")
}
