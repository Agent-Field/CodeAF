package claudeagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/skills"
)

// ── CODEAF'S SKILLS, HANDED TO CLAUDE CODE AS A PLUGIN ─────────────────────
//
// A model seated in codeaf is shown every skill the person keeps for codeaf or
// for another program, and fetches one when it applies. Claude Code reads only
// its own folders, so the canvas skill the app installs into .codeaf/skills —
// and anything kept for Codex, Cursor or the shared .agents folder — would be
// invisible to a conversation on Claude Code, which would then answer "make me
// a landing page" with a file nobody can see.
//
// So a session — a conversation's or a task's — starts with one extra plugin, built for this folder: a plugin
// manifest and a `skills` folder of LINKS to the skills where they already
// live. Claude Code lists them as `codeaf:<name>` beside its own and loads one
// by meaning, as it does any skill. Nothing is copied, so a skill updated in
// place is the one Claude Code reads next session.
//
// THE SKILLS CLAUDE CODE ALREADY HAS ARE LEFT OUT: those in a .claude/skills
// folder and those inside a Claude Code plugin it loads itself. A skill another
// folder shadows is left out too, exactly as codeaf's own list leaves it out.

// skillPluginName is the plugin's name, and so the prefix Claude Code shows
// on each skill.
const skillPluginName = "codeaf"

// skillPluginMu keeps two conversations in one folder from rebuilding the same
// plugin folder over each other.
var skillPluginMu sync.Mutex

// SkillPlugin builds the plugin folder for Claude Code working in dir and
// answers its path, or "" when there is no skill to hand over or the folder
// cannot be written. It never fails a session: a Claude Code without codeaf's
// skills still works. Pass it as [Options.PluginDirs].
func SkillPlugin(dir string) string {
	login, err := home.Login()
	if err != nil {
		return ""
	}
	found, err := skills.Discover(skills.Options{ProjectDir: dir, HomeDir: login, OmitSize: true})
	if err != nil {
		return ""
	}
	links := map[string]string{}
	for _, skill := range found {
		if skill.Shadowed || skill.Plugin != "" || skill.Root == ".claude/skills" || skill.Root == skills.RootClaudePlugins {
			continue
		}
		if _, taken := links[skill.Name]; !taken && skill.Name != "" {
			links[skill.Name] = skill.Dir
		}
	}
	if len(links) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(dir))
	root := home.Join("claude-code", "skills", hex.EncodeToString(sum[:6]))
	skillPluginMu.Lock()
	defer skillPluginMu.Unlock()
	// THE FOLDER IS REBUILT EVERY TIME, so a skill removed since the last
	// session is not still offered through a stale link.
	if err := os.RemoveAll(filepath.Join(root, "skills")); err != nil {
		return ""
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o700); err != nil {
		return ""
	}
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o700); err != nil {
		return ""
	}
	manifest, _ := json.Marshal(map[string]string{
		"name": skillPluginName, "version": "1.0.0",
		"description": "The skills the person keeps for codeaf and other programs, handed to Claude Code by codeaf.",
	})
	if err := os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), manifest, 0o600); err != nil {
		return ""
	}
	linked := 0
	for name, target := range links {
		if os.Symlink(target, filepath.Join(root, "skills", name)) == nil {
			linked++
		}
	}
	if linked == 0 {
		return ""
	}
	return root
}
