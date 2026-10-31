package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/agentdid"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Run make build first. This drives the shipped stdio door in a fresh process,
// including a real shell tool, restart, and migration. Only the model is scripted.
func TestAppAgentIdentityBinaryE2E(t *testing.T) {
	binary, err := filepath.Abs(filepath.Join("..", "..", "bin", "codeaf"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skip("run make build to exercise the actual app binary")
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		var body struct {
			Stream   bool `json:"stream"`
			Tools    []json.RawMessage
			Messages []struct{ Role string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		needsTool := len(body.Tools) > 0
		for i := len(body.Messages) - 1; i >= 0; i-- {
			if body.Messages[i].Role == "tool" {
				needsTool = false
				break
			}
			if body.Messages[i].Role == "user" {
				break
			}
		}
		delta := map[string]any{"content": "Identity QA complete"}
		finish := "stop"
		if needsTool {
			args, _ := json.Marshal(map[string]string{"command": `printf '%s' "$CODEAF_AGENT_DID" > did.txt; env | sed -n '/^CODEAF_AGENT_/p' > identity-env.txt`})
			delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "identity-qa", "type": "function", "function": map[string]any{"name": "bash", "arguments": string(args)}}}}
			finish = "tool_calls"
		}
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			chunk, _ := json.Marshal(map[string]any{"id": "qa", "choices": []any{map[string]any{"index": 0, "delta": delta}}})
			fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"qa\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", chunk, finish)
			return
		}
		delta["role"] = "assistant"
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "message": delta, "finish_reason": finish}}})
	}))
	defer model.Close()
	root := resolvedTempDir(t)
	appHome, workspace := filepath.Join(root, "state"), filepath.Join(root, "project")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	start := func() *identityQAProcess {
		return startIdentityQAProcess(t, binary, root, appHome, workspace, model.URL)
	}
	p := start()
	created := p.call("agents.create", map[string]any{"workspace": workspace, "model": "openai/gpt-4.1", "approval_posture": "allow"})
	var result struct{ Agent struct{ ID string } }
	if err := json.Unmarshal(created, &result); err != nil || result.Agent.ID == "" {
		t.Fatalf("create: %s, %v", created, err)
	}
	id := result.Agent.ID
	run := func(p *identityQAProcess) {
		p.call("agents.send", map[string]any{"id": id, "text": "Run the identity QA shell command"})
		p.wait(func(f identityQAFrame) bool { return f.Event == "turn.done" })
	}
	run(p)
	p.stop()

	var truth string
	if err := filepath.WalkDir(appHome, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "session.json" && filepath.Base(filepath.Dir(filepath.Dir(path))) == id {
			truth = path
		}
		return nil
	}); err != nil || truth == "" {
		t.Fatalf("no sealed session truth: %s %v", truth, err)
	}
	readDoc := func() agentdid.Document {
		raw, err := os.ReadFile(truth)
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			Identity agentdid.Document `json:"agent_identity"`
		}
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatal(err)
		}
		return state.Identity
	}
	doc := readDoc()
	if doc.V != agentdid.Version || doc.Signing == nil || doc.Encryption == nil || len(doc.Custody) == 0 {
		t.Fatalf("incomplete identity after a real turn: %+v", doc)
	}
	// The real bash tool wrote only the public identity into its environment.
	checkShell := func() {
		did, err := os.ReadFile(filepath.Join(workspace, "did.txt"))
		if err != nil || string(did) != doc.DID {
			t.Fatalf("shell DID: %q %v", did, err)
		}
		env, err := os.ReadFile(filepath.Join(workspace, "identity-env.txt"))
		if err != nil || string(env) != agentdid.AgentDIDEnv+"="+doc.DID+"\n" {
			t.Fatalf("shell identity environment: %q %v", env, err)
		}
	}
	checkShell()
	owner, err := identity.Load(appHome)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := doc.Unlock(owner.CellKey())
	if err != nil {
		t.Fatal(err)
	}
	sig, err := keys.Sign(doc.DID, "receipt/v1", []byte("real process"))
	if err != nil || !agentdid.Verify(doc.DID, "receipt/v1", []byte("real process"), sig) {
		t.Fatalf("persisted signing custody: %v", err)
	}
	p = start()
	listed := p.call("agents.list", map[string]any{"state": "all"})
	if !bytes.Contains(listed, []byte(doc.DID)) || bytes.Contains(listed, []byte("custody")) || bytes.Contains(listed, []byte("encryption")) {
		t.Fatalf("agent list must expose only the public DID: %s", listed)
	}
	run(p)
	p.stop()
	checkShell()
	again := readDoc()
	if !bytes.Equal(again.Custody, doc.Custody) || again.DID != doc.DID {
		t.Fatal("restarting replaced the agent's keys")
	}

	// Model an actual #2027 folder, then use the app's open and send doors.
	writeDocument := func(document string) []byte {
		raw, _ := os.ReadFile(truth)
		var state map[string]json.RawMessage
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatal(err)
		}
		state["agent_identity"] = json.RawMessage(document)
		raw, _ = json.Marshal(state)
		if err := os.WriteFile(truth, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	legacy, _ := json.Marshal(map[string]string{"did": doc.DID, "session": id})
	before := writeDocument(string(legacy))
	p = start()
	p.call("agents.open", map[string]any{"id": id})
	after, _ := os.ReadFile(truth)
	if !bytes.Equal(before, after) {
		t.Fatalf("opening a legacy chat rewrote its identity: before %s; after %s", before, after)
	}
	run(p)
	p.stop()
	upgraded := readDoc()
	if upgraded.DID != doc.DID || upgraded.V != agentdid.Version || !upgraded.SigningUnavailable || upgraded.Encryption == nil || len(upgraded.Custody) == 0 {
		t.Fatalf("legacy chat did not lazily upgrade: %+v", upgraded)
	}
	future := fmt.Sprintf(`{"v":"v99", "did":%q, "session":%q, "statements": [ {"future": 1.00} ]}`, doc.DID, id)
	// Preserve the future value's original whitespace inside its enclosing file.
	raw := writeDocument(future)
	var compact json.RawMessage
	var state map[string]json.RawMessage
	_ = json.Unmarshal(raw, &state)
	compact = state["agent_identity"]
	raw = bytes.Replace(raw, compact, []byte(future), 1)
	if err := os.WriteFile(truth, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	p = start()
	p.call("agents.rename", map[string]any{"id": id, "title": "Future identity QA"})
	p.stop()
	raw, _ = os.ReadFile(truth)
	if !bytes.Contains(raw, []byte(future)) {
		t.Fatalf("rename rewrote a future document: %s", raw)
	}
	// Reading the chat through its public session API must preserve that document.
	meta, err := session.LoadMeta(filepath.Dir(filepath.Dir(truth)))
	if err != nil || meta.AgentDID() != doc.DID {
		t.Fatalf("public DID disappeared after future write: %v", err)
	}
	t.Log("actual app binary: create, real bash environment, custody, restart, legacy open/send, future rename passed")
}

type identityQAFrame struct {
	ID     int             `json:"id"`
	Event  string          `json:"event"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type identityQAProcess struct {
	t       *testing.T
	cmd     *exec.Cmd
	in      io.WriteCloser
	frames  chan identityQAFrame
	pending []identityQAFrame
	stderr  bytes.Buffer
	next    int
	stopped bool
}

func startIdentityQAProcess(t *testing.T, binary, root, home, workspace, base string) *identityQAProcess {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	p := &identityQAProcess{t: t, frames: make(chan identityQAFrame, 4096)}
	p.cmd = exec.CommandContext(ctx, binary, "app", "--home", home)
	p.cmd.Dir = workspace
	p.cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(root, "home"), "TMPDIR=" + root, "OPENROUTER_API_KEY=sk-or-v1-identity-qa-dummy", "CODEAF_BASE_URL=" + base, "TERM=dumb"}
	p.cmd.Stderr = &p.stderr
	var err error
	p.in, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !p.stopped {
			_ = p.cmd.Process.Kill()
			_ = p.cmd.Wait()
			t.Logf("app stderr: %s", &p.stderr)
		}
	})
	go func() {
		defer close(p.frames)
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		for scanner.Scan() {
			var frame identityQAFrame
			if json.Unmarshal(scanner.Bytes(), &frame) == nil {
				p.frames <- frame
			}
		}
	}()
	p.wait(func(f identityQAFrame) bool { return f.Event == "hello" })
	return p
}

func (p *identityQAProcess) wait(matches func(identityQAFrame) bool) identityQAFrame {
	p.t.Helper()
	for i, frame := range p.pending {
		if matches(frame) {
			p.pending = append(p.pending[:i], p.pending[i+1:]...)
			return frame
		}
	}
	deadline := time.NewTimer(40 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case frame, ok := <-p.frames:
			if !ok {
				p.t.Fatal("app stdout ended before the expected response")
			}
			if matches(frame) {
				return frame
			}
			p.pending = append(p.pending, frame)
		case <-deadline.C:
			p.t.Fatal("app did not answer within 40 seconds")
		}
	}
}

func (p *identityQAProcess) call(method string, params any) json.RawMessage {
	p.t.Helper()
	p.next++
	if err := json.NewEncoder(p.in).Encode(map[string]any{"id": p.next, "method": method, "params": params}); err != nil {
		p.t.Fatal(err)
	}
	f := p.wait(func(f identityQAFrame) bool { return f.ID == p.next })
	if len(f.Error) != 0 && string(f.Error) != "null" {
		p.t.Fatalf("%s: %s", method, f.Error)
	}
	return f.Result
}

func (p *identityQAProcess) stop() {
	p.t.Helper()
	p.call("shutdown", map[string]any{})
	_ = p.in.Close()
	err := p.cmd.Wait()
	p.stopped = true
	if err != nil {
		p.t.Fatalf("app exit: %v\n%s", err, &p.stderr)
	}
}
