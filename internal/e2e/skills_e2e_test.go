//go:build e2e

package e2e

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// ── the skills a person already has ─────────────────────────────────────────
//
// testForeignSkills is the goal "use Claude Code and Codex skills directly"
// measured on the real binary, in a real terminal, against a real model. A
// fresh home holds three skills the way the other tools install them — one in
// Claude Code's skills folder, one in Codex's, and one inside a Claude Code
// plugin that is installed and enabled — and each one carries a code word that
// exists nowhere else, so an answer that says the word is an answer that read
// the skill.
//
// IT RUNS ON THE ORDINARY LAUNCH. `chat` with no --no-host attaches to this
// workspace's session host, which is the road a person's bare `codeaf` takes,
// and it is the road /skill could not attach on until the attachment crossed
// the socket.
//
// THE KEY TRAVELS BY THE VARIABLE ONLY. The home here is written from nothing —
// the rows this scenario needs and no copy of anybody's profile — so no key is
// ever written into it ([start] hands the child the key [liveKey] resolved).

// The three skills, their folders, their descriptions and the words only
// their bodies hold.
const (
	tideSkill    = "tide-almanac"
	tideCode     = "QUILLON-TIDE-7431"
	ledgerSkill  = "lantern-ledger"
	ledgerCode   = "LANTERN-LEDGER-2958"
	orchardSkill = "orchard-census"
	orchardCode  = "ORCHARD-CENSUS-6612"
	orchardID    = "orchard@fixture-market"
)

func testForeignSkills(t *testing.T) {
	t.Run("memory_on", func(t *testing.T) { foreignSkillsRun(t, config.MemoryOn) })
	t.Run("memory_off", func(t *testing.T) { foreignSkillsRun(t, config.MemoryOff) })
}

func foreignSkillsRun(t *testing.T, memory string) {
	home := skillsHome(t, memory)
	ws := newWorkspace(t, "skillsdoor", false)
	r := startWithEnv(t, []string{
		config.APIKeyEnv + "=" + liveKey(t),
		"CODEAF_TASK_BELT=node",
		"CODEAF_TELEMETRY=off",
		// THE LOGIN HOME IS THE STATE ROOT, both ways it is asked for: the
		// launch's import pass reads CODEAF_HOME (internal/home's Login) and
		// anything that asks the process for ~ gets the same folder.
		"HOME=" + home,
	}, "afe2e_skills_"+memory, home, ws, tuiWide, 40, "chat", "--one-model")
	statesPastTheDoor(t, r)

	// (a) AUTOMATIC. The words of the message match the Claude Code skill's
	// description and nothing names it, so the turn carries it by itself, and
	// the answer carries the word only its body holds.
	r.lit("What does the Port Quillon tide almanac say about the harbour tide at noon? Keep it to one line.")
	r.keys("Enter")
	carried := carriedAndFollowed(t, r, tideSkill, tideCode)
	t.Logf("memory %s — the tide skill carried and followed:\n%s", memory, carried)

	// And the Codex skill the same way, which is the other harness's folder.
	r.lit("What does the Brassmoor lantern ledger record for entry nine? Keep it to one line.")
	r.keys("Enter")
	carried = carriedAndFollowed(t, r, ledgerSkill, ledgerCode)
	t.Logf("memory %s — the Codex skill carried and followed:\n%s", memory, carried)

	// (b) BY HAND. The plugin skill's description has nothing to do with the
	// question asked next, so only the attachment can carry it. The list opens
	// on the plugin skill, and no row says it cannot be attached.
	r.lit("/skill orchard")
	picker := r.waitFor(20*time.Second, orchardSkill)
	for _, refusal := range []string{say(t, "skillNoShelfWord"), say(t, "skillCannotCarryWord"), "memory is off"} {
		if strings.Contains(picker, refusal) {
			t.Fatalf("memory %s — the picker says %q:\n%s", memory, refusal, picker)
		}
	}
	r.keys("Enter")
	time.Sleep(700 * time.Millisecond)
	if screen := r.capture(); strings.Contains(screen, say(t, "skillCannotCarryWord")) {
		t.Fatalf("memory %s — the attachment was refused:\n%s", memory, screen)
	}
	r.keys("Escape")
	r.keys("C-u")
	time.Sleep(500 * time.Millisecond)
	// The question names no skill and shares no word with the orchard one, so
	// the model can only find it through what the attachment carried with the
	// message; the catalog lists every skill and says nothing about which one
	// the person put in front.
	r.lit("Following the skill attached to this conversation, answer in one short line: what is seven times six?")
	r.keys("Enter")
	carried = carriedAndFollowed(t, r, orchardSkill, orchardCode)
	t.Logf("memory %s — the attached plugin skill carried and followed:\n%s", memory, carried)

	// (c) use_skill, both modes, read off the conversation's own record
	// rather than guessed from the answer's wording.
	r.lit("Call the use_skill tool with mode list, then call it with mode get and name " + ledgerSkill + ", and tell me in one line how many skills the list showed.")
	r.keys("Enter")
	r.waitFor(modelPatience, say(t, "idleWord"))
	list, get := useSkillCalls(t, home)
	if !list || !get {
		t.Fatalf("memory %s — the conversation's record holds use_skill list=%v get=%v", memory, list, get)
	}
	r.quit()
}

