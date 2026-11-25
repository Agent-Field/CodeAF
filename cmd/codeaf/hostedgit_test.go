package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testGitURL = "https://org.test:8443/v1/hosted/git"

func TestP1_6_GitCredentialCommand(t *testing.T) {
	binary := buildCodeafStamped(t, "")
	home := t.TempDir()
	broker := filepath.Join(home, ".config", "codeaf-hosted", "broker")
	raw := []byte("url=" + testGitURL + "\nbearer=cmt_fake-chat-access\n")
	if err := os.MkdirAll(filepath.Dir(broker), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broker, raw, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(verb, input, want string) {
		t.Helper()
		cmd := exec.Command(binary, "git-credential", verb)
		cmd.Env = append(os.Environ(), "HOME="+home, "CODEAF_HOME="+filepath.Join(home, "unused-codeaf-home"),
			"CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1")
		cmd.Stdin = strings.NewReader(input + "\n")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		got, err := cmd.Output()
		if err != nil || string(got) != want || stderr.Len() != 0 {
			t.Fatalf("git-credential %s with %q: stdout %q, stderr %q, err %v", verb, input, got, &stderr, err)
		}
	}
	const origin = "protocol=https\nhost=org.test:8443\npath=v1/hosted/git\n"
	run("get", origin, "username=codeaf\npassword=cmt_fake-chat-access\n\n")
	for _, input := range []string{
		"protocol=https\nhost=org.test.attacker.test:8443\n",
		"protocol=https\nhost=extra-org.test:8443\n",
		"protocol=https\nhost=org.test:8444\n",
		"protocol=https\nhost=org.test\n",
		"protocol=http\nhost=org.test:8443\n",
		"protocol=https\nhost=user@org.test:8443\n",
		"protocol=https\nhost=org.test:8443@other.test\n",
		"protocol=https\nhost=other.test\nhost=org.test:8443\n",
	} {
		run("get", input, "")
	}
	info, err := os.Stat(broker)
	if err != nil {
		t.Fatal(err)
	}
	run("store", origin+"password=should-not-be-stored\n", "")
	run("erase", origin, "")
	after, err := os.Stat(broker)
	if err != nil || !os.SameFile(info, after) || !info.ModTime().Equal(after.ModTime()) {
		t.Fatalf("store or erase changed broker: %v", err)
	}
	if got, err := os.ReadFile(broker); err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("store or erase changed broker contents: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "unused-codeaf-home")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the helper touched the general codeaf home: %v", err)
	}
	if err := os.Remove(broker); err != nil {
		t.Fatal(err)
	}
	run("get", origin, "")
}

func TestP1_6_PrepareGitWritesOnlyTheChatConfigAndBroker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOSTED_GIT_URL", "")
	t.Setenv("CODEAF_HOSTED_GIT_HOSTS", "https://github.com,http://gitstub:8891")
	t.Setenv("OPENAI_API_KEY", "cmt_fake-chat-access")
	gitConfig := filepath.Join(home, ".config", "git", "config")
	broker := filepath.Join(home, ".config", "codeaf-hosted", "broker")
	personal := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(personal, []byte("[user]\n\tname = Chat\n"), 0600); err != nil {
		t.Fatal(err)
	}
	hostedPrepareGit()
	if _, err := os.Stat(filepath.Dir(filepath.Dir(gitConfig))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("setup without a git URL touched .config: %v", err)
	}

	t.Setenv("CODEAF_HOSTED_GIT_URL", testGitURL)
	hostedPrepareGit()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config := readGitTestFile(t, gitConfig)
	for _, line := range []string{
		`[url "` + testGitURL + `/github.com/"]`, "insteadOf = https://github.com/",
		`[url "` + testGitURL + `/gitstub:8891/"]`, "insteadOf = http://gitstub:8891/",
		`[credential "` + testGitURL + `/"]`, "useHttpPath = false", filepath.ToSlash(exe), " git-credential",
	} {
		if !strings.Contains(config, line) {
			t.Errorf("generated config is missing %q:\n%s", line, config)
		}
	}
	if strings.Contains(config, "[credential]") || strings.Contains(config, "cmt_") {
		t.Fatalf("git config is unscoped or carries a credential: %s", config)
	}
	if got := readGitTestFile(t, broker); got != "url="+testGitURL+"\nbearer=cmt_fake-chat-access\n" {
		t.Fatalf("broker contents: %q", got)
	}
	for file, want := range map[string]os.FileMode{broker: 0600, filepath.Dir(broker): 0700, filepath.Dir(gitConfig): 0700} {
		info, err := os.Stat(file)
		if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != want) {
			t.Fatalf("%s permissions, want %o: %v %v", file, want, info, err)
		}
	}
	if got := readGitTestFile(t, personal); got != "[user]\n\tname = Chat\n" {
		t.Fatalf(".gitconfig was changed: %q", got)
	}

	git, err := exec.LookPath("git")
	if err == nil {
		cmd := exec.Command(git, "ls-remote", "--get-url", "http://gitstub:8891/org/repo.git")
		cmd.Dir = home
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "GIT_CONFIG_NOSYSTEM=1",
			"CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1", "CODEAF_HOME=" + os.Getenv("CODEAF_HOME")}
		if got, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(got)) != testGitURL+"/gitstub:8891/org/repo.git" {
			t.Fatalf("git did not apply the generated rewrite: %q %v", got, err)
		}
	}

	t.Setenv("OPENAI_API_KEY", "cmt_rotated")
	t.Setenv("CODEAF_HOSTED_GIT_HOSTS", "https://github.com")
	hostedPrepareGit()
	if got := readGitTestFile(t, broker); got != "url="+testGitURL+"\nbearer=cmt_rotated\n" {
		t.Fatalf("broker access did not rotate: %q", got)
	}
	if got := readGitTestFile(t, gitConfig); strings.Contains(got, "gitstub:8891") {
		t.Fatalf("stale rewrite remains: %q", got)
	}
	t.Setenv("OPENAI_API_KEY", "")
	hostedPrepareGit()
	if _, err := os.Stat(broker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale broker was left behind: %v", err)
	}
	t.Setenv("HOME", "")
	hostedPrepareGit()
}

func readGitTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
