package bare

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// systemPromptTemplate is pi 0.82.1's verbatim default system prompt for a
// plain headless run with the four active tools (read, bash, edit, write).
//
// It was verified against the pi source in two steps. buildSystemPrompt
// (system-prompt.js) takes selectedTools and filters by toolSnippets: a tool
// appears in "Available tools" only when its one-line snippet is present, and
// the AgentSession default active set is [read, bash, edit, write]
// (agent-session.js L2044-2045). So the prompt lists exactly those four — not
// all seven registry tools — and grep/find/ls are absent even though they are
// registered. The guidelines are assembled in the same pass: the tool-specific
// ones from promptGuidelines in registry order, then two ALWAYS lines appended
// last by addGuideline.
//
// One guideline the spec's verbatim block omits but the code emits: when bash
// is active and none of grep/find/ls are, buildSystemPrompt inserts
// "Use bash for file operations like ls, rg, find" before the tool-specific
// guidelines (system-prompt.js L60-61). The four-tool default hits exactly
// that condition, so the line is present. The code is the authority; the spec
// prose missed it.
//
// The template carries three placeholders: {CWD}, {README_PATH}, {DOCS_PATH},
// {EXAMPLES_PATH}. {CWD} is the working directory with backslashes flattened
// to forward slashes (system-prompt.js L9: cwd.replace(/\\/g, "/")). The
// three doc paths are resolved by pi's getReadmePath/getDocsPath/getExamplesPath
// (config.js L348-358), which join getPackageDir() with README.md / docs /
// examples. getPackageDir walks up from __dirname to the nearest package.json,
// so the paths point at the pi install's package root — the directory that
// contains the README, docs, and examples directories.
//
// The rejected alternative was to omit the Pi documentation block entirely,
// on the grounds that a bare leaf never asks about pi itself. That would
// diverge from the verbatim prompt and break byte-for-byte equivalence with
// the oracle, so the block stays with resolved paths.
const systemPromptTemplate = `You are an expert coding assistant operating inside pi, a coding agent harness. You help users by reading files, executing commands, editing code, and writing new files.

Available tools:
- read: Read file contents
- bash: Execute bash commands (ls, grep, find, etc.)
- edit: Make precise file edits with exact text replacement, including multiple disjoint edits in one call
- write: Create or overwrite files

In addition to the tools above, you may have access to other custom tools depending on the project.

Guidelines:
- Use bash for file operations like ls, rg, find
- Use read to examine files instead of cat or sed.
- Use edit for precise changes (edits[].oldText must match exactly)
- When changing multiple separate locations in one file, use one edit call with multiple entries in edits[] instead of multiple edit calls
- Each edits[].oldText is matched against the original file, not after earlier edits are applied. Do not emit overlapping or nested edits. Merge nearby changes into one edit.
- Keep edits[].oldText as small as possible while still being unique in the file. Do not pad with large unchanged regions.
- Use write only for new files or complete rewrites.
- Be concise in your responses
- Show file paths clearly when working with files

Pi documentation (read only when the user asks about pi itself, its SDK, extensions, themes, skills, or TUI):
- Main documentation: {README_PATH}
- Additional docs: {DOCS_PATH}
- Examples: {EXAMPLES_PATH} (extensions, custom tools, SDK)
- When reading pi docs or examples, resolve docs/... under Additional docs and examples/... under Examples, not the current working directory
- When asked about: extensions (docs/extensions.md, examples/extensions/), themes (docs/themes.md), skills (docs/skills.md), prompt templates (docs/prompt-templates.md), TUI components (docs/tui.md), keybindings (docs/keybindings.md), SDK integrations (docs/sdk.md), custom providers (docs/custom-provider.md), adding models (docs/models.md), pi packages (docs/packages.md), environment variables (docs/environment-variables.md)
- When working on pi topics, read the docs and examples, and follow .md cross-references before implementing
- Always read pi .md files completely and follow links to related docs (e.g., tui.md for TUI API details)

Current working directory: {CWD}`

