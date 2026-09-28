package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/opener"
)

type fakeCodexConnect struct {
	address string
	tokens  codexauth.Tokens
	err     error
}

func (f *fakeCodexConnect) URL() string                                    { return f.address }
func (f *fakeCodexConnect) Wait(context.Context) (codexauth.Tokens, error) { return f.tokens, f.err }
func (f *fakeCodexConnect) Cancel()                                        {}

type fakeOpenRouterConnect struct {
	address string
	key     string
	err     error
}

func (f *fakeOpenRouterConnect) URL() string                          { return f.address }
func (f *fakeOpenRouterConnect) Wait(context.Context) (string, error) { return f.key, f.err }
func (f *fakeOpenRouterConnect) Cancel()                              {}

func captureConnect(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	output, errorsOut := &bytes.Buffer{}, &bytes.Buffer{}
	oldOut, oldErr := usageOut, usageErr
	usageOut, usageErr = output, errorsOut
	return output, func() { usageOut, usageErr = oldOut, oldErr }
}

func TestC1ConnectCodexPrintsOneAddressOpensAndEndsConnected(t *testing.T) {
	// C1: the terminal Codex door prints the link, opens it, waits and reports success.
	dir := t.TempDir()
	t.Setenv(config.ProfileDirEnv, dir)
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"models":[{"slug":"gpt-5.5","visibility":"list"}]}`))
	}))
	defer backend.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
	flow := &fakeCodexConnect{address: "https://auth.example/?redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback", tokens: codexauth.Tokens{AccessToken: "cli-access-token", RefreshToken: "cli-refresh-token", IDToken: "cli-identity-token", Email: "person@example.com", Plan: "pro", ExpiresAt: time.Now().Add(time.Hour)}}
	oldFlow, oldOpen := connectCodexFlow, connectOpen
	connectCodexFlow = func(context.Context) (codexConnectFlow, error) { return flow, nil }
	opened := ""
	connectOpen = func(address string) error { opened = address; return nil }
	t.Cleanup(func() { connectCodexFlow, connectOpen = oldFlow, oldOpen })
	output, restore := captureConnect(t)
	defer restore()
	if err := runConnect([]string{"codex"}); err != nil {
		t.Fatal(err)
	}
	if opened != flow.address || strings.Count(output.String(), flow.address) != 1 || !strings.Contains(output.String(), "codex connected · person@example.com · pro plan") {
		t.Fatalf("opened %q, output %q", opened, output.String())
	}
}

func TestC1NoBrowserPrintsForwardingInstructionWithoutOpening(t *testing.T) {
	// C1 and D9: --no-browser leaves the handoff to the person and names the fixed port forward.
	dir := t.TempDir()
	t.Setenv(config.ProfileDirEnv, dir)
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { writer.WriteHeader(http.StatusBadGateway) }))
	defer backend.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
	oldFlow, oldOpen := connectCodexFlow, connectOpen
	connectCodexFlow = func(context.Context) (codexConnectFlow, error) {
		return &fakeCodexConnect{address: "https://auth.example/?redirect_uri=http%3A%2F%2Flocalhost%3A1457%2Fauth%2Fcallback", tokens: codexauth.Tokens{AccessToken: "access-eight", RefreshToken: "refresh-eight", IDToken: "identity-eight", ExpiresAt: time.Now().Add(time.Hour)}}, nil
	}
	opened := false
	connectOpen = func(string) error { opened = true; return nil }
	t.Cleanup(func() { connectCodexFlow, connectOpen = oldFlow, oldOpen })
	output, restore := captureConnect(t)
	defer restore()
	if err := runConnect([]string{"codex", "--no-browser"}); err != nil {
		t.Fatal(err)
	}
	if opened || !strings.Contains(output.String(), "ssh -L 1457:localhost:1457") {
		t.Fatalf("opened=%t output=%q", opened, output.String())
	}
}

func TestC5BusyPortsAreAPlainExitOneOutcome(t *testing.T) {
	// C5: a browser listener failure is reported on stdout and exits one without writing tokens.
	dir := t.TempDir()
	t.Setenv(config.ProfileDirEnv, dir)
	old := connectCodexFlow
	connectCodexFlow = func(context.Context) (codexConnectFlow, error) {
		return nil, errors.New("connect Codex: both browser return ports are busy · finish or cancel the other sign-in and try again")
	}
	t.Cleanup(func() { connectCodexFlow = old })
	output, restore := captureConnect(t)
	defer restore()
	err := runConnect([]string{"codex"})
	var status exitStatus
	if !errors.As(err, &status) || status != 1 || !strings.Contains(output.String(), "codex did not connect · both browser return ports are busy") {
		t.Fatalf("error=%v output=%q", err, output.String())
	}
	if _, err := os.Stat(codexauth.Path(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token file exists after refusal: %v", err)
	}
}

func TestC7ConnectOpenRouterReusesBrowserRoadAndProfileKey(t *testing.T) {
	// C7: the terminal OpenRouter door keeps the existing browser exchange and profile key writer.
	dir := t.TempDir()
	t.Setenv(config.ProfileDirEnv, dir)
	oldFlow, oldOpen := connectOpenRouterFlow, connectOpen
	connectOpenRouterFlow = func(context.Context) (openRouterConnectFlow, error) {
		return &fakeOpenRouterConnect{address: "https://open.example/sign-in", key: "sk-openrouter-test-value"}, nil
	}
	connectOpen = func(string) error { return nil }
	t.Cleanup(func() { connectOpenRouterFlow, connectOpen = oldFlow, oldOpen })
	output, restore := captureConnect(t)
	defer restore()
	if err := runConnect([]string{modelsource.DefaultID}); err != nil {
		t.Fatal(err)
	}
	if config.PersistedAPIKey(dir) != "sk-openrouter-test-value" || !strings.Contains(output.String(), modelsource.DefaultID+" connected") {
		t.Fatalf("key=%q output=%q", config.PersistedAPIKey(dir), output.String())
	}
}

func TestConnectCommandsReportABrowserStartFailureAndKeepWaiting(t *testing.T) {
	// C1: the link remains usable when its automatic handoff fails. Both
	// browser services print the first-run recovery line, then accept the flow's
	// successful return rather than abandoning a sign-in already in progress.
	t.Run("codex", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(config.ProfileDirEnv, dir)
		backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte(`{"models":[{"slug":"gpt-5.5","visibility":"list"}]}`))
		}))
		defer backend.Close()
		t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
		flow := &fakeCodexConnect{
			address: "https://auth.example/sign-in",
			tokens: codexauth.Tokens{
				AccessToken: "browser-failure-access", RefreshToken: "browser-failure-refresh",
				IDToken: "browser-failure-identity", ExpiresAt: time.Now().Add(time.Hour),
			},
		}
		oldFlow, oldOpen := connectCodexFlow, connectOpen
		connectCodexFlow = func(context.Context) (codexConnectFlow, error) { return flow, nil }
		connectOpen = func(string) error { return errors.New("exec: xdg-open not found") }
		t.Cleanup(func() { connectCodexFlow, connectOpen = oldFlow, oldOpen })
		output, restore := captureConnect(t)
		defer restore()
		if err := runConnect([]string{"codex"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), opener.BrowserFailureWord) || !strings.Contains(output.String(), "codex connected") {
			t.Fatalf("connect output = %q", output.String())
		}
	})

	t.Run("openrouter", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(config.ProfileDirEnv, dir)
		oldFlow, oldOpen := connectOpenRouterFlow, connectOpen
		connectOpenRouterFlow = func(context.Context) (openRouterConnectFlow, error) {
			return &fakeOpenRouterConnect{address: "https://openrouter.example/sign-in", key: "sk-or-v1-browser-failure-value"}, nil
		}
		connectOpen = func(string) error { return errors.New("exec: xdg-open not found") }
		t.Cleanup(func() { connectOpenRouterFlow, connectOpen = oldFlow, oldOpen })
		output, restore := captureConnect(t)
		defer restore()
		if err := runConnect([]string{modelsource.DefaultID}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), opener.BrowserFailureWord) || !strings.Contains(output.String(), "openrouter connected") {
			t.Fatalf("connect output = %q", output.String())
		}
	})
}

func TestC8ConnectWithoutAServiceListsMethodsAndNeverDrawsNothing(t *testing.T) {
	// C8: the no-argument door lists each known service and says explicitly when none is connected.
	dir := t.TempDir()
	t.Setenv(config.ProfileDirEnv, dir)
	output, restore := captureConnect(t)
	defer restore()
	if err := runConnect(nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{modelsource.DefaultID + " · not connected · browser or key", "codex · not connected · browser", "deepseek · not connected · key", "no provider is connected"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("listing missing %q: %q", want, output.String())
		}
	}
}

func TestC9KeyServiceReadsStdinAndUsesTheSharedConnectProbeWords(t *testing.T) {
	// C9 and C20: a custom service reads a non-terminal key and returns the panel's unchanged success sentence.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer sk-custom-terminal-value" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		_, _ = writer.Write([]byte(`{"data":[{"id":"model-one"}]}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Setenv(config.ProfileDirEnv, dir)
	listed := true
	if err := config.WriteSources(dir, []config.PersistedSource{{ID: modelsource.CustomID, Written: "lab", Address: server.URL, Key: "old-key-value", Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = writer.WriteString("sk-custom-terminal-value\n")
	_ = writer.Close()
	oldInput, oldTTY := connectInput, connectInputIsTTY
	connectInput, connectInputIsTTY = reader, func(*os.File) bool { return false }
	t.Cleanup(func() { connectInput, connectInputIsTTY = oldInput, oldTTY; _ = reader.Close() })
	output, restore := captureConnect(t)
	defer restore()
	if err := runConnect([]string{"lab"}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "lab is connected · 1 model" {
		t.Fatalf("outcome = %q", output.String())
	}
}

func TestC10DisconnectForgetsCodexAndRejectsAnUnknownService(t *testing.T) {
	// C10: disconnect removes both Codex stores, while an unknown service is a plain exit-one sentence.
	dir := t.TempDir()
	t.Setenv(config.ProfileDirEnv, dir)
	if err := codexauth.Save(dir, codexauth.Tokens{AccessToken: "disconnect-access", RefreshToken: "disconnect-refresh", IDToken: "disconnect-identity"}); err != nil {
		t.Fatal(err)
	}
	listed := true
	if err := config.WriteSources(dir, []config.PersistedSource{{ID: "codex", Written: "codex", Key: codexauth.Sentinel, Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	output, restore := captureConnect(t)
	defer restore()
	if err := runDisconnect([]string{"codex"}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "codex disconnected" || codexauth.Connected(dir) || len(config.PersistedSources(dir)) != 0 {
		t.Fatalf("disconnect output=%q connected=%t rows=%v", output.String(), codexauth.Connected(dir), config.PersistedSources(dir))
	}
	output.Reset()
	err := runDisconnect([]string{"nowhere"})
	var status exitStatus
	if !errors.As(err, &status) || status != 1 || strings.TrimSpace(output.String()) != "nowhere is not connected" {
		t.Fatalf("unknown disconnect = %v, %q", err, output.String())
	}
}

func TestC11ConnectHelpIsLiftedFromTheEightyColumnTable(t *testing.T) {
	// C11: both terminal doors are present in the shared usage source.
	for _, want := range []string{"codeaf connect", "codeaf connect <provider> [--no-browser] [--region intl|cn]", "codeaf disconnect <provider>"} {
		if !strings.Contains(usageText, want) {
			t.Errorf("usage is missing %q", want)
		}
	}
	if page := usageForCommand("connect"); !strings.Contains(page, "list the providers") || !strings.Contains(page, "--no-browser") {
		t.Fatalf("connect help = %q", page)
	}
}

func TestFirstRunStillBuildsItsOpenRouterBrowserRoad(t *testing.T) {
	// The rendered C19 contract lives with the surface in internal/tui3. This
	// pins the production command seam that hands that surface its browser flow.
	settings := config.Config{BaseURL: config.DefaultBaseURL}
	if v3OpenRouterConnection(settings, true) == nil {
		t.Fatal("first-run OpenRouter browser connection disappeared")
	}
}
