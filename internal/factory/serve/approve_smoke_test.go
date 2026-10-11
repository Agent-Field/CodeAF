package serve_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// TestApprovePlaywright drives the approve verb through a real browser on the
// fixture floor. Opt-in like the other browser smokes (`make test-web-e2e`).
// The fixture leaves Answer nil so a still floor cannot change; this test
// supplies only that door.
func TestApprovePlaywright(t *testing.T) {
	if os.Getenv("CODEAF_WEB_SMOKE") != "1" {
		t.Skip("browser smoke: set CODEAF_WEB_SMOKE=1 to run it")
	}
	for _, bin := range []string{"npm"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip("approve smoke needs " + bin)
		}
	}
	seam := factory.FixtureSeam(now)
	seam.Answer = func(int, bool, string) error { return nil }
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{Login: "santosh", Via: factory.ViaGH}, nil
	}
	modules := playwrightModules(t)
	linkPlaywright(t, modules)
	srv := httptest.NewServer(serve.New(seam, recordConfig(t)))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(modules, ".bin", "playwright"), "test", "e2e/approve.spec.ts", "--reporter=line")
	cmd.Dir = "web"
	cmd.Env = append(os.Environ(), "FACTORY_WEB="+srv.URL, "FACTORY_APPROVE=1", "NODE_PATH="+modules)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("playwright smoke: %v\n%s", err, out)
	}
}