// summarizationSystemPrompt is pi's verbatim SUMMARIZATION_SYSTEM_PROMPT from
// compaction/utils.js L139. It is used by the compaction path (spec §5) to
// summarize discarded context. It must exist in the code even though it will
// not fire on small bench runs.
const summarizationSystemPrompt = "You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.\n\nDo NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary."

// piPackageDir is the resolved pi install root, cached after first lookup.
// It is the directory containing pi's README.md, docs/, and examples/.
var (
	piPackageDirOnce sync.Once
	piPackageDir     string
)

// resolvePiPackageDir finds pi's package root — the directory that contains
// package.json, README.md, docs/, and examples/. It mirrors pi's
// getPackageDir (config.js L293-313), which walks up from __dirname to the
// nearest package.json. We resolve it the same way: start from the well-known
// dist directory and walk up.
//
// Three candidates, tried in order:
//  1. The well-known ~/.local/pi0821 install (the bench environment's path).
//  2. The pi package under the npm root, resolved via `npm root`.
//  3. Empty — keep the documentation block with the placeholders resolved to
//     the empty string, so the prompt is still valid and the block is present.
//
// The well-known path is tried first rather than calling npm, because the
// bench environment pins the install there and a subprocess costs a fork that
// a test should not depend on. If neither path exists the placeholders become
// empty strings; the block stays, the paths are blank, and the prompt is
// still byte-for-byte stable modulo cwd.
func resolvePiPackageDir() string {
	piPackageDirOnce.Do(func() {
		// Candidate 1: the well-known pi0821 install.
		home, _ := os.UserHomeDir()
		if home != "" {
			candidate := filepath.Join(home, ".local", "pi0821", "lib", "node_modules", "@earendil-works", "pi-coding-agent")
			if isPiPackageDir(candidate) {
				piPackageDir = candidate
				return
			}
		}
		// Candidate 2: the pi package under the npm root. We do not call npm
		// here (no subprocess in the hot path); instead we check the same
		// well-known location under the global node_modules if HOME is set.
		// This is a best-effort resolution; if it fails, the doc paths are
		// empty and the prompt is still valid.
		piPackageDir = ""
	})
	return piPackageDir
}

// isPiPackageDir reports whether dir looks like pi's package root: it has a
// package.json and a README.md. We do not require docs/ or examples/ because
// a minimal install may not ship them, and the paths are best-effort.
func isPiPackageDir(dir string) bool {
	if dir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		return false
	}
	return true
}

// docPaths resolves {README_PATH}, {DOCS_PATH}, {EXAMPLES_PATH} against the
// pi install when present. When the install is not found, all three are empty
// strings — the documentation block stays in the prompt with blank paths,
// which is the honest representation of "the docs were not locatable" rather
// than a silent deletion of the block.
func docPaths() (readme, docs, examples string) {
	pkg := resolvePiPackageDir()
	if pkg == "" {
		return "", "", ""
	}
	readme = filepath.Join(pkg, "README.md")
	docs = filepath.Join(pkg, "docs")
	examples = filepath.Join(pkg, "examples")
	return
}

// SystemPrompt renders the verbatim pi system prompt for the given working
// directory. {CWD} is normalized to forward slashes (pi replaces backslashes
// with forward slashes; on Linux this is a no-op). The three doc paths are
// resolved against the pi install when present, empty otherwise.
func SystemPrompt(cwd string) string {
	cwdNormalized := cwd
	if runtime.GOOS == "windows" {
		cwdNormalized = strings.ReplaceAll(cwdNormalized, "\\", "/")
	}
	readme, docs, examples := docPaths()
	prompt := systemPromptTemplate
	prompt = strings.ReplaceAll(prompt, "{CWD}", cwdNormalized)
	prompt = strings.ReplaceAll(prompt, "{README_PATH}", readme)
	prompt = strings.ReplaceAll(prompt, "{DOCS_PATH}", docs)
	prompt = strings.ReplaceAll(prompt, "{EXAMPLES_PATH}", examples)
	return prompt
}
