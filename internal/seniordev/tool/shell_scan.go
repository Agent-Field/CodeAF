//go:build !windows

// The deterministic shell core shared by executors: shell identification and
// the permission scan that extracts paths and command patterns from a command
// line. Process execution lives in bash.go.
package tool

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// shellMeta lists the shells that are refused as tool shells and the ones
// that use PowerShell syntax. Any shell absent from it is accepted and
// scanned as POSIX.
var shellMeta = map[string]struct {
	deny bool
	ps   bool
}{
	"fish":       {deny: true},
	"nu":         {deny: true},
	"powershell": {ps: true},
	"pwsh":       {ps: true},
}

// ShellName returns the lowercase executable basename, without its extension
// on Windows.
func ShellName(file string) string {
	base := filepath.Base(file)
	if runtime.GOOS == "windows" {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return strings.ToLower(base)
}

// ShellPowerShell reports whether a shell uses PowerShell syntax.
func ShellPowerShell(file string) bool { return shellMeta[ShellName(file)].ps }

// ShellAcceptable reports whether a shell may run tool commands; fish and nu
// are refused.
func ShellAcceptable(file string) bool { return !shellMeta[ShellName(file)].deny }

// shellKind classifies a shell for permission scanning as bash, pwsh,
// powershell or cmd; anything else is scanned as bash.
func shellKind(file string) string {
	switch name := ShellName(file); name {
	case "bash", "pwsh", "powershell", "cmd":
		return name
	default:
		return "bash"
	}
}

// ShellPermissionScan is the ordered permission material gathered from a
// parsed shell command.
type ShellPermissionScan struct {
	Dirs     []string `json:"dirs"`
	Patterns []string `json:"patterns"`
	Always   []string `json:"always"`
}

// ShellScanOptions supplies the path context used by the permission scanner.
type ShellScanOptions struct {
	CWD       string
	Shell     string
	Workspace string
	Home      string
	Env       map[string]string
	IsDir     func(string) bool
}

var shellCWDCommands = stringSet("cd", "chdir", "popd", "pushd", "push-location", "set-location")
var shellFileCommands = stringSet(
	"cd", "chdir", "popd", "pushd", "push-location", "set-location",
	"rm", "cp", "mv", "mkdir", "touch", "chmod", "chown", "cat",
	"get-content", "set-content", "add-content", "copy-item", "move-item",
	"remove-item", "new-item", "rename-item",
)
var cmdFileCommands = stringSet(
	"copy", "del", "dir", "erase", "md", "mkdir", "move", "rd", "ren",
	"rename", "rmdir", "type",
)

// ScanShellPermissions extracts the permission material from a command line:
// directories it touches outside the workspace, the simple-command patterns,
// and their arity prefixes. It keeps shell source strings and token boundaries
// intact and skips dynamic path expressions rather than guess at them.
func ScanShellPermissions(command string, opts ShellScanOptions) ShellPermissionScan {
	if opts.Home == "" {
		opts.Home, _ = os.UserHomeDir()
	}
	if opts.CWD == "" {
		opts.CWD = "."
	}
	ps := ShellPowerShell(opts.Shell)
	kind := shellKind(opts.Shell)
	dirs := newOrderedStrings()
	patterns := newOrderedStrings()
	always := newOrderedStrings()

	var process func(string)
	process = func(simple string) {
		tokens := shellWords(simple)
		if len(tokens) == 0 {
			return
		}
		cmd := tokens[0]
		if ps || kind == "cmd" {
			cmd = strings.ToLower(cmd)
		}

		if shellFileCommands[cmd] || (kind == "cmd" && cmdFileCommands[cmd]) {
			for _, arg := range shellPathArgs(tokens, kind == "cmd") {
				file := shellArgPath(arg, opts, ps)
				if file == "" || pathWithin(file, opts.Workspace) {
					continue
				}
				dir := filepath.Dir(file)
				if opts.IsDir != nil && opts.IsDir(file) {
					dir = file
				}
				dirs.Add(dir)
			}
		}

		if !shellCWDCommands[cmd] {
			patterns.Add(strings.TrimSpace(simple))
			always.Add(strings.Join(shellArityPrefix(tokens), " ") + " *")
		}
		for _, nested := range shellSubstitutions(simple) {
			for _, command := range splitShellCommands(nested) {
				process(command)
			}
		}
	}
	for _, simple := range splitShellCommands(command) {
		process(simple)
	}
	return ShellPermissionScan{Dirs: dirs.Values(), Patterns: patterns.Values(), Always: always.Values()}
}

func shellSubstitutions(text string) []string {
	var out []string
	for start := 0; start+1 < len(text); {
		rel := strings.Index(text[start:], "$(")
		if rel < 0 {
			break
		}
		open := start + rel + 1
		depth := 1
		quote := byte(0)
		escaped := false
		end := open + 1
		for ; end < len(text); end++ {
			c := text[end]
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' && quote != '\'' {
				escaped = true
				continue
			}
			if quote != 0 {
				if c == quote {
					quote = 0
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				continue
			}
			if c == '(' {
				depth++
			} else if c == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		if depth != 0 {
			break
		}
		out = append(out, text[open+1:end])
		start = end + 1
	}
	return out
}

func shellPathArgs(tokens []string, cmd bool) []string {
	out := make([]string, 0, len(tokens)-1)
	for _, token := range tokens[1:] {
		if strings.HasPrefix(token, "-") || (cmd && strings.HasPrefix(token, "/")) ||
			(tokens[0] == "chmod" && strings.HasPrefix(token, "+")) {
			continue
		}
		out = append(out, token)
	}
	return out
}

func shellArgPath(arg string, opts ShellScanOptions, ps bool) string {
	text := unquoteShell(arg)
	if ps {
		text = expandPowerShellPath(text, opts)
	} else {
		text = expandHome(text, opts.Home)
	}
	text = globPrefix(text)
	if text == "" || dynamicShellPath(text, ps) {
		return ""
	}
	if ps {
		text = filesystemProvider(text)
		if text == "" {
			return ""
		}
	}
	if filepath.IsAbs(text) {
		return filepath.Clean(text)
	}
	return filepath.Clean(filepath.Join(opts.CWD, text))
}

func unquoteShell(text string) string {
	if len(text) < 2 {
		return text
	}
	if (text[0] == '"' || text[0] == '\'') && text[len(text)-1] == text[0] {
		return text[1 : len(text)-1]
	}
	return text
}

func expandHome(text, home string) string {
	if text == "~" {
		return home
	}
	if strings.HasPrefix(text, "~/") || strings.HasPrefix(text, `~\`) {
		return filepath.Join(home, text[2:])
	}
	return text
}

func expandPowerShellPath(text string, opts ShellScanOptions) string {
	// PowerShell arguments are not parsed by a grammar here; these are the
	// deterministic expansion rules for already-tokenized arguments.
	replaceEnv := func(s string) string {
		lower := strings.ToLower(s)
		for key, value := range opts.Env {
			if strings.ToLower(key) == lower {
				return value
			}
		}
		return ""
	}
	for {
		lower := strings.ToLower(text)
		start := strings.Index(lower, "${env:")
		if start < 0 {
			break
		}
		endRel := strings.IndexByte(text[start:], '}')
		if endRel < 0 {
			break
		}
		end := start + endRel
		text = text[:start] + replaceEnv(text[start+6:end]) + text[end+1:]
	}
	for _, prefix := range []string{"$env:"} {
		for {
			lower := strings.ToLower(text)
			start := strings.Index(lower, prefix)
			if start < 0 {
				break
			}
			end := start + len(prefix)
			for end < len(text) && (text[end] == '_' || text[end] >= '0' && text[end] <= '9' ||
				text[end] >= 'A' && text[end] <= 'Z' || text[end] >= 'a' && text[end] <= 'z') {
				end++
			}
			text = text[:start] + replaceEnv(text[start+len(prefix):end]) + text[end:]
		}
	}
	autos := map[string]string{"HOME": opts.Home, "PWD": opts.CWD, "PSHOME": filepath.Dir(opts.Shell)}
	for key, value := range autos {
		for _, spelling := range []string{"$" + key, "$" + strings.ToLower(key)} {
			text = strings.ReplaceAll(text, spelling+"/", value+"/")
			text = strings.ReplaceAll(text, spelling+`\`, value+`\`)
			if text == spelling {
				text = value
			}
		}
	}
	return expandHome(text, opts.Home)
}

func filesystemProvider(text string) string {
	if i := strings.Index(text, "::"); i > 0 {
		if strings.EqualFold(text[:i], "filesystem") {
			return text[i+2:]
		}
		return ""
	}
	if i := strings.IndexByte(text, ':'); i > 0 {
		if i == 1 {
			return text
		}
		return ""
	}
	return text
}

func dynamicShellPath(text string, ps bool) bool {
	if strings.HasPrefix(text, "(") || strings.HasPrefix(text, "@(") ||
		strings.Contains(text, "$(") || strings.Contains(text, "${") ||
		strings.Contains(text, "`") {
		return true
	}
	if ps {
		for i := 0; i < len(text); i++ {
			if text[i] == '$' && !strings.HasPrefix(strings.ToLower(text[i:]), "$env:") {
				return true
			}
		}
		return false
	}
	return strings.Contains(text, "$")
}

func globPrefix(text string) string {
	for i, r := range text {
		if r == '?' || r == '*' || r == '[' {
			if i == 0 {
				return ""
			}
			return text[:i]
		}
	}
	return text
}

func pathWithin(candidate, root string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// splitShellCommands preserves simple-command source while respecting quoted
// separators and nested substitutions. Redirections remain attached to the
// command they belong to.
func splitShellCommands(text string) []string {
	var out []string
	start := 0
	quote := byte(0)
	escaped := false
	paren, brace := 0, 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		switch c {
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case '{':
			brace++
		case '}':
			if brace > 0 {
				brace--
			}
		}
		if paren != 0 || brace != 0 {
			continue
		}
		separator := c == '\n' || c == ';' || c == '|'
		if c == '&' {
			separator = i+1 < len(text) && text[i+1] == '&'
		}
		if !separator {
			continue
		}
		if part := strings.TrimSpace(text[start:i]); part != "" {
			out = append(out, part)
		}
		if i+1 < len(text) && text[i+1] == c && (c == '|' || c == '&') {
			i++
		}
		start = i + 1
	}
	if part := strings.TrimSpace(text[start:]); part != "" {
		out = append(out, part)
	}
	return out
}

func shellWords(text string) []string {
	var words []string
	var b strings.Builder
	quote := byte(0)
	escaped := false
	flush := func() {
		if b.Len() > 0 {
			words = append(words, b.String())
			b.Reset()
		}
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' && quote != '\'' {
			b.WriteByte(c)
			escaped = true
			continue
		}
		if quote != 0 {
			b.WriteByte(c)
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			b.WriteByte(c)
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			flush()
			continue
		}
		if c == '>' || c == '<' {
			flush()
			break
		}
		b.WriteByte(c)
	}
	flush()
	return words
}

var oneTokenCommands = stringSet(
	"cat", "cd", "chmod", "chown", "cp", "echo", "env", "export", "grep",
	"kill", "killall", "ln", "ls", "mkdir", "mv", "ps", "pwd", "rm",
	"rmdir", "sleep", "source", "tail", "touch", "unset", "which",
)

var threeTokenPrefixes = stringSet(
	"bun run", "bun x", "cargo add", "cargo run", "consul kv", "docker builder",
	"docker compose", "docker container", "docker image", "docker network",
	"docker volume", "eksctl create", "ip addr", "ip link", "ip netns",
	"ip route", "kind create", "kubectl kustomize", "kubectl rollout",
	"mc admin", "npm exec", "npm init", "npm run", "npm view", "openssl req",
	"openssl x509", "pnpm dlx", "pnpm exec", "pnpm run", "podman container",
	"podman image", "pulumi stack", "terraform workspace", "vault auth",
	"vault kv", "yarn dlx", "yarn run",
)

var twoTokenCommands = stringSet(
	"bazel", "brew", "bun", "cargo", "cdk", "cf", "cmake", "composer",
	"consul", "crictl", "deno", "docker", "eksctl", "firebase", "flyctl",
	"git", "go", "gradle", "helm", "heroku", "hugo", "ip", "kind",
	"kubectl", "kustomize", "make", "mc", "minikube", "mongosh", "mysql",
	"mvn", "ng", "npm", "nvm", "nx", "openssl", "pip", "pipenv", "pnpm",
	"poetry", "podman", "psql", "pulumi", "pyenv", "python", "rake",
	"rbenv", "redis-cli", "rustup", "serverless", "skaffold", "sls", "sst",
	"swift", "systemctl", "terraform", "tmux", "turbo", "ufw", "vault",
	"vercel", "volta", "wp", "yarn",
)

var threeTokenCommands = stringSet("aws", "az", "doctl", "gcloud", "gh", "sfdx")

func shellArityPrefix(tokens []string) []string {
	if len(tokens) == 0 {
		return []string{}
	}
	arity := 1
	if oneTokenCommands[tokens[0]] {
		arity = 1
	} else if threeTokenCommands[tokens[0]] {
		arity = 3
	} else if twoTokenCommands[tokens[0]] {
		arity = 2
	}
	if len(tokens) >= 2 && threeTokenPrefixes[tokens[0]+" "+tokens[1]] {
		arity = 3
	}
	if arity > len(tokens) {
		arity = len(tokens)
	}
	return append([]string(nil), tokens[:arity]...)
}

type orderedStrings struct {
	seen map[string]bool
	list []string
}

func newOrderedStrings() *orderedStrings { return &orderedStrings{seen: map[string]bool{}} }
func (s *orderedStrings) Add(value string) {
	if !s.seen[value] {
		s.seen[value] = true
		s.list = append(s.list, value)
	}
}
func (s *orderedStrings) Values() []string {
	out := make([]string, len(s.list))
	copy(out, s.list)
	return out
}

func stringSet(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
