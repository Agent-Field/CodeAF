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
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// TestNewJobAndAuditPlaywright opens both pages on a FixtureSeam floor with a
// record. New and Launch are the two doors the form posts through; the fixture
// leaves them nil so a still floor cannot change, and this test supplies only
// those doors. The audit page reads the record, not the seam.
func TestNewJobAndAuditPlaywright(t *testing.T) {
	if os.Getenv("CODEAF_WEB_SMOKE") != "1" {
		t.Skip("browser smoke: set CODEAF_WEB_SMOKE=1 to run it")
	}
	if _, err := exec.LookPath("google-chrome"); err != nil {
		if _, err := exec.LookPath("google-chrome-stable"); err != nil {
			t.Skip("new job and audit smoke needs google-chrome")
		}
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("new job and audit smoke needs npm")
	}
	cfg := recordConfig(t)
	ctx := context.Background()
	for _, event := range []record.Event{
		{Kind: record.Approved, Job: 9, Handle: "santosh", Cost: 1.5, Detail: "proof"},
		{Kind: record.Answered, Job: 1, Handle: "santosh", Detail: "yes"},
		{Kind: record.Started, Job: 2},
		{Kind: record.AutoStarted, Job: 3, Handle: "santosh", Detail: "agent-ready"},
		{Kind: record.Took, Job: 4, Handle: "priya"},
		{Kind: record.Dropped, Job: 8, Handle: "santosh", Detail: "duplicate of #1490"},
	} {
		if _, err := cfg.Record.Append(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	seam := factory.FixtureSeam(now)
	seam.New = func(repo, words string) (int, error) { return 42, nil }
	seam.Launch = func(int) error { return nil }
	modules := playwrightModules(t)
	linkPlaywright(t, modules)
	srv := httptest.NewServer(serve.New(seam, cfg))
	t.Cleanup(srv.Close)
	runCtx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(runCtx, filepath.Join(modules, ".bin", "playwright"), "test", "e2e/new-audit.spec.ts", "--reporter=line")
	cmd.Dir = "web"
	cmd.Env = append(os.Environ(), "FACTORY_WEB="+srv.URL, "FACTORY_AUDIT=1", "NODE_PATH="+modules)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("playwright smoke: %v\n%s", err, out)
	}
}
