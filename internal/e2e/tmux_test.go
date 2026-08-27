//go:build e2e

// Package e2e drives the real `bin/aforge` binary inside a real terminal.
//
// THE ONLY THING UNDER TEST IS THE PRODUCT AS A PERSON MEETS IT. Every other
// suite in this repository reaches inside — it builds an app struct, hands it a
// scripted agent, and reads rows back out of a model. That is the right shape
// for a unit test and it cannot answer the one question this file exists for:
// does the thing a person launches, on a real terminal, talking to a real
// model, do what the manual says it does. So this file starts tmux, sends the
// bytes a keyboard sends, and reads the screen back with `capture-pane`.
//
// IT SKIPS RATHER THAN FAILS when it cannot be honest: no OPENROUTER_API_KEY,
// no tmux, no built binary. A suite that "passes" by not talking to a model is
// a suite lying about the only thing it was written to check.
//
// EVERY RUN IS ITS OWN MACHINE. Each rig gets its own AFORGE_HOME under
// t.TempDir() — the whole state root moves with that one variable
// (internal/home) — and its own git-initialised workspace. A workspace under
// /tmp that is NOT a repository is treated by the door as "somewhere the person
// stood by accident" and gets an owned session in a private work directory
// (cmd/aforge's chatv3_layout.go), which is not the shape any of these
// scenarios are about, so the workspace is always a repository.
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pollEvery is how often waitFor reads the screen. It is a quarter second
// because the two things this suite has to catch in flight — the spinner's
// `thinking · Ns` and a consent card in another window — live for seconds, not
// for minutes.
const pollEvery = 250 * time.Millisecond

// rig is one running aforge in one tmux session.
type rig struct {
	t    *testing.T
	name string
	home string
	ws   string
	dead bool
}

// requireTmuxAndKey skips the whole suite unless it can be run honestly.
func requireTmuxAndKey(t *testing.T) string {
	t.Helper()
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		t.Skip("no OPENROUTER_API_KEY: this suite talks to a real model or it says nothing")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this suite drives the real binary in a real terminal")
	}
	return key
}

// repoRoot walks up from the test's own directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// binary is the built product. It is NEVER built here: `make build` is the
// door, and a suite that rebuilt would be testing a binary nobody ran.
func binary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), "bin", "aforge")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no built binary at %s — run `make build` first", path)
	}
	return path
}

// newHome makes this test's whole state root: a fresh directory with the
// person's own config.json copied in and the rows this suite needs overridden.
//
// THE FILE IS NEVER PRINTED. It holds an api key and two OAuth secrets; the
// dump on failure walks the tree and deliberately skips it.
func newHome(t *testing.T, overrides map[string]any) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("home: %v", err)
	}
	rows := map[string]any{}
	userHome, err := os.UserHomeDir()
	if err == nil {
		if raw, err := os.ReadFile(filepath.Join(userHome, ".aforge", "config.json")); err == nil {
			if err := json.Unmarshal(raw, &rows); err != nil {
				t.Fatalf("the profile config would not parse: %v", err)
			}
		}
	}
	// The model this suite is about, and the gate posture every scenario but
	// the consent one wants.
	rows["model.talk"] = "deepseek/deepseek-v4-flash"
	if _, ok := rows["tools.approvalMode"]; !ok {
		rows["tools.approvalMode"] = "allow"
	}
	for key, value := range overrides {
		rows[key] = value
	}
	raw, err := json.MarshalIndent(rows, "", " ")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0o600); err != nil {
		t.Fatalf("config: %v", err)
	}
	return home
}

// newWorkspace makes a throwaway project directory and puts a repository in it.
//
// A REPOSITORY IS NOT DECORATION HERE. It is what makes a /tmp directory a
// project at all (chatv3_layout.go's v3NoProjectPlace), and it is what the repo
// band on home reads.
func newWorkspace(t *testing.T, name string, dirty bool) string {
	t.Helper()
	ws := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("workspace: %v", err)
	}
	run := func(args ...string) {
		command := exec.Command("git", args...)
		command.Dir = ws
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	run("add", "README.md")
	run("-c", "user.email=e2e@example.com", "-c", "user.name=e2e", "commit", "-qm", "first")
	if dirty {
		if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("seed\nchanged\n"), 0o644); err != nil {
			t.Fatalf("dirty: %v", err)
		}
	}
	return ws
}

