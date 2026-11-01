package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// limitedGitHub answers every GitHub call the way GitHub does once the rate
// limit is spent: 403 with the limit headers naming the reset an hour away.
type limitedGitHub struct{ reset time.Time }

func (g limitedGitHub) RoundTrip(r *http.Request) (*http.Response, error) {
	h := make(http.Header)
	h.Set("X-RateLimit-Limit", "5000")
	h.Set("X-RateLimit-Remaining", "0")
	h.Set("X-RateLimit-Reset", strconv.FormatInt(g.reset.Unix(), 10))
	body := `{"message":"API rate limit exceeded for user ID 1."}`
	return &http.Response{StatusCode: http.StatusForbidden, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

// TestFactoryLimitedServeProcess is the serve command in a subprocess with
// GitHub spent; only the GitHub network boundary is replaced.
func TestFactoryLimitedServeProcess(t *testing.T) {
	if os.Getenv("CODEAF_LIMITED_SERVE_HELPER") != "1" {
		return
	}
	t.Setenv("CODEAF_HOME", os.Getenv("LIMITED_TEST_HOME"))
	http.DefaultTransport = limitedGitHub{reset: time.Now().Add(time.Hour)}
	if err := runFactory([]string{"serve", "--listen", "127.0.0.1:0"}); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

// A SPENT GITHUB RATE LIMIT NEVER HOLDS THE LISTENER: the factory answers at
// once, says when GitHub reads again, and logs the limit once.
func TestFactoryServeAnswersWhileGitHubIsRateLimited(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "factory", "config.yaml")
	if err := writeFactoryPrivate(config, []byte("listen: 127.0.0.1:0\naccess_token: script-secret\ngithub_client_id: client\n")); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestFactoryLimitedServeProcess$")
	cmd.Env = append(os.Environ(), "CODEAF_LIMITED_SERVE_HELPER=1", "LIMITED_TEST_HOME="+root, "GH_TOKEN=limited-secret", "GITHUB_TOKEN=", "OPENROUTER_API_KEY=", "OPENAI_API_KEY=")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	logged := make(chan string, 64)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			logged <- scanner.Text()
		}
	}()
	address := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			address <- scanner.Text()
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()
	var url string
	select {
	case url = <-address:
	case <-time.After(10 * time.Second):
		t.Fatal("serve printed no address while GitHub was rate-limited")
	}
	client := &http.Client{Timeout: time.Second}
	reqStart := time.Now()
	resp, err := client.Get(url + "/v1/about")
	if err != nil {
		t.Fatalf("the listener did not answer within 1 s (%v after start): %v", time.Since(started), err)
	}
	defer resp.Body.Close()
	if took := time.Since(reqStart); took > time.Second {
		t.Fatalf("about took %v", took)
	}
	deadline := time.After(10 * time.Second)
	limitLines := 0
	for limitLines == 0 {
		select {
		case line := <-logged:
			if strings.Contains(line, "GitHub is rate-limited until") {
				limitLines++
			}
		case <-deadline:
			t.Fatal("serve never logged the rate limit")
		}
	}
	var about struct {
		GitHub    string     `json:"github_limited"`
		ResumesAt *time.Time `json:"github_resumes_at"`
	}
	for end := time.Now().Add(5 * time.Second); ; {
		r, err := client.Get(url + "/v1/about")
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewDecoder(r.Body).Decode(&about)
		r.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if about.GitHub != "" || time.Now().After(end) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.HasPrefix(about.GitHub, "GitHub is rate-limited until ") || about.ResumesAt == nil {
		t.Fatalf("about while limited: %+v", about)
	}
	// The poll meets the same limit again; the limit is still logged once.
	time.Sleep(500 * time.Millisecond)
	for {
		select {
		case line := <-logged:
			if strings.Contains(line, "GitHub is rate-limited until") {
				t.Fatalf("the limit was logged twice: %q", line)
			}
			continue
		default:
		}
		break
	}
}

// THE LIMIT IS LOGGED ONCE WHEN MET AND ONCE WHEN IT CLEARS, whatever reads
// meet it in between, and a refusal that is not a limit changes nothing.
func TestGitHubLimitLogsOnceWhenMetAndOnceWhenCleared(t *testing.T) {
	var log strings.Builder
	now := time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC)
	l := newGitHubLimit(&log)
	l.now = func() time.Time { return now }
	spent := &forge.APIError{StatusCode: http.StatusForbidden, Body: "API rate limit exceeded", RetryAfter: 16 * time.Minute}
	l.saw(spent)
	l.saw(spent)
	l.saw(&forge.APIError{StatusCode: http.StatusNotFound})
	if got := l.resets(); !got.Equal(now.Add(16 * time.Minute)) {
		t.Fatalf("resets = %v", got)
	}
	l.saw(nil)
	l.saw(nil)
	lines := strings.Split(strings.TrimSpace(log.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "GitHub is rate-limited until ") || !strings.Contains(lines[1], "cleared") {
		t.Fatalf("log = %q", log.String())
	}
	if !l.resets().IsZero() {
		t.Fatal("a cleared limit is still reported")
	}
}
