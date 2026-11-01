package github_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestPipelineReadsDefaultBranchWithoutCheckoutAndRefreshesByBlob(t *testing.T) {
	text, sha := "start:\n  labels: [agent-ready]\n", "blob-one"
	reads := 0
	notModified := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo":
			fmt.Fprint(w, `{"default_branch":"trunk"}`)
		case "/repos/owner/repo/contents/" + pipeline.RepoFile:
			if r.URL.Query().Get("ref") != "" {
				t.Errorf("ref = %q", r.URL.Query().Get("ref"))
			}
			reads++
			w.Header().Set("ETag", sha)
			if notModified {
				if r.Header.Get("If-None-Match") != sha {
					t.Errorf("conditional tag = %q", r.Header.Get("If-None-Match"))
				}
				w.WriteHeader(http.StatusNotModified)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(text)), "sha": sha})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	loader := factorygithub.NewPipelineLoader(func(context.Context) factorygithub.PipelineAPI { return forge.NewClient("stub").WithBaseURL(srv.URL) })
	ctx := context.Background()
	load := func() pipeline.File {
		t.Helper()
		f, err := loader.Load(ctx, "owner/repo", "", "")
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	f := load()
	if f.Source != pipeline.SourceFile || f.From != factory.FromMainBranch || strings.Join(f.Start.Labels, ",") != "agent-ready" {
		t.Fatalf("file = %+v", f)
	}
	f.Start.Labels[0] = "mutated"
	if strings.Join(load().Start.Labels, ",") != "agent-ready" || reads != 1 {
		t.Fatal("cache was mutated or reread outside intake")
	}
	text = "start:\n  labels: [different]\n"
	loader.Refresh(ctx, "owner/repo", "")
	if strings.Join(load().Start.Labels, ",") != "agent-ready" {
		t.Fatal("unchanged SHA was reparsed")
	}
	sha = "blob-two"
	loader.Refresh(ctx, "owner/repo", "")
	if strings.Join(load().Start.Labels, ",") != "different" || reads != 3 {
		t.Fatal("new SHA did not refresh")
	}
	notModified = true
	loader.Refresh(ctx, "owner/repo", "")
	if strings.Join(load().Start.Labels, ",") != "different" || reads != 4 {
		t.Fatal("conditional read lost the cached file")
	}
}

// A PIPELINE MERGED ON GITHUB REACHES THE NEXT START with no intake tick: a
// read older than PipelineFresh asks the forge again.
func TestAPipelineReadIsAskedAgainOnceItIsStale(t *testing.T) {
	text, sha := "start:\n  labels: [one]\n", "blob-one"
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/contents/"+pipeline.RepoFile {
			http.NotFound(w, r)
			return
		}
		reads++
		w.Header().Set("ETag", sha)
		if r.Header.Get("If-None-Match") == sha {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(text)), "sha": sha})
	}))
	defer srv.Close()
	now := time.Unix(1000, 0)
	loader := factorygithub.NewPipelineLoader(func(context.Context) factorygithub.PipelineAPI { return forge.NewClient("stub").WithBaseURL(srv.URL) }).WithClock(func() time.Time { return now })
	labels := func() string {
		t.Helper()
		f, err := loader.Load(context.Background(), "owner/repo", "", "")
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(f.Start.Labels, ",")
	}
	if got := labels(); got != "one" || reads != 1 {
		t.Fatalf("first read %q after %d reads", got, reads)
	}
	text, sha = "start:\n  labels: [two]\n", "blob-two"
	now = now.Add(factorygithub.PipelineFresh - time.Second)
	if got := labels(); got != "one" || reads != 1 {
		t.Fatalf("a fresh read asked again: %q after %d reads", got, reads)
	}
	now = now.Add(2 * time.Second)
	if got := labels(); got != "two" || reads != 2 {
		t.Fatalf("a stale read was not asked again: %q after %d reads", got, reads)
	}
	// Unchanged since: one conditional request, the same file.
	now = now.Add(factorygithub.PipelineFresh + time.Second)
	if got := labels(); got != "two" || reads != 3 {
		t.Fatalf("a 304 lost the file: %q after %d reads", got, reads)
	}
}

func TestPipelineFailuresAndMissingFiles(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		status     int
		wantError  bool
	}{
		{"syntax", "start: [", 200, true},
		{"refused", "", 403, true},
		{"missing", "", 404, false},
		{"bad encoding", "", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo" {
					fmt.Fprint(w, `{"default_branch":"main"}`)
					return
				}
				if tc.status != 200 {
					w.WriteHeader(tc.status)
					return
				}
				encoding := "base64"
				if tc.name == "bad encoding" {
					encoding = "none"
				}
				json.NewEncoder(w).Encode(map[string]any{"type": "file", "encoding": encoding, "content": base64.StdEncoding.EncodeToString([]byte(tc.text)), "sha": "blob"})
			}))
			defer srv.Close()
			loader := factorygithub.NewPipelineLoader(func(context.Context) factorygithub.PipelineAPI { return forge.NewClient("stub").WithBaseURL(srv.URL) })
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, pipeline.RepoFile), []byte("start:\n  labels: [local]\n"), 0600); err != nil {
				t.Fatal(err)
			}
			f, err := loader.Load(context.Background(), "owner/repo", dir, "")
			if (err != nil) != tc.wantError || (len(f.Problems) > 0) != tc.wantError || len(f.Start.Labels) != 0 {
				t.Fatalf("file %+v, error %v", f, err)
			}
			if tc.wantError && !strings.Contains(f.RecipeProblems()[0].Error(), pipeline.RepoFile) {
				t.Fatal("reason lost its file location")
			}
			if !tc.wantError && f.Source != pipeline.SourceDefaults {
				t.Fatal("missing file did not use defaults")
			}
		})
	}
}