// start launches the binary in its own tmux session at the given size.
//
// THE SIZE IS SET AFTER THE SESSION EXISTS. `new-session -x/-y` is honoured
// only when the server's window-size option agrees; `resize-window` sets it
// outright and is the one that always lands.
func start(t *testing.T, name, home, ws string, cols, rows int, args ...string) *rig {
	t.Helper()
	// EVERY RUN ON THIS HOST NAMES ITS OWN RIG. Several checkouts run this
	// suite at once on one machine, and with a fixed session name each start()
	// kills the other run's rig before opening its own — a whole suite then
	// times out in a test that was simply looking at somebody else's screen.
	// The pid LEADS the name: tmux falls back to prefix matching on -t, so a
	// sibling's `kill-session -t afe2e_a` would still reach `afe2e_a-<pid>`.
	name = fmt.Sprintf("p%d-%s", os.Getpid(), name)
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	command := []string{
		"env",
		"AFORGE_HOME=" + home,
		"OPENROUTER_API_KEY=" + key,
		"TERM=xterm-256color",
		binary(t),
	}
	command = append(command, args...)
	quoted := make([]string, 0, len(command))
	for _, part := range command {
		quoted = append(quoted, shellQuote(part))
	}
	_ = exec.Command("tmux", "kill-session", "-t", name).Run()
	// THE WINDOW IS SIZED BEFORE THE APP EVER SEES IT. `new-session -x/-y` is
	// honoured only when the server's window-size option agrees, and resizing
	// a window the app has already painted into leaves the frame it drew at
	// the old size behind on the terminal. So the session is opened on a
	// placeholder, resized, and only then respawned on the binary.
	launch := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", ws, "sleep 600")
	if out, err := launch.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v\n%s", err, out)
	}
	r := &rig{t: t, name: name, home: home, ws: ws}
	t.Cleanup(func() {
		if t.Failed() {
			r.dump()
		}
		r.kill()
	})
	r.resize(cols, rows)
	respawn := exec.Command("tmux", "respawn-window", "-k", "-t", name, "-c", ws, strings.Join(quoted, " "))
	if out, err := respawn.CombinedOutput(); err != nil {
		t.Fatalf("tmux respawn-window: %v\n%s", err, out)
	}
	// And the app has to be past its first frame before it can be typed at.
	time.Sleep(3 * time.Second)
	return r
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (r *rig) resize(cols, rows int) {
	r.t.Helper()
	out, err := exec.Command("tmux", "resize-window", "-t", r.name,
		"-x", fmt.Sprint(cols), "-y", fmt.Sprint(rows)).CombinedOutput()
	if err != nil {
		r.t.Fatalf("tmux resize-window: %v\n%s", err, out)
	}
}

// keys sends key NAMES tmux knows — "Enter", "Escape", "Up", "C-u".
func (r *rig) keys(names ...string) {
	r.t.Helper()
	args := append([]string{"send-keys", "-t", r.name}, names...)
	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		r.t.Fatalf("tmux send-keys %v: %v\n%s", names, err, out)
	}
	time.Sleep(120 * time.Millisecond)
}

// lit sends literal bytes — typed text, and the escape sequences a keyboard
// protocol or a mouse sends.
func (r *rig) lit(text string) {
	r.t.Helper()
	if out, err := exec.Command("tmux", "send-keys", "-t", r.name, "-l", text).CombinedOutput(); err != nil {
		r.t.Fatalf("tmux send-keys -l: %v\n%s", err, out)
	}
	time.Sleep(120 * time.Millisecond)
}

