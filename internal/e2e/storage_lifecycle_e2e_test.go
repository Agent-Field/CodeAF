//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
	"github.com/Agent-Field/codeaf/internal/taskstorage"
)

// This acceptance uses the real terminal to create and continue a task. Public
// storage operations independently check that the continued work was retained.
// The task prompt is fictional and the profile configuration is minimal. Raw
// evidence stays private, including any incidental host metadata a tool observes.
func TestStorageLifecycleShellInterruptionKeepsResidentWork(t *testing.T) {
	archive := os.Getenv("CODEAF_PROTOTYPE_ARCHIVE_BINARY")
	if archive == "" {
		t.Skip("set CODEAF_PROTOTYPE_ARCHIVE_BINARY to the task-owned archive executable")
	}
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
	}
	home := lifecycleHome(t, model)
	ws := newWorkspace(t, "cold-task", false)
	env := []string{config.APIKeyEnv + "=" + liveKey(t), "CODEAF_BASE_URL=" + config.DefaultBaseURL,
		"CODEAF_PROTOTYPE_TASK_STORAGE=1", "CODEAF_PROTOTYPE_ARCHIVE_BINARY=" + archive,
		furrow.BinaryEnvVar + "=" + archive,
		"CODEAF_SYNC_URL=off"}
	lifecycleDaemonCleanup(t, home, archive)
	stopRecoveryEngineOnCleanup(t, env, home, ws)
	r := startWithEnv(t, env, "storage_lifecycle", home, ws, 180, 48, "chat", "--model", model)
	r.skipSetup(t)
	r.lit("/task solo This is a resumable fictional workflow. First read cold-proof.txt if it exists. If it does not exist: use one foreground bash command to write exactly first-stage-copper-heron followed by a newline, then sleep for120 seconds in that same command and wait for it. If it already contains that first line: preserve it and append exactly second-stage-resumed followed by a newline, then finish. Do not delegate, edit other files or run the sleep in the background.")
	r.keys("Enter")
	path := lifecycleAwaitPartial(t, home, 6*time.Minute)
	oldPID := upgradePID(upgradeEngineCommand(t, binary(t), env, home, ws, "--status"))
	if oldPID == "" {
		t.Fatal("task was not served by a background engine")
	}
	// Closing the application interrupts unfinished work. Explicit Stop is a
	// different user decision: stopped runs intentionally cannot be continued.
	r.kill()
	upgradeEngineCommand(t, binary(t), env, home, ws, "--stop")
	m := taskstorage.Manager{Root: filepath.Join(home, "task-storage"), Trees: filepath.Dir(path), Vault: taskstorage.CLIBackend{Binary: archive, Store: filepath.Join(home, "task-storage", "vault")}}
	state, err := m.Inspect(context.Background(), path)
	if err != nil || state.Phase != taskstorage.Resident {
		t.Fatalf("uncontained shell task was retired: %+v %v", state, err)
	}
	if data, err := os.ReadFile(filepath.Join(path, "cold-proof.txt")); err != nil || string(data) != "first-stage-copper-heron\n" {
		t.Fatal("interrupted shell task did not retain its exact partial work")
	}
	t.Log("PASS interrupted shell task conservatively kept resident; no unproven writer retirement")
	r = startWithEnv(t, env, "storage_lifecycle_reopen", home, ws, 180, 48, "chat", "--model", model)
	r.skipSetup(t)
	if strings.Contains(r.capture(), "Move this conversation here?") {
		r.keys("1", "Enter")
	}
	// Normal startup picks up interrupted work using its existing continuation
	// road. No replacement task or altered execution semantics are introduced.
	var b []byte
	for until := time.Now().Add(6 * time.Minute); time.Now().Before(until); {
		b, _ = os.ReadFile(filepath.Join(path, "cold-proof.txt"))
		if string(b) == "first-stage-copper-heron\nsecond-stage-resumed\n" {
			break
		}
		time.Sleep(pollEvery)
	}
	if string(b) != "first-stage-copper-heron\nsecond-stage-resumed\n" {
		t.Fatal("restarted shell workflow did not preserve and continue original work")
	}
	newPID := upgradePID(upgradeEngineCommand(t, binary(t), env, home, ws, "--status"))
	if newPID == "" || newPID == oldPID {
		t.Fatal("restart did not create a new background engine")
	}
	openTasksPlace(t, r)
	r.waitFor(2*time.Minute, "completed", fmt.Sprintf("%d done", 1))
	state, err = m.Inspect(context.Background(), path)
	if err != nil || state.Phase != taskstorage.Resident {
		t.Fatal("continued uncontained shell task lost its conservative residency")
	}
	models := callModels(t, home)
	if len(models) == 0 {
		t.Fatal("missing task model receipts")
	}
	for _, got := range models {
		if got != model {
			t.Fatalf("unexpected recorded model %q", got)
		}
	}
	lifecycleCompletedCalls(t, home, model)
	t.Logf("PASS real terminal shell task interrupted, retained, engine restarted and same task continued with original bytes; no unsafe retirement; %d exact model receipts", len(models))
}

