package serve_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// gitFolder is a throwaway git repository with one commit, and a folder
// inside it, so a pick of the inner folder reads the repository's top.
func gitFolder(t *testing.T, remote string) (top, inner string) {
	t.Helper()
	top, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inner = filepath.Join(top, "src", "app")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	steps := [][]string{{"init", "-q", "-b", "main"}, {"add", "README.md"}, {"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "one"}}
	if remote != "" {
		steps = append(steps, []string{"remote", "add", "origin", remote})
	}
	for _, args := range steps {
		if out, err := exec.Command("git", append([]string{"-C", top}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return top, inner
}

// personalFolders is newV2Jobs as Personal: a solo factory whose seam makes
// jobs the way this machine's does.
func personalFolders(t *testing.T) *v2Jobs { return foldersIn(t, api.FactorySolo) }

func foldersIn(t *testing.T, mode string) *v2Jobs {
	t.Helper()
	f := newV2Jobs(t)
	f.h.Close()
	local := factory.LocalSeam(f.st, time.Now())
	local.Launch = f.seam.Launch
	f.seam = local
	f.cfg.Mode = mode
	f.h = serve.New(f.seam, f.cfg)
	return f
}

func features(t *testing.T, h http.Handler) []string {
	t.Helper()
	w := verbRequest(h, "GET", "/v1/features", "")
	var got api.Features
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("features = %d %s", w.Code, w.Body)
	}
	return got.Features
}

// A JOB IN PERSONAL CAN BE POINTED AT A FOLDER ON THIS COMPUTER (owner
// decision 2, 2026-11-17): the factory lists the door, takes the folder,
// files the job on the top of the repository the folder is in, and the
// job's read says where it works.
func TestPersonalTakesAFolderOnThisComputer(t *testing.T) {
	f := personalFolders(t)
	if got := features(t, f.h); !slices.Contains(got, factory.FeatureLocalFolder) {
		t.Fatalf("Personal features = %v, want %q", got, factory.FeatureLocalFolder)
	}
	top, inner := gitFolder(t, "")
	raw, _ := json.Marshal(api.NewJob{Folder: inner, Title: "Tidy the readme", Source: factory.AskApp})
	w := verbRequest(f.h, "POST", "/v1/jobs", string(raw))
	checkJSON(t, w, 201)
	var made struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &made); err != nil {
		t.Fatal(err)
	}
	saved, err := f.st.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Repo != top || factory.FolderOf(saved) != top {
		t.Fatalf("job repo = %q, want the repository's top %q", saved.Repo, top)
	}
	j := f.read(t, saved)
	if j.Folder == nil || j.Folder.Path != top {
		t.Fatalf("job folder = %+v, want %q", j.Folder, top)
	}
	if j.Checkout != "" && j.Checkout != string(factory.CheckoutHere) {
		t.Fatalf("checkout = %q, want here or nothing", j.Checkout)
	}
	if j.Can.OpenPR {
		t.Fatal("a job that has not run offered Open a pull request")
	}
}

func TestPersonalRefusesAFolderWithNoRepository(t *testing.T) {
	f := personalFolders(t)
	bare := t.TempDir()
	raw, _ := json.Marshal(api.NewJob{Folder: bare, Title: "Tidy", Source: factory.AskApp})
	w := verbRequest(f.h, "POST", "/v1/jobs", string(raw))
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), factory.FolderNotGitWords) {
		t.Fatalf("folder with no repository = %d %s", w.Code, w.Body)
	}
	raw, _ = json.Marshal(api.NewJob{Folder: bare, Repo: "acme/api", Title: "Tidy", Source: factory.AskApp})
	if w := verbRequest(f.h, "POST", "/v1/jobs", string(raw)); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("folder beside a repository = %d %s", w.Code, w.Body)
	}
}

// IN AN ORGANIZATION THE DOOR IS ABSENT: the server cannot reach a person's
// laptop, so it neither lists the feature nor takes a folder.
func TestAnOrganizationHasNoFolderDoor(t *testing.T) {
	f := foldersIn(t, api.FactoryTeam)
	if got := features(t, f.h); slices.Contains(got, factory.FeatureLocalFolder) {
		t.Fatalf("organization features = %v, listed %q", got, factory.FeatureLocalFolder)
	}
	_, inner := gitFolder(t, "")
	raw, _ := json.Marshal(api.NewJob{Folder: inner, Title: "Tidy", Source: factory.AskApp})
	w := verbRequest(f.h, "POST", "/v1/jobs", string(raw))
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "only be used in Personal") {
		t.Fatalf("organization folder = %d %s", w.Code, w.Body)
	}
}

// A FINISHED FOLDER JOB OFFERS A PULL REQUEST ONLY WHEN IT CAN WORK: a folder
// with no GitHub remote never offers it, and the door says why.
func TestAFolderWithNoGitHubRemoteOffersNoPullRequest(t *testing.T) {
	f := personalFolders(t)
	f.cfg.GitHubToken = nil
	top, _ := gitFolder(t, "")
	it, err := f.st.Create(factory.Item{Repo: top, Kind: factory.KindIssue, Title: "Tidy", State: factory.StateShipped, HeadCommit: strings.Repeat("a", 40),
		Stream: &factory.Stream{Log: []factory.LogLine{{Text: "branch: factory/1-tidy"}}}})
	if err != nil {
		t.Fatal(err)
	}
	j := f.read(t, it)
	if j.State != "done" || j.Folder == nil || j.Folder.Branch != "factory/1-tidy" {
		t.Fatalf("finished folder job = state %q folder %+v", j.State, j.Folder)
	}
	if j.Can.OpenPR {
		t.Fatal("a folder with no GitHub remote offered Open a pull request")
	}
	w := verbRequest(f.h, "POST", fmt.Sprintf("/v1/jobs/%d/open_pr", it.ID), "{}")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), factory.FolderNoGitHubWords) {
		t.Fatalf("open_pr = %d %s", w.Code, w.Body)
	}
}
