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

// emptyHome is a state root WITH NOTHING IN IT — an empty directory, and not one
// thing more.
//
// [newHome] is the fixture every other rig here uses and it writes a config.json
// on the way out: the person's own rows, the suite's model, an approval posture.
// That file is what makes those runs about the surface rather than about setup,
// and it is exactly what a fresh-install run must not have. A machine that has
// never run aforge has no profile file, no key, no crew and no marker, and the
// first thing that writes into this directory is the product.
func emptyHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("home: %v", err)
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
	return workspaceAt(t, filepath.Join(t.TempDir(), name), dirty)
}

// workspaceAt seeds the same repository at a caller-chosen path when the
// path length itself is part of a terminal layout scenario.
func workspaceAt(t *testing.T, ws string, dirty bool) string {
	t.Helper()
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
	r := startWithEnv(t, []string{"OPENROUTER_API_KEY=" + strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))},
		name, home, ws, cols, rows, args...)
	r.skipSetup(t)
	return r
}

// skipSetup presses esc until the first-run flow is off the screen, and it is
// part of [start] rather than of any one scenario because EVERY SCENARIO HERE IS
// ABOUT WHAT IS BEHIND IT.
//
// A STATE ROOT BUILT ONE MINUTE AGO OPENS ON THE SETUP HOWEVER COMPLETE THE
// PROFILE IT COPIED IS. The marker that says the setup has been seen is a file in
// that root ([newHome] writes a config.json and nothing else), so every rig here
// meets the flow — and the flow is SEVERAL STEPS, so one esc leaves the one under
// it and whatever the scenario types next goes into that step's own box. That is
// how a suite came to record a model answering "" to `what is 2+2`, a home with
// no foot rule, and a task brief typed into a daily-limit field.
//
// [startFresh] deliberately does NOT go through this door: a machine that has
// never run aforge is the subject of its own subtest, and skipping the screen it
// exists to read would be skipping the test.
func (r *rig) skipSetup(t *testing.T) {
	t.Helper()
	for press := 0; press < 5; press++ {
		screen := r.capture()
		if !strings.Contains(screen, setupSkipKeysWord) && !strings.Contains(screen, setupTitleWord) {
			return
		}
		r.keys("Escape")
		time.Sleep(900 * time.Millisecond)
	}
	t.Logf("the setup was still on screen after five escapes:\n%s", r.capture())
}

// The two sentences that say the first-run flow is up. They are the SUITE'S OWN
// copies of internal/tui3's [setupSkipKeysWord] and the setup title, and they are
// spelled here rather than reached through [say] because tuiwords_test.go's own
// gate reads this file and every other one for the names it hands out — a door
// used by [start] itself has to stand before any scenario asks for a word.
const (
	setupSkipKeysWord = "esc skips setup"
	setupTitleWord    = "setting up"
)

// keylessEnv is every variable a fresh-install run must not inherit: the two the
// key resolution reads in order (internal/config's APIKeyAt), the three capability
// keys the belt reads, and the one override that would move the profile out from
// under an empty state root.
//
// THEY ARE UNSET AND NOT BLANKED. A blank is what the suite's own rigs pass and it
// reads as absent everywhere in internal/config, but a fresh install is a machine
// where the variable is NOT THERE, and the two have been different before now.
var keylessEnv = []string{
	"OPENROUTER_API_KEY", "OPENAI_API_KEY",
	"EXA_API_KEY", "FIRECRAWL_API_KEY", "JINA_API_KEY",
	"AFORGE_PROFILE_DIR", "AFORGE_DAILY_BUDGET",
}

// startFresh is [start] for A MACHINE THAT HAS NEVER RUN AFORGE: the same rig,
// with every provider key and the profile override taken out of the environment
// rather than passed through it.
//
// It exists because the one thing a fixture cannot fake is emptiness. The class
// of defects #322 closes is emptiness read as absence, so a run that pre-created
// a config file — or exported a key the way every other rig here does — would
// hide exactly the failure it is there to catch.
func startFresh(t *testing.T, name, home, ws string, cols, rows int, args ...string) *rig {
	t.Helper()
	unset := make([]string, 0, 2*len(keylessEnv))
	for _, name := range keylessEnv {
		unset = append(unset, "-u", name)
	}
	return startWithEnv(t, unset, name, home, ws, cols, rows, args...)
}