func lifecycleAwaitPartial(t *testing.T, home string, within time.Duration) string {
	t.Helper()
	for deadline := time.Now().Add(within); time.Now().Before(deadline); {
		var found string
		_ = filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && entry.Name() == "cold-proof.txt" && strings.Contains(path, string(filepath.Separator)+"trees"+string(filepath.Separator)) {
				if b, err := os.ReadFile(path); err == nil && string(b) == "first-stage-copper-heron\n" {
					found = filepath.Dir(path)
				}
			}
			return nil
		})
		if found != "" {
			return found
		}
		time.Sleep(pollEvery)
	}
	t.Fatal("real interrupted workflow never wrote its expected partial file")
	return ""
}

// Live task stages use fictional data in fresh profiles. Real-copy acceptance
// uses the separate offline fixture, zero provider requests and a controlled
// local relay. Full local archives and withheld files remain at source.
func TestStorageLifecycleColdTaskMovesThroughPublicPairAndTake(t *testing.T) {
	lifecycleRelayJourney(t, true)
}

func TestStorageLifecycleOfflineRealCopyMovesThroughPublicPairAndTake(t *testing.T) {
	lifecycleRelayJourney(t, false)
}

func lifecycleRelayJourney(t *testing.T, live bool) {
	if live && os.Getenv("CODEAF_E2E_REAL_TASK_CORPUS") != "" {
		t.Fatal("real corpus requires the offline fixture journey; live post-start injection is not supported")
	}
	archive := os.Getenv("CODEAF_PROTOTYPE_ARCHIVE_BINARY")
	if archive == "" {
		t.Skip("set CODEAF_PROTOTYPE_ARCHIVE_BINARY")
	}
	if live {
		requireTmuxAndKey(t)
	} else if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	const model = "deepseek/deepseek-v4.1-flash"
	if live && e2eModel != model {
		t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
	}
	key, base := "unused-offline-fixture-key", config.DefaultBaseURL
	var refused atomic.Int64
	if live {
		key = liveKey(t)
	} else {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(r.URL.Path, "completions") || strings.Contains(r.URL.Path, "responses") {
				refused.Add(1)
				http.Error(w, "offline fixture must never call a model", 500)
				return
			}
			fmt.Fprint(w, `{"data":[]}`)
		}))
		defer server.Close()
		base = server.URL
		t.Cleanup(func() {
			if refused.Load() != 0 {
				t.Errorf("offline fixture attempted %d model requests", refused.Load())
			}
		})
	}
	relayStore := filepath.Join(t.TempDir(), "relay-store")
	relay := startRelayAt(t, relayStore)
	env := []string{config.APIKeyEnv + "=" + key, "CODEAF_BASE_URL=" + base,
		"CODEAF_SYNC_URL=" + relay, "CODEAF_SYNC_INTERVAL_MS=1000",
		"CODEAF_PROTOTYPE_TASK_STORAGE=1", "CODEAF_PROTOTYPE_ARCHIVE_BINARY=" + archive,
		furrow.BinaryEnvVar + "=" + archive}
	homeA, homeB := lifecycleHome(t, model), lifecycleHome(t, model)
	var fullReal, portableReal map[string]string
	wsA, wsB := newWorkspace(t, "cold-source", false), newWorkspace(t, "cold-receiver", false)
	lifecycleDaemonCleanup(t, homeA, archive)
	lifecycleDaemonCleanup(t, homeB, archive)
	t.Cleanup(func() {
		// A takeover can start an engine for its new local workspace, beyond
		// wsB. Stop only processes belonging to these two fresh profile roots.
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
	stopRecoveryEngineOnCleanup(t, env, homeA, wsA)
	stopRecoveryEngineOnCleanup(t, env, homeB, wsB)
	// The application's public pairing command establishes the first identity.
	setup := startCommand(t, env, "storage_pair_setup", homeA, wsA, "pair", "--code")
	setup.waitFor(20*time.Second, "codeaf pair")
	setup.kill()
	a := startWithEnv(t, env, "storage_pair_a", homeA, wsA, 180, 48)
	a.skipSetup(t)
	a.keys("Space", "Space")
	a.waitFor(20*time.Second, "Add another device")
	b := startCommand(t, env, "storage_pair_b", homeB, wsB, "pair")
	if _, asked := waitPlain(b, 10*time.Second, "Press Enter to keep that"); asked {
		b.keys("Enter")
	}
	screen := b.waitFor(20*time.Second, "/p/", "Check number")
	link := linkShape.FindString(screen)
	if link == "" {
		t.Fatal("public pairing command did not show a link")
	}
	a.keys("M-d")
	a.waitFor(10*time.Second, "Paste it here")
	a.paste(link)
	a.waitFor(20*time.Second, "wants to pair")
	a.keys("a")
	b.waitFor(25*time.Second, "Paired")
	b.kill()
	a.kill()
	a = startWithEnv(t, env, "storage_cold_source", homeA, wsA, 180, 48, "chat", "--model", model)
	a.skipSetup(t)
	var source string
	var manager taskstorage.Manager
	if live {
		a.lit("/task solo Create relay-cold-proof.txt containing exactly portable-copper-heron followed by a newline using a simple finite foreground printf command. Do not start background commands, daemons or delegates. Do not edit other files. Do not inspect ps, env, processes, host information or anything outside the assigned directory. Complete normally through plandb.")
		a.keys("Enter")
		openTasksPlace(t, a)
		a.waitFor(6*time.Minute, "completed", fmt.Sprintf("%d done", 1))
		a.keys("Escape")
		_ = filepath.WalkDir(homeA, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.Name() == "relay-cold-proof.txt" && strings.Contains(path, string(filepath.Separator)+"trees"+string(filepath.Separator)) {
				source = filepath.Dir(path)
			}
			return nil
		})
		if source == "" {
			t.Fatal("completed default task has no resident proof file")
		}
		manager = taskstorage.Manager{Root: filepath.Join(homeA, "task-storage"), Trees: filepath.Dir(source), Vault: taskstorage.CLIBackend{Binary: archive, Store: filepath.Join(homeA, "task-storage", "vault")}}
		resident, err := manager.Inspect(context.Background(), source)
		if err != nil || resident.Phase != taskstorage.Resident {
			t.Fatal("ordinary shell task must stay conservatively resident")
		}
	}
	a.kill()
	upgradeEngineCommand(t, binary(t), env, homeA, wsA, "--stop")
	if len(engineDaemonsUnder(homeA)) != 0 {
		t.Fatal("owned engine is still writing")
	}
	stopNative, cancelNative := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelNative()
	if err := (cellstore.Daemon{Socket: filepath.Join(homeA, "v3", "engine-"+strings.TrimPrefix(filepath.Base(archive), "furrow-")+".sock"), Binary: archive}).Stop(stopNative); err != nil {
		t.Fatal(err)
	}
	if live {
		lifecycleCertifyOfflineFixture(t, homeA)
	} else {
		manager, source = lifecycleSeedOfflineTask(t, homeA, wsA, archive)
		if corpus := os.Getenv("CODEAF_E2E_REAL_TASK_CORPUS"); corpus != "" {
			fullReal, portableReal = lifecycleInjectRealCopy(t, corpus, source)
			lifecycleSaveManifest(t, "expected-portable", portableReal)
			lifecycleSaveManifest(t, "expected-full-local", fullReal)
		}
	}
	manager.Busy = func(string) bool { return false }
	ownerForCold := cell.Cell{Root: filepath.Dir(manager.Trees)}
	carryForCold := taskcopy.Carry{}
	manager.BeforeCapture = func(_ context.Context, path string) error { return carryForCold.PrepareCold(ownerForCold, path) }
	manager.BeforeRetire = func(_ context.Context, snapshot, path string) error {
		return carryForCold.MarkCold(ownerForCold, path, snapshot)
	}
	if err := manager.Park(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if live {
		t.Log("PASS default task remained resident; manually certified offline fixture retired through public Manager; automatic shell retirement is NOT claimed")
	} else {
		t.Log("PASS explicitly seeded offline fixture retired through public Manager; no worker or model ran; automatic task lifecycle is NOT claimed")
	}
	if !live {
		if err := manager.Ensure(context.Background(), source); err != nil {
			t.Fatal(err)
		}
		if fullReal != nil {
			actual := lifecycleContentManifest(t, source)
			lifecycleSaveManifest(t, "offline-reopened-full-local", actual)
			lifecycleRequireManifest(t, "offline full local reopen", fullReal, actual)
		}
		if err := manager.Park(context.Background(), source); err != nil {
			t.Fatal(err)
		}
		t.Log("PASS full offline public Manager reopen verified before source application opens; source starts with the complete real cold artifact")
	}
	a = startWithEnv(t, env, "storage_certified_source", homeA, wsA, 180, 48, "chat", "--model", model)
	a.skipSetup(t)
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatal("source was not cold before transfer")
	}
	lifecycleOpenCompletedCard(t, a)
	cardScreen := a.waitFor(20*time.Second, "relay-cold-proof", "m puts it in your message")
	if !strings.Contains(cardScreen, "done") {
		t.Fatal("completed default task result card did not show its outcome")
	}
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatal("reading completed result card eagerly expanded cold files")
	}
	t.Log("PASS actual completed default task result card opened and remained cold; this surface has no Work tab")
	a.keys("Escape", "Escape")
	manager.Busy = func(string) bool { return false }
	owner := cell.Cell{Root: filepath.Dir(manager.Trees)}
	carry := taskcopy.Carry{}
	manager.BeforeCapture = func(_ context.Context, path string) error { return carry.PrepareCold(owner, path) }
	manager.BeforeRetire = func(_ context.Context, snapshot, path string) error { return carry.MarkCold(owner, path, snapshot) }
	if err := manager.Park(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	// The existing read tool is a real user's route back to a completed copy;
	// showing its record or hovering a task must not expand anything.
	if live {
		readPrompt := "Read the existing file " + filepath.Join(source, "relay-cold-proof.txt") + " using the read tool and report its exact line. Do not recreate it or edit any file."
		a.lit(readPrompt)
		a.keys("Enter")
		answer, tools := privateMemoryAwaitAnswer(homeA, readPrompt, 2*time.Minute)
		if !strings.Contains(answer, "portable-copper-heron") || !strings.Contains(strings.Join(tools, ","), "read") {
			t.Fatal("actual read tool did not reopen the completed cold task")
		}
	} else {
		t.Log("PASS offline public Manager exact reopen already verified before source application started; no model integration claimed")
	}
	postModelCount := len(callModels(t, homeA))
	postReceiverModelCount := len(callModels(t, homeB))
	if err := manager.Park(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatal("source did not return to cold storage")
	}
	// The source remains open so the normal online transfer asks for explicit
	// takeover. The receiver has neither this task nor any local archive bytes.
	b = startWithEnv(t, env, "storage_cold_receiver", homeB, wsB, 180, 48)
	b.skipSetup(t)
	b.keys("Space", "Space")
	if live {
		b.waitFor(45*time.Second, "cold-source")
	} else {
		b.waitFor(45*time.Second, "Offline real workspace")
	}
	b.keys("M-m")
	b.waitFor(20*time.Second, "Continue this chat here?")
	b.keys("1", "Enter")
	b.waitFor(90*time.Second, "Moved from")
	var copied string
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); {
		_ = filepath.WalkDir(homeB, func(path string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && entry.Name() == "relay-cold-proof.txt" && strings.Contains(path, string(filepath.Separator)+"trees"+string(filepath.Separator)) {
				if data, err := os.ReadFile(path); err == nil && string(data) == "portable-copper-heron\n" {
					copied = path
				}
			}
			return nil
		})
		if copied != "" {
			break
		}
		time.Sleep(pollEvery)
	}
	if copied == "" {
		t.Fatal("public takeover did not restore the cold task's exact portable file")
	}
	if live {
		lifecycleOpenCompletedCard(t, b)
		b.waitFor(20*time.Second, "relay-cold-proof", "m puts it in your message")
		t.Log("PASS actual receiver completed result card reopened transferred task without a model call")
	} else {
		b.waitFor(20*time.Second, "Manually seeded offline fixture only")
		t.Log("PASS actual public takeover reopened the labelled offline conversation; task-history migration is not claimed")
	}
	lifecycleDiskReport(t, "after_transfer_source_cold", homeA, homeB, wsA, wsB, relayStore)
	if portableReal != nil {
		if got := len(callModels(t, homeA)); got != postModelCount {
			t.Fatal("unexpected model request after real-data injection")
		}
		if got := len(callModels(t, homeB)); got != postReceiverModelCount {
			t.Fatal("unexpected receiver model request after real-data injection")
		}
		received := lifecycleContentManifest(t, filepath.Dir(copied))
		lifecycleSaveManifest(t, "received-all", received)
		// A native fork creates these three local ownership identifiers. They
		// are not user payload; permit only their exact paths and ID format.
		for _, name := range []string{".furrow/family-id", ".furrow/fork-id", ".furrow/workspace-id"} {
			if _, portable := portableReal[name]; portable {
				continue
			}
			if _, exists := received[name]; !exists {
				continue
			}
			path := filepath.Join(filepath.Dir(copied), filepath.FromSlash(name))
			parentInfo, parentErr := os.Lstat(filepath.Dir(path))
			info, err := os.Lstat(path)
			if parentErr != nil || !parentInfo.IsDir() || err != nil || !info.Mode().IsRegular() {
				t.Fatalf("native ownership identifier is not a local regular file: %s", name)
			}
			raw, readErr := os.ReadFile(path)
			valid := readErr == nil && len(raw) == 33 && raw[32] == '\n'
			if valid {
				for _, b := range raw[:32] {
					if !(b >= '0' && b <= '9' || b >= 'a' && b <= 'f') {
						valid = false
						break
					}
				}
			}
			if !valid {
				t.Fatalf("unexpected native ownership identifier at %s", name)
			}
			delete(received, name)
		}
		lifecycleSaveManifest(t, "received-portable", received)
		lifecycleRequireManifest(t, "portable receiver", portableReal, received)
		if err := manager.Ensure(context.Background(), source); err != nil {
			t.Fatal(err)
		}
		preserved := lifecycleContentManifest(t, source)
		lifecycleSaveManifest(t, "reopened-full-local", preserved)
		lifecycleRequireManifest(t, "full source capsule", fullReal, preserved)
		if err := manager.Park(context.Background(), source); err != nil {
			t.Fatal(err)
		}
		t.Logf("PASS complete real workspace after owned Git rebinding: %d local files retained, %d portable files exact at receiver; original copied Git metadata retained outside fixture", len(fullReal), len(portableReal))
	}
	lifecycleDiskReport(t, "after_source_reopen_verified_and_reparked", homeA, homeB, wsA, wsB, relayStore)
	for _, home := range []string{homeA, homeB} {
		for _, got := range callModels(t, home) {
			if got != model {
				t.Fatalf("unexpected recorded model %q", got)
			}
		}
	}
	if live {
		lifecycleCompletedCalls(t, homeA, model)
	} else {
		if len(callModels(t, homeA)) != 0 || len(callModels(t, homeB)) != 0 || refused.Load() != 0 {
			t.Fatal("offline fixture made a model request")
		}
		t.Log("PASS offline fixture ZERO model requests, enforced by local refusing endpoint and both call logs")
	}
	// Capture the authentic terminal rendering; no reconstructed UI is evidence.
	t.Logf("PASS public pair, approval and online takeover restored an archived task on a receiver without source objects.\n%s", b.capture())
	if !live {
		lifecycleHandoff(t, homeB, wsB, relay, b.name, archive, filepath.Dir(copied), portableReal)
	}
}