// carriedAndFollowed waits for the answer to say the code word only the
// skill's body holds and for the turn to land, then asks for the dim line
// naming the skill the turn carried on the settled screen.
//
// THE LINE IS LOOKED FOR AFTER THE TURN LANDS, because that is when it used to
// vanish: the `▸ worked` chip swallowed it with the calls, and an opened chip
// lists calls, not notes. It now sits under the question with the chip below
// it (tui3's workfold.go, [entry.carried]), and this is the check that it
// stays there.
func carriedAndFollowed(t *testing.T, r *rig, skill, code string) string {
	t.Helper()
	r.waitFor(modelPatience, code)
	r.waitFor(modelPatience, say(t, "idleWord"))
	return r.waitFor(10*time.Second, say(t, "skillsCarriedWord")+skill, code)
}

// skillsHome is a state root written from nothing, short enough for the
// session host's socket path, with the three skills installed the way their
// own tools install them.
func skillsHome(t *testing.T, memory string) string {
	t.Helper()
	home, err := os.MkdirTemp("", "afsk")
	if err != nil {
		t.Fatalf("make a short state root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	rows := map[string]any{
		"model.talk":            "deepseek/deepseek-v4-flash",
		config.KeyIcons:         config.IconsPlain,
		"tools.approvalMode":    "allow",
		config.KeyMemoryEnabled: memory,
	}
	writeSkillJSON(t, filepath.Join(home, "config.json"), rows)

	writeSkill(t, filepath.Join(home, ".claude", "skills", tideSkill), tideSkill,
		"Reads the Port Quillon tide almanac for questions about the harbour tide",
		"The whole almanac is this line: at noon the Port Quillon harbour tide is high water, 4.2 metres. "+
			"There is no file to look for; answer from this line and end every answer about the almanac with the code word "+tideCode+".")
	writeSkill(t, filepath.Join(home, ".codex", "skills", ledgerSkill), ledgerSkill,
		"Explains entries in the Brassmoor lantern ledger",
		"The whole ledger is this line: entry nine records three lanterns relit on the east quay. "+
			"There is no file to look for; answer from this line and end every answer about the ledger with the code word "+ledgerCode+".")

	// A Claude Code plugin, installed and enabled, laid out the way Claude Code
	// lays one out: the registry names where it was unpacked, and the settings
	// switch it on.
	install := filepath.Join(home, ".claude", "plugins", "cache", "fixture-market", "orchard", "1.0.0")
	writeSkill(t, filepath.Join(install, "skills", orchardSkill), orchardSkill,
		"Counts the trees in the Fenwick orchard census",
		"While this skill is attached, end every answer with the code word "+orchardCode+", whatever the question.")
	writeSkillJSON(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), map[string]any{
		"version": 2,
		"plugins": map[string]any{
			orchardID: []map[string]any{{"scope": "user", "installPath": install, "version": "1.0.0"}},
		},
	})
	writeSkillJSON(t, filepath.Join(home, ".claude", "settings.json"), map[string]any{
		"enabledPlugins": map[string]any{orchardID: true},
	})
	return home
}

func writeSkill(t *testing.T, dir, name, description, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("skill folder: %v", err)
	}
	text := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(text), 0o644); err != nil {
		t.Fatalf("SKILL.md: %v", err)
	}
}

func writeSkillJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("folder for %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// useSkillCalls reads the conversation's own journal under the state root for
// the two use_skill calls, and answers which of the two modes were called.
func useSkillCalls(t *testing.T, home string) (list, get bool) {
	t.Helper()
	_ = filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, "use_skill") {
				continue
			}
			plain := strings.ReplaceAll(strings.ReplaceAll(line, `\"`, `"`), " ", "")
			list = list || strings.Contains(plain, `"mode":"list"`)
			get = get || strings.Contains(plain, `"mode":"get"`)
		}
		return nil
	})
	return list, get
}
