package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// fakeFilesForge is a fake GitHub serving one pull request whose head and
// file list a test can move, counting every file-list page it is asked for.
type fakeFilesForge struct {
	mu     sync.Mutex
	head   string
	state  string
	files  []map[string]any
	total  int
	fail   bool
	listed atomic.Int32
}

func (f *fakeFilesForge) serve(t *testing.T) API {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/api/pulls/7":
			pr := map[string]any{
				"number": 7, "html_url": "https://github.com/acme/api/pull/7", "state": f.state, "changed_files": f.total,
				"head": map[string]any{"sha": f.head, "ref": "factory/7"}, "base": map[string]any{"ref": "main"},
			}
			if f.state == "closed" {
				pr["merged"], pr["merged_at"] = true, "2026-11-01T10:00:00Z"
			}
			_ = json.NewEncoder(w).Encode(pr)
		case "/repos/acme/api/pulls/7/files":
			f.listed.Add(1)
			if f.fail {
				http.Error(w, "boom", http.StatusForbidden)
				return
			}
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
			lo, hi := (page-1)*per, page*per
			lo, hi = min(lo, len(f.files)), min(hi, len(f.files))
			_ = json.NewEncoder(w).Encode(f.files[lo:hi])
		default:
			http.Error(w, r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return forge.NewClient("test-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
}

// Every followed pull request carries its own file list, each path with its
// status, keyed to the head it was read on: a removed file is named as
// removed, a rename keeps where it came from, and GitHub's mode-only
// "changed" reads as a modification.
func TestFollowPRCarriesTheFileListOfItsHead(t *testing.T) {
	f := &fakeFilesForge{head: "aaa", state: "open", total: 4, files: []map[string]any{
		{"filename": "ledger.go", "status": "modified", "additions": 3, "deletions": 1},
		{"filename": "ledger_test.go", "status": "added", "additions": 40},
		{"filename": ".codeaf/factory.yaml", "status": "removed", "deletions": 12},
		{"filename": "cmd/new.go", "status": "renamed", "previous_filename": "cmd/old.go"},
	}}
	s := New(f.serve(t), nil, nil)
	got, err := s.FollowPR(context.Background(), "acme", "api", 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []factory.ChangedFile{
		{Path: "ledger.go", Status: "modified", Added: 3, Removed: 1},
		{Path: "ledger_test.go", Status: "added", Added: 40},
		{Path: ".codeaf/factory.yaml", Status: "removed", Removed: 12},
		{Path: "cmd/new.go", Status: "renamed", PreviousPath: "cmd/old.go"},
	}
	if fmt.Sprint(got.Files) != fmt.Sprint(want) || got.FilesTotal != 4 || got.FilesHead != "aaa" || got.HeadSHA != "aaa" {
		t.Fatalf("files %+v total %d head %q", got.Files, got.FilesTotal, got.FilesHead)
	}
	// A list that cannot be read is not a problem with the pull request.
	for _, p := range got.Problems {
		if p.Area == "files" {
			t.Fatalf("a file list read became a read problem: %+v", p)
		}
	}

	// The wire says it the way the desk reads it: pr.files with path and
	// status, pr.files_total, and pr.files_head beside pr.head_sha.
	b, err := json.Marshal(api.FromItem(factory.Item{ID: 1, PullRequest: &got}).PR)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		HeadSHA    string           `json:"head_sha"`
		FilesHead  string           `json:"files_head"`
		FilesTotal int              `json:"files_total"`
		Raw        []map[string]any `json:"files"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.HeadSHA != "aaa" || wire.FilesHead != "aaa" || wire.FilesTotal != 4 || len(wire.Raw) != 4 ||
		wire.Raw[2]["path"] != ".codeaf/factory.yaml" || wire.Raw[2]["status"] != "removed" || wire.Raw[3]["previous_path"] != "cmd/old.go" {
		t.Fatalf("wire %s", b)
	}
}

// A list is read once per head. The same head again costs nothing, a stored
// list for the same head answers even a fresh process, and a new head reads
// again so a stale list never describes it.
func TestPRFilesAreReadOncePerHead(t *testing.T) {
	f := &fakeFilesForge{head: "aaa", state: "open", total: 1, files: []map[string]any{{"filename": "a.go", "status": "modified"}}}
	client := f.serve(t)
	s := New(client, nil, nil)
	first, _ := s.followPR(context.Background(), "acme", "api", 7, nil)
	if _, err := s.followPR(context.Background(), "acme", "api", 7, nil); err != nil {
		t.Fatal(err)
	}
	if n := f.listed.Load(); n != 1 {
		t.Fatalf("the same head was listed %d times", n)
	}
	if got, _ := New(client, nil, nil).followPR(context.Background(), "acme", "api", 7, &first); f.listed.Load() != 1 || got.FilesHead != "aaa" || len(got.Files) != 1 {
		t.Fatalf("a stored list for this head was read again: %d %+v", f.listed.Load(), got)
	}

	f.mu.Lock()
	f.head, f.files = "bbb", []map[string]any{{"filename": "b.go", "status": "added"}, {"filename": "a.go", "status": "removed"}}
	f.total = 2
	f.mu.Unlock()
	moved, _ := s.followPR(context.Background(), "acme", "api", 7, &first)
	if f.listed.Load() != 2 || moved.FilesHead != "bbb" || len(moved.Files) != 2 || moved.Files[1].Status != "removed" {
		t.Fatalf("a new head kept the old list: %d %+v", f.listed.Load(), moved)
	}

	// A new head whose list cannot be read is unknown, never the old head's.
	f.mu.Lock()
	f.head, f.fail = "ccc", true
	f.mu.Unlock()
	failed, _ := s.followPR(context.Background(), "acme", "api", 7, &moved)
	if failed.HeadSHA != "ccc" || failed.FilesHead != "" || len(failed.Files) != 0 {
		t.Fatalf("an unread head carried a list: %+v", failed)
	}
}

// The list is bounded: a change of many files keeps the first PRFilesMost in
// GitHub's order and says the whole change's size, paging a hundred at a time.
func TestPRFilesAreBounded(t *testing.T) {
	f := &fakeFilesForge{head: "aaa", state: "open", total: 450}
	for i := range 450 {
		f.files = append(f.files, map[string]any{"filename": fmt.Sprintf("f%03d.go", i), "status": "added"})
	}
	got, err := New(f.serve(t), nil, nil).FollowPR(context.Background(), "acme", "api", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != factory.PRFilesMost || got.FilesTotal != 450 || got.Files[0].Path != "f000.go" || got.Files[factory.PRFilesMost-1].Path != "f299.go" {
		t.Fatalf("bounded list: %d files of %d", len(got.Files), got.FilesTotal)
	}
	if n := f.listed.Load(); n != factory.PRFilesMost/100 {
		t.Fatalf("listed %d pages for %d files", n, factory.PRFilesMost)
	}
}

// The poll writes the list onto every job with a pull request, including one
// a run on another service delivered, which has no worktree here.
func TestFollowOpenedStoresTheFileListOnARunElsewhereJob(t *testing.T) {
	st := openStore(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateShipped, Publish: "pr", RunOn: "swe-af.build",
		PullRequest: &factory.PullRequest{Number: 7, State: factory.PROpen, URL: "https://github.com/acme/api/pull/7"}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeFilesForge{head: "aaa", state: "open", total: 1, files: []map[string]any{{"filename": ".codeaf/factory.yaml", "status": "removed"}}}
	s := New(f.serve(t), nil, nil)
	if err := followOpened(context.Background(), s, st); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(it.ID)
	if got.PullRequest == nil || got.PullRequest.FilesHead != "aaa" || len(got.PullRequest.Files) != 1 || got.PullRequest.Files[0].Status != "removed" {
		t.Fatalf("stored %+v", got.PullRequest)
	}
	if err := followOpened(context.Background(), New(f.serve(t), nil, nil), st); err != nil {
		t.Fatal(err)
	}
	if n := f.listed.Load(); n != 1 {
		t.Fatalf("a stored list was read again after a restart: %d", n)
	}

	// Merged on GitHub: the job keeps the list for that head, with no read.
	f.mu.Lock()
	f.state = "closed"
	f.mu.Unlock()
	if err := followOpened(context.Background(), New(f.serve(t), nil, nil), st); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get(it.ID)
	if n := f.listed.Load(); n != 1 || got.PullRequest.State != factory.PRMerged || got.PullRequest.FilesHead != "aaa" || len(got.PullRequest.Files) != 1 {
		t.Fatalf("merged: listed %d, stored %+v", n, got.PullRequest)
	}
}