func lifecycleSaveManifest(t *testing.T, name string, manifest map[string]string) {
	t.Helper()
	gate := os.Getenv("CODEAF_E2E_REAL_RELAY_HANDOFF")
	if gate == "" {
		return
	}
	rel, err := filepath.Rel(filepath.Dir(repoRoot(t)), gate)
	if err != nil || !filepath.IsLocal(rel) {
		t.Fatal("private manifest must stay task owned")
	}
	if err := os.MkdirAll(gate, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gate, name+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func lifecycleRequireManifest(t *testing.T, scope string, expected, actual map[string]string) {
	t.Helper()
	for path, want := range expected {
		got, ok := actual[path]
		if !ok {
			t.Fatalf("%s file absent: %s", scope, path)
		}
		if got != want {
			wantMode, wantBody, _ := strings.Cut(want, ":")
			gotMode, gotBody, _ := strings.Cut(got, ":")
			if wantMode != gotMode {
				t.Fatalf("%s mode/type mismatch at %s: expected %s received %s", scope, path, wantMode, gotMode)
			}
			if wantBody != gotBody {
				t.Fatalf("%s bytes/link-target mismatch at %s", scope, path)
			}
		}
	}
	for path := range actual {
		if _, ok := expected[path]; !ok {
			t.Fatalf("%s unexpected file (including withheld payload): %s", scope, path)
		}
	}
}

func lifecycleGit(t *testing.T, directory string, args ...string) []byte {
	t.Helper()
	argv := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "user.email=e2e@example.com", "-c", "user.name=e2e", "-c", "commit.gpgsign=false", "-C", directory}, args...)
	cmd := exec.Command("git", argv...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("owned fixture Git command %s failed: %v", args[0], err)
	}
	return out
}

