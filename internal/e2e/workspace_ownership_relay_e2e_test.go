//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// A scripted endpoint drives an actual task, including its worker and durable
// checkpoint. Pairing and takeover use the real terminal doors. No checkpoint,
// transcript, primary task, or workspace identity is fabricated by this fixture.
// This is protocol acceptance with fictional data, not live-model acceptance.
func TestWorkspaceOwnershipTaskIdentitySurvivesPublicRelayTake(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	ownershipRelayRequireCurrentBuild(t)
	const model = "stub/workspace-uri-relay"
	var modelCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": model, "context_length": 131072, "pricing": map[string]string{"prompt": "0.000001", "completion": "0.000001"}, "supported_parameters": []string{"tools", "tool_choice"}}}})
		case "/key", "/auth/key", "/credits":
			fmt.Fprint(w, `{"data":{"limit_remaining":100,"total_credits":100,"total_usage":0}}`)
		case "/chat/completions":
			var request struct {
				Stream   bool   `json:"stream"`
				Model    string `json:"model"`
				Messages []struct {
					Content json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, "fixture decode", 400)
				return
			}
			if request.Model != model {
				http.Error(w, "unexpected fixture model", 400)
				return
			}
			modelCalls.Add(1)
			for _, message := range request.Messages {
				if assignment := freeConsentAssignment.FindStringSubmatch(string(message.Content)); len(assignment) == 3 {
					command := `printf 'MOCK_URI_RELAY_HERON\n' > uri-relay-proof.txt && printf '%s\n' "$CODEAF_WORKSPACE_URI" > uri-relay-workspace.txt && printf '%s\n' "$XDG_CACHE_HOME" > uri-relay-cache-path.txt && printf source-private-cache > "$XDG_CACHE_HOME/source-private-cache.bin" && plandb done ` + assignment[1] + ` --agent ` + assignment[2] + ` --result 'uri-relay-proof mock artifact verified'`
					arguments, _ := json.Marshal(map[string]string{"command": command})
					freeConsentResponse(w, model, "", map[string]any{"index": 0, "id": "uri-relay-" + assignment[2], "type": "function", "function": map[string]string{"name": "bash", "arguments": string(arguments)}}, request.Stream)
					return
				}
			}
			freeConsentResponse(w, model, "The fictional URI relay task is ready.", nil, request.Stream)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	relayStore := filepath.Join(t.TempDir(), "relay-store")
	relay := startRelayAt(t, relayStore)
	homeA, homeB := lifecycleHome(t, model), lifecycleHome(t, model)
	sourceProject := workspaceAt(t, filepath.Join(shortRoot(t), "uri-source"), false)
	receiverProject := workspaceAt(t, filepath.Join(shortRoot(t), "uri-receiver"), false)
	baseEnv := []string{config.APIKeyEnv + "=fixture-uri-relay-key", "CODEAF_BASE_URL=" + server.URL, "CODEAF_PROFILE_DIR=", "CODEAF_SYNC_URL=" + relay, "CODEAF_SYNC_INTERVAL_MS=1000", "CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP=1", "CODEAF_PROTOTYPE_TASK_STORAGE=1", "CODEAF_PROTOTYPE_ARCHIVE_BINARY=", furrow.BinaryEnvVar + "="}
	envFor := func(home string) []string {
		return append(append([]string(nil), baseEnv...), "FURROW_DATA_DIR="+filepath.Join(home, "native-store"), "FURROW_REMOTE_DATA_DIR="+filepath.Join(home, "native-remote"))
	}
	// The test's conservative storage flag preserves completed shell copies. No
	// retirement or archive proof is claimed; unrestricted writers stay resident.
	t.Cleanup(func() {
		for _, home := range []string{homeA, homeB} {
			binaries, _ := filepath.Glob(filepath.Join(home, "bin", "furrow-*"))
			for _, native := range binaries {
				lifecycleDaemonCleanup(t, home, native)
			}
		}
	})
	t.Cleanup(func() {
		for _, home := range []string{homeA, homeB} {
			for _, pid := range engineDaemonsUnder(home) {
				_ = syscall.Kill(pid, syscall.SIGTERM)
				for until := time.Now().Add(10 * time.Second); time.Now().Before(until) && !terminalProcessExited(pid); {
					time.Sleep(20 * time.Millisecond)
				}
				if !terminalProcessExited(pid) {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			}
		}
	})
	stopRecoveryEngineOnCleanup(t, envFor(homeA), homeA, sourceProject)
	stopRecoveryEngineOnCleanup(t, envFor(homeB), homeB, receiverProject)
	setup := startCommand(t, envFor(homeA), "uri_pair_setup", homeA, sourceProject, "pair", "--code")
	setup.waitFor(20*time.Second, "codeaf pair")
	setup.kill()
	a := startWithEnv(t, envFor(homeA), "uri_pair_source", homeA, sourceProject, 180, 48, "chat", "--model", model, "--one-model", "--yolo")
	a.skipSetup(t)
	a.keys("Space", "Space")
	a.waitFor(20*time.Second, "Add another device")
	pairing := startCommand(t, envFor(homeB), "uri_pair_receiver", homeB, receiverProject, "pair")
	if _, asked := waitPlain(pairing, 10*time.Second, "Press Enter to keep that"); asked {
		pairing.keys("Enter")
	}
	screen := pairing.waitFor(20*time.Second, "/p/", "Check number")
	link := linkShape.FindString(screen)
	if link == "" {
		t.Fatal("public pairing did not show its link")
	}
	a.keys("M-d")
	a.waitFor(10*time.Second, "Paste it here")
	a.paste(link)
	a.waitFor(20*time.Second, "wants to pair")
	a.keys("a")
	pairing.waitFor(25*time.Second, "Paired")
	pairing.kill()
	a.keys("Escape", "Escape")
	a.lit("/task solo Create uri-relay-proof.txt containing exactly MOCK_URI_RELAY_HERON followed by a newline. Write your CODEAF_WORKSPACE_URI into uri-relay-workspace.txt. Write source-private-cache.bin only inside XDG_CACHE_HOME. Finish normally through plandb. Do not delegate or inspect other projects.")
	a.keys("Enter")
	sourceRun, sourceChat, sourceCache := ownershipRelayActualTask(t, homeA, a)
	sourceRegistry, err := workspaceidentity.Open(filepath.Join(homeA, "workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	sourceProjectRecord, err := sourceRegistry.Resolve(sourceRun.ProjectURI)
	if err != nil {
		t.Fatal(err)
	}
	if sourceProjectRecord.Path != sourceProject {
		t.Fatal("actual source task was not assigned to its project")
	}
	b := startWithEnv(t, envFor(homeB), "uri_take_receiver", homeB, receiverProject, 180, 48, "chat", "--model", model, "--one-model", "--yolo")
	b.skipSetup(t)
	b.keys("Space", "Space")
	// A direct /task has no foreground message to title this conversation.
	// Select its actual remote activity row, not an elided project-sidebar path.
	b.waitFor(60*time.Second, "untitled", "running on")
	b.keys("M-m")
	b.waitFor(20*time.Second, "Continue this chat here?")
	takeBegan := time.Now()
	b.keys("1", "Enter")
	b.waitFor(90*time.Second, "Moved from")
	coldTake := time.Since(takeBegan)
	receiverRegistry, err := workspaceidentity.Open(filepath.Join(homeB, "workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	var receivedRun, receivedChat workspaceidentity.Record
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); {
		receivedRun, err = receiverRegistry.Resolve(sourceRun.URI)
		if err == nil {
			receivedChat, err = receiverRegistry.Resolve(sourceChat.URI)
		}
		if err == nil {
			break
		}
		time.Sleep(pollEvery)
	}
	if err != nil {
		t.Fatalf("accepted takeover did not admit the actual source identities: %v; screen=%s", err, b.capture())
	}
	if receivedRun.URI != sourceRun.URI || receivedChat.URI != sourceChat.URI || receivedRun.Path == sourceRun.Path || receivedChat.Path == sourceChat.Path || receivedRun.OwnerURI != receivedChat.URI || receivedRun.ProjectURI != sourceRun.ProjectURI {
		t.Fatalf("takeover lost stable identities: run=%+v chat=%+v", receivedRun, receivedChat)
	}
	for _, path := range []string{receivedRun.Path, receivedChat.Path} {
		relative, err := filepath.Rel(homeB, path)
		if err != nil || !filepath.IsLocal(relative) {
			t.Fatal("receiver identity trusted a source-machine binding")
		}
	}
	for name, want := range map[string]string{"uri-relay-proof.txt": "MOCK_URI_RELAY_HERON\n", "uri-relay-workspace.txt": sourceRun.URI + "\n"} {
		raw, err := os.ReadFile(filepath.Join(receivedRun.Path, name))
		if err != nil || string(raw) != want {
			t.Fatalf("transferred task artifact %s differs: %q %v", name, raw, err)
		}
	}
	if _, err := receiverRegistry.Resolve(sourceCache.URI); err == nil {
		t.Fatal("source-private cache identity was imported with task material")
	}
	_ = filepath.WalkDir(homeB, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && entry.Name() == "source-private-cache.bin" {
			t.Errorf("private source cache was copied to receiver: %s", path)
		}
		return nil
	})
	lifecycleOpenCompletedCard(t, b)
	b.waitFor(20*time.Second, "uri-relay-proof", "m puts it in your message")
	if len(callModels(t, homeB)) != 0 {
		t.Fatal("receiver takeover or task-page inspection unexpectedly called the model")
	}
	for _, home := range []string{homeA, homeB} {
		for _, recorded := range callModels(t, home) {
			if recorded != model {
				t.Fatalf("unexpected recorded fixture model %q", recorded)
			}
		}
	}
	coldCounts := ownershipRelayCounters(t, homeB)
	warmBefore := ownershipRelayCounters(t, homeA)
	// Take the same real chat back onto its already populated device. This
	// measures the warm protocol without fabricating a checkpoint or cache hit.
	a.keys("Space", "Space")
	a.waitFor(30*time.Second, "untitled", "running on")
	a.keys("M-m")
	a.waitFor(20*time.Second, "Continue this chat here?")
	takeBegan = time.Now()
	a.keys("1", "Enter")
	a.waitFor(90*time.Second, "Moved from")
	warmTake := time.Since(takeBegan)
	warmAfter := ownershipRelayCounters(t, homeA)
	returned, err := sourceRegistry.Resolve(sourceRun.URI)
	if err != nil || returned.Path != sourceRun.Path {
		t.Fatal("warm return changed the original task workspace identity")
	}
	if raw, err := os.ReadFile(filepath.Join(returned.Path, "uri-relay-proof.txt")); err != nil || string(raw) != "MOCK_URI_RELAY_HERON\n" {
		t.Fatal("warm return lost the actual task output")
	}
	measurements := map[string]any{
		"cold_confirm_to_moved_ms":              float64(coldTake) / float64(time.Millisecond),
		"warm_confirm_to_moved_ms":              float64(warmTake) / float64(time.Millisecond),
		"cold_receiver_blob_payload_bytes_down": coldCounts.BytesDown,
		"warm_receiver_blob_payload_bytes_down": warmAfter.BytesDown - warmBefore.BytesDown,
		"cold_receiver_blob_gets":               coldCounts.Gets,
		"warm_receiver_blob_gets":               warmAfter.Gets - warmBefore.Gets,
		"relay_allocated_bytes":                 lifecycleAllocated(relayStore),
		"source_profile_allocated_bytes":        lifecycleAllocated(homeA),
		"receiver_profile_allocated_bytes":      lifecycleAllocated(homeB),
		"total_fixture_allocated_bytes":         lifecycleAllocatedMany(homeA, homeA+".host", filepath.Dir(sourceProject), homeB, homeB+".host", filepath.Dir(receiverProject), relayStore),
		"scope":                                 "two local isolated profiles; tiny fictional task; byte counters exclude HTTP/TLS overhead; timing includes terminal delivery/polling; allocations include fixture runtime stores",
	}
	raw, err := json.MarshalIndent(measurements, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cold and warm public transfer measurements:\n%s", raw)
	if evidence := os.Getenv("CODEAF_E2E_EVIDENCE_DIR"); evidence != "" {
		if err := os.WriteFile(filepath.Join(evidence, "ownership-relay-measurements.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("PASS actual /task produced durable run identity before transfer; public pair/approval/take preserved run/chat/project URIs with receiver-local bindings and exact artifacts; real task card opened without model calls; source-private cache excluded. Scripted protocol calls=%d, paid model calls=0.\n%s", modelCalls.Load(), b.capture())
}

func ownershipRelayCounters(t *testing.T, home string) cellstats.Line {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(home, "v3", "sync", "stats", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []cellstats.Line
	for _, path := range files {
		rows, err := cellstats.Read(home, strings.TrimSuffix(filepath.Base(path), ".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, rows...)
	}
	return cellstats.Total(lines)
}

// ownershipRelayActualTask observes output and the primary checkpoint written by
// the real run. A side index or manually seeded transcript is insufficient.
func ownershipRelayActualTask(t *testing.T, home string, r *rig) (workspaceidentity.Record, workspaceidentity.Record, workspaceidentity.Record) {
	t.Helper()
	registry, err := workspaceidentity.Open(filepath.Join(home, "workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	var run, chat, cache workspaceidentity.Record
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); {
		var uri string
		_ = filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && entry.Name() == "uri-relay-workspace.txt" && strings.Contains(path, string(filepath.Separator)+"trees"+string(filepath.Separator)) {
				raw, _ := os.ReadFile(path)
				uri = strings.TrimSpace(string(raw))
				return filepath.SkipAll
			}
			return nil
		})
		if uri != "" {
			run, err = registry.Resolve(uri)
			if err == nil && run.Role == workspaceidentity.Run {
				chat, err = registry.Resolve(run.OwnerURI)
				if err == nil && ownershipRelayCheckpointDone(home, uri) {
					cachePath, readErr := os.ReadFile(filepath.Join(run.Path, "uri-relay-cache-path.txt"))
					if readErr != nil {
						err = readErr
						continue
					}
					cache, err = registry.Lookup(workspaceidentity.Cache, strings.TrimSpace(string(cachePath)))
					if err == nil && cache.OwnerURI == run.ProjectURI && fileExists(filepath.Join(cache.Path, "source-private-cache.bin")) {
						return run, chat, cache
					}
				}
			}
		}
		time.Sleep(pollEvery)
	}
	t.Fatalf("actual task never produced its finished primary checkpoint and registered artifact: %v; screen=%s", err, r.capture())
	return run, chat, cache
}

func ownershipRelayCheckpointDone(home, uri string) bool {
	found := false
	_ = filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "tasks.json" {
			return nil
		}
		raw, _ := os.ReadFile(path)
		var checkpoint struct {
			Runs []struct {
				State session.TaskState       `json:"state"`
				Copy  *session.TaskCopyRecord `json:"copy"`
			} `json:"runs"`
		}
		if json.Unmarshal(raw, &checkpoint) == nil {
			for _, run := range checkpoint.Runs {
				if run.Copy != nil && run.Copy.URI == uri && run.State == session.TaskDone {
					found = true
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	return found
}

// The application is built by the caller once; the terminal must consume that
// exact committed candidate rather than quietly testing an earlier executable.
func ownershipRelayRequireCurrentBuild(t *testing.T) {
	t.Helper()
	version := upgradeEngineVersion(t, binary(t), nil, lifecycleHome(t, e2eModel))
	// Go omits vcs rows for linked worktrees; make build's application stamp
	// is the supported identity in that environment.
	command := exec.Command("git", "rev-parse", "--short", "HEAD")
	command.Dir = repoRoot(t)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.TrimSpace(string(output))
	if !strings.HasPrefix(version, "codeaf "+expected+" built ") || strings.Contains(version, "(dirty)") {
		t.Fatalf("relay proof requires a clean application built from current HEAD: application=%s expected=%s", strings.TrimSpace(version), expected)
	}
	t.Logf("Application artifact=%s; scripted protocol fixture, zero paid model calls", strings.TrimSpace(version))
}
