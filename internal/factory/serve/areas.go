package serve

import (
	"slices"
	"sync"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// repoAreas keeps the areas repositories declared as of the handler's last
// read of the floor. Every response that carries a job read the floor first
// (the queue, a job's read, each verb's findJob), so every job a response
// sends is judged by [api.AreaIn] against the same declarations, and a verb's
// job never names another area than the queue's.
type repoAreas struct {
	mu    sync.Mutex
	repos []factory.Repo
}

// remembering is seam with each successful Load noting the declared areas.
func (a *repoAreas) remembering(seam factory.Seam) factory.Seam {
	load := seam.Load
	if load == nil {
		return seam
	}
	seam.Load = func() (factory.Snapshot, error) {
		s, err := load()
		if err == nil {
			a.note(s.Repos)
		}
		return s, err
	}
	return seam
}

func (a *repoAreas) note(repos []factory.Repo) {
	var declared []factory.Repo
	for _, r := range repos {
		if len(r.Areas) > 0 {
			declared = append(declared, factory.Repo{Name: r.Name, Areas: slices.Clone(r.Areas)})
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.repos = declared
}

func (a *repoAreas) declared() []factory.Repo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.repos
}