func lifecycleSeedOfflineTask(t *testing.T, home, workspace, archive string) (taskstorage.Manager, string) {
	t.Helper()
	metas, err := filepath.Glob(filepath.Join(home, "v3", "projects", "*", "*", cell.MetaPath))
	if err != nil || len(metas) == 0 {
		t.Fatal("public blank chat did not create its cell")
	}
	sort.Strings(metas)
	root := filepath.Dir(filepath.Dir(metas[len(metas)-1]))
	sid := filepath.Base(root)
	source := filepath.Join(root, cell.TreesDir, "1")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	lifecycleGit(t, source, "init", "-q", "-b", "fixture/offline-storage")
	if err := os.WriteFile(filepath.Join(source, "relay-cold-proof.txt"), []byte("portable-copper-heron\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycleGit(t, source, "add", "--", "relay-cold-proof.txt")
	lifecycleGit(t, source, "commit", "-q", "-m", "offline fixture marker")
	now := time.Now().UTC()
	journal := []map[string]any{
		{"type": "session", "version": 1, "id": sid, "cwd": workspace, "model": "deepseek/deepseek-v4.1-flash", "timestamp": now},
		{"type": "message", "role": "user", "content": "Offline real workspace storage fixture. No model or worker ran this fixture.", "timestamp": now},
		{"type": "message", "role": "assistant", "content": "Manually seeded offline fixture only. No model or worker generated this record.", "timestamp": now.Add(time.Nanosecond)},
	}
	var lines strings.Builder
	for _, line := range journal {
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		lines.Write(raw)
		lines.WriteByte('\n')
	}
	transcript := filepath.Join(root, cell.TranscriptPath)
	if err := os.WriteFile(transcript, []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(root, "meta.json"), map[string]any{"id": sid, "title": "Offline real workspace storage fixture", "workspace": workspace, "created": now, "lastUserAt": now})
	entry := session.TaskIndexEntry{SessionID: sid, ID: "1", Title: "relay-cold-proof OFFLINE FIXTURE", Status: string(session.TaskDone), EndedAt: now, Outcome: "Manually seeded offline storage fixture. No model or worker ran.", ArtifactURI: "file://" + source}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.TaskIndexPath(transcript), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := taskstorage.Manager{Root: filepath.Join(home, "task-storage"), Trees: filepath.Dir(source), Vault: taskstorage.CLIBackend{Binary: archive, Store: filepath.Join(home, "task-storage", "vault")}}
	return manager, source
}

func lifecycleOpenCompletedCard(t *testing.T, r *rig) {
	t.Helper()
	r.lit("/history")
	state, _ := r.waitForAny(20*time.Second, "/history", "m puts it in your message", say(t, "tasksFoldShutWord"), say(t, "tasksFoldOpenWord"))
	if state == "/history" {
		r.keys("Enter")
		state, _ = r.waitForAny(20*time.Second, "m puts it in your message", say(t, "tasksFoldShutWord"), say(t, "tasksFoldOpenWord"))
	}
	// Returning to history preserves its selected child. The command can
	// already open that record; only a conversation row has a fold to expand.
	if state == "m puts it in your message" {
		return
	}
	if state == say(t, "tasksFoldShutWord") {
		r.keys("Right")
		r.waitFor(20*time.Second, say(t, "tasksFoldOpenWord"))
	}
	r.keys("Down", "Enter")
	r.waitFor(20*time.Second, "m puts it in your message")
}

// This seeded UI regression never submits a model request. It checks the actual
// completed-record variant before the live storage journey spends any tokens.
func TestStorageLifecycleCompletedResultCardNavigationNoModel(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	home := lifecycleHome(t, "deepseek/deepseek-v4.1-flash")
	ws := newWorkspace(t, "completed-card", false)
	bucket := filepath.Join(home, "v3", "projects", strings.ReplaceAll(filepath.Clean(ws), string(filepath.Separator), "-"))
	sid := "7100000000000001"
	dir := filepath.Join(bucket, sid)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	statesSeedTranscript(t, dir, sid, ws, "inspect the completed mock task")
	now := time.Now().Add(-time.Minute)
	writeJSON(t, filepath.Join(dir, "meta.json"), map[string]any{"id": sid, "title": "Mock completed conversation", "workspace": ws, "created": now, "lastUserAt": now})
	entry := session.TaskIndexEntry{SessionID: sid, ID: "1", Title: "Mock completed storage fixture", Status: string(session.TaskDone), EndedAt: now, Outcome: "Saved the fictional marker."}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "tasks.jsonl"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	r := startWithEnv(t, []string{"CODEAF_BASE_URL=http://127.0.0.1:1", config.APIKeyEnv + "=unused-mock-key"}, "storage_card_navigation", home, ws, 180, 48, "chat", "--no-host")
	r.skipSetup(t)
	for i := 0; i < 2; i++ {
		lifecycleOpenCompletedCard(t, r)
		screen := r.waitFor(20*time.Second, "Mock completed storage fixture", "m puts it in your message")
		if strings.Contains(screen, say(t, "roomTabsWords")) {
			t.Fatal("completed record unexpectedly claimed a live room")
		}
		r.keys("Escape", "Escape")
	}
	if len(callModels(t, home)) != 0 {
		t.Fatal("navigation called a model")
	}
	t.Log("PASS actual completed result card, Escape twice to conversation, repeat history navigation; zero model calls")
}

// Certification is an explicit test-operator audit of this finite mock workflow.
// It does not remove or override the product's sticky unknown-writer marker.
func lifecycleCertifyOfflineFixture(t *testing.T, home string) {
	t.Helper()
	gate := os.Getenv("CODEAF_E2E_REAL_RELAY_HANDOFF")
	if gate == "" {
		t.Fatal("controlled offline fixture requires an explicit private audit gate")
	}
	rel, err := filepath.Rel(filepath.Dir(repoRoot(t)), gate)
	if err != nil || !filepath.IsLocal(rel) {
		t.Fatal("audit gate must be task owned")
	}
	if err := os.MkdirAll(gate, 0o700); err != nil {
		t.Fatal(err)
	}
	audit := filepath.Join(gate, "offline-audit.json")
	certificate := filepath.Join(gate, "offline-certified")
	if _, err := os.Lstat(certificate); !os.IsNotExist(err) {
		t.Fatal("offline certificate must be fresh")
	}
	raw, _ := json.Marshal(map[string]string{"home": home, "certificate": certificate, "requirement": "Review every actual worker shell command and confirm finite known fixture operations with no detached writer; all owned engines are stopped. This certifies only this copied fixture."})
	if err := os.WriteFile(audit, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Log("OFFLINE AUDIT READY: private fixture audit receipt; waiting for explicit test-operator certification")
	for until := time.Now().Add(5 * time.Minute); time.Now().Before(until); {
		if _, err := os.Lstat(certificate); err == nil {
			t.Log("controlled offline mock fixture explicitly certified")
			return
		}
		select {
		case <-t.Context().Done():
			t.Fatal("offline audit cancelled")
		case <-time.After(time.Second):
		}
	}
	t.Fatal("offline fixture was not explicitly certified; refusing retirement")
}

// Keep the actual successful public-relay fixture alive for a physical receiver.
// The optional gate never sends a chat message or changes product behavior.
func lifecycleHandoff(t *testing.T, home, workspace, relay, session, archive, task string, manifest map[string]string) {
	t.Helper()
	if t.Failed() {
		t.Log("handoff withheld because a prior acceptance assertion failed")
		return
	}
	gate := os.Getenv("CODEAF_E2E_REAL_RELAY_HANDOFF")
	if gate == "" {
		return
	}
	owned := filepath.Dir(repoRoot(t))
	rel, err := filepath.Rel(owned, gate)
	if err != nil || !filepath.IsLocal(rel) {
		t.Fatal("handoff must stay within owned task root")
	}
	if manifest == nil {
		t.Fatal("physical relay handoff requires verified real corpus")
	}
	if err := os.MkdirAll(gate, 0o700); err != nil {
		t.Fatal(err)
	}
	release := filepath.Join(gate, "release")
	if _, err := os.Lstat(release); !os.IsNotExist(err) {
		t.Fatal("handoff release must be fresh")
	}
	manifestPath := filepath.Join(gate, "portable.json")
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	count := len(callModels(t, home))
	receipt := map[string]any{"homeB": home, "wsB": workspace, "relayURL": relay, "tmuxSessionB": session, "binary": binary(t), "archive": archive, "receivedTaskRoot": task, "portableManifest": manifestPath, "modelCountBeforeHandoff": count, "releaseFile": release}
	raw, err = json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gate, "ready.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Log("PASS same-host assertions; private physical receiver handoff ready")
	deadline := time.NewTimer(45 * time.Minute)
	defer deadline.Stop()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	progress := time.NewTicker(30 * time.Second)
	defer progress.Stop()
	for {
		select {
		case <-t.Context().Done():
			t.Fatal("physical relay handoff context ended")
		case <-deadline.C:
			t.Fatal("physical relay handoff expired; fixture cleanup proceeding")
		case <-progress.C:
			t.Log("physical receiver handoff still active; owned fixture retained")
		case <-poll.C:
			if _, err := os.Lstat(release); err == nil {
				if len(callModels(t, home)) != count {
					t.Fatal("physical handoff unexpectedly made a model call")
				}
				t.Log("physical receiver released fixture; cleanup proceeding")
				return
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
}

func lifecycleInjectRealCopy(t *testing.T, corpus, destination string) (map[string]string, map[string]string) {
	t.Helper()
	owned := filepath.Dir(repoRoot(t))
	rel, err := filepath.Rel(owned, corpus)
	if err != nil || !filepath.IsLocal(rel) {
		t.Fatal("real corpus must be an already-owned isolated copy")
	}
	if info, err := os.Lstat(corpus); err != nil || !info.IsDir() {
		t.Fatal("missing owned real corpus")
	}
	git := func(args ...string) []byte {
		argv := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-C", destination}, args...)
		cmd := exec.Command("git", argv...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("owned fixture git %s failed: %v", args[0], err)
		}
		return out
	}
	branch := strings.TrimSpace(string(git("symbolic-ref", "--short", "HEAD")))
	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(destination, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
	entries, err = os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		} // Copied original repository metadata remains outside the test workspace.
		cmd := exec.Command("cp", "-a", "--reflink=auto", "--", filepath.Join(corpus, entry.Name()), destination)
		if err := cmd.Run(); err != nil {
			t.Fatalf("copy owned real workspace entry: %v", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(destination, "relay-cold-proof.txt")); !os.IsNotExist(err) {
		t.Fatal("real corpus collides with generated proof marker")
	}
	if err := os.WriteFile(filepath.Join(destination, "relay-cold-proof.txt"), []byte("portable-copper-heron\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q", "-b", branch)
	listing := strings.Split(strings.TrimRight(string(git("ls-files", "--others", "--exclude-standard", "-z")), "\x00"), "\x00")
	held := map[string]bool{}
	for _, finding := range (keys.Scanner{}).Paths(destination, listing) {
		held[finding.Path] = true
	}
	var cleared []byte
	portable := make(map[string]string)
	full := lifecycleContentManifest(t, destination)
	for _, path := range listing {
		if path == "" || held[path] {
			continue
		}
		if value, ok := full[filepath.FromSlash(path)]; ok {
			portable[filepath.FromSlash(path)] = value
			cleared = append(cleared, []byte(path)...)
			cleared = append(cleared, 0)
		}
	}
	cmd := exec.Command("git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-C", destination, "add", "--pathspec-from-file=-", "--pathspec-file-nul")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader(string(cleared))
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	git("-c", "user.email=e2e@example.com", "-c", "user.name=e2e", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "owned relay fixture baseline")
	t.Logf("REAL COPY baseline after Git rebinding: local files=%d portable files=%d withheld-secret-files=%d ignored-or-nonportable=%d; no subsequent model calls", len(full), len(portable), len(held), len(full)-len(portable)-len(held))
	return full, portable
}

func lifecycleContentManifest(t *testing.T, root string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			out[rel] = "link:" + target
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported real corpus node")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = fmt.Sprintf("%o:%x", info.Mode().Perm(), sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func lifecycleCompletedCalls(t *testing.T, home, model string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, "logs", "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	completed := 0
	for _, line := range strings.Split(string(raw), "\n") {
		var row calllog.Record
		if json.Unmarshal([]byte(line), &row) != nil || row.Phase != "" {
			continue
		}
		if row.Model != "" && row.Model != model {
			t.Fatalf("unexpected completed model %q", row.Model)
		}
		if row.Model == model && row.Status >= 200 && row.Status < 300 && row.CompletionTokens > 0 {
			completed++
		}
	}
	if completed == 0 {
		t.Fatal("no successful nonempty live model completion receipts")
	}
	t.Logf("verified %d successful nonempty OpenRouter-model completion receipts", completed)
}

// Allocated ext4 blocks include catalogs, archive payloads, journals, WAL and
// relay frames. Inodes are counted once for hardlinks. APFS corpus measurements
// use isolated-container accounting instead of this file-block approximation.
func lifecycleAllocated(root string) int64 {
	return lifecycleAllocatedMany(root)
}

func lifecycleAllocatedMany(roots ...string) int64 {
	seen := make(map[[2]uint64]bool)
	var bytes int64
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			info, err := os.Lstat(path)
			if err != nil {
				return nil
			}
			if s, ok := info.Sys().(*syscall.Stat_t); ok {
				key := [2]uint64{uint64(s.Dev), s.Ino}
				if !seen[key] {
					bytes += s.Blocks * 512
					seen[key] = true
				}
			}
			return nil
		})
	}
	return bytes
}

func lifecycleDiskReport(t *testing.T, phase, a, b, wsA, wsB, relay string) {
	t.Helper()
	rows := map[string]int64{
		"source_profile_including_vault_and_cell_stores":       lifecycleAllocated(a),
		"source_isolated_login_including_native_fork_stores":   lifecycleAllocated(a + ".host"),
		"source_project_and_sibling_forks":                     lifecycleAllocated(filepath.Dir(wsA)),
		"receiver_profile_including_vault_and_cell_stores":     lifecycleAllocated(b),
		"receiver_isolated_login_including_native_fork_stores": lifecycleAllocated(b + ".host"),
		"receiver_project_and_sibling_forks":                   lifecycleAllocated(filepath.Dir(wsB)),
		"relay_store":                                          lifecycleAllocated(relay),
		"total_hardlink_deduplicated":                          lifecycleAllocatedMany(a, a+".host", filepath.Dir(wsA), b, b+".host", filepath.Dir(wsB), relay),
	}
	raw, _ := json.Marshal(rows)
	t.Logf("ALLOCATED %s %s", phase, raw)
}

func lifecycleAwaitCold(t *testing.T, home, archive, previous string, within time.Duration) (taskstorage.Manager, string, taskstorage.State) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		var candidates []string
		_ = filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() && entry.Name() == "trees" {
				candidates = append(candidates, path)
				return filepath.SkipDir
			}
			return nil
		})
		for _, trees := range candidates {
			m := taskstorage.Manager{Root: filepath.Join(home, "task-storage"), Trees: trees,
				Vault: taskstorage.CLIBackend{Binary: archive, Store: filepath.Join(home, "task-storage", "vault")}}
			paths, err := m.Paths(context.Background())
			if err != nil {
				continue
			}
			for _, path := range paths {
				s, err := m.Inspect(context.Background(), path)
				if err == nil && s.Phase == taskstorage.Cold && s.Snapshot != previous {
					return m, path, s
				}
			}
		}
		time.Sleep(pollEvery)
	}
	t.Fatal("real task did not reach a new cold state within acceptance deadline")
	return taskstorage.Manager{}, "", taskstorage.State{}
}

func lifecycleHome(t *testing.T, model string) string {
	t.Helper()
	home := filepath.Join(shortRoot(t), "h")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	rows := map[string]any{
		config.KeyIcons:         config.IconsPlain,
		config.KeyMemoryEnabled: false,
		config.KeyTaskMaxLoad:   0,
		"tools.approvalMode":    "allow",
	}
	for _, key := range []string{config.KeyChatModel, config.KeyTaskModel,
		config.KeyTierWorkerModel, config.KeyTierLowModel, config.KeyTierHighModel,
		config.KeyTierReflexModel, config.KeyTierMastermindModel, config.KeyModelFallbacks} {
		rows[key] = model
	}
	var pins []string
	for _, role := range textRoles {
		pins = append(pins, string(role)+":"+model)
	}
	rows[config.KeyModelRoles] = strings.Join(pins, ",")
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func lifecycleDaemonCleanup(t *testing.T, home, archive string) {
	t.Helper()
	// Register before engine/window cleanup so those writers finish before the
	// private storage process flushes and releases this fixture's store handles.
	t.Cleanup(func() {
		socket := filepath.Join(home, "v3", "engine-"+strings.TrimPrefix(filepath.Base(archive), "furrow-")+".sock")
		if len(socket) >= 100 {
			sum := sha256.Sum256([]byte(home + "\x00" + archive))
			base := os.TempDir()
			name := fmt.Sprintf("codeaf-e-%d-%x", os.Getuid(), sum[:12])
			if len(filepath.Join(base, name, "e.sock")) >= 100 {
				base = "/tmp"
			}
			socket = filepath.Join(base, name, "e.sock")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var pids []int
		entries, _ := os.ReadDir("/proc")
		for _, entry := range entries {
			pid, err := strconv.Atoi(entry.Name())
			if err != nil {
				continue
			}
			raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
			if err == nil && strings.HasPrefix(string(raw), archive+"\x00serve\x00--socket\x00"+socket+"\x00") {
				pids = append(pids, pid)
			}
		}
		if err := (cellstore.Daemon{Socket: socket, Binary: archive}).Stop(ctx); err != nil {
			t.Errorf("fixture storage daemon cleanup: %v", err)
		}
		for _, pid := range pids {
			for ctx.Err() == nil && !terminalProcessExited(pid) {
				time.Sleep(20 * time.Millisecond)
			}
			if !terminalProcessExited(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				t.Errorf("fixture storage daemon did not finish graceful shutdown")
			}
		}
	})
}