// startWithEnv is the body both doors share: `env`, whatever the caller puts in
// front of the assignments, then the state root and the terminal.
func startWithEnv(t *testing.T, env []string, name, home, ws string, cols, rows int, args ...string) *rig {
	t.Helper()
	// EVERY RUN ON THIS HOST NAMES ITS OWN RIG. Several checkouts run this
	// suite at once on one machine, and with a fixed session name each start()
	// kills the other run's rig before opening its own — a whole suite then
	// times out in a test that was simply looking at somebody else's screen.
	// The pid LEADS the name: tmux falls back to prefix matching on -t, so a
	// sibling's `kill-session -t afe2e_a` would still reach `afe2e_a-<pid>`.
	name = fmt.Sprintf("p%d-%s", os.Getpid(), name)
	command := []string{"env"}
	command = append(command, env...)
	command = append(command,
		"AFORGE_HOME="+home,
		"TERM=xterm-256color",
		binary(t),
	)
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
	// Keep a crashed terminal readable so failures include the program's error.
	if out, err := exec.Command("tmux", "set-option", "-w", "-t", name, "remain-on-exit", "on").CombinedOutput(); err != nil {
		t.Fatalf("tmux remain-on-exit: %v\n%s", err, out)
	}
	respawn := exec.Command("tmux", "respawn-window", "-k", "-t", name, "-c", ws, strings.Join(quoted, " "))
	if out, err := respawn.CombinedOutput(); err != nil {
		t.Fatalf("tmux respawn-window: %v\n%s", err, out)
	}
	// The local engine connection can outlast a fixed launch delay. Wait for
	// an interactive surface before typing, or the first request is lost.
	if hit, _ := r.waitForAny(45*time.Second, say(t, "homeFootWord"),
		say(t, "starterTaskWord"), say(t, "setupTitleWord"), say(t, "landingKeysWord")); hit == "" {
		t.Fatal("the terminal never reached an interactive surface")
	}
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

// seedDecidedFamily writes one conversation for a workspace with a FINISHED
// FAMILY already in it: a parent that came home, and a part one level under it
// that nobody could check.
//
// IT NEEDS NO MODEL AT ALL, and that is the point. The graph survives the
// process in the folder's tasks.json (session's task_store.go), and a surface
// that attaches replays one update per node off it (session's
// [Agent.replayTaskRoster]) — so the whole of what this fixture proves is what
// the SURFACE does with a nested landing nobody has decided about, driven
// through the real binary in a real terminal, and settled by a real keystroke
// against the real engine door.
func seedDecidedFamily(t *testing.T, home, ws string) string {
	t.Helper()
	// The binary resolves a macOS /var workspace to /private/var before naming
	// its project bucket. Seed under that same canonical path or the fixture and
	// the process describe two different projects and the UI opens an empty one.
	if canonical, err := filepath.EvalSymlinks(ws); err == nil {
		ws = canonical
	}
	bucket := strings.ReplaceAll(filepath.Clean(ws), string(filepath.Separator), "-")
	sid := fmt.Sprintf("%016x", 0x3000000000000001)
	dir := filepath.Join(home, "v3", "projects", bucket, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed family: %v", err)
	}
	// THE JOURNAL HAS SOMEBODY'S WORDS IN IT, and an empty one is not a cheaper
	// fixture — it is a different screen. A conversation with nothing in it opens
	// on the greeting, which on the machine's FIRST conversation stands through
	// typing (welcome.go's welcomeStandsThroughTyping) and stands the chord keys
	// down while it is up (stop.go), so the card's own answer letters are refused.
	// It is also nothing like the shape this fixture is for: a landing that is
	// somebody's call arrives in a conversation they started the work from.
	statesSeedTranscript(t, dir, sid, ws, "rebuild the index and port the parser")
	at := time.Now().Add(-3 * time.Minute)
	meta := map[string]any{
		"id": sid, "title": "The nested gate", "workspace": ws,
		"created":    at.Format(time.RFC3339Nano),
		"lastUserAt": at.Format(time.RFC3339Nano),
	}
	writeJSON(t, filepath.Join(dir, "meta.json"), meta)
	writeJSON(t, filepath.Join(dir, "tasks.json"), map[string]any{
		"type": "tasks", "version": 1, "seq": 2,
		"nodes": []map[string]any{{
			"id": 1, "title": "Rebuild the index", "brief": "rebuild it", "acceptance": "it builds",
			"state": "done", "report": "the index is rebuilt", "merge": "inplace",
			"ground": ws, "groundMode": "folder", "elapsed_ms": 61000,
		}, {
			"id": 2, "title": "Port the parser", "brief": "port it", "acceptance": "it parses",
			"parent": 1, "depth": 1, "state": "unverified", "merge": "inplace",
			"report":  "finished, but needs your look — nobody could check it in 5m0s",
			"changed": []string{"parser.go"},
			"ground":  ws, "groundMode": "folder", "elapsed_ms": 42000,
		}},
	})
	return dir
}

// seedUndecidedRoot writes one top-level landing nobody could check. A person's
// not-right answer resettles a root through the real engine door and therefore
// emits the second landing card whose status the tmux acceptance reads.
func seedUndecidedRoot(t *testing.T, home, ws string) string {
	t.Helper()
	if canonical, err := filepath.EvalSymlinks(ws); err == nil {
		ws = canonical
	}
	bucket := strings.ReplaceAll(filepath.Clean(ws), string(filepath.Separator), "-")
	sid := fmt.Sprintf("%016x", 0x3000000000000002)
	dir := filepath.Join(home, "v3", "projects", bucket, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed undecided root: %v", err)
	}
	// With the person's own words in it, for [seedDecidedFamily]'s reason.
	statesSeedTranscript(t, dir, sid, ws, "review the pull request diff")
	at := time.Now().Add(-3 * time.Minute)
	writeJSON(t, filepath.Join(dir, "meta.json"), map[string]any{
		"id": sid, "title": "The review gate", "workspace": ws,
		"created": at.Format(time.RFC3339Nano), "lastUserAt": at.Format(time.RFC3339Nano),
	})
	writeJSON(t, filepath.Join(dir, "tasks.json"), map[string]any{
		"type": "tasks", "version": 1, "seq": 1,
		"nodes": []map[string]any{{
			"id": 1, "title": "Review the pull request diff", "brief": "review it", "acceptance": "the diff is correct",
			"state": "unverified", "merge": "inplace",
			"report": "finished, but needs your look — nobody could check it in 5m0s",
			"ground": ws, "groundMode": "folder", "elapsed_ms": 42000,
		}},
	})
	return dir
}

// writeJSON is the fixture's one file writer, so a seed that produces invalid
// JSON fails where it was written rather than as an empty screen ten seconds
// later.
func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
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

// ── what stood, as the store wrote it ───────────────────────────────────────

// standingRecord is the part of one standing item's file this suite reads.
//
// THE MODEL OWNS THIS VOCABULARY AND THE TEST MAY NOT ASSUME IT. What a reminder
// is CALLED and what it SAYS when it fires are written by the model on the day —
// one run named the same order `drink water reminder` and fired `💧 Time to
// drink water!` — so a test that waited for the person's own sentence would be
// waiting for words nothing promised. The record on disk is the one place both
// are stated, so the needles come off it, exactly as every needle about the
// SURFACE comes off internal/tui3's own sources (tuiwords_test.go).
type standingRecord struct {
	ID            string `json:"id"`
	Words         string `json:"words"`
	Status        string `json:"status"`
	RetiredWhy    string `json:"retiredWhy"`
	LastCheckLine string `json:"lastCheckLine"`
	When          struct {
		Kind string    `json:"kind"`
		At   time.Time `json:"at"`
	} `json:"when"`
	Does struct {
		Kind string `json:"kind"`
		Say  string `json:"say"`
	} `json:"does"`
	Rails struct {
		Expires time.Time `json:"expires"`
	} `json:"rails"`
	Brief struct {
		Title string `json:"title"`
	} `json:"brief"`
}

// standingRecords is every item this run stood, read from the store's own
// directory. The layout is internal/standing's: one `<id>.json` beside a folder
// of the same name.
func standingRecords(t *testing.T, home string) []standingRecord {
	t.Helper()
	dir := filepath.Join(home, "v3", "standing")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []standingRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var record standingRecord
		// A file in this directory that is not an item — the watch offer is one —
		// simply has none of these fields, and is told apart by having no id.
		if err := json.Unmarshal(raw, &record); err != nil || strings.TrimSpace(record.ID) == "" {
			continue
		}
		out = append(out, record)
	}
	return out
}

