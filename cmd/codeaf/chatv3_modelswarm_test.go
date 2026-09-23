package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A PROCESS'S MODEL WARM IS A WRITER IT MUST JOIN AT CLOSE.
//
// [v3Process.warmModels] resolves the model catalog — a network round-trip on a
// cold cache — and then writes the picker's cache through
// [tui3.WriteModelCache]. That write resolves CODEAF_HOME WHEN IT WRITES, not
// when the warm started, and [guard.Go] joins nothing at shutdown — so a warm
// that outlives the process that asked for it lands in whichever state root is
// current when the rows finally arrive. In a package run that is the NEXT test's
// own TempDir, whose clean-up then fails with `unlinkat …: directory not empty`
// (the same race #1124 closed for the pool errands; poolindex.go).
//
// THIS IS THE RACE, MADE CERTAIN. The endpoint below holds the catalog fetch
// open until the test releases it, so a warm that is not seated on the profile's
// errand tracker is certainly still waiting when the process closes — and once
// released it resolves CODEAF_HOME, which the test has already moved to a fresh
// directory. The tracker's own context is what ends the wait at the close
// ([poolErrandGoCtx]), and `stopPoolErrands` is what joins the goroutine; a warm
// started fire-and-forget does neither, and the write lands in the moved root.
func TestTheLaunchesModelWarmStopsWhenTheProcessCloses(t *testing.T) {
	held := make(chan struct{})
	var release sync.Once
	free := func() { release.Do(func() { close(held) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-held:
		case <-r.Context().Done():
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"vendor/sees","context_length":200000,`+
			`"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}}]}`)
	}))
	t.Cleanup(func() { free(); server.Close() })

	// The process resolves its catalog against the held endpoint, so the warm
	// has something to wait on and the writer is genuinely in flight.
	t.Setenv("CODEAF_BASE_URL", server.URL)
	proc := v3TestProcess(t)
	agent := v3TrackedAgent(t, t.TempDir())
	proc.warmModels("chatv3/models", agent, "test/model")

	// THE NEXT TEST'S ROOT. A warm that outlives this process resolves
	// CODEAF_HOME here and writes into a directory this test owns.
	landing := t.TempDir()
	t.Setenv("CODEAF_HOME", landing)

	// The owner closes, and then the held fetch is released: a writer that was
	// joined is done and writes nothing; one that was not lands here.
	proc.closeAll()
	free()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(landing, "v3", "models.json")); err == nil {
			t.Fatalf("the model warm wrote %s after the process closed.\n"+
				"The warm resolves CODEAF_HOME at the moment it writes, so a warm started outside the\n"+
				"profile's errand tracker lands in whichever root is current when the rows arrive — the\n"+
				"TempDir clean-up's own \"directory not empty\". It must be seated on the tracker and joined\n"+
				"at close (chatv3_process.go, poolindex.go).", filepath.Join(landing, "v3", "models.json"))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A WARM WRITES THE DEFAULT SERVICE'S LIST AND NOTHING ELSE (#1383). A
// conversation that opens on `codex/gpt-5.5` — every launch, once a Codex
// sign-in has saved it as the chat model — used to hand the warm the Codex
// catalog it started on, and the warm wrote Codex's bare ids into the
// OpenRouter model list. The warm now always warms the process's own catalog,
// so the list it writes is the router's rows whatever the conversation is on.
func TestAWarmForAConversationOnCodexWritesOnlyTheRoutersList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"vendor/router-model","context_length":200000}]}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("CODEAF_BASE_URL", server.URL)
	proc := v3TestProcess(t)
	agent := v3TrackedAgent(t, t.TempDir())
	written := filepath.Join(os.Getenv("CODEAF_HOME"), "v3", "models.json")
	proc.warmModels("chatv3/models", agent, "codex/gpt-5.5")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(written); err == nil {
			if !strings.Contains(string(raw), "vendor/router-model") || strings.Contains(string(raw), `"gpt-5.5"`) {
				t.Fatalf("the router's model list after a Codex conversation's warm = %s", raw)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the warm wrote no router list at %s", written)
}
