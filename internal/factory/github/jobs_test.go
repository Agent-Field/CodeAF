package github

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/factory"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type jobTransport func(*http.Request) (*http.Response, error)

func (f jobTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJobAssignmentAndRetainedBranchDeletionUseGitHubDoors(t *testing.T) {
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	it := factory.Item{Num: 1498, Repo: "acme/api", Origin: factory.OriginForge, DoneHow: "dropped", BranchDeleteAt: at.Add(factory.DroppedBranchRetention)}
	calls := 0
	writes := JobWrites{Token: func(context.Context) string { return "secret" }, HTTP: &http.Client{Transport: jobTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("wrong GitHub door")
		}
		if r.Method == "PATCH" {
			data, _ := io.ReadAll(r.Body)
			if r.URL.Path != "/repos/acme/api/issues/1498" || string(data) != `{"assignees":["sam"]}` {
				t.Fatalf("assign %s %s", r.URL, data)
			}
		} else if r.Method != "DELETE" || r.URL.Path != "/repos/acme/api/git/refs/heads/factory/1498-fix" {
			t.Fatalf("delete %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}}
	if err := writes.Assign(context.Background(), it, "sam"); err != nil {
		t.Fatal(err)
	}
	if err := writes.DeleteDroppedBranch(context.Background(), it, "factory/1498-fix", at); err == nil {
		t.Fatal("deleted before seven days")
	}
	if calls != 1 {
		t.Fatal("early GitHub write")
	}
	if err := writes.DeleteDroppedBranch(context.Background(), it, "main", it.BranchDeleteAt); err == nil {
		t.Fatal("deleted unrelated branch")
	}
	if err := writes.DeleteDroppedBranch(context.Background(), it, "factory/1498-fix", it.BranchDeleteAt); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestJobWritesRefuseMissingIdentityAndTreatAlreadyDeletedAsDone(t *testing.T) {
	j := JobWrites{}
	it := factory.Item{Origin: factory.OriginForge, Num: 1, Repo: "acme/api"}
	if err := j.Assign(context.Background(), it, "sam"); err == nil {
		t.Fatal("assignment without identity")
	}
	it.DoneHow = "dropped"
	it.BranchDeleteAt = time.Now().Add(-time.Second)
	j.Token = func(context.Context) string { return "secret" }
	j.HTTP = &http.Client{Transport: jobTransport(func(r *http.Request) (*http.Response, error) {
		status := 404
		if r.Method == "GET" {
			status = 200
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("private")), Header: make(http.Header)}, nil
	})}
	if err := j.DeleteDroppedBranch(context.Background(), it, "factory/1-fix", time.Now()); err != nil {
		t.Fatal(err)
	}
}
