package github

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/factory"
	"io"
	"net/http"
	"strings"
	"testing"
)

type jobTransport func(*http.Request) (*http.Response, error)

func (f jobTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJobAssignmentUsesTheGitHubDoor(t *testing.T) {
	it := factory.Item{Num: 1498, Repo: "acme/api", Origin: factory.OriginForge}
	calls := 0
	writes := JobWrites{Token: func(context.Context) string { return "secret" }, HTTP: &http.Client{Transport: jobTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("wrong GitHub door")
		}
		data, _ := io.ReadAll(r.Body)
		if r.Method != "PATCH" || r.URL.Path != "/repos/acme/api/issues/1498" || string(data) != `{"assignees":["sam"]}` {
			t.Fatalf("assign %s %s %s", r.Method, r.URL, data)
		}
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}}
	if err := writes.Assign(context.Background(), it, "sam"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestJobWritesRefuseMissingIdentity(t *testing.T) {
	it := factory.Item{Origin: factory.OriginForge, Num: 1, Repo: "acme/api"}
	if err := (JobWrites{}).Assign(context.Background(), it, "sam"); err == nil {
		t.Fatal("assignment without identity")
	}
}
