package main

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestFactoryAddPostsToARunningFactory(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := serve.New(factory.FixtureSeam(time.Now()), serve.Config{Store: st, AccessToken: "floor-secret"})
	defer h.Close()
	srv := httptest.NewServer(h)
	defer srv.Close()

	args := []string{"--source", "inbox:ops", "--id", "A1", "--title", "Fix the login timeout", "--body", "-",
		"--label", "bug", "--label", "auth", "--priority", "p0", "--repo", "acme/web",
		"--url", "https://ops.example.com/A1", "--factory", srv.URL, "--token", "floor-secret"}
	var out bytes.Buffer
	if err := runFactoryAdd(args, strings.NewReader("It times out after 3s.\n"), &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.HasPrefix(got, "filed job ") || !strings.Contains(got, "inbox:ops:A1") {
		t.Fatalf("first = %q", got)
	}
	out.Reset()
	if err := runFactoryAdd(args, strings.NewReader("It times out after 3s.\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "updated job ") {
		t.Fatalf("again = %q", out.String())
	}
	items, err := st.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("jobs = %d, %v", len(items), err)
	}
	it := items[0]
	if it.Body != "It times out after 3s." || len(it.Labels) != 2 || it.Repo != "acme/web" || it.Priority == nil || it.Priority.Value != "urgent" {
		t.Fatalf("job = body %q labels %v repo %q priority %+v", it.Body, it.Labels, it.Repo, it.Priority)
	}

	// The factory's refusal comes back in its own words.
	bad := []string{"--source", "inbox:ops", "--id", "A2", "--title", "t", "--factory", srv.URL, "--token", "wrong"}
	if err := runFactoryAdd(bad, strings.NewReader(""), &out); err == nil || !strings.Contains(err.Error(), "the factory refused it") {
		t.Fatalf("wrong token = %v", err)
	}
}

func TestFactoryAddChecksTheEntryBeforeSending(t *testing.T) {
	var out bytes.Buffer
	err := runFactoryAdd([]string{"--source", "github", "--id", "1", "--title", "t", "--factory", "http://127.0.0.1:1"}, strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("a reserved source was sent")
	}
	if err := runFactoryAdd([]string{"--source", "inbox:ops", "stray"}, strings.NewReader(""), &out); err == nil {
		t.Fatal("a stray argument was taken")
	}
}