// ctrlEnter is `ask here` without leaving the box.
//
// TWO SPELLINGS, BOTH SENT AS THE BYTES A TERMINAL SENDS. home.go binds
// "ctrl+enter" and "alt+enter" to one call and says why: ctrl+enter only
// reaches the app on a terminal that can tell it from a plain enter. Bubble Tea
// v2 asks for the kitty protocol and modifyOtherKeys unconditionally, so
// CSI 13;5u decodes as ctrl+enter — this suite sends that one, and alt+enter
// (ESC CR) is the fallback every terminal delivers.
func (r *rig) ctrlEnter() { r.lit("\x1b[13;5u") }

func (r *rig) altEnter() { r.lit("\x1b\r") }

// mouseTo moves the pointer to a 1-based cell with an SGR motion report, which
// is what the all-motion mode the app turns on in View() asks for.
func (r *rig) mouseTo(col, row int) {
	r.t.Helper()
	r.lit(fmt.Sprintf("\x1b[<35;%d;%dM", col, row))
	time.Sleep(400 * time.Millisecond)
}

// capture is the screen, exactly as it stands.
func (r *rig) capture() string {
	r.t.Helper()
	out, err := exec.Command("tmux", "capture-pane", "-p", "-t", r.name).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// lines is the screen split, with the trailing empty rows kept: the padding row
// above the foot is a fact about those rows.
func (r *rig) lines() []string {
	return strings.Split(strings.TrimRight(r.capture(), "\n"), "\n")
}

// waitFor polls the screen until every wanted substring is on it at once.
func (r *rig) waitFor(within time.Duration, want ...string) string {
	r.t.Helper()
	deadline := time.Now().Add(within)
	screen := ""
	for {
		screen = r.capture()
		missing := false
		for _, sub := range want {
			if !strings.Contains(screen, sub) {
				missing = true
				break
			}
		}
		if !missing {
			return screen
		}
		if time.Now().After(deadline) {
			r.t.Errorf("waited %s for %q and never saw it. the screen was:\n%s", within, want, screen)
			return screen
		}
		time.Sleep(pollEvery)
	}
}

// waitForAny polls until one of the wanted substrings is on screen, and answers
// which. It exists for the places where the product has two honest answers —
// `4` or `four` — and the test must not pick one for it.
func (r *rig) waitForAny(within time.Duration, want ...string) (string, string) {
	r.t.Helper()
	deadline := time.Now().Add(within)
	for {
		screen := r.capture()
		for _, sub := range want {
			if strings.Contains(screen, sub) {
				return sub, screen
			}
		}
		if time.Now().After(deadline) {
			r.t.Errorf("waited %s for any of %q and saw none. the screen was:\n%s", within, want, screen)
			return "", screen
		}
		time.Sleep(pollEvery)
	}
}

// glimpse polls fast for something that is only on screen while a turn runs,
// and answers whether it was ever caught. It NEVER fails on its own: a spinner
// missed on a fast reply is a fast reply, not a defect.
func (r *rig) glimpse(within time.Duration, want ...string) (string, bool) {
	r.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		screen := r.capture()
		for _, sub := range want {
			if strings.Contains(screen, sub) {
				return screen, true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", false
}

// quit closes the window the way a person does.
func (r *rig) quit() {
	r.t.Helper()
	r.lit("/quit")
	r.keys("Enter")
	time.Sleep(3 * time.Second)
	r.kill()
}

func (r *rig) kill() {
	if r.dead {
		return
	}
	r.dead = true
	_ = exec.Command("tmux", "kill-session", "-t", r.name).Run()
}

// dump is the transcript this suite owes anybody reading a failure: the screen,
// and the state root the run built. config.json is skipped on purpose.
func (r *rig) dump() {
	r.t.Logf("── screen of %s ──\n%s", r.name, r.capture())
	var b strings.Builder
	_ = filepath.WalkDir(r.home, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(r.home, path)
		if entry.IsDir() {
			fmt.Fprintf(&b, "  %s/\n", rel)
			return nil
		}
		if filepath.Base(path) == "config.json" && filepath.Dir(path) == r.home {
			fmt.Fprintf(&b, "  %s (not shown — secrets)\n", rel)
			return nil
		}
		info, _ := entry.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		fmt.Fprintf(&b, "  %s (%d bytes)\n", rel, size)
		switch filepath.Base(path) {
		case "transcript.jsonl", "inbox.jsonl", "log", "wake.log":
			if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
				fmt.Fprintf(&b, "%s\n", clip(string(raw), 4000))
			}
		default:
			if strings.HasSuffix(path, ".json") && strings.Contains(path, "standing") {
				if raw, err := os.ReadFile(path); err == nil {
					fmt.Fprintf(&b, "%s\n", clip(string(raw), 2000))
				}
			}
		}
		return nil
	})
	r.t.Logf("── AFORGE_HOME %s ──\n%s", r.home, b.String())
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n… clipped"
}

// tick runs one pass of the ambient side from outside every window, exactly as
// the launchd agent and the systemd timer do.
func tick(t *testing.T, home string) string {
	t.Helper()
	command := exec.Command(binary(t), "tick")
	command.Env = append(os.Environ(), "AFORGE_HOME="+home)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("aforge tick: %v\n%s", err, out)
	}
	return string(out)
}

