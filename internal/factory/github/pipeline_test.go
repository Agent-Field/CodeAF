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
