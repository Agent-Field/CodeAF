package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

type operatorGitHubTransport struct{}

func (operatorGitHubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var body string
	switch r.URL.String() {
	case "https://github.com/login/device/code":
		body = `{"device_code":"test-device","user_code":"CODE-1234","verification_uri":"https://github.com/login/device","interval":1,"expires_in":900}`
	case "https://github.com/login/oauth/access_token":
		body = `{"access_token":"read-secret","scope":"read:user","token_type":"bearer"}`
	case "https://api.github.com/user":
		body = `{"login":"owner","name":"Owner"}`
	default:
		if r.URL.Host == "api.github.com" && r.Method == http.MethodGet && r.Header.Get("Authorization") == "Bearer read-secret" {
			switch r.URL.Path {
			case "/user/repos":
				body = `[{"full_name":"owner/repo","name":"repo","owner":{"login":"owner"},"private":false}]`
			case "/repos/owner/repo":
				body = `{}`
			default:
				return nil, fmt.Errorf("unexpected GitHub read: %s", r.URL)
			}
		} else {
			return nil, fmt.Errorf("unexpected GitHub request: %s %s", r.Method, r.URL)
		}
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

// TestFactoryOperatorServeProcess runs the actual serve command in a subprocess
// with only the GitHub network boundary replaced, leaving its listener and files real.
func TestFactoryOperatorServeProcess(t *testing.T) {
	if os.Getenv("CODEAF_OPERATOR_SERVE_HELPER") != "1" {
		return
	}
	t.Setenv("CODEAF_HOME", os.Getenv("OPERATOR_TEST_HOME"))
	http.DefaultTransport = operatorGitHubTransport{}
	args := []string{"serve", "--listen", "127.0.0.1:0"}
	if os.Getenv("OPERATOR_TEST_RECOVERY") == "1" {
		args = append(args, "--new-operator-link")
	}
	if err := runFactory(args); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

type operatorServeProcess struct {
	cmd     *exec.Cmd
	lines   chan string
	address string
}

func startOperatorServe(t *testing.T, root string, claimed bool, recovery ...bool) (*operatorServeProcess, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestFactoryOperatorServeProcess$")
	cmd.Env = append(os.Environ(), "CODEAF_OPERATOR_SERVE_HELPER=1", "OPERATOR_TEST_HOME="+root, "GH_TOKEN=", "GITHUB_TOKEN=", "OPENROUTER_API_KEY=", "OPENAI_API_KEY=")
	if len(recovery) > 0 && recovery[0] {
		cmd.Env = append(cmd.Env, "OPERATOR_TEST_RECOVERY=1")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &operatorServeProcess{cmd: cmd, lines: make(chan string, 16)}
	go func() {
		defer close(p.lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			p.lines <- scanner.Text()
		}
	}()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	p.address = p.nextLine(t)
	if !strings.HasPrefix(p.address, "http://127.0.0.1:") {
		t.Fatalf("serve address: %q", p.address)
	}
	if claimed {
		return p, ""
	}
	line := p.nextLine(t)
	if !strings.HasPrefix(line, "operator access link: codeaf://factory/join?") || strings.Contains(line, "script-secret") {
		t.Fatalf("serve link: %q", line)
	}
	u, err := url.Parse(strings.TrimPrefix(line, "operator access link: "))
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := url.ParseQuery(u.Fragment)
	if err != nil || u.Query().Get("url") != p.address || u.Query().Get("operator") != "true" || fragment.Get("token") == "" {
		t.Fatalf("join link: %s %v", line, err)
	}
	return p, fragment.Get("token")
}

func (p *operatorServeProcess) nextLine(t *testing.T) string {
	t.Helper()
	select {
	case line, ok := <-p.lines:
		if !ok {
			t.Fatal("serve exited before printing startup")
		}
		return line
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not print startup")
	}
	return ""
}

func (p *operatorServeProcess) stop(t *testing.T, noLink bool) {
	t.Helper()
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
	for line := range p.lines {
		if noLink && strings.Contains(line, "codeaf://factory/join") {
			t.Fatalf("link printed after claim: %s", line)
		}
		if strings.Contains(line, "script-secret") {
			t.Fatal("serve printed raw host access token")
		}
	}
}

func operatorHTTP(t *testing.T, address, path, token, body string, want int, out any) {
	t.Helper()
	operatorRequest(t, http.MethodPost, address, path, token, body, want, out)
}

func operatorRequest(t *testing.T, method, address, path, token, body string, want int, out any) {
	t.Helper()
	req, err := http.NewRequest(method, address+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != want {
		t.Fatalf("%s: %d %s %v", path, response.StatusCode, data, err)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFactoryOperatorClaimEndToEnd(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "factory", "config.yaml")
	if err := writeFactoryPrivate(config, []byte("listen: 127.0.0.1:0\naccess_token: script-secret\ngithub_client_id: client\n")); err != nil {
		t.Fatal(err)
	}
	first, firstGate := startOperatorServe(t, root, false)
	first.stop(t, false)
	second, secondGate := startOperatorServe(t, root, false)
	operatorHTTP(t, second.address, "/v1/auth/device", firstGate, `{}`, 401, nil)
	rows, err := tokens.Load(factoryTokensPath(config))
	if err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		if rows[i].Matches(secondGate) {
			rows[i].Expires = time.Now().Add(-time.Second)
		}
	}
	if err := tokens.Save(factoryTokensPath(config), rows); err != nil {
		t.Fatal(err)
	}
	operatorHTTP(t, second.address, "/v1/auth/device", secondGate, `{}`, 401, nil)
	second.stop(t, false)
	third, gate := startOperatorServe(t, root, false)
	var device api.Device
	operatorHTTP(t, third.address, "/v1/auth/device", gate, `{}`, 200, &device)
	// The service's real clock enforces GitHub's poll interval. One bounded
	// second is the only wall-clock wait in this subprocess acceptance test.
	timer := time.NewTimer(time.Duration(device.Interval) * time.Second)
	<-timer.C
	var signed api.SignedIn
	operatorHTTP(t, third.address, "/v1/auth/device/poll", gate, fmt.Sprintf(`{"device_code":%q}`, device.DeviceCode), 200, &signed)
	if !signed.Me.Operator || signed.Me.Profile.Handle != "owner" {
		t.Fatalf("operator claim: %+v", signed.Me)
	}
	operatorHTTP(t, third.address, "/v1/auth/device", gate, `{}`, 401, nil)
	var grant struct {
		Login     string    `json:"login"`
		GrantedBy string    `json:"granted_by"`
		GrantedAt time.Time `json:"granted_at"`
	}
	operatorRequest(t, http.MethodPut, third.address, "/v1/github/grant", signed.AccessToken, `{"token":"read-secret"}`, 200, &grant)
	if grant.Login != "owner" || grant.GrantedBy != "owner" || grant.GrantedAt.IsZero() {
		t.Fatalf("read grant: %+v", grant)
	}
	third.stop(t, false)
	fourth, _ := startOperatorServe(t, root, true)
	var me api.Me
	operatorRequest(t, http.MethodGet, fourth.address, "/v1/me", signed.AccessToken, "", 200, &me)
	if !me.Operator || me.Profile.Handle != "owner" {
		t.Fatalf("persisted operator: %+v", me)
	}
	operatorRequest(t, http.MethodGet, fourth.address, "/v1/github", signed.AccessToken, "", 200, &grant)
	if grant.GrantedBy != "owner" {
		t.Fatal("grant lost at restart")
	}
	var repos struct {
		Available []struct {
			Full string `json:"full"`
		} `json:"available"`
	}
	operatorRequest(t, http.MethodGet, fourth.address, "/v1/repos", signed.AccessToken, "", 200, &repos)
	if len(repos.Available) != 1 || repos.Available[0].Full != "owner/repo" {
		t.Fatalf("read grant repository list: %+v", repos)
	}
	operatorHTTP(t, fourth.address, "/v1/access/links", signed.AccessToken, `{}`, 201, nil)
	fourth.stop(t, true)
	recovery, recoveryGate := startOperatorServe(t, root, false, true)
	operatorHTTP(t, recovery.address, "/v1/auth/device", gate, `{}`, 401, nil)
	operatorHTTP(t, recovery.address, "/v1/auth/device", recoveryGate, `{}`, 200, nil)
	recovery.stop(t, false)
}