func TestPipelineOfflineFallbackAndPersonalLimits(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pipeline.RepoFile), []byte("start:\n  labels: [ready]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	personal := filepath.Join(t.TempDir(), "personal.yaml")
	if err := os.WriteFile(personal, []byte("start:\n  max_cost: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	loader := factorygithub.NewPipelineLoader(nil)
	f, err := loader.Load(context.Background(), "owner/repo", dir, personal)
	if err != nil || f.Start.MaxCost != 1 || strings.Join(f.Start.Labels, ",") != "ready" {
		t.Fatalf("file %+v, error %v", f, err)
	}
	f, err = loader.Load(context.Background(), "owner/repo", dir, "")
	if err != nil || f.Start.MaxCost != pipeline.DefaultMaxCost {
		t.Fatal("personal limits altered cache")
	}
}

func TestPipelineOfflineFallbackReadsNewLocalRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0700); err != nil {
		t.Fatal(err)
	}
	loader := factorygithub.NewPipelineLoader(nil)
	f, err := loader.Load(context.Background(), "owner/repo", dir, "")
	if err != nil || len(f.Start.Labels) != 0 {
		t.Fatalf("missing offline file: %+v, %v", f, err)
	}
	if err := os.WriteFile(filepath.Join(dir, pipeline.RepoFile), []byte("start:\n  labels: [ready]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err = loader.Load(context.Background(), "owner/repo", dir, "")
	if err != nil || strings.Join(f.Start.Labels, ",") != "ready" {
		t.Fatalf("offline edits stayed cached: %+v, %v", f, err)
	}
}

// hangingContents is a forge that never answers until it is given up on.
type hangingContents struct{ asked chan struct{} }

func (h hangingContents) Contents(ctx context.Context, _, _, _, _, _ string) (string, string, string, error) {
	h.asked <- struct{}{}
	<-ctx.Done()
	return "", "", "", ctx.Err()
}

// A READ THAT ONLY SHOWS THE FILE NEVER WAITS ON GITHUB: a forge that does not
// answer once held every queue read of the factory on this Mac until the app
// gave up and called it "not answering". A first read answers from the
// checkout in a moment, and a stale one keeps the last file while the forge is
// asked behind.
func TestAPipelineReadNeverWaitsOnASilentForge(t *testing.T) {
	text, sha := "start:\n  labels: [one]\n", "blob-one"
	hang := false
	asked := make(chan struct{}, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", sha)
		json.NewEncoder(w).Encode(map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(text)), "sha": sha})
	}))
	defer srv.Close()
	now := time.Unix(1000, 0)
	loader := factorygithub.NewPipelineLoader(func(context.Context) factorygithub.PipelineAPI {
		if hang {
			return hangingContents{asked}
		}
		return forge.NewClient("stub").WithBaseURL(srv.URL)
	}).WithClock(func() time.Time { return now })
	timed := func(repo string) (pipeline.File, time.Duration) {
		t.Helper()
		began := time.Now()
		f, _ := loader.Load(factorygithub.ShowOnly(context.Background()), repo, "", "")
		return f, time.Since(began)
	}
	if f, _ := timed("owner/repo"); strings.Join(f.Start.Labels, ",") != "one" {
		t.Fatalf("first read: %+v", f)
	}
	hang = true
	now = now.Add(factorygithub.PipelineFresh + time.Second)
	f, took := timed("owner/repo")
	if strings.Join(f.Start.Labels, ",") != "one" || took > time.Second {
		t.Fatalf("a stale read with the forge silent took %v: %+v", took, f.Start.Labels)
	}
	if _, took := timed("owner/other"); took > time.Second {
		t.Fatalf("a first read with the forge silent took %v", took)
	}
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("the forge was never asked behind the read")
	}
}

func (h hangingContents) User(ctx context.Context, _, _ string) (forge.UserProfile, string, error) {
	<-ctx.Done()
	return forge.UserProfile{}, "", ctx.Err()
}

// The queue and a job's card read a repository's CODEOWNERS; a forge that
// does not answer once held them, under one lock, for its whole timeout.
func TestCodeOwnersNeverHoldAReadOnASilentForge(t *testing.T) {
	asked := make(chan struct{}, 16)
	people := factorygithub.NewPeopleLoader(func(context.Context) factorygithub.PeopleAPI { return hangingContents{asked} }, time.Now)
	began := time.Now()
	for range 5 {
		if owners, err := people.Owners(context.Background(), "owner/repo"); err != nil || len(owners) != 0 {
			t.Fatalf("owners %v %v", owners, err)
		}
	}
	if took := time.Since(began); took > time.Second {
		t.Fatalf("five owner reads with the forge silent took %v", took)
	}
}
