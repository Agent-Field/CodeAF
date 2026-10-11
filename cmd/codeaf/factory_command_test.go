package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

type factoryReady chan string

func (ready factoryReady) Write(body []byte) (int, error) {
	ready <- strings.TrimSpace(string(body))
	return len(body), nil
}

func TestFactoryCommandServesQueueAndOwnsDiscovery(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	configPath := home.Join("factory", "config.yaml")
	if err := writeFactoryPrivate(configPath, []byte("listen: 127.0.0.1:0\naccess_token: test-secret\nrepos: [owner/repo]\n")); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(v3FactoryRoot())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create(factory.Item{Repo: "owner/repo", Title: "command fixture", Kind: factory.KindIssue, State: factory.StateNew}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(factoryReady, 1)
	done := make(chan error, 1)
	go func() { done <- serveFactory(ctx, configPath, "", ready) }()
	var address string
	select {
	case address = <-ready:
	case err := <-done:
		t.Fatalf("service failed before printing its address: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("service did not print its address")
	}
	discoveryPath := filepath.Join(st.Root(), "serve.json")
	body, err := os.ReadFile(discoveryPath)
	if err != nil {
		t.Fatal(err)
	}
	var discovery factoryDiscovery
	if err := json.Unmarshal(body, &discovery); err != nil {
		t.Fatal(err)
	}
	if discovery.URL != address || discovery.PID != os.Getpid() || discovery.Token != "test-secret" {
		t.Fatalf("discovery does not identify this service: %+v", discovery)
	}
	originalDiscovery := append([]byte(nil), body...)
	info, err := os.Stat(discoveryPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("discovery must be private: %v, %v", info, err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(address + "/v1/queue")
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("command fixture")) {
		t.Fatalf("queue: status=%d body=%s error=%v", response.StatusCode, body, err)
	}
	repos, err := st.Repos()
	if err != nil || len(repos) != 1 || repos[0] != "owner/repo" {
		t.Fatalf("configured repos: %v, %v", repos, err)
	}
	if err := serveFactory(ctx, configPath, "127.0.0.1:0", io.Discard); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second service should refuse: %v", err)
	}
	if after := readFactoryTestFile(t, discoveryPath); !bytes.Equal(after, originalDiscovery) {
		t.Fatal("second service replaced discovery")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("service did not stop")
	}
	if _, err := os.Stat(discoveryPath); !os.IsNotExist(err) {
		t.Fatalf("discovery survives service exit: %v", err)
	}
}

func readFactoryTestFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestFactoryHostConfigTokenPersistsAndRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := writeFactoryPrivate(path, []byte("listen: 127.0.0.1:9876\nrepos: []\n")); err != nil {
		t.Fatal(err)
	}
	first, err := factoryHostConfigAt(path, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := factoryHostConfigAt(path, false)
	if err != nil || first.AccessToken != second.AccessToken || len(first.AccessToken) != 64 {
		t.Fatalf("token did not persist: %v", err)
	}
	rotated, err := factoryHostConfigAt(path, true)
	if err != nil || rotated.AccessToken == first.AccessToken || rotated.Listen != first.Listen || rotated.Repos == nil {
		t.Fatalf("rotation lost configuration: %+v, %v", rotated, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token config must be private: %v, %v", info, err)
	}
}

func TestFactoryHostConfigKeepsOmittedReposOmitted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	first, err := factoryHostConfigAt(path, false)
	if err != nil || first.Listen != factoryDefaultListen {
		t.Fatalf("default configuration: %+v, %v", first, err)
	}
	second, err := factoryHostConfigAt(path, true)
	if err != nil || second.Repos != nil {
		t.Fatalf("rotation turned omitted repos into a clear instruction: %+v, %v", second, err)
	}
	reloaded, err := readFactoryHostConfig(path)
	if err != nil || reloaded.Repos != nil {
		t.Fatalf("saved configuration changed the repository selection: %+v, %v", reloaded, err)
	}
}

func TestFactoryHostConfigRejectsInvalidYAML(t *testing.T) {
	for _, body := range []string{"unknown: yes", "listen: [wrong]", "repos: [wrong]", "access_token: 'two words'", "listen: localhost:1\n---\nrepos: []"} {
		t.Run(body, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := factoryHostConfigAt(path, false); err == nil {
				t.Fatal("invalid host config was accepted")
			}
			if after := readFactoryTestFile(t, path); string(after) != body {
				t.Fatal("invalid host config was rewritten")
			}
		})
	}
}

func TestFactoryStepsPrintsEveryBuiltInPrompt(t *testing.T) {
	var out bytes.Buffer
	if err := printFactorySteps(&out); err != nil {
		t.Fatal(err)
	}
	recipe := factory.DefaultRecipe()
	for _, kind := range []factory.Kind{factory.KindIssue, factory.KindPR, factory.KindCI} {
		for _, step := range recipe.For(kind) {
			if step.Ask != "" && !strings.Contains(out.String(), step.Name+": "+step.Ask) {
				t.Errorf("%s prompt missing from built-ins", step.Name)
			}
		}
	}
}