// ── seeding ─────────────────────────────────────────────────────────────────

// seedProject writes the smallest thing world.go will read as a project: a
// bucket, a session directory inside it, a transcript file, and a meta.json
// with a non-zero lastUserAt. Nothing is faked about the SHAPE — this is the
// same layout a real run leaves behind (internal/session's place.go) — only
// about the conversation, which these scenarios are not about.
func seedProject(t *testing.T, home, name string, id int, ago time.Duration) string {
	t.Helper()
	ws := "/tmp/aforge-e2e-seed/" + name
	bucket := strings.ReplaceAll(filepath.Clean(ws), string(filepath.Separator), "-")
	sid := fmt.Sprintf("%016x", 0x1000000000000000+id)
	dir := filepath.Join(home, "v3", "projects", bucket, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), nil, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	at := time.Now().Add(-ago)
	meta := map[string]any{
		"id":         sid,
		"title":      "Seed " + strings.Title(name), //nolint:staticcheck // a fixture name, not prose
		"workspace":  ws,
		"created":    at.Format(time.RFC3339Nano),
		"lastUserAt": at.Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return dir
}

// seedNews puts notes in one conversation's inbox — what a firing left when no
// window was open. It is the [standing.Note] shape, written the way
// standing.Deliver writes it.
func seedNews(t *testing.T, sessionDir string, texts ...string) {
	t.Helper()
	var b strings.Builder
	for i, text := range texts {
		note := map[string]any{
			"at":    time.Now().Add(-time.Duration(i+1) * time.Minute).Format(time.RFC3339Nano),
			"item":  fmt.Sprintf("%016x", 0x2000000000000000+i),
			"words": fmt.Sprintf("watch number %d", i+1),
			"kind":  "said",
			"text":  text,
		}
		raw, err := json.Marshal(note)
		if err != nil {
			t.Fatalf("seed news: %v", err)
		}
		b.Write(raw)
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "inbox.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("seed news: %v", err)
	}
}

// projectInbox is what the project's own inbox holds — road 4 of a firing's
// delivery, and the only address an `ask here` errand has when no window is up.
func projectInbox(t *testing.T, home, workspace string) string {
	t.Helper()
	root := filepath.Join(home, "v3", "standing")
	var found string
	_ = filepath.WalkDir(filepath.Join(root, "inbox"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Base(path) != "inbox.jsonl" {
			return nil
		}
		found = path
		return nil
	})
	if found == "" {
		// The layout is standing.ProjectInboxDir's; walk the whole standing root
		// rather than repeating its encoding here.
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || filepath.Base(path) != "inbox.jsonl" {
				return nil
			}
			found = path
			return nil
		})
	}
	if found == "" {
		return ""
	}
	raw, err := os.ReadFile(found)
	if err != nil {
		return ""
	}
	return string(raw)
}

// sessionTranscripts is every conversation this run wrote, so a test can prove
// what reached the journal when the screen is the thing in question.
func sessionTranscripts(t *testing.T, home string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(filepath.Join(home, "v3", "projects"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Base(path) != "transcript.jsonl" {
			return nil
		}
		if raw, err := os.ReadFile(path); err == nil {
			out[path] = string(raw)
		}
		return nil
	})
	return out
}
