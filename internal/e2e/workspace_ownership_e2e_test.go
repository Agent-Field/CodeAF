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
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

func TestWorkspaceOwnershipPlainProjectOnRealTerminal(t *testing.T) {
	workspaceOwnershipTerminal(t, false, false)
}

func TestWorkspaceOwnershipLivePlainProject(t *testing.T) {
	workspaceOwnershipTerminal(t, true, false)
}

func TestWorkspaceOwnershipLiveConcurrentChats(t *testing.T) {
	workspaceOwnershipTerminal(t, true, true)
}

func TestWorkspaceOwnershipConcurrentChatsOnRealTerminal(t *testing.T) {
	workspaceOwnershipTerminal(t, false, true)
}

// The original is an ordinary folder, with a prepared brief that must travel
// into the owned copy. Actual worker writes and independent source comparison
// verify placement; an assistant's claim about where it worked is insufficient.
func workspaceOwnershipTerminal(t *testing.T, live, concurrent bool) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux required")
	}
	model, key, base := "stub/workspace-ownership", "fixture-workspace-key", ""
	if live {
		requireTmuxAndKey(t)
		model = "deepseek/deepseek-v4.1-flash"
		if e2eModel != model {
			t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
		}
		key, base = liveKey(t), "https://openrouter.ai/api/v1"
	}
	home := lifecycleHome(t, model)
	release := filepath.Join(home, "ownership-release")
	project := filepath.Join(shortRoot(t), "plain-project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	const brief = "Fictional packaging handoff. Keep the exact marker MOCK_COPPER_HERON.\n"
	for name, body := range map[string]string{"BRIEF.md": brief, "input.txt": "mock source remains unchanged\n"} {
		if err := os.WriteFile(filepath.Join(project, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !live {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/models":
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": model, "context_length": 131072, "pricing": map[string]string{"prompt": "0.000001", "completion": "0.000001"}, "supported_parameters": []string{"tools", "tool_choice"}}}})
			case "/key", "/auth/key", "/credits":
				fmt.Fprint(w, `{"data":{"limit_remaining":100,"total_credits":100,"total_usage":0}}`)
			case "/chat/completions":
				var body struct {
					Stream   bool `json:"stream"`
					Messages []struct {
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "fixture decode", 400)
					return
				}
				for _, msg := range body.Messages {
					if assignment := freeConsentAssignment.FindStringSubmatch(string(msg.Content)); len(assignment) == 3 {
						command := "grep -q MOCK_COPPER_HERON BRIEF.md && printf 'MOCK_COPPER_HERON\\n' > ownership-proof.txt && pwd > ownership-pwd.txt && printf '%s\\n' \"$CODEAF_WORKSPACE_URI\" \"$TMPDIR\" \"$XDG_CACHE_HOME\" > ownership-runtime.txt && mktemp > ownership-temp.txt && printf mock-temp > \"$(cat ownership-temp.txt)\" && printf mock-cache > \"$XDG_CACHE_HOME/ownership-cache\" && mkdir -p " + cell.TaskDropping + " && printf mock-scratch > " + cell.TaskDropping + "/ownership-scratch && printf ready > " + cell.TaskDropping + "/ownership-ready && for ownership_wait in $(seq 1 90); do [ -f " + shellQuote(release) + " ] && break; sleep 1; done && test -f " + shellQuote(release) + " && plandb done " + assignment[1] + " --agent " + assignment[2] + " --result 'mock prepared brief verified and owned artifact written'"
						args, _ := json.Marshal(map[string]string{"command": command})
						freeConsentResponse(w, model, "", map[string]any{"index": 0, "id": "ownership-" + assignment[2], "type": "function", "function": map[string]string{"name": "bash", "arguments": string(args)}}, body.Stream)
						return
					}
				}
				freeConsentResponse(w, model, "Acknowledged.", nil, body.Stream)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()
		base = server.URL
	}
	env := []string{config.APIKeyEnv + "=" + key, "CODEAF_BASE_URL=" + base, "CODEAF_PROFILE_DIR=", "CODEAF_SYNC_URL=off", "CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP=1"}
	// Resolve the unpacked binary at cleanup, after the owned engine stops.
	t.Cleanup(func() {
		binaries, _ := filepath.Glob(filepath.Join(home, "bin", "furrow-*"))
		for _, binary := range binaries {
			lifecycleDaemonCleanup(t, home, binary)
		}
	})
	stopRecoveryEngineOnCleanup(t, env, home, project)
	rig := startWithEnv(t, env, "workspace_ownership", home, project, 180, 48, "chat", "--model", model, "--one-model", "--yolo")
	rig.skipSetup(t)
	baseline := lifecycleContentManifest(t, project)
	prompt := "/task solo In this fictional project, inspect the prepared BRIEF.md in your working directory. Verify that it contains MOCK_COPPER_HERON. Write ownership-proof.txt containing exactly MOCK_COPPER_HERON followed by a newline, and ownership-pwd.txt containing the output of pwd. Also write ownership-runtime.txt with CODEAF_WORKSPACE_URI, TMPDIR and XDG_CACHE_HOME environment values, one per line. Use mktemp, record its path in ownership-temp.txt and write exactly mock-temp without a trailing newline into that temporary file. Write exactly mock-cache without a trailing newline to a file ownership-cache inside XDG_CACHE_HOME. Check the proof with grep. Once all these writes and checks are complete, create .codeaf/ownership-ready containing ready. Before finishing, wait up to 90 seconds for the file " + release + " to exist; do not create it yourself. While waiting, leave your shell in the foreground. Then finish normally with plandb done using your actual assigned id and claimed agent. Do not delegate further or modify BRIEF.md or input.txt."
	rig.lit(prompt)
	rig.keys("Enter")
	deadline := time.Now().Add(90 * time.Second)
	if live {
		deadline = time.Now().Add(5 * time.Minute)
	}
	var proof string
	for time.Now().Before(deadline) {
		_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "ownership-proof.txt" {
				if raw, e := os.ReadFile(path); e == nil && string(raw) == "MOCK_COPPER_HERON\n" && fileExists(filepath.Join(filepath.Dir(path), cell.TaskDropping, "ownership-ready")) {
					proof = path
					return filepath.SkipAll
				}
			}
			return nil
		})
		if proof != "" {
			break
		}
		time.Sleep(pollEvery)
	}
	if proof == "" {
		t.Fatalf("no actual proof under owned profile; original_written=%t; screen=%s", fileExists(filepath.Join(project, "ownership-proof.txt")), rig.capture())
	}
	root := filepath.Dir(proof)
	pwd, err := os.ReadFile(filepath.Join(root, "ownership-pwd.txt"))
	if err != nil || filepath.Clean(strings.TrimSpace(string(pwd))) != root {
		t.Fatalf("worker pwd does not match owned proof: %v", err)
	}
	runtimeRaw, err := os.ReadFile(filepath.Join(root, "ownership-runtime.txt"))
	if err != nil {
		t.Fatal("worker did not record its runtime environment")
	}
	runtimeLines := strings.Split(strings.TrimSpace(string(runtimeRaw)), "\n")
	if len(runtimeLines) != 3 || runtimeLines[0] == "" {
		t.Fatalf("incomplete worker runtime: %q", runtimeRaw)
	}
	registry, err := workspaceidentity.Open(filepath.Join(home, "workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	run, err := registry.Resolve(runtimeLines[0])
	if err != nil || run.Role != workspaceidentity.Run || run.Path != root || run.OwnerURI == "" || run.ProjectURI == "" {
		t.Fatalf("worker identity does not resolve to its admitted copy: %+v %v", run, err)
	}
	chat, err := registry.Resolve(run.OwnerURI)
	if err != nil || chat.Role != workspaceidentity.Chat {
		t.Fatal("run owner is not its registered conversation")
	}
	projectRecord, err := registry.Resolve(run.ProjectURI)
	if err != nil || projectRecord.Role != workspaceidentity.Project || projectRecord.Path != project {
		t.Fatal("run project identity does not resolve to the original project")
	}
	for _, path := range runtimeLines[1:] {
		relative, e := filepath.Rel(home, path)
		if e != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) || path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			t.Fatalf("runtime scratch/cache is not outside the deliverable copy under owned profile: %s", path)
		}
	}
	tempRaw, err := os.ReadFile(filepath.Join(root, "ownership-temp.txt"))
	tempPath := strings.TrimSpace(string(tempRaw))
	if err != nil || filepath.Dir(tempPath) != runtimeLines[1] {
		t.Fatal("actual mktemp file escaped assigned temporary directory")
	}
	if raw, err := os.ReadFile(tempPath); err != nil || string(raw) != "mock-temp" {
		t.Fatal("worker temporary file was not written at the recorded path")
	}
	if raw, err := os.ReadFile(filepath.Join(runtimeLines[2], "ownership-cache")); err != nil || string(raw) != "mock-cache" {
		t.Fatal("worker cache was not written under assigned cache directory")
	}
	reopened, err := workspaceidentity.Open(filepath.Join(home, "workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := reopened.Resolve(run.URI)
	if err != nil || resolved != run {
		t.Fatal("registry reopen changed stable workspace identity")
	}
	if raw, err := os.ReadFile(filepath.Join(root, "BRIEF.md")); err != nil || string(raw) != brief {
		t.Fatal("prepared brief did not survive into owned workspace")
	}
	actual := lifecycleContentManifest(t, project)
	for _, name := range []string{".furrow/family-id", ".furrow/fork-id", ".furrow/workspace-id"} {
		if _, existed := baseline[name]; existed {
			continue
		}
		if _, exists := actual[name]; !exists {
			continue
		}
		path := filepath.Join(project, filepath.FromSlash(name))
		info, err := os.Lstat(path)
		parent, pe := os.Lstat(filepath.Dir(path))
		raw, re := os.ReadFile(path)
		if err != nil || pe != nil || !parent.IsDir() || !info.Mode().IsRegular() || re != nil || len(raw) != 33 || raw[32] != '\n' {
			t.Fatalf("invalid generated native ownership marker %s", name)
		}
		for _, b := range raw[:32] {
			if !(b >= '0' && b <= '9' || b >= 'a' && b <= 'f') {
				t.Fatalf("invalid generated native ownership ID %s", name)
			}
		}
		delete(actual, name)
	}
	lifecycleRequireManifest(t, "original while task held", baseline, actual)
	for name, want := range map[string]string{"BRIEF.md": brief, "input.txt": "mock source remains unchanged\n"} {
		raw, err := os.ReadFile(filepath.Join(project, name))
		if err != nil || string(raw) != want {
			t.Fatalf("original %s changed", name)
		}
	}
	if concurrent {
		rig.lit("/new")
		rig.keys("Enter")
		time.Sleep(time.Second)
		rig.lit(prompt)
		rig.keys("Enter")
		var secondRoot string
		for until := time.Now().Add(45 * time.Second); secondRoot == ""; {
			_ = filepath.WalkDir(home, func(path string, d os.DirEntry, e error) error {
				if e != nil || d.IsDir() || d.Name() != "ownership-runtime.txt" || filepath.Dir(path) == root {
					return nil
				}
				if !fileExists(filepath.Join(filepath.Dir(path), cell.TaskDropping, "ownership-ready")) {
					return nil
				}
				raw, e := os.ReadFile(path)
				if e != nil {
					return nil
				}
				rows := strings.Split(strings.TrimSpace(string(raw)), "\n")
				if len(rows) != 3 {
					return nil
				}
				second, e := registry.Resolve(rows[0])
				if e != nil || second.URI == run.URI || second.Path == root || second.OwnerURI == run.OwnerURI || second.ProjectURI != run.ProjectURI {
					t.Fatalf("second concurrent chat did not receive distinct owned run/chat linked to same project: %+v %v", second, e)
				}
				if second.Role != workspaceidentity.Run || second.Path != filepath.Dir(path) {
					t.Fatal("second worker identity and actual output directory disagree")
				}
				secondChat, e := registry.Resolve(second.OwnerURI)
				if e != nil || secondChat.Role != workspaceidentity.Chat {
					t.Fatal("second run chat owner invalid")
				}
				secondRoot = second.Path
				return filepath.SkipAll
			})
			if secondRoot != "" {
				break
			}
			if time.Now().After(until) {
				t.Fatalf("second concurrent chat never received its own working copy: %s", rig.capture())
			}
			time.Sleep(pollEvery)
		}
		for _, path := range []string{root, secondRoot} {
			if raw, e := os.ReadFile(filepath.Join(path, "ownership-proof.txt")); e != nil || string(raw) != "MOCK_COPPER_HERON\n" {
				t.Fatal("concurrent task proof missing")
			}
		}
		if fileExists(filepath.Join(project, "ownership-proof.txt")) {
			t.Fatal("concurrent worker output reached project before release")
		}
		t.Log("PASS two concurrently held chats have different registered run and chat identities, same project identity, and independent actual working copies")
	}
	if err := os.WriteFile(release, []byte("release\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for until := time.Now().Add(90 * time.Second); !freeConsentDoneNote(home); {
		if time.Now().After(until) {
			t.Fatalf("owned task did not settle after release: %s", rig.capture())
		}
		time.Sleep(pollEvery)
	}
	for until := time.Now().Add(20 * time.Second); !fileExists(filepath.Join(project, "ownership-proof.txt")); {
		if time.Now().After(until) {
			break
		}
		time.Sleep(pollEvery)
	}
	if fileExists(filepath.Join(project, cell.TaskDropping, "ownership-scratch")) {
		t.Fatal("private worker scratch landed in original project")
	}
	for _, name := range []string{"ownership-proof.txt", "ownership-pwd.txt", "ownership-runtime.txt", "ownership-temp.txt"} {
		if !fileExists(filepath.Join(project, name)) {
			t.Fatalf("named deliverable %s did not land", name)
		}
	}
	if concurrent {
		for until := time.Now().Add(30 * time.Second); ownershipFinishedChats(home) < 2; {
			if time.Now().After(until) {
				t.Fatal("both concurrent chats must settle before engine restart")
			}
			time.Sleep(pollEvery)
		}
		oldPID := upgradePID(upgradeEngineCommand(t, binary(t), env, home, project, "--status"))
		if oldPID == "" {
			t.Fatal("no owned engine before idle restart")
		}
		rig.kill()
		upgradeEngineCommand(t, binary(t), env, home, project, "--stop")
		rig = startWithEnv(t, env, "workspace_ownership_reopen", home, project, 180, 48, "chat", "--model", model, "--one-model", "--yolo")
		rig.skipSetup(t)
		newPID := upgradePID(upgradeEngineCommand(t, binary(t), env, home, project, "--status"))
		if newPID == "" || newPID == oldPID {
			t.Fatal("owned engine restart did not create new process")
		}
		reopenedRegistry, e := workspaceidentity.Open(filepath.Join(home, "workspace-identities"))
		if e != nil {
			t.Fatal(e)
		}
		retained, e := reopenedRegistry.Resolve(run.URI)
		if e != nil || retained != run {
			t.Fatal("actual engine restart changed previously issued workspace identity")
		}
		if raw, e := os.ReadFile(filepath.Join(retained.Path, "ownership-proof.txt")); e != nil || string(raw) != "MOCK_COPPER_HERON\n" {
			t.Fatal("prior workspace artifact unavailable after actual engine restart")
		}
		t.Log("PASS actual owned engine restarted after delivery; prior run URI still resolves to retained exact artifact; no task resume claim")
	}
	if live {
		models := callModels(t, home)
		if len(models) == 0 {
			t.Fatal("no recorded live model calls")
		}
		for _, got := range models {
			if got != model {
				t.Fatalf("unexpected recorded model %q", got)
			}
		}
		t.Logf("exact live model verified on %d call receipts", len(models))
		usage, err := os.ReadFile(filepath.Join(home, "v3", "usage.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		var totals struct {
			Input  int     `json:"input_tokens"`
			Output int     `json:"output_tokens"`
			USD    float64 `json:"recorded_usd"`
		}
		for _, line := range strings.Split(strings.TrimSpace(string(usage)), "\n") {
			var row struct {
				Input  int     `json:"in"`
				Output int     `json:"out"`
				USD    float64 `json:"usd"`
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				t.Fatal(err)
			}
			totals.Input += row.Input
			totals.Output += row.Output
			totals.USD += row.USD
		}
		summary, err := json.MarshalIndent(map[string]any{"model": model, "call_receipts": len(models), "usage": totals}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("live usage receipts before fixture cleanup: %s", summary)
		if evidence := os.Getenv("CODEAF_E2E_EVIDENCE_DIR"); evidence != "" {
			for name, data := range map[string][]byte{"ownership-live-usage.json": summary, "ownership-live-ui.ansi": []byte(captureStyled(rig))} {
				if err := os.WriteFile(filepath.Join(evidence, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	t.Logf("PASS registered URI resolves to owned copy and survives registry reopen; actual mktemp/cache writes routed outside deliverable copy; prepared brief available in owned worker copy; pwd and actual output agree; original user files byte-identical and startup manifest unchanged while held; named outputs landed after release and private scratch stayed private.\n%s", rig.capture())
}

// Each chat's public completion note must exist before the acceptance restarts
// its engine. A single done note could belong only to the first finished chat.
func ownershipFinishedChats(home string) int {
	count := 0
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "transcript.jsonl" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(string(raw), "\n") {
			var row struct {
				Note bool `json:"note"`
				Tags []struct {
					State string `json:"state"`
				} `json:"replyTags"`
			}
			if json.Unmarshal([]byte(line), &row) != nil || !row.Note {
				continue
			}
			for _, tag := range row.Tags {
				if tag.State == "done" {
					count++
					return nil
				}
			}
		}
		return nil
	})
	return count
}
