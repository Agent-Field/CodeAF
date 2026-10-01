//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestResumeBriefAfterTakeover is the live proof of the resume brief: a chat
// that installed its packages and started a server on one machine is continued
// on a second machine, where the install folder is NOT among the files that
// arrived, the takeover raises the setup card, and `set up` brings the folder
// and the server back.
//
// TWO STATE ROOTS AND ONE RELAY OF ITS OWN, on a port the OS hands out, so no
// other run's relay is ever touched. Every model role rides one model.
func TestResumeBriefAfterTakeover(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	overrides := map[string]any{"model.talk": model, "tools.approvalMode": "allow", "daily_budget_usd": 0,
		"models.crew.allowed": model, "model_pool": "off"}
	homeA, homeB := newHome(t, overrides), newHome(t, overrides)
	t.Cleanup(func() { logInventories(t, homeA, homeB) })
	relay := startOwnRelay(t)
	shareIdentity(t, homeA, homeB)

	port := freePort(t)
	ws := jsWorkspace(t)
	t.Cleanup(func() { logServerLog(t, ws) })
	cache := warmNpmCache(t, ws)
	sync := []string{"CODEAF_CELLS=1", "CODEAF_SYNC_URL=" + relay, "CODEAF_SYNC_INTERVAL_MS=500"}

	a := startWithEnv(t, append([]string{config_key(t)}, append(sync, "CODEAF_TASK_BELT=node")...), "resume_a", homeA, ws, 160, 45, "chat", "--model", model, "--one-model", "--no-host")
	a.skipSetup(t)
	a.lit(fmt.Sprintf("Do exactly this with bash and nothing else. 1) run `npm ci --offline --cache %s`. 2) start the server in the background with `nohup node server.js %d >server.log 2>&1 &`. 3) wait 2 seconds. Then answer with the sum of 19273 and 28114, as digits only.", cache, port))
	a.keys("Enter")
	// The reply is a sum that is on screen nowhere until the model says it. A word
	// quoted in the prompt would be found while the call is still running, and so
	// would one the title writer copies into the chat's name; quitting then ends
	// the server and the seal before either could be recorded.
	a.waitFor(4*time.Minute, "47387")
	waitUp(t, port)
	a.quit()
	stopServer(t, port)

	b := startWithEnv(t, append([]string{config_key(t)}, sync...), "resume_b", homeB, newWorkspace(t, "elsewhere", false), 160, 45, "chat", "--model", model, "--one-model", "--no-host")
	b.skipSetup(t)
	b.keys("Space", "Space")
	b.waitFor(60*time.Second, keyedWord("1", "chat"))
	time.Sleep(8 * time.Second)
	t.Logf("B home:\n%s", b.capture())
	b.keys("Up")
	t.Logf("after up:\n%s", b.capture())
	b.keys("Enter")
	b.waitFor(30*time.Second, "Continue this chat here?")
	// Answer by the option's own key, then enter. Under home the up arrow is home's and
	// disarms the row, so walking the pointer would close the card.
	b.keys("1", "Enter")
	card := b.waitFor(90*time.Second, "Set this machine up like")
	t.Logf("the card:\n%s", card)
	if !strings.Contains(card, "was running there") {
		t.Errorf("the card does not say the server was running there:\n%s", card)
	}
	tree := takenTree(t, homeB)
	if _, err := os.Stat(filepath.Join(tree, "node_modules")); err == nil {
		t.Fatalf("node_modules travelled to %s", tree)
	}
	b.keys("1", "Enter")
	waitUp(t, port)
	b.waitForSetupTurn(t)
	if _, err := os.Stat(filepath.Join(tree, "node_modules")); err != nil {
		t.Fatalf("set up did not rebuild node_modules: %v", err)
	}
	t.Logf("E2E resume: model=%s card=shown node_modules=absent-then-rebuilt server=back port=%d", model, port)
	b.quit()
	stopServer(t, port)
}

