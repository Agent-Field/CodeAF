package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A missing key must abort at startup. Otherwise the classifier, architecture,
// and planner stages each spend a turn rediscovering it.
func TestRunCLIFailsFastWithoutAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
	))
	defer server.Close()
	t.Setenv("CODEAF_CP_URL", server.URL)
	t.Setenv("OPENROUTER_API_KEY", "")

	workspace := t.TempDir()
	t.Setenv("PLANDB_DB", workspace+"/.plandb.db")

	err := runCLI(
		context.Background(), gateArgs(workspace), nil, io.Discard, io.Discard,
	)
	if err == nil {
		t.Fatal("expected a startup error when OPENROUTER_API_KEY is unset")
	}
	if !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("error should name the missing variable, got %q", err)
	}
}
