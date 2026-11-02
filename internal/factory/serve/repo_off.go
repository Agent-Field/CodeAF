package serve

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── A REPOSITORY THAT IS OFF ────────────────────────────────────────────────
//
// TURNING A REPOSITORY OFF STOPS THE FACTORY WORKING ON IT ENTIRELY (the
// owner's decision, [factory.RepoSwitch]). This file is the factory service's
// half of that: the queue leaves the repository's jobs out, a job's own read
// says so, and every door that would work on one of its jobs is refused in
// one sentence ([factory.RepoOffWords]). Nothing is deleted or re-made, so
// turning the repository back on brings every job back as it was.

// offVerbs are the job doors that make the factory work on a job: start or
// run it, run a step again, carry it on from a pause, publish it, merge it,
// send it back, run its checks again, open it on GitHub, or comment on its
// issue. postJob refuses
// each of them, before anything is read or written, for a job whose
// repository is off. A door that only reads, stops, drops or changes who a job
// is for stays open: a person may still tidy a job that nothing works on.
var offVerbs = map[string]bool{
	"run": true, "start_now": true, "run_again": true, "steps/{sid}/rerun": true, "resume": true,
	"approve": true, "retry_publish": true, "merge": true, "request_changes": true, "recheck": true,
	"sync": true, "issue_comment": true,
}

// repoSwitch is the switch the store keeps. A factory with no store of its
// own keeps no list, so nothing on it is off.
func (h *Handler) repoSwitch() (factory.RepoSwitch, error) {
	if h.config.Store == nil {
		return factory.RepoSwitch{}, nil
	}
	return h.config.Store.RepoSwitch()
}

// refuseOff answers 409 repo_off and true when repo is off. state is the
// job's state for now_state, "" when there is no job yet.
func (h *Handler) refuseOff(w http.ResponseWriter, repo, state string) bool {
	sw, err := h.repoSwitch()
	if h.failed(w, err) {
		return true
	}
	if !sw.Off(repo) {
		return false
	}
	h.refuse(w, http.StatusConflict, factory.RepoOffWords(repo), api.ReasonRepoOff, "", nil, state)
	return true
}

// onlyOn is the snapshot as the queue shows it: the jobs of repositories that
// are off left out, in every state, and so are those repositories, while the
// ones that stay carry whether they are watched.
func onlyOn(s factory.Snapshot, sw factory.RepoSwitch) factory.Snapshot {
	items := make([]factory.Item, 0, len(s.Items))
	for _, it := range s.Items {
		if !sw.OffJob(it) {
			items = append(items, it)
		}
	}
	s.Items = items
	repos := make([]factory.Repo, 0, len(s.Repos))
	for _, r := range s.Repos {
		if sw.Off(r.Name) {
			continue
		}
		r.Watched = sw.Watched(r.Name)
		repos = append(repos, r)
	}
	s.Repos = repos
	return s
}

// offJob is a job of a repository that is off, as its own read shows it: it
// says so, and the doors postJob would refuse are not offered, because a
// capability that cannot work is absent, not broken.
func offJob(j *api.Job) {
	j.RepoOff = true
	j.Can.Run, j.Can.StartNow, j.Can.RunAgain, j.Can.Resume = false, false, false, false
	j.Can.Approve, j.Can.RetryPublish, j.Can.Merge = false, false, false
	j.Controls.RerunStep, j.Controls.Sync = false, false
}
