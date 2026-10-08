package mock

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// settings is the floor's own settings as the mock keeps them: a GitHub
// account that starts unconnected with gh logged in, so the connect prompt is
// what `R` shows first; the repositories it watches, which are the world's
// own; and every repository's recipe, saved in memory over the world's.
type settings struct {
	link    factory.GitHubLink
	watched []string
}

// mockLogin is the account the mock's gh is logged in as. It is a made-up
// name, because a mock is never anybody's account.
const mockLogin = "floor-tester"

// settingsDoors fills the settings doors, each under the world's one lock.
func (w *World) settingsDoors(s *factory.Seam) {
	s.Repos = func(ctx context.Context) ([]string, []factory.RepoInfo, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		var available []factory.RepoInfo
		for i, r := range w.repos {
			owner, name, _ := strings.Cut(r.Name, "/")
			available = append(available, factory.RepoInfo{Full: r.Name, Owner: owner, Name: name, Private: i%2 == 1, Pushed: w.now.Add(-time.Duration(i+1) * 7 * time.Hour)})
		}
		return append([]string(nil), w.set.watched...), available, nil
	}
	s.SetRepos = func(repos []string) error {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.set.watched = append([]string(nil), repos...)
		return nil
	}
	s.GitHub = func(ctx context.Context) (factory.GitHubLink, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.set.link, nil
	}
	s.GHLogin = func(ctx context.Context) (string, error) { return mockLogin, nil }
	s.ConnectGitHub = func(ctx context.Context, token string) error {
		w.mu.Lock()
		defer w.mu.Unlock()
		via := factory.ViaGH
		if strings.TrimSpace(token) != "" {
			via = factory.ViaToken
		}
		w.set.link = factory.GitHubLink{Login: mockLogin, Via: via}
		return nil
	}
	s.RecipeAt = func(repo string) (factory.Recipe, []factory.Problem, string, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, r := range w.repos {
			if r.Name == repo {
				return r.Recipe, nil, "/mock/" + repo, nil
			}
		}
		return factory.DefaultRecipe(), nil, "", nil
	}
	s.SaveRecipe = func(repo string, rec factory.Recipe) error {
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, r := range w.repos {
			if r.Name == repo {
				r.Recipe = rec
				return nil
			}
		}
		return errors.New("the mock has no " + repo)
	}
	s.SetRail = func(usd float64) error {
		if usd < 0 {
			return errors.New("a rail is never below nothing")
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		w.rail = usd
		return nil
	}
}
