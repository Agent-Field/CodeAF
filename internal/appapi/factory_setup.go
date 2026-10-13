package appapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// Team setup reads and ingestion belong to the server; checkouts belong to this app host.
type localFactorySetup struct {
	Done  bool                          `json:"done"`
	Repos map[string]localSetupCheckout `json:"repos"`
}
type localSetupCheckout struct {
	Path  string  `json:"path"`
	State string  `json:"state"`
	Error *string `json:"error,omitempty"`
}

func (f *FactoryRegistry) teamSetup(w http.ResponseWriter, r *http.Request, id string, body factoryapi.FactorySetupRequest) bool {
	if id == "local" || id == "solo" {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rows, err := f.connections(nil)
	if err != nil {
		agentFailure(w, err)
		return true
	}
	var row factoryConnection
	for _, candidate := range rows {
		if candidate.ID == id && candidate.Mode == "team" {
			row = candidate
			break
		}
	}
	if row.ID == "" {
		fail(w, 404, "not_found", "factory setup not found")
		return true
	}
	source := f.source(row)
	var view factoryapi.FactorySetup
	if err = source.request(r.Context(), "GET", "/v1/factory-setup/local", nil, &view); err != nil {
		agentFailure(w, err)
		return true
	}
	var local localFactorySetup
	_, err = f.connections(func(saved *factoryConnections) error {
		local = saved.Setups[id]
		if local.Repos == nil {
			local.Repos = map[string]localSetupCheckout{}
		}
		return nil
	})
	if err != nil {
		agentFailure(w, err)
		return true
	}
	selected := func(repo string) bool {
		for _, r := range view.Repos {
			if r.ID == repo && r.Selected {
				return true
			}
		}
		return false
	}
	localStep := view.State == "checkouts" || view.State == "done"
	if r.Method == "POST" {
		switch body.Verb {
		case "connect", "scope":
			fail(w, 409, "not_allowed", "Give the factory read-only GitHub access.")
			return true
		case "clone", "checkout":
			if !localStep || !selected(body.RepoID) || strings.TrimSpace(body.Path) == "" || f.SetupCheckout == nil {
				fail(w, 409, "not_allowed", "Choose a checkout folder")
				return true
			}
			if f.setupBusy[id+"\x00"+body.RepoID] {
				fail(w, 409, "conflict", "Wait for the clone to finish")
				return true
			}
			if body.Verb == "checkout" {
				path, e := f.SetupCheckout(r.Context(), body.RepoID, body.Path, false)
				if e != nil {
					agentFailure(w, e)
					return true
				}
				local.Repos[body.RepoID] = localSetupCheckout{Path: path, State: "found"}
			} else {
				local.Repos[body.RepoID] = localSetupCheckout{Path: body.Path, State: "cloning"}
			}
		case "skip":
			if !localStep || !selected(body.RepoID) || f.setupBusy[id+"\x00"+body.RepoID] {
				fail(w, 409, "conflict", "No checkout to skip")
				return true
			}
			local.Repos[body.RepoID] = localSetupCheckout{State: "skipped"}
		case "done":
			if !localStep {
				fail(w, 409, "conflict", "finish reading before Done")
				return true
			}
			for _, repo := range view.Repos {
				if f.setupBusy[id+"\x00"+repo.ID] {
					fail(w, 409, "conflict", "Wait for the clone to finish")
					return true
				}
			}
			local.Done = true
			for _, repo := range view.Repos {
				if repo.Selected && local.Repos[repo.ID].State != "found" {
					local.Repos[repo.ID] = localSetupCheckout{State: "skipped"}
				}
			}
		default:
			body.ID = "local"
			if err = source.request(r.Context(), "POST", "/v1/factory-setup/local", body, &view); err != nil {
				agentFailure(w, err)
				return true
			}
			local.Done = false
		}
	}
	if _, err = f.connections(func(saved *factoryConnections) error {
		if saved.Setups == nil {
			saved.Setups = map[string]localFactorySetup{}
		}
		saved.Setups[id] = local
		return nil
	}); err != nil {
		agentFailure(w, err)
		return true
	}
	if f.SetupCheckouts != nil {
		dirs, e := f.SetupCheckouts()
		if e != nil {
			agentFailure(w, e)
			return true
		}
		for i := range view.Repos {
			repo := &view.Repos[i]
			repo.Checkout = nil
			repo.Clone = "missing"
			repo.CloneError = nil
			kept := local.Repos[repo.ID]
			if kept.State != "" {
				repo.Clone = kept.State
				repo.CloneError = kept.Error
				if kept.Path != "" {
					path := kept.Path
					repo.Checkout = &path
				}
			}
			if dir := dirs[strings.ToLower(repo.ID)]; dir != "" {
				repo.Checkout = &dir
				repo.Clone = "found"
				repo.CloneError = nil
			}
			if localStep && repo.Selected && repo.Clone == "cloning" {
				f.startTeamClone(id, repo.ID, kept.Path)
			}
		}
	}
	if localStep {
		view.State = "checkouts"
		if local.Done {
			view.State = "done"
		}
	}
	view.ID = id
	view.Mode = "team"
	view.Server = row.URL
	write(w, 200, view)
	return true
}
func (f *FactoryRegistry) startTeamClone(id, repo, path string) {
	if f.SetupCheckout == nil || f.closed {
		return
	}
	if f.setupBusy == nil {
		f.setupBusy = map[string]bool{}
	}
	key := id + "\x00" + repo
	if f.setupBusy[key] {
		return
	}
	f.setupBusy[key] = true
	guard.Go("app/factory checkout", func() {
		ctx, cancel := context.WithTimeout(f.server.inbox.ctx, 10*time.Minute)
		defer cancel()
		got, err := f.SetupCheckout(ctx, repo, path, true)
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.setupBusy, key)
		// An unfinished clone keeps its durable intent so a restarted app can continue it.
		if ctx.Err() != nil {
			return
		}
		kept := localSetupCheckout{Path: got, State: "found"}
		if err != nil {
			words := "Clone failed; check GitHub access and the destination, then try again"
			kept = localSetupCheckout{Path: path, State: "failed", Error: &words}
		}
		_, _ = f.connections(func(saved *factoryConnections) error {
			local := saved.Setups[id]
			if local.Repos == nil {
				local.Repos = map[string]localSetupCheckout{}
			}
			local.Repos[repo] = kept
			saved.Setups[id] = local
			return nil
		})
	})
}
