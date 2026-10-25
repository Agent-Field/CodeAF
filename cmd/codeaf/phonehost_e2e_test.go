package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"golang.org/x/crypto/nacl/box"
)

// A real app process and a real relay. Pairing goes through `codeaf phone pair`.
// The ask is opened by testing.open_ask, with no model call. The answer is a
// queued call, and the queue is empty once the app has acknowledged it.
func TestRealAppHostsThePhoneRoundTrip(t *testing.T) {
	bin := buildCodeafStamped(t, "")
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	relayURL, relayProc := startPhoneRelay(t, bin, filepath.Join(root, "relay"))
	t.Cleanup(func() { killProc(relayProc) })

	app, logs := startPhoneApp(t, bin, homeDir, relayURL)
	t.Cleanup(func() { killProc(app.cmd) })
	base := waitText(t, app.ready, logs, app.cmd, "PHONE_READY ")
	ask := callOpenAsk(t, app, logs)
	if ask.Title != "Apply the selected change?" || ask.Option != "allow_once" || ask.Ask == "" || ask.Agent == "" {
		t.Fatalf("fixture ask: %+v", ask)
	}

	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(map[string]any{"signing_key": seed.Public(), "box_key": pub[:]})
	grant := phonePairCLI(t, bin, homeDir, public)
	if len(grant.Identity) != ed25519.PublicKeySize || len(grant.BoxKey) != 32 || grant.Writer == "" || grant.Relay == "" {
		t.Fatalf("grant: %+v", grant)
	}
	if grant.Cert.Device != hex.EncodeToString(seed.Public()) {
		t.Fatalf("grant cert is for %s", grant.Cert.Device)
	}
	clientDev, err := seed.Join(grant.Cert)
	if err != nil {
		t.Fatal(err)
	}
	client := scopedSigner{grant.Identity, clientDev}

	var plain []byte
	slotPath := "/v1/clients/" + clientDev.ID() + "/slots/" + clientrun.SlotName("asks.list", grant.Writer)
	deadline := time.Now().Add(30 * time.Second)
	for {
		postStep(t, base)
		body, status := signedGET(t, relayURL, client, slotPath)
		if status == 200 {
			var slot struct{ Sealed []byte }
			if err := json.Unmarshal(body, &slot); err != nil {
				t.Fatal(err)
			}
			opened, ok := box.OpenAnonymous(nil, slot.Sealed, pub, priv)
			if !ok {
				t.Fatal("client cannot open slot")
			}
			plain = opened
			var envelopes []struct {
				Agent  string
				Result appwire.AsksResult
			}
			if err := json.Unmarshal(plain, &envelopes); err != nil {
				t.Fatal(err)
			}
			if len(envelopes) == 1 && envelopes[0].Agent == ask.Agent && len(envelopes[0].Result.Asks) == 1 &&
				envelopes[0].Result.Asks[0].Title == "Apply the selected change?" &&
				envelopes[0].Result.Asks[0].Irreversible &&
				len(envelopes[0].Result.Asks[0].Options) > 0 &&
				envelopes[0].Result.Asks[0].Options[0].ID == "allow_once" &&
				envelopes[0].Result.Asks[0].Options[0].Label == "Allow once" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot never carried the ask (%d): %s\n%s", status, plain, logs.String())
		}
		time.Sleep(200 * time.Millisecond)
	}

	call, _ := json.Marshal(clientrun.Call{
		Method: "asks.answer",
		Params: json.RawMessage(`{"agent":"` + ask.Agent + `","id":"` + ask.Ask + `","option":"allow_once"}`),
		FaceID: true,
	})
	sealed, err := box.SealAnonymous(nil, call, (*[32]byte)(grant.BoxKey), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientWire(t, relayURL, client, "POST", "/v1/clients/"+clientDev.ID()+"/queue", map[string]any{
		"number": 1, "at": time.Now().UnixMilli(), "method": "asks.answer", "target": grant.Writer, "sealed": sealed,
	}, nil)
	var stepped struct {
		Entries int `json:"entries"`
	}
	deadline = time.Now().Add(20 * time.Second)
	for {
		stepped = postStep(t, base)
		if stepped.Entries == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue still has %d entries\n%s", stepped.Entries, logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	body, status := signedGET(t, relayURL, client, slotPath)
	if status != 200 {
		t.Fatalf("updated slot %d %s", status, body)
	}
	var slot struct{ Sealed []byte }
	if err := json.Unmarshal(body, &slot); err != nil {
		t.Fatal(err)
	}
	plain, ok := box.OpenAnonymous(nil, slot.Sealed, pub, priv)
	if !ok {
		t.Fatal("cannot open updated slot")
	}
	var left []struct {
		Result appwire.AsksResult
	}
	if err := json.Unmarshal(plain, &left); err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || len(left[0].Result.Asks) != 0 {
		t.Fatalf("answered ask remains in slot: %s", plain)
	}
}

type phoneApp struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  <-chan []byte
	ready  <-chan string
	logs   *bytes.Buffer
	logsMu *sync.Mutex
}

func startPhoneRelay(t *testing.T, bin, store string) (string, *exec.Cmd) {
	t.Helper()
	cmd := exec.Command(bin, "relay", "--listen", "127.0.0.1:0", "--store", store, "--quiet")
	cmd.Env = phoneChildEnv("", "CODEAF_TELEMETRY=off")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		sent := false
		for sc.Scan() {
			line := sc.Text()
			if sent {
				continue
			}
			if _, addr, ok := strings.Cut(line, "relay on "); ok {
				addr, _, _ = strings.Cut(strings.TrimSpace(addr), ",")
				found <- "http://" + addr
				sent = true
			}
		}
		if !sent {
			found <- ""
		}
	}()
	select {
	case got := <-found:
		if got == "" || got == "http://" {
			killProc(cmd)
			t.Fatal("relay did not say its address")
		}
		return got, cmd
	case <-time.After(20 * time.Second):
		killProc(cmd)
		t.Fatal("relay did not become ready")
	}
	return "", nil
}

