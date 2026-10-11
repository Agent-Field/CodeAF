package serve_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// settingsSmokeSeam is a floor the settings page can change. The repositories
// have no checkout, so the pipeline read is the defaults and the page has to
// say there is no file. Watched starts without acme/web so the smoke's switch
// is a real PUT, read back from this seam.
func settingsSmokeSeam() factory.Seam {
	var mu sync.Mutex
	watched := []string{"acme/api"}
	seam := factory.FixtureSeam(now)
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{Login: "santosh", Via: factory.ViaGH}, nil
	}
	seam.ConnectGitHub = func(context.Context, string) error { return nil }
	seam.Repos = func(context.Context) ([]string, []factory.RepoInfo, error) {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), watched...), []factory.RepoInfo{
			{Full: "acme/api", Name: "api", Owner: "acme"},
			{Full: "acme/web", Name: "web", Owner: "acme"},
		}, nil
	}
	seam.SetRepos = func(names []string) error {
		mu.Lock()
		defer mu.Unlock()
		watched = append([]string(nil), names...)
		return nil
	}
	seam.RecipeAt = func(string) (factory.Recipe, []factory.Problem, string, error) {
		return factory.DefaultRecipe(), nil, "", nil
	}
	return seam
}

// playwrightModules installs the test runner where the page's package.json
// cannot see it. A devDependency there would make every `make web` download
// a browser. System Chrome is the browser; the install skips that download.
func playwrightModules(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{\"private\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("npm", "install", "--silent", "--ignore-scripts", "@playwright/test@1.55.1")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install playwright: %v\n%s", err, out)
	}
	return filepath.Join(dir, "node_modules")
}

// linkPlaywright makes the runner importable from the dashboard directory.
// Node resolves the config's import from that directory, and NODE_PATH is
// not consulted for an ESM import.
func linkPlaywright(t *testing.T, modules string) {
	t.Helper()
	dest := filepath.Join("web", "node_modules")
	entries, err := os.ReadDir(modules)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == ".bin" {
			continue
		}
		target := filepath.Join(dest, entry.Name())
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		if err := os.Symlink(filepath.Join(modules, entry.Name()), target); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(target) })
	}
}

func TestSettingsPlaywright(t *testing.T) {
	// A BROWSER SMOKE IS OPT-IN. A clean checkout has no node_modules and CI
	// runs no browser, so the ordinary suite must pass without either; the
	// smoke runs only when someone asks for it with CODEAF_WEB_SMOKE=1.
	if os.Getenv("CODEAF_WEB_SMOKE") != "1" {
		t.Skip("browser smoke: set CODEAF_WEB_SMOKE=1 to run it")
	}
	if _, err := exec.LookPath("google-chrome"); err != nil {
		if _, err := exec.LookPath("google-chrome-stable"); err != nil {
			t.Skip("settings smoke needs google-chrome")
		}
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("settings smoke needs npm")
	}
	modules := playwrightModules(t)
	linkPlaywright(t, modules)
	srv := httptest.NewServer(serve.New(settingsSmokeSeam(), config))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	bin := filepath.Join(modules, ".bin", "playwright")
	cmd := exec.CommandContext(ctx, bin, "test", "--reporter=line")
	cmd.Dir = "web"
	cmd.Env = append(os.Environ(), "FACTORY_WEB="+srv.URL, "NODE_PATH="+modules)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("playwright smoke: %v\n%s", err, out)
	}
}
