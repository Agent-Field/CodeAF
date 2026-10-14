package agentsession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/agentsession/appx"
)

func TestSessionCanReadARepositoryWhoseRootIsASymlink(t *testing.T) {
	root := repo(t)
	alias := filepath.Join(t.TempDir(), "repository-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("this platform cannot create the root alias: %v", err)
	}
	api := (&fakeAPI{}).then(callTool("read_file", `{"path":"README.md"}`)).then(func(body wireRequest) (int, any) {
		observation := body.Messages[len(body.Messages)-1].Content[0].Text
		if !strings.Contains(observation, "# demo") || strings.Contains(observation, "outside") {
			t.Errorf("a file inside the aliased repository was refused: %s", observation)
		}
		return answer("done", 0)(body)
	})
	result, err := RunSession(t.Context(), api.serve(t), SessionOrder{Root: alias, MaxTurns: 6, Policy: testPolicy})
	if err != nil || result.Failed != "" {
		t.Fatalf("session = %+v, error = %v", result, err)
	}
}

func TestAliasedRootKeepsEveryToolInsideTheRepository(t *testing.T) {
	root := repo(t)
	alias := filepath.Join(t.TempDir(), "repository-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("this platform cannot create the root alias: %v", err)
	}
	tools, err := newToolbox(alias, testPolicy.Tools)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"README.md", filepath.Join(alias, "README.md"), filepath.Join(root, "README.md")} {
		if text, err := tools.read(path, 0, 0); err != nil || !strings.Contains(text, "# demo") {
			t.Fatalf("read %q = %q, %v", path, text, err)
		}
	}
	if text, err := tools.list("."); err != nil || !strings.Contains(text, "README.md") {
		t.Fatalf("list = %q, %v", text, err)
	}
	if text, err := tools.glob("**/*.py"); err != nil || !strings.Contains(text, "app/views.py") {
		t.Fatalf("glob = %q, %v", text, err)
	}
	if text, err := tools.grep("popen", ".", "", false); err != nil || !strings.Contains(text, "app/views.py:4:") {
		t.Fatalf("grep = %q, %v", text, err)
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(secret, []byte("outside the owned repository"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	// A path outside the lexical root is still refused even if it links in.
	inward := filepath.Join(outside, "inward.txt")
	if err := os.Symlink(filepath.Join(root, "README.md"), inward); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{secret, inward, "escape.txt", filepath.Join(alias, "escape.txt"), "../outside.txt"} {
		if _, err := tools.read(path, 0, 0); err == nil {
			t.Fatalf("read escaped through %q", path)
		}
	}
}

func TestHarnessAcceptsTheCanonicalSpellingOfItsAliasedRepository(t *testing.T) {
	root := repo(t)
	alias := filepath.Join(t.TempDir(), "repository-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("this platform cannot create the root alias: %v", err)
	}
	api := (&fakeAPI{}).then(answer("done", 0))
	app, err := New(api.serve(t), Config{Root: alias, Sessions: 1, Calls: 1, MaxTurns: 6, SessionWall: time.Minute, Policy: testPolicy})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := app.Harness(t.Context(), "answer", nil, nil, appx.HarnessOptions{ProjectDir: root}); err != nil || result.IsError {
		t.Fatalf("the canonical owned project was refused: %+v, %v", result, err)
	}
	if _, err := app.Harness(t.Context(), "answer", nil, nil, appx.HarnessOptions{ProjectDir: t.TempDir()}); err == nil {
		t.Fatal("an unrelated project was accepted")
	}
}