// waitForSetupTurn waits until the agent has finished the setup turn: the turn's
// summary line, which reads "worked" once it is over and "working" while it runs.
// Quitting before then cancels the call in flight and ends the server it started.
func (r *rig) waitForSetupTurn(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		screen := r.capture()
		if at := strings.Index(screen, "› set up this machine"); at >= 0 && strings.Contains(screen[at:], "worked ") {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("the setup turn never finished")
}

func config_key(t *testing.T) string { return "OPENROUTER_API_KEY=" + liveKey(t) }

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startOwnRelay(t *testing.T) string {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	state := t.TempDir()
	cmd := guardedCommand(t, context.Background(), state, append(os.Environ(), "CODEAF_HOME="+state), binary(t), "relay", "--listen", addr, "--store", t.TempDir())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	url := "http://" + addr
	for i := 0; i < 50; i++ {
		if resp, err := http.Get(url + "/status"); err == nil {
			resp.Body.Close()
			return url
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("the relay never answered")
	return ""
}

func shareIdentity(t *testing.T, from, to string) {
	t.Helper()
	blob := filepath.Join(t.TempDir(), "id.blob")
	run := func(home, phrase string, args ...string) {
		cmd := guardedCommand(t, context.Background(), home, append(os.Environ(), "CODEAF_HOME="+home, "CODEAF_CELLS=1"), binary(t), append([]string{"identity"}, args...)...)
		cmd.Stdin = strings.NewReader(phrase + "\n")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("identity %v: %v\n%s", args, err, out)
		}
	}
	run(from, "resume-e2e", "export", blob)
	run(to, "resume-e2e", "import", blob)
}

func jsWorkspace(t *testing.T) string {
	t.Helper()
	ws := workspaceAt(t, filepath.Join(t.TempDir(), "resume-probe"), false)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(ws, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"name":"resume-probe","version":"1.0.0","dependencies":{"is-odd":"3.0.1"}}`)
	write("server.js", "const odd=require('is-odd');require('http').createServer((q,s)=>s.end(String(odd(3)))).listen(+process.argv[2]);\n")
	write(".gitignore", "node_modules\nserver.log\n")
	lock := exec.Command("npm", "install", "--package-lock-only", "--silent")
	lock.Dir = ws
	if out, err := lock.CombinedOutput(); err != nil {
		t.Fatalf("npm lock: %v\n%s", err, out)
	}
	return ws
}

// serverPids are the processes running this run's server, found by its own
// command line. The chat's shell may sit in a network space the test cannot
// reach, so the process is the fact, and not an answer over a socket.
func serverPids(port int) []int {
	want := fmt.Sprintf("server.js\x00%d", port)
	var pids []int
	dirs, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, path := range dirs {
		if raw, err := os.ReadFile(path); err == nil && strings.Contains(string(raw), want) {
			var pid int
			fmt.Sscanf(strings.TrimPrefix(path, "/proc/"), "%d", &pid)
			pids = append(pids, pid)
		}
	}
	return pids
}

func waitUp(t *testing.T, port int) {
	t.Helper()
	for i := 0; i < 120; i++ {
		if len(serverPids(port)) > 0 {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("no server process for port %d", port)
}

// stopServer ends the server this run started.
func stopServer(t *testing.T, port int) {
	t.Helper()
	for _, pid := range serverPids(port) {
		_ = exec.Command("kill", fmt.Sprint(pid)).Run()
	}
	time.Sleep(500 * time.Millisecond)
}

// takenTree is the folder the second machine holds the chat's files in.
func takenTree(t *testing.T, home string) string {
	t.Helper()
	found, _ := filepath.Glob(filepath.Join(home, "v3", "projects", "*", "*", "work"))
	if len(found) == 0 {
		t.Fatalf("no taken tree under %s", home)
	}
	return found[0]
}

// warmNpmCache installs the lockfile once from outside the chat, into a cache
// folder of its own, because the chat's shell may have no network and no user
// cache. The chat then installs from that folder, offline.
func warmNpmCache(t *testing.T, ws string) string {
	t.Helper()
	cache, scratch := t.TempDir(), t.TempDir()
	for _, name := range []string{"package.json", "package-lock.json"} {
		raw, err := os.ReadFile(filepath.Join(ws, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scratch, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	warm := exec.Command("npm", "ci", "--cache", cache)
	warm.Dir = scratch
	if out, err := warm.CombinedOutput(); err != nil {
		t.Fatalf("npm cache warm: %v\n%s", err, out)
	}
	return cache
}

// logInventories prints what each machine recorded of the chat's environment,
// when the run failed: the record is the fact the card is built from, and a
// failure that shows no card line cannot be read without it.
func logInventories(t *testing.T, homes ...string) {
	if !t.Failed() {
		return
	}
	for _, home := range homes {
		files, _ := filepath.Glob(filepath.Join(home, "v3", "projects", "*", "*", ".cell", "env", "inventory.json"))
		for _, f := range files {
			raw, _ := os.ReadFile(f)
			t.Logf("INVENTORY %s\n%s", f, raw)
		}
	}
}

// logServerLog prints what the chat's server wrote, when the run failed: a server
// that died at once says why there.
func logServerLog(t *testing.T, ws string) {
	if !t.Failed() {
		return
	}
	raw, err := os.ReadFile(filepath.Join(ws, "server.log"))
	t.Logf("server.log (%v): %q", err, raw)
}
