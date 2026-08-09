package codeaf

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// deadControlPlaneURL returns a URL nothing is listening on.
func deadControlPlaneURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return "http://" + address
}

func gateArgs(workspace string) []string {
	return []string{
		"run", "--dir", workspace, "--high", "provider/high",
		"Implement the thing.",
	}
}

func TestRunRefusesWithoutControlPlane(t *testing.T) {
	dead := deadControlPlaneURL(t)
	t.Setenv("CODEAF_CP_URL", dead)
	t.Setenv("AGENTFIELD_URL", "")

	var stdout, stderr bytes.Buffer
	err := runCLI(
		context.Background(), gateArgs(t.TempDir()), nil, &stdout, &stderr,
	)
	if err == nil {
		t.Fatal("run without a control plane succeeded; want a hard refusal")
	}
	if !strings.Contains(err.Error(), "AgentField control plane") {
		t.Fatalf("refusal does not name AgentField: %v", err)
	}
	if !strings.Contains(err.Error(), dead) {
		t.Fatalf("refusal does not name the probed URL %s: %v", dead, err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("pipeline emitted events despite refusal:\n%s", stdout.String())
	}
}

func TestRunGateFallsBackToAgentFieldURL(t *testing.T) {
	dead := deadControlPlaneURL(t)
	t.Setenv("CODEAF_CP_URL", "")
	t.Setenv("AGENTFIELD_URL", dead)

	var stdout, stderr bytes.Buffer
	err := runCLI(
		context.Background(), gateArgs(t.TempDir()), nil, &stdout, &stderr,
	)
	if err == nil {
		t.Fatal("run without a control plane succeeded; want a hard refusal")
	}
	if !strings.Contains(err.Error(), dead) {
		t.Fatalf("gate did not probe AGENTFIELD_URL %s: %v", dead, err)
	}
}

func TestResumeRefusesWithoutControlPlane(t *testing.T) {
	dead := deadControlPlaneURL(t)
	t.Setenv("CODEAF_CP_URL", dead)
	t.Setenv("AGENTFIELD_URL", "")
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	writeTerminalCheckpoint(workspace, resumeCheckpointFile{
		Goal: "Finish the thing.", FinalStatus: "fail", Cycle: 1,
	})

	var stdout, stderr bytes.Buffer
	err := runCLI(
		context.Background(),
		[]string{"resume", "--dir", workspace, "--high", "provider/high"},
		nil, &stdout, &stderr,
	)
	if err == nil {
		t.Fatal("resume without a control plane succeeded; want a hard refusal")
	}
	if !strings.Contains(err.Error(), "AgentField control plane") {
		t.Fatalf("refusal does not name AgentField: %v", err)
	}
}

func TestRunProceedsWithHealthyControlPlane(t *testing.T) {
	var posts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				posts.Add(1)
			}
			w.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()
	t.Setenv("CODEAF_CP_URL", server.URL)
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")

	backend := &validityBackend{intakeReply: validityJSON(
		"invalid", "high", "contradicts the documented contract",
		"do not implement",
	)}
	var stdout, stderr bytes.Buffer
	err := runCLI(
		context.Background(), gateArgs(workspace), backend, &stdout, &stderr,
	)
	if err != nil {
		t.Fatalf("run with a healthy control plane failed: %v", err)
	}
	if backend.count("validity-judge") != 1 {
		t.Fatalf(
			"validity judge calls = %d, want 1", backend.count("validity-judge"),
		)
	}
	if posts.Load() == 0 {
		t.Fatal("no events were mirrored onto the control plane")
	}
}

func TestRunGateRetriesTransientProbeFailures(t *testing.T) {
	var health atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/health") {
				if health.Add(1) <= 2 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
			}
			w.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()
	t.Setenv("CODEAF_CP_URL", server.URL)
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")

	backend := &validityBackend{intakeReply: validityJSON(
		"invalid", "high", "contradicts the documented contract",
		"do not implement",
	)}
	var stdout, stderr bytes.Buffer
	err := runCLI(
		context.Background(), gateArgs(workspace), backend, &stdout, &stderr,
	)
	if err != nil {
		t.Fatalf("run failed despite the probe recovering on retry: %v", err)
	}
	if got := health.Load(); got != 3 {
		t.Fatalf("health probes = %d, want 3 (two failures then success)", got)
	}
	if backend.count("validity-judge") != 1 {
		t.Fatalf(
			"validity judge calls = %d, want 1", backend.count("validity-judge"),
		)
	}
}

func TestInjectedBackendKeepsGracefulDegrade(t *testing.T) {
	dead := deadControlPlaneURL(t)
	t.Setenv("CODEAF_CP_URL", dead)
	workspace := validityTestRepo(t)
	setValidityTestEnv(t, workspace)
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")

	backend := &validityBackend{intakeReply: validityJSON(
		"invalid", "high", "contradicts the documented contract",
		"do not implement",
	)}
	var stdout, stderr bytes.Buffer
	err := runCLI(
		context.Background(), gateArgs(workspace), backend, &stdout, &stderr,
	)
	if err != nil {
		t.Fatalf("injected-backend run degraded hard instead of soft: %v", err)
	}
	if !strings.Contains(stderr.String(), "control plane disabled") {
		t.Fatalf(
			"missing graceful-degrade notice on stderr:\n%s", stderr.String(),
		)
	}
}
