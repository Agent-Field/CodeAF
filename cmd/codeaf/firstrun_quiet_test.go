package main

// A first launch prints nothing a person did not ask for, and the minutes after
// a key is pasted into setup are the same install's first minutes: the fresh-
// install check of 2026-09-25 (dev 1194d4b8f) found a raw log line on the
// terminal before any key existed, and a Model Pool judge that went on asking
// with the key the boot did not have, failing every landing of the first
// session and marking each one judged so no later start ever scored it.

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/judge"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// captureStandardLog points the standard logger at a buffer for one test and
// puts the writer that was there back afterwards; before the surface parks it,
// the standard logger IS the person's terminal.
func captureStandardLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &buf
}

// Contract 3.1: with no key, the media client is an ABSENT capability — no
// generation tool on the belt and no line on the terminal. Nothing asked for
// it, so nothing says it is missing.
func TestAFirstLaunchWithNoKeyLeavesMediaAbsentWithoutALine(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	logged := captureStandardLog(t)

	settings := config.Config{Model: "vendor/model", BaseURL: "https://openrouter.ai/api/v1"}
	if media := v3MediaClient(settings); media != nil {
		t.Fatalf("a keyless install built a media client: %#v", media)
	}
	if logged.Len() != 0 {
		t.Fatalf("a keyless first launch wrote %q to the terminal, want nothing", logged.String())
	}
}

// Contract 3.1, the other half: a media client that fails for any reason
// other than the missing key is still a fault somebody may need to read, so
// that one keeps its line in the log.
func TestAMediaFailureThatIsNotTheMissingKeyStillLogs(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	logged := captureStandardLog(t)

	settings := config.Config{Model: "vendor/model", APIKey: "a-key", BaseURL: ""}
	if media := v3MediaClient(settings); media != nil {
		t.Fatalf("a media client with no base URL was built: %#v", media)
	}
	if !strings.Contains(logged.String(), "media: no generation endpoint") {
		t.Fatalf("a real media fault left no line in the log: %q", logged.String())
	}
}

// Contract 3.4: a judge with no key is an absent judge. It leaves no reason
// naming the missing key and, above all, no judged marker, so the landing is
// still there for the next start's sweep to score once a key exists.
func TestAJudgeWithNoKeyLeavesTheLandingForTheNextStart(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")
	profileDir := t.TempDir()
	poolDir := config.ProfilePath(profileDir, "pool")

	asked := map[string]bool{}
	noKey := func(model string) judge.Ask {
		return func(context.Context, string, string) (string, error) {
			asked[model] = true
			return "", provider.ErrNoAPIKey
		}
	}
	hook := poolJudgeHook(config.Config{}, profileDir, t.TempDir(), poolTestCatalog, noKey, time.Now, "task")
	if hook == nil {
		t.Fatal("a pool whose mode allows reading built no hook")
	}
	landing := poolTestLanding()
	hook(landing)

	if alreadyJudged(poolDir, landing.ID, landing.Attempt) {
		t.Fatal("a landing no judge could ask for want of a key was marked judged, so no later start will ever score it")
	}
	if last := readJudgeLast(poolDir); last != nil && strings.Contains(last.Reason, "no API key") {
		t.Fatalf("the missing key was recorded as the judge's reason: %q", last.Reason)
	}
	if len(asked) > 1 {
		t.Fatalf("the judge asked %d candidates with no key to ask with, want it to stop at the first", len(asked))
	}
}

// Contract 3.3: the chat's judge is built when the conversation is, which on
// a first launch is before setup has a key. The key pasted into setup reaches
// the process ([v3Process.setAPIKey]), and the judge's next question carries
// it — the judge reads the process's settings when it asks, not the copy the
// boot held.
func TestTheChatsJudgeAsksWithTheKeyPastedAfterBoot(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	var mu sync.Mutex
	var bearer []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		bearer = append(bearer, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"other/judge","choices":[{"index":0,"message":{"role":"assistant","content":"{\"score\": 88, \"reason\": \"it does what was asked\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer server.Close()

	profileDir := t.TempDir()
	boot := config.Config{BaseURL: server.URL, ProfileDir: profileDir}
	proc := &v3Process{ProfileDir: profileDir, Settings: boot}
	ask := poolJudgeAsk(proc.liveSettings(boot), profileDir)

	if err := proc.setAPIKey("key-pasted-in-setup"); err != nil {
		t.Fatal(err)
	}
	answer, err := ask("other/judge")(context.Background(), "system", "user")
	if err != nil {
		t.Fatalf("the judge asked after setup failed: %v", err)
	}
	if !strings.Contains(answer, "88") {
		t.Fatalf("the judge's answer = %q", answer)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bearer) == 0 || bearer[len(bearer)-1] != "Bearer key-pasted-in-setup" {
		t.Fatalf("the judge asked with %q, want the key pasted in setup", bearer)
	}
}

// Contract 3.3, the wiring: the chat door builds its judge from the live
// settings, never from the boot's copy. The helper above is only as good as
// the one line that uses it.
func TestTheChatDoorBuildsItsJudgeFromTheLiveSettings(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(".", "chatv3.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Contains(text, "poolJudgeAsk(settings,") {
		t.Fatal("chatv3.go builds a judge from the boot's settings copy, which never learns the key pasted into setup")
	}
	if strings.Count(text, "poolJudgeAsk(proc.liveSettings(settings)") < 2 {
		t.Fatal("chatv3.go does not build both its judges (the landing hook and the start sweep) from proc.liveSettings")
	}
}