func startPhoneApp(t *testing.T, bin, homeDir, relay string, extra ...string) (*phoneApp, *lockedLog) {
	t.Helper()
	cmd := exec.Command(bin, "app", "--home", homeDir)
	env := []string{
		"CODEAF_SYNC_URL=" + relay,
		"CODEAF_PHONE_FIXTURE=1",
		"CODEAF_NO_AUTOMATIONS=1",
		"CODEAF_TELEMETRY=off",
		"CODEAF_PROFILE_DIR=" + homeDir,
		"OPENROUTER_API_KEY=",
		"OPENAI_API_KEY=",
	}
	cmd.Env = phoneChildEnv(homeDir, append(env, extra...)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	logs := &lockedLog{}
	ready := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			logs.add(line)
			if addr, ok := strings.CutPrefix(line, "PHONE_READY "); ok {
				select {
				case ready <- strings.TrimSpace(addr):
				default:
				}
			}
		}
	}()
	lines := make(chan []byte, 256)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
		for sc.Scan() {
			lines <- append([]byte(nil), sc.Bytes()...)
		}
		close(lines)
	}()
	return &phoneApp{cmd: cmd, stdin: stdin, lines: lines, ready: ready}, logs
}

type lockedLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedLog) add(line string) {
	l.mu.Lock()
	l.b.WriteString(line)
	l.b.WriteByte('\n')
	l.mu.Unlock()
}

func (l *lockedLog) String() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func phoneChildEnv(homeDir string, extra ...string) []string {
	over := map[string]string{}
	if homeDir != "" {
		over["CODEAF_HOME"] = homeDir
		over["CODEAF_PROFILE_DIR"] = homeDir
	}
	for _, pair := range extra {
		k, v, _ := strings.Cut(pair, "=")
		over[k] = v
	}
	var out []string
	seen := map[string]bool{}
	for _, pair := range os.Environ() {
		k, _, _ := strings.Cut(pair, "=")
		if v, ok := over[k]; ok {
			if !seen[k] {
				out = append(out, k+"="+v)
				seen[k] = true
			}
			continue
		}
		out = append(out, pair)
	}
	for k, v := range over {
		if !seen[k] {
			out = append(out, k+"="+v)
		}
	}
	return out
}

func waitText(t *testing.T, ready <-chan string, logs *lockedLog, cmd *exec.Cmd, prefix string) string {
	t.Helper()
	select {
	case got := <-ready:
		if got == "" {
			t.Fatalf("%s missing\n%s", prefix, logs.String())
		}
		return got
	case <-time.After(30 * time.Second):
		killProc(cmd)
		t.Fatalf("timed out waiting for %s\n%s", prefix, logs.String())
	}
	return ""
}

func callOpenAsk(t *testing.T, app *phoneApp, logs *lockedLog) appwire.PhoneFixtureAsk {
	t.Helper()
	if _, err := io.WriteString(app.stdin, "{\"id\":1,\"method\":\"testing.open_ask\",\"params\":{}}\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Minute)
	for {
		select {
		case line, ok := <-app.lines:
			if !ok {
				t.Fatalf("app exited before the ask opened\n%s", logs.String())
			}
			var frame struct {
				ID     any             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *appwire.Error  `json:"error"`
				Event  string          `json:"event"`
			}
			if err := json.Unmarshal(line, &frame); err != nil || frame.Event != "" {
				continue
			}
			if frame.ID == nil {
				continue
			}
			if frame.Error != nil {
				t.Fatalf("testing.open_ask: %s: %s\n%s", frame.Error.Code, frame.Error.Message, logs.String())
			}
			var ask appwire.PhoneFixtureAsk
			if err := json.Unmarshal(frame.Result, &ask); err != nil {
				t.Fatal(err)
			}
			go func() {
				for range app.lines {
				}
			}()
			return ask
		case <-deadline:
			t.Fatalf("testing.open_ask timed out\n%s", logs.String())
		}
	}
}

func phonePairCLI(t *testing.T, bin, homeDir string, public []byte) appwire.ClientGrant {
	t.Helper()
	cmd := exec.Command(bin, "phone", "pair")
	cmd.Env = phoneChildEnv(homeDir, "CODEAF_TELEMETRY=off")
	cmd.Stdin = bytes.NewReader(public)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("phone pair: %v\n%s", err, stderr.String())
	}
	var grant appwire.ClientGrant
	if err := json.Unmarshal(stdout.Bytes(), &grant); err != nil {
		t.Fatalf("grant %s: %v", stdout.String(), err)
	}
	return grant
}

func signedGET(t *testing.T, base string, s reqsign.Signer, path string) ([]byte, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(req, nil, s, time.Now())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body, resp.StatusCode
}

func postStep(t *testing.T, base string) (out struct {
	Entries int `json:"entries"`
}) {
	t.Helper()
	resp, err := http.Post(base+"/step", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("step %d %s", resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func killProc(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}
