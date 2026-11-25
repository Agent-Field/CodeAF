package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestP1_6_StubAcceptsARealPushAndClone(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	const password = "sk-test-github-0001"
	stub := newGitStub(git, t.TempDir(), password)
	server := httptest.NewServer(stub)
	defer server.Close()

	workspace := t.TempDir()
	run := func(input string, args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = workspace
		cmd.Stdin = strings.NewReader(input)
		cmd.Env = append(os.Environ(), "HOME="+workspace, "GIT_TERMINAL_PROMPT=0")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	repo := "/org/sample.git"
	wrong := server.URL + repo + "/info/refs?service=git-upload-pack"
	resp, err := http.Get(wrong)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 || !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Basic ") {
		t.Fatalf("wrong credentials: %d %v", resp.StatusCode, resp.Header)
	}

	remote := strings.Replace(server.URL, "http://", "http://x-access-token:"+password+"@", 1) + repo
	run("", "init", "-q", "-b", "main", "source")
	run("commit refs/heads/main\ncommitter Tester <test@example.test> 0 +0000\ndata 4\nseed\n"+
		"M 100644 inline hello\ndata 10\nhello git\n\n", "-C", "source", "fast-import", "--quiet")
	run("", "-C", "source", "push", remote, "HEAD:refs/heads/main")
	run("", "clone", "-q", remote, "copy")
	got, err := os.ReadFile(filepath.Join(workspace, "copy", "hello"))
	if err != nil || string(got) != "hello git\n" {
		t.Fatalf("clone contents = %q, %v", got, err)
	}

	resp, err = http.Get(server.URL + "/_calls")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	var calls []gitCall
	if err != nil || resp.StatusCode != 200 || json.Unmarshal(raw, &calls) != nil || strings.Contains(string(raw), password) {
		t.Fatalf("invalid call report: %d %s %v", resp.StatusCode, raw, err)
	}
	seen := map[string]bool{}
	for _, call := range calls {
		if !call.PasswordOK {
			if call.PasswordTail != "" {
				t.Fatal("the call report exposed part of an invalid password")
			}
			continue
		}
		if call.User != "x-access-token" || call.PasswordTail != "0001" || !strings.HasPrefix(call.Path, repo+"/") || call.At.IsZero() {
			t.Errorf("unexpected authenticated call: %#v", call)
		}
		seen[call.Service] = true
	}
	if len(calls) < 3 || calls[0].PasswordOK || !seen["git-upload-pack"] || !seen["git-receive-pack"] {
		t.Fatalf("missing git calls: %#v", calls)
	}
}
