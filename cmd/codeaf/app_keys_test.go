package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/remote"
)

func TestAppServeStdinKeysReachTheAuthenticatedRouteWithoutDiskSecrets(t *testing.T) {
	launch := launchAppForTest(t)
	const key = "opaque-stdin-provider-key-123456789"
	const session = "opaque-stdin-factory-session-123456789"
	_, err := io.WriteString(launch.input, `{"keys":{"openrouter":"`+key+`"},"factory_sessions":{"https://factory.example":"`+session+`"}}`+"\n")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response := launch.call(t, "GET", "/app/v1/keys", "", launch.hello.Token)
		raw, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || bytes.Contains(raw, []byte(key)) || bytes.Contains(raw, []byte(session)) {
			t.Fatalf("key read: %d %v", response.StatusCode, err)
		}
		var rows []appapi.KeyStatus
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row.Provider == "openrouter" {
				found = row.Present && row.Last4 == "6789"
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stdin key did not reach the key route")
		}
	}
	response := launch.call(t, "DELETE", "/app/v1/keys/openrouter", "", launch.hello.Token)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("authenticated delete failed")
	}
	launch.input.Close()
	launch.wait(t)
	err = filepath.WalkDir(launch.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if bytes.Contains(raw, []byte(key)) || bytes.Contains(raw, []byte(session)) {
			t.Fatalf("stdin credential written to %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAppServeBadHandoffStopsWithoutEchoingCredentials(t *testing.T) {
	launch := launchAppForTest(t)
	const secret = "opaque-invalid-stdin-secret-123456789"
	if _, err := io.WriteString(launch.input, `{"keys":{"openrouter":"`+secret+`"},"unknown":true}`+"\n"); err != nil {
		t.Fatal(err)
	}
	launch.wait(t)
	if strings.Contains(launch.errors.String(), secret) {
		t.Fatal("invalid handoff leaked to stderr")
	}
}

func TestAppModeConnectRefusesBeforeReadingAKeyOrOpeningABrowser(t *testing.T) {
	defer config.UseAppCredentials(config.NewAppCredentials())()
	old := connectInput
	connectInput = nil
	t.Cleanup(func() { connectInput = old })
	for _, args := range [][]string{{"openrouter"}, {"deepseek"}, {"codex"}, nil} {
		if err := runConnect(args); !errors.Is(err, config.ErrAppKeys) {
			t.Fatalf("connect did not refuse: %v", err)
		}
	}
	if err := runDisconnect([]string{"openrouter"}); !errors.Is(err, config.ErrAppKeys) {
		t.Fatalf("disconnect did not refuse: %v", err)
	}
}

func TestAppEngineRefusesMissingOrProtocolDescriptorsForKeys(t *testing.T) {
	for _, fd := range []string{"0", "1", "2"} {
		if err := runRemoteEngine([]string{"--keys-fd", fd}); err == nil || err.Error() != "app keys need an inherited pipe fd" {
			t.Fatalf("invalid descriptor accepted: %s %v", fd, err)
		}
	}
	t.Setenv(config.AppModeEnv, "1")
	if err := runRemoteEngine(nil); err == nil || err.Error() != "app engine needs --keys-fd" {
		t.Fatalf("missing pipe accepted: %v", err)
	}
}

func TestAppEngineKeysHelper(t *testing.T) {
	if os.Getenv("CODEAF_TEST_APP_ENGINE_CHILD") != "1" {
		return
	}
	_ = os.Setenv("CODEAF_HOME", os.Getenv("CODEAF_TEST_APP_ENGINE_HOME"))
	_ = os.Setenv("CODEAF_BASE_URL", os.Getenv("CODEAF_TEST_APP_ENGINE_URL"))
	for index, argument := range os.Args {
		if argument == "--" {
			os.Args = append([]string{"codeaf", "engine"}, os.Args[index+1:]...)
			os.Exit(execute())
		}
	}
	os.Exit(2)
}

type appEngineTestPipe struct {
	io.Reader
	io.WriteCloser
}

func (p appEngineTestPipe) Close() error { return p.WriteCloser.Close() }

func TestAppChildEngineUsesTheInheritedKeyOnAnActualModelRequest(t *testing.T) {
	server := newV3ClientDoorServer(t)
	root, workspace := t.TempDir(), t.TempDir()
	const key = "opaque-app-engine-key-123456789"
	source := config.NewAppCredentials()
	source.Set("openrouter", key)
	defer config.UseAppCredentials(source)()
	command := exec.Command(os.Args[0], "-test.run=^TestAppEngineKeysHelper$", "--", "--workspace", workspace)
	command.Env = append(os.Environ(), "CODEAF_TEST_APP_ENGINE_CHILD=1", "CODEAF_TEST_APP_ENGINE_HOME="+root, "CODEAF_TEST_APP_ENGINE_URL="+server.URL)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	closeKeys, err := config.PrepareAppCredentialPipe(command)
	if err != nil {
		t.Fatal(err)
	}
	err = command.Start()
	closeKeys()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close(); _ = command.Process.Kill() })
	client, err := remote.Dial(appEngineTestPipe{output, input}, "app-key-test", remote.Hello{Workspace: workspace, Model: "test/model"})
	if err != nil {
		t.Fatalf("app engine did not accept the key pipe: %v", err)
	}
	events, err := client.Agent().Submit(t.Context(), "say done")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	if got := server.call(t, 0).authorization; got != "Bearer "+key {
		t.Fatal("child engine did not send the inherited key")
	}
	_ = client.Close()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("app engine exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("app engine did not exit after protocol stdin closed")
	}
	if strings.Contains(stderr.String(), key) {
		t.Fatal("app child logged the key")
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if bytes.Contains(raw, []byte(key)) {
			t.Fatalf("app child persisted a key in %s", path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
