//go:build linux

package sessionhost

import (
	"strings"
	"testing"
)

// The contract (P1-0c), in the words of the operator: a chat's commands may
// connect only to the ports I allow (a short default, or my own list) and to
// the organization's own door; an entry I mistype stops the server from
// starting, with the setting named; and where no command is fenced there is
// no list to apply.

func configWith(t *testing.T, ports string) (Config, error) {
	t.Helper()
	return ConfigFromEnv(func(name string) string {
		switch name {
		case "CODEAF_SESSIONHOST_SECRET":
			return strings.Repeat("S", 32)
		case "CODEAF_SESSIONHOST_CONNECT_PORTS":
			return ports
		}
		return ""
	})
}

func TestP1_0c_DefaultListAndTheOperatorsOwn(t *testing.T) {
	cfg, err := configWith(t, "")
	if err != nil || !cfg.LimitPorts || portList(cfg.ConnectPorts) != "22,80,443,9418" {
		t.Fatalf("default: %+v, %v", cfg, err)
	}
	cfg, err = configWith(t, "443, 8443")
	if err != nil || portList(cfg.ConnectPorts) != "443,8443" {
		t.Fatalf("operator list: %+v, %v", cfg, err)
	}
	cfg, err = configWith(t, "none")
	if err != nil || !cfg.LimitPorts || len(cfg.ConnectPorts) != 0 {
		t.Fatalf("none keeps the organization's own ports alone: %+v, %v", cfg, err)
	}
	for _, bad := range []string{"https", "443,99999", "0", "443;80"} {
		_, err := configWith(t, bad)
		if err == nil || !strings.Contains(err.Error(), "CODEAF_SESSIONHOST_CONNECT_PORTS") || strings.Contains(err.Error(), strings.Repeat("S", 32)) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestP1_0c_EngineGetsTheListAndTheOrganizationsPorts(t *testing.T) {
	cfg := testConfig(t)
	folders, err := layoutFor(cfg.Data, "alice", chatA)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LimitPorts, cfg.ConnectPorts = false, nil
	creds := credentials{ModelURL: "http://org.test:8080/v1/model", ModelToken: "t",
		GitURL: "http://org.test:8080/v1/hosted/git", GitHosts: []string{"https://github.com"}}
	has := func(env []string) (string, bool) {
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, "CODEAF_FENCE_CONNECT_PORTS="); ok {
				return v, true
			}
		}
		return "", false
	}
	if _, ok := has(engineEnv(cfg, folders, creds)); ok {
		t.Fatal("a supervisor with no list wrote one into the engine's environment")
	}
	cfg.LimitPorts, cfg.ConnectPorts = true, []int{443, 80, 22, 9418}
	got, ok := has(engineEnv(cfg, folders, creds))
	if !ok || got != "22,80,443,8080,9418" {
		t.Fatalf("ports = %q, %v", got, ok)
	}
	creds.ModelURL, creds.GitURL = "https://org.test/v1/model", ""
	if got, _ := has(engineEnv(cfg, folders, creds)); got != "22,80,443,9418" {
		t.Fatalf("an organization on 443 adds nothing new: %q", got)
	}
	cfg.ConnectPorts = nil
	creds.ModelURL = "http://org.test:8080/v1/model"
	if got, _ := has(engineEnv(cfg, folders, creds)); got != "8080" {
		t.Fatalf("none leaves the organization's own door: %q", got)
	}
	cfg.Require = modeUID
	if _, ok := has(engineEnv(cfg, folders, creds)); ok {
		t.Fatal("process-only isolation fences no command, so there is no list to write")
	}
}