// standingRecordAbout is the item whose words hold a given word, which is how a
// test names the one it asked for without knowing what the model called it.
func standingRecordAbout(t *testing.T, home, word string) (standingRecord, bool) {
	t.Helper()
	for _, record := range standingRecords(t, home) {
		if strings.Contains(strings.ToLower(record.Words), strings.ToLower(word)) {
			return record, true
		}
	}
	return standingRecord{}, false
}

// standingRecordByID is that item read again, after the pass has had its say
// about it.
func standingRecordByID(t *testing.T, home, id string) standingRecord {
	t.Helper()
	for _, record := range standingRecords(t, home) {
		if record.ID == id {
			return record
		}
	}
	return standingRecord{}
}

// standingRecordsDump is every record in one paragraph, for a failure that has
// to say what the store actually holds.
func standingRecordsDump(t *testing.T, home string) string {
	t.Helper()
	var b strings.Builder
	for _, record := range standingRecords(t, home) {
		fmt.Fprintf(&b, "  %s %q — %s/%s · due %s · expires %s · says %q · %s\n",
			record.ID, record.Words, record.Status, record.RetiredWhy,
			record.When.At.Format(time.RFC3339), record.Rails.Expires.Format(time.RFC3339),
			record.Does.Say, record.LastCheckLine)
	}
	if b.Len() == 0 {
		return "  (the store holds no items)"
	}
	return b.String()
}

// firstWords is the first n words of a sentence, which is the needle a screen
// can be searched for when the whole sentence would be cut by a column edge.
func firstWords(said string, n int) string {
	fields := strings.Fields(said)
	if len(fields) > n {
		fields = fields[:n]
	}
	return strings.Join(fields, " ")
}
