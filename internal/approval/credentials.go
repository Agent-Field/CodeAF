package approval

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
)

// ── the credential floor ────────────────────────────────────────────────────
//
// AN AGENT DOES NOT READ THE PERSON'S CREDENTIALS, IN ANY APPROVAL MODE.
//
// The other floors in this package are questions. This one is a refusal, because
// a crew running under the default allow has nobody to answer a question, and a
// person who did answer would be pasting their login into a transcript. It sits
// above every policy, memo, guardian and remembered rule. The engine's own
// GitHub calls and the person's own `!` commands do not come through this door.
//
// It stops an agent that runs the ordinary command for the job, and it stops the
// cheap disguises of that command. IT IS NOT A SANDBOX. It reads text before a
// shell runs it, so a compiled program, a script called from a Makefile, or a
// path assembled at run time can still reach whatever this user account can
// reach. Only an operating-system boundary closes those.

// CredentialRead is one call the floor refused and what the call was reaching for.
type CredentialRead struct {
	// What names the command or file, in the words the call used.
	What string
}

// Message is what the model is told. It is written to end the attempt: a refusal
// that only says no reads as a broken route, and a model looks for another one.
func (c CredentialRead) Message() string {
	return "Blocked: this call reaches for a credential (a login, key or token): " + c.What + ". Agents cannot read credentials in any approval mode, and this is not a fault to work around. " +
		"Do not try another route: a script, an encoded command, a copy of the file, or another tool will be treated the same way. " +
		"You do not need the secret to do the work: gh and git sign themselves in, so run them normally (`gh pr list`, `git push`). " +
		"If this work really needs the credential, stop and tell the person what it is for; they can run it themselves with `!` in front of the command."
}

// CredentialScope is where the floor is standing: whose home it protects, which
// folders hold this program's own keys, and what a relative path starts from.
type CredentialScope struct {
	// Dir is what a relative path means. Empty leaves the path relative, which
	// the file table can still read because most of its rows are a path's tail.
	Dir string

	home     []string
	appHomes []string
}

// DefaultCredentialScope is the floor on this machine, standing in dir.
func DefaultCredentialScope(dir string) CredentialScope {
	scope := CredentialScope{Dir: dir}
	person, _ := os.UserHomeDir()
	if person != "" {
		scope.home = bothSpellings(person)
		scope.appHomes = append(scope.appHomes, bothSpellings(home.DefaultUnder(person))...)
		scope.appHomes = append(scope.appHomes, bothSpellings(filepath.Join(person, ".aforge"))...) // legacy-name
	}
	scope.appHomes = append(scope.appHomes, bothSpellings(home.Dir())...)
	// The name is config.ProfileDirEnv. This package cannot import internal/config,
	// which imports it, and a test in internal/session pins the two together.
	if profile := env.Get("CODEAF_PROFILE_DIR"); strings.TrimSpace(profile) != "" {
		scope.appHomes = append(scope.appHomes, bothSpellings(profile)...)
	}
	return scope
}

// bothSpellings is a folder as written and as the filesystem resolves it, because
// a link followed by the floor arrives under the resolved name.
func bothSpellings(folder string) []string {
	absolute, err := filepath.Abs(folder)
	if err != nil {
		absolute = folder
	}
	spellings := []string{normalLower(absolute)}
	if real, err := filepath.EvalSymlinks(absolute); err == nil && normalLower(real) != spellings[0] {
		spellings = append(spellings, normalLower(real))
	}
	return spellings
}

func normalLower(value string) string {
	return strings.ToLower(path.Clean(strings.ReplaceAll(value, `\`, "/")))
}

// Read reports whether one tool call reaches for a credential. It reads the
// arguments of ANY tool rather than a list of tools that read files, so that a
// tool added next month cannot forget to be on the list.
func (s CredentialScope) Read(tool string, args json.RawMessage) (CredentialRead, bool) {
	if strings.TrimSpace(tool) == ToolBash {
		command, ok := bashCommand(args)
		if !ok {
			return CredentialRead{}, false
		}
		return s.command(command)
	}
	var value any
	if json.Unmarshal(args, &value) != nil {
		return CredentialRead{}, false
	}
	return s.walkArguments("", value)
}

// File reports whether one path is a credential file, for a reader that walks a
// tree and is judging the files it found rather than the path it was given.
func (s CredentialScope) File(name string) (string, bool) {
	return s.pathHit(name, s.Dir, nil)
}

func (s CredentialScope) walkArguments(key string, value any) (CredentialRead, bool) {
	switch typed := value.(type) {
	case string:
		if shellArgumentKeys[strings.ToLower(key)] {
			return s.command(typed)
		}
		if what, hit := s.pathHit(typed, s.Dir, nil); hit {
			return CredentialRead{What: "the file " + what}, true
		}
	case []any:
		for _, item := range typed {
			if read, hit := s.walkArguments(key, item); hit {
				return read, true
			}
		}
	case map[string]any:
		for name, item := range typed {
			if read, hit := s.walkArguments(name, item); hit {
				return read, true
			}
		}
	}
	return CredentialRead{}, false
}

// shellArgumentKeys are the argument names that hold a command line, on the tools
// that are a shell under another name: jobs, watch, keep-running and the rest.
var shellArgumentKeys = map[string]bool{"command": true, "cmd": true, "job_command": true, "check": true, "script": true}

// ── which environment variables are secrets ─────────────────────────────────

var secretEnvExact = map[string]bool{"AWS_ACCESS_KEY_ID": true, "TOKEN": true, "SECRET": true, "PASSWORD": true, "API_KEY": true, "APIKEY": true}

var secretEnvSuffixes = []string{"_TOKEN", "_SECRET", "_PASSWORD", "_PASSWD", "_PASSPHRASE", "_API_KEY", "_APIKEY", "_SECRET_KEY", "_ACCESS_KEY", "_PRIVATE_KEY", "_CREDENTIAL", "_CREDENTIALS", "_AUTH_CONFIG"}

// SecretEnvName reports whether a variable's NAME says it holds a credential. It
// reads the shape of the name rather than a list of services, for the reason
// internal/redact reads the shape of a value: the next provider is covered on
// the day it launches. It is a name test, so SSH_AUTH_SOCK, which is how a shell
// reaches the agent without holding a key, stays out of it.
func SecretEnvName(name string) bool {
	name = strings.ToUpper(strings.TrimSpace(name))
	if secretEnvExact[name] {
		return true
	}
	for _, suffix := range secretEnvSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// ── which files are credentials ─────────────────────────────────────────────

// credentialTails are files judged by the end of their path, so they are caught
// wherever they stand: `../../.aws/credentials`, another account's home, or a
// home this process cannot name. Each element is a glob over one path component,
// matched in lower case because two of the three platforms ignore case.
var credentialTails = splitTails(
	".netrc", "_netrc",
	".git-credentials", ".config/git/credentials",
	".aws/credentials", ".aws/sso/cache/*", ".aws/cli/cache/*",
	".config/gh/hosts.yml", "github cli/hosts.yml", ".config/hub", ".config/glab-cli/config.yml",
	".docker/config.json", ".kube/*",
	".config/gcloud/credentials.db", ".config/gcloud/access_tokens.db", ".config/gcloud/application_default_credentials.json",
	".config/gcloud/legacy_credentials/*/*",
	".azure/accesstokens.json", ".azure/msal_token_cache*", ".azure/service_principal_entries*",
	".gnupg/secring.gpg", ".cargo/credentials", ".cargo/credentials.toml", ".gem/credentials",
	".vault-token", ".terraform.d/credentials.tfrc.json", ".config/rclone/rclone.conf", ".config/doctl/config.yaml",
	".codex/auth.json", ".claude/.credentials.json",
	".mozilla/firefox/*/logins.json", ".mozilla/firefox/*/key4.db", "user data/*/login data", "google/chrome/*/login data",
	"microsoft/credentials/*", "microsoft/protect/*/*",
	"/proc/*/environ", "/proc/*/mem", "/etc/shadow", "/etc/gshadow",
)

func splitTails(tails ...string) [][]string {
	out := make([][]string, 0, len(tails))
	for _, tail := range tails {
		out = append(out, strings.Split(tail, "/"))
	}
	return out
}

// credentialFolders hold nothing but secrets at any depth below them.
var credentialFolders = splitTails(".password-store", "library/keychains", ".local/share/keyrings", ".gnupg/private-keys-v1.d")

// homeCredentials are secrets only when they stand directly in somebody's home.
// A project's own `.npmrc` usually names a registry, and refusing to read it would
// refuse ordinary work, so the person's global one is what this table protects.
var homeCredentials = map[string]bool{".npmrc": true, ".yarnrc.yml": true, ".pypirc": true, ".pgpass": true, ".my.cnf": true, ".boto": true, ".s3cfg": true, ".dockercfg": true}

// appCredentials are this program's own files that hold keys, directly in one of
// its state folders. A repository's own `.codeaf/config.json` is not in one, so
// it stays readable. Config backups match too: they hold the same keys.
var appCredentials = []string{
	"config.json", "config.json.*", "vault.*", "identity.json", "identity.json.*", "identity-solo", "device.json",
	"credentials.json", "credentials.json.*", "crew-credentials.json", "codex.json", "tokens.json", "operator.json",
	"github-grant.json", "provider-keys.ledger",
}

// sshReadable is what `.ssh` holds besides keys, which the ordinary work of SSH
// needs to read. Anything else in there is treated as a key, because a private
// key can have any name.
var sshReadable = map[string]bool{"config": true, "known_hosts": true, "known_hosts.old": true, "authorized_keys": true, "authorized_keys2": true}

// holderFolders are the folders that contain credential files, for the commands
// that read a whole tree. Reading one of those is reading what is in it.
var holderFolders = splitTails(".ssh", ".aws", ".kube", ".gnupg", ".docker", ".azure", ".password-store", ".config", ".config/gh", ".config/gcloud", "library/keychains", ".local/share/keyrings")

func tailMatches(components, tail []string) bool {
	anchored := tail[0] == ""
	if anchored {
		tail = tail[1:]
		if len(components) != len(tail) {
			return false
		}
	}
	if len(components) < len(tail) {
		return false
	}
	offset := len(components) - len(tail)
	for index, pattern := range tail {
		if matched, _ := path.Match(pattern, components[offset+index]); !matched {
			return false
		}
	}
	return true
}

func (s CredentialScope) isCredentialFile(clean string) bool {
	components := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	absolute := strings.HasPrefix(clean, "/")
	for _, tail := range credentialTails {
		if tail[0] == "" && !absolute {
			continue
		}
		if tailMatches(components, tail) {
			return true
		}
	}
	for _, folder := range credentialFolders {
		for end := len(folder); end < len(components); end++ {
			if tailMatches(components[:end], folder) {
				return true
			}
		}
	}
	for index, component := range components {
		if component != ".ssh" || index == len(components)-1 {
			continue
		}
		rest := components[index+1:]
		if rest[0] == "config.d" || rest[0] == "sockets" {
			continue
		}
		if len(rest) > 1 || (!sshReadable[rest[0]] && !strings.HasSuffix(rest[0], ".pub")) {
			return true
		}
	}
	name := components[len(components)-1]
	if homeCredentials[name] && s.isSomeonesHome(path.Dir(clean)) {
		return true
	}
	for _, app := range s.appHomes {
		rest, inside := strings.CutPrefix(clean, app+"/")
		if !inside {
			continue
		}
		parts := strings.Split(rest, "/")
		if strings.HasPrefix(parts[0], "settings-backup-") {
			return true
		}
		if len(parts) != 1 {
			continue
		}
		for _, pattern := range appCredentials {
			if matched, _ := path.Match(pattern, parts[0]); matched {
				return true
			}
		}
	}
	return false
}

func (s CredentialScope) isSomeonesHome(folder string) bool {
	for _, person := range s.home {
		if folder == person {
			return true
		}
	}
	if folder == "/root" {
		return true
	}
	parent := path.Dir(folder)
	return parent == "/home" || parent == "/users"
}

// holdsCredentials reports whether a folder contains credentials somewhere below
// it, and whether the reason is only that it is a home or a filesystem root,
// which a search that skips hidden files passes through safely.
func (s CredentialScope) holdsCredentials(clean string) (holds, viaHome bool) {
	if clean == "/" || clean == "/home" || clean == "/users" || s.isSomeonesHome(clean) {
		return true, true
	}
	for _, app := range s.appHomes {
		if clean == app {
			return true, false
		}
	}
	components := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	for _, tail := range holderFolders {
		if tailMatches(components, tail) {
			return true, false
		}
	}
	return false, false
}

// ── reading a path the way the shell will ───────────────────────────────────

var shellVariable = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)

// expand writes a path out as far as text alone can. A variable it cannot place
// stays in the word, where the file table's globs can still see the rest.
func (s CredentialScope) expand(word string, vars map[string]string) string {
	person, _ := os.UserHomeDir()
	if word == "~" || strings.HasPrefix(word, "~/") || strings.HasPrefix(word, `~\`) {
		word = person + word[1:]
	}
	word = strings.NewReplacer("%USERPROFILE%", person, "%APPDATA%", person+"/AppData/Roaming", "%LOCALAPPDATA%", person+"/AppData/Local").Replace(word)
	return shellVariable.ReplaceAllStringFunc(word, func(found string) string {
		name := shellVariable.FindStringSubmatch(found)[1]
		if value, known := vars[name]; known {
			return value
		}
		switch name {
		case "HOME", "USERPROFILE":
			return person
		case "CODEAF_HOME":
			return home.Dir()
		}
		return found
	})
}

// isPathShaped keeps a sentence out of the file table. A word with spaces before
// its first slash is prose or a command line handed to another program, as in
// `core.sshCommand=ssh -i ~/.ssh/key`, and not a file this call opens.
func isPathShaped(word string) bool {
	if word == "" || strings.ContainsAny(word, "\n\r") {
		return false
	}
	head := word
	if at := strings.IndexAny(word, `/\`); at >= 0 {
		head = word[:at]
	}
	return !strings.ContainsAny(head, " \t")
}

// pathHit judges one word as a path: as written, as its glob expands, and as the
// links under it resolve. It answers the word as it was written, for the refusal.
func (s CredentialScope) pathHit(word, cwd string, vars map[string]string) (string, bool) {
	word = strings.TrimSpace(word)
	if !isPathShaped(word) {
		return "", false
	}
	expanded := s.expand(word, vars)
	if !isPathShaped(expanded) {
		return "", false
	}
	full := joinTo(cwd, expanded)
	if s.isCredentialFile(normalLower(full)) {
		return word, true
	}
	matches := []string{full}
	if strings.ContainsAny(full, "*?[") {
		found, _ := filepath.Glob(full)
		matches = found
	}
	for _, match := range matches {
		if s.isCredentialFile(normalLower(match)) {
			return word, true
		}
		if real, err := filepath.EvalSymlinks(match); err == nil && s.isCredentialFile(normalLower(real)) {
			return word, true
		}
	}
	return "", false
}

func joinTo(cwd, name string) string {
	slashed := strings.ReplaceAll(name, `\`, "/")
	if cwd == "" || strings.HasPrefix(slashed, "/") || (len(slashed) > 1 && slashed[1] == ':') {
		return name
	}
	return filepath.Join(cwd, name)
}

// holderHit judges one word as a tree a command is about to read whole.
func (s CredentialScope) holderHit(word, cwd string, vars map[string]string, skipsHidden bool) (string, bool) {
	if !isPathShaped(word) {
		return "", false
	}
	full := joinTo(cwd, s.expand(word, vars))
	spellings := []string{normalLower(full)}
	if real, err := filepath.EvalSymlinks(full); err == nil {
		spellings = append(spellings, normalLower(real))
	}
	for _, spelling := range spellings {
		if holds, viaHome := s.holdsCredentials(spelling); holds && !(viaHome && skipsHidden) {
			return word, true
		}
	}
	return "", false
}

// ── reading a command line ──────────────────────────────────────────────────

// wrapperFlagsWithValue are the wrappers' flags that swallow the next word.
var wrapperFlagsWithValue = map[string]map[string]bool{
	"sudo":    {"-u": true, "-g": true, "-h": true, "-p": true, "-C": true},
	"doas":    {"-u": true},
	"env":     {"-u": true, "-C": true, "-S": true},
	"nice":    {"-n": true},
	"ionice":  {"-c": true, "-n": true, "-p": true},
	"timeout": {"-s": true, "-k": true},
	"xargs":   {"-I": true, "-n": true, "-P": true, "-L": true, "-d": true, "-E": true, "-s": true},
	"watch":   {"-n": true, "-d": false},
}

// wrappers run the command that follows them, so the next word is the command.
var wrappers = map[string]bool{
	"sudo": true, "doas": true, "env": true, "command": true, "builtin": true, "exec": true, "nohup": true, "nice": true,
	"ionice": true, "time": true, "timeout": true, "stdbuf": true, "setsid": true, "caffeinate": true, "unbuffer": true,
	"xargs": true, "busybox": true, "watch": true, "parallel": true, "then": true, "else": true, "do": true, "if": true,
	"while": true, "until": true, "!": true, "elif": true,
}

var declarations = map[string]bool{"export": true, "declare": true, "typeset": true, "local": true, "readonly": true}

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "ash": true, "fish": true}

// inlineCodeFlags are the flags that hand an interpreter its program as a word.
var interpreters = map[string]bool{
	"python": true, "python2": true, "python3": true, "pypy": true, "pypy3": true, "node": true, "nodejs": true, "deno": true,
	"bun": true, "ruby": true, "perl": true, "php": true, "lua": true, "osascript": true, "tsx": true, "ts-node": true,
}

var windowsShells = map[string]bool{"powershell": true, "pwsh": true, "cmd": true}

func commandBase(word string) string {
	word = strings.ReplaceAll(word, `\`, "/")
	base := strings.ToLower(path.Base(word))
	return strings.TrimSuffix(base, ".exe")
}

// unwrap walks past what introduces a command, so `sudo env -i timeout 5 gh auth
// token` is read as the `gh auth token` it runs.
func unwrap(words []string, vars map[string]string, s CredentialScope) []string {
	for len(words) > 0 {
		base := commandBase(words[0])
		if name, value, ok := shellAssignment(words[0]); ok {
			vars[name] = s.expand(value, vars)
			words = words[1:]
			continue
		}
		if declarations[base] {
			for _, word := range words[1:] {
				if name, value, ok := shellAssignment(word); ok {
					vars[name] = s.expand(value, vars)
				}
			}
			return nil
		}
		if !wrappers[base] {
			return words
		}
		flags := wrapperFlagsWithValue[base]
		words = words[1:]
		for len(words) > 0 {
			word := words[0]
			if strings.HasPrefix(word, "-") {
				words = words[1:]
				if flags[word] && len(words) > 0 {
					words = words[1:]
				}
				continue
			}
			if (base == "timeout" || base == "nice") && len(word) > 0 && unicode.IsDigit(rune(word[0])) {
				words = words[1:]
				continue
			}
			break
		}
	}
	return nil
}

func shellAssignment(word string) (string, string, bool) {
	at := strings.Index(word, "=")
	if at <= 0 {
		return "", "", false
	}
	for index, letter := range word[:at] {
		if letter != '_' && !unicode.IsLetter(letter) && (index == 0 || !unicode.IsDigit(letter)) {
			return "", "", false
		}
	}
	return word[:at], word[at+1:], true
}

// withoutFlags is a command's words past its own flags.
func withoutFlags(words []string) []string {
	kept := make([]string, 0, len(words))
	for _, word := range words {
		if !strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "/") {
			kept = append(kept, word)
		}
	}
	return kept
}

func anyWord(words []string, wanted ...string) bool {
	for _, word := range words {
		for _, want := range wanted {
			if strings.EqualFold(word, want) {
				return true
			}
		}
	}
	return false
}

func firstIs(words []string, wanted ...string) bool {
	return len(words) > 0 && anyWord(words[:1], wanted...)
}

// credentialVerb recognises the commands that exist to hand a secret over. The
// answer is the command's own name for the refusal, or "" when it does not.
func credentialVerb(head string, rest []string) string {
	base := commandBase(head)
	verbs := withoutFlags(rest)
	said := func(words ...string) string { return "`" + strings.Join(words, " ") + "`" }
	switch base {
	case "security":
		if firstIs(verbs, "find-generic-password", "find-internet-password", "find-key", "dump-keychain", "export", "dump-trust-settings") {
			return said(base, verbs[0])
		}
	case "secret-tool":
		if firstIs(verbs, "lookup", "search") {
			return said(base, verbs[0])
		}
	case "pass", "gopass":
		if len(verbs) > 0 && !anyWord(verbs[:1], "ls", "list", "init", "help", "version", "generate", "insert", "edit", "rm", "mv", "cp", "git") {
			return said(base, verbs[0])
		}
	case "kwallet-query":
		if anyWord(rest, "-r", "--read-password") {
			return said(base, "-r")
		}
	case "keyctl":
		if firstIs(verbs, "print", "read", "pipe") {
			return said(base, verbs[0])
		}
	case "gpg", "gpg2":
		if anyWord(rest, "--export-secret-keys", "--export-secret-subkeys") {
			return said(base, "--export-secret-keys")
		}
	case "cmdkey":
		for _, word := range rest {
			if strings.HasPrefix(strings.ToLower(word), "/l") {
				return said(base, "/list")
			}
		}
	case "vaultcmd":
		for _, word := range rest {
			if strings.HasPrefix(strings.ToLower(word), "/listcreds") {
				return said(base, "/listcreds")
			}
		}
	case "get-storedcredential", "get-vaultcredential":
		return said(base)
	case "gh", "glab":
		if firstIs(verbs, "auth") && len(verbs) > 1 {
			switch verbs[1] {
			case "token", "git-credential":
				return said(base, "auth", verbs[1])
			case "status":
				if anyWord(rest, "-t", "--show-token") {
					return said(base, "auth status --show-token")
				}
			}
		}
		if len(verbs) > 2 && verbs[0] == "config" && verbs[1] == "get" && strings.Contains(strings.ToLower(verbs[2]), "token") {
			return said(base, "config get", verbs[2])
		}
	case "git":
		sub := gitSubcommand(rest)
		if firstIs(sub, "credential") && len(sub) > 1 && sub[1] == "fill" {
			return said("git credential fill")
		}
		if len(sub) > 0 && strings.HasPrefix(sub[0], "credential-") {
			return said("git", sub[0])
		}
	case "gcloud":
		if anyWord(rest, "print-access-token", "print-identity-token") {
			return said("gcloud auth print-access-token")
		}
	case "aws":
		if len(verbs) > 1 && verbs[0] == "configure" {
			if verbs[1] == "export-credentials" {
				return said("aws configure export-credentials")
			}
			if verbs[1] == "get" && len(verbs) > 2 && regexp.MustCompile(`(?i)secret|token|access_key`).MatchString(verbs[2]) {
				return said("aws configure get", verbs[2])
			}
		}
		if anyWord(verbs, "get-login-password", "get-session-token", "get-authorization-token") {
			return said("aws", verbs[0], verbs[1])
		}
	case "az":
		if anyWord(verbs, "get-access-token") {
			return said("az account get-access-token")
		}
	case "kubectl", "oc":
		if len(verbs) > 1 && verbs[0] == "config" && verbs[1] == "view" && anyWord(rest, "--raw", "--flatten") {
			return said(base, "config view --raw")
		}
		if len(verbs) > 1 && verbs[0] == "get" && strings.HasPrefix(verbs[1], "secret") {
			return said(base, "get secret")
		}
	case "op":
		if firstIs(verbs, "read") || (len(verbs) > 1 && verbs[0] == "item" && verbs[1] == "get") {
			return said("op", verbs[0])
		}
	case "bw", "lpass":
		if firstIs(verbs, "get", "show") {
			return said(base, verbs[0])
		}
	case "npm":
		if len(verbs) > 1 && verbs[0] == "token" && (verbs[1] == "list" || verbs[1] == "ls" || verbs[1] == "create") {
			return said("npm token", verbs[1])
		}
	case "heroku":
		if firstIs(verbs, "auth:token") {
			return said("heroku auth:token")
		}
	case "vault":
		if firstIs(verbs, "read") || (len(verbs) > 1 && verbs[0] == "kv" && verbs[1] == "get") {
			return said("vault", verbs[0])
		}
	case "ps":
		for _, word := range rest {
			if word == "-E" || (!strings.HasPrefix(word, "-") && strings.Contains(word, "e") && !unicode.IsDigit(rune(word[0]))) {
				return said("ps", word)
			}
		}
	}
	if strings.HasPrefix(base, "git-credential-") || strings.HasPrefix(base, "docker-credential-") {
		return said(base)
	}
	return ""
}

func gitSubcommand(rest []string) []string {
	for index := 0; index < len(rest); index++ {
		word := rest[index]
		switch word {
		case "-c", "-C", "--git-dir", "--work-tree", "--namespace", "--exec-path":
			index++
			continue
		}
		if !strings.HasPrefix(word, "-") {
			return rest[index:]
		}
	}
	return nil
}

// credentialUsers run a key without showing it. SSH-based git is the point: the
// key is opened by ssh and only ever signs, so naming it is use and not a read.
var credentialUsers = map[string]bool{"ssh": true, "sftp": true, "sshfs": true, "ssh-add": true, "ssh-keygen": true, "ssh-copy-id": true}

// nameOnly commands handle a path without opening what is in it.
var nameOnly = map[string]bool{
	"ls": true, "stat": true, "test": true, "[": true, "[[": true, "du": true, "echo": true, "realpath": true, "dirname": true,
	"basename": true, "chmod": true, "chown": true, "dir": true, "exa": true, "eza": true, "tree": true,
}

var secretVariableWord = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)`)

func printsSecretVariable(base string, rest []string) string {
	switch base {
	case "printenv":
		for _, word := range rest {
			if SecretEnvName(word) {
				return "`printenv " + word + "`"
			}
		}
	case "echo", "printf", "print":
		for _, word := range rest {
			for _, found := range secretVariableWord.FindAllStringSubmatch(word, -1) {
				if SecretEnvName(found[1]) {
					return "printing $" + found[1]
				}
			}
		}
	}
	return ""
}

var heredocOpen = regexp.MustCompile(`<<-?[ \t]*['"\\]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)

// splitHeredocs takes heredoc bodies out of a line before it is read as commands.
// A body is data until something runs it, and `git commit -F - <<EOF` with a
// message that mentions a command is the commonest shape there is.
func splitHeredocs(command string) (string, []string) {
	lines := strings.Split(command, "\n")
	var kept, bodies []string
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		kept = append(kept, line)
		found := heredocOpen.FindStringSubmatch(line)
		if found == nil || strings.Contains(line, "<<<") {
			continue
		}
		var body []string
		for index+1 < len(lines) {
			index++
			if strings.TrimSpace(lines[index]) == found[1] {
				break
			}
			body = append(body, lines[index])
		}
		bodies = append(bodies, strings.Join(body, "\n"))
	}
	return strings.Join(kept, "\n"), bodies
}

var braceWord = regexp.MustCompile(`[^\s'"(){}|;&<>]*\{[^\s{}$]*,[^\s{}]*\}[^\s'"(){}|;&<>]*`)

// braceExpansions writes `~/.ssh/{id_rsa,config}` out as the two words the shell
// sees. The reader treats a brace as a boundary, which suits a group and not this.
func braceExpansions(command string) []string {
	var out []string
	for _, word := range braceWord.FindAllString(command, -1) {
		open, end := strings.Index(word, "{"), strings.Index(word, "}")
		if open < 0 || end < open {
			continue
		}
		for _, choice := range strings.Split(word[open+1:end], ",") {
			out = append(out, word[:open]+choice+word[end+1:])
		}
	}
	return out
}

// maxShellDepth bounds `sh -c "sh -c ..."`. Nobody writes that deep by accident,
// and an unreadable call at the bottom is judged on what was read above it.
const maxShellDepth = 6

// scriptReadLimit bounds the one file read this floor does before a script runs.
const scriptReadLimit = 256 << 10

func (s CredentialScope) command(command string) (CredentialRead, bool) {
	return s.shellLine(command, s.Dir, map[string]string{}, 0)
}

func hit(what string) (CredentialRead, bool) { return CredentialRead{What: what}, true }

func (s CredentialScope) shellLine(command, cwd string, vars map[string]string, depth int) (CredentialRead, bool) {
	if depth > maxShellDepth {
		return CredentialRead{}, false
	}
	line, bodies := splitHeredocs(command)
	segments := CommandWords(line)
	runsCode, decodes := false, false
	dumpsEnv, filtersSecrets := false, false
	var earlier []string
	for _, words := range segments {
		rawBase := ""
		if len(words) > 0 {
			rawBase = commandBase(words[0])
		}
		if rawBase == "base64" || rawBase == "openssl" || rawBase == "xxd" {
			decodes = true
		}
		runnable := unwrap(words, vars, s)
		if len(runnable) == 0 {
			// `env` on its own is a dump of the environment and not a wrapper.
			if rawBase == "env" {
				dumpsEnv = true
			}
			continue
		}
		head := s.expand(runnable[0], vars)
		rest := runnable[1:]
		base := commandBase(head)
		if shells[base] || interpreters[base] || windowsShells[base] {
			runsCode = true
		}
		if base == "base64" || base == "openssl" || base == "xxd" {
			decodes = true
		}
		if base == "printenv" || base == "set" || base == "compgen" {
			dumpsEnv = true
		}
		switch base {
		case "grep", "egrep", "fgrep", "rg", "ag", "awk", "sed":
			for _, word := range rest {
				if secretFilter.MatchString(word) {
					filtersSecrets = true
				}
			}
		}
		if base == "cd" || base == "pushd" {
			if len(rest) == 0 {
				person, _ := os.UserHomeDir()
				cwd = person
			} else {
				cwd = joinTo(cwd, s.expand(rest[0], vars))
			}
			continue
		}
		expanded := make([]string, len(rest))
		for index, word := range rest {
			expanded[index] = s.expand(word, vars)
		}
		if what := credentialVerb(head, expanded); what != "" {
			return hit(what)
		}
		if what := printsSecretVariable(base, rest); what != "" {
			return hit(what)
		}
		if read, found := s.runsOtherCode(base, head, expanded, cwd, vars, depth, earlier); found {
			return read, true
		}
		if read, found := s.segmentPaths(base, expanded, cwd, vars); found {
			return read, true
		}
		earlier = append(earlier, expanded...)
	}
	for _, word := range braceExpansions(line) {
		if what, found := s.pathHit(word, cwd, vars); found {
			return hit("the file " + what)
		}
	}
	if decodes {
		for _, words := range segments {
			for _, word := range words {
				if text, ok := decodedText(word); ok {
					if read, found := s.shellLine(text, cwd, vars, depth+1); found {
						return read, true
					}
				}
			}
		}
	}
	if dumpsEnv && filtersSecrets {
		return hit("searching the environment for secrets")
	}
	if runsCode {
		for _, body := range bodies {
			if read, found := s.scanCode(body, cwd, vars, depth); found {
				return read, true
			}
		}
	}
	return CredentialRead{}, false
}

var secretFilter = regexp.MustCompile(`(?i)token|secret|passw|api_?key|credential|\bkey\b`)

// decodedText reads a word as base64, when it is long enough to mean anything and
// decodes to words a shell could read.
func decodedText(word string) (string, bool) {
	if len(word) < 12 {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(word)
	if err != nil || !utf8.Valid(raw) {
		return "", false
	}
	for _, letter := range string(raw) {
		if !unicode.IsPrint(letter) && !unicode.IsSpace(letter) {
			return "", false
		}
	}
	return string(raw), true
}

// runsOtherCode follows a command line into the program that will run it.
func (s CredentialScope) runsOtherCode(base, head string, rest []string, cwd string, vars map[string]string, depth int, earlier []string) (CredentialRead, bool) {
	switch {
	case base == "eval":
		return s.shellLine(strings.Join(rest, " "), cwd, vars, depth+1)
	case base == "alias":
		for _, word := range rest {
			if _, value, ok := shellAssignment(word); ok {
				if read, found := s.shellLine(value, cwd, vars, depth+1); found {
					return read, true
				}
			}
		}
	case base == "find":
		for index, word := range rest {
			switch word {
			case "-exec", "-execdir", "-ok", "-okdir":
				return s.shellLine(strings.Join(rest[index+1:], " "), cwd, vars, depth+1)
			}
		}
	case shells[base]:
		for index, word := range rest {
			if strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.HasSuffix(word, "c") && index+1 < len(rest) {
				return s.shellLine(rest[index+1], cwd, vars, depth+1)
			}
		}
		script := withoutFlags(rest)
		if len(script) == 0 {
			// A shell with no program reads one from its input, which is whatever
			// the earlier stages of the pipeline said.
			if read, found := s.shellLine(strings.Join(earlier, " "), cwd, vars, depth+1); found {
				return read, true
			}
			for _, word := range earlier {
				if read, found := s.scanScript(word, cwd, vars, depth); found {
					return read, true
				}
			}
			return CredentialRead{}, false
		}
		if read, found := s.shellLine(strings.Join(script, " "), cwd, vars, depth+1); found {
			return read, true
		}
		return s.scanScript(script[0], cwd, vars, depth)
	case interpreters[base]:
		for index, word := range rest {
			if isInlineCodeFlag(base, word) && index+1 < len(rest) {
				return s.scanCode(rest[index+1], cwd, vars, depth)
			}
			if !strings.HasPrefix(word, "-") {
				return s.scanScript(word, cwd, vars, depth)
			}
		}
	case windowsShells[base]:
		// `-Command`, `-NoProfile` and `/c` introduce the program; the rest is it.
		program := rest
		for len(program) > 0 && (strings.HasPrefix(program[0], "-") || strings.HasPrefix(program[0], "/")) {
			program = program[1:]
		}
		code := strings.Join(program, " ")
		if read, found := s.shellLine(code, cwd, vars, depth+1); found {
			return read, true
		}
		return s.scanCode(code, cwd, vars, depth)
	case strings.Contains(head, "/"):
		return s.scanScript(head, cwd, vars, depth)
	}
	return CredentialRead{}, false
}

func isInlineCodeFlag(base, word string) bool {
	if !strings.HasPrefix(word, "-") {
		return false
	}
	switch word {
	case "--eval", "--print":
		return true
	}
	if strings.HasPrefix(word, "--") {
		return false
	}
	switch base {
	case "python", "python2", "python3", "pypy", "pypy3":
		return strings.HasSuffix(word, "c")
	case "php":
		return strings.HasSuffix(word, "r")
	case "node", "nodejs", "deno", "bun", "tsx", "ts-node":
		return strings.HasSuffix(word, "e") || strings.HasSuffix(word, "p")
	}
	return strings.HasSuffix(word, "e") || strings.HasSuffix(word, "E")
}

// scanScript reads a script before it runs. It is a speed bump: a script that
// imports another, or that a Makefile calls, is not read. But the plain case of a
// script written a moment ago is one bounded file read.
func (s CredentialScope) scanScript(name, cwd string, vars map[string]string, depth int) (CredentialRead, bool) {
	file, err := os.Open(joinTo(cwd, s.expand(name, vars)))
	if err != nil {
		return CredentialRead{}, false
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		return CredentialRead{}, false
	}
	body := make([]byte, scriptReadLimit)
	count, _ := file.Read(body)
	body = body[:count]
	if strings.ContainsRune(string(body), 0) {
		return CredentialRead{}, false
	}
	read, found := s.scanCode(string(body), cwd, vars, depth)
	if found {
		read.What += " (in the script " + name + ")"
	}
	return read, found
}

var (
	codeToken = regexp.MustCompile("[^\\s'\"`()\\[\\],;=<>|&]+")
	codeWord  = regexp.MustCompile(`[A-Za-z0-9_./:-]+`)
	envReader = regexp.MustCompile(`environ|getenv|Getenv|ENV\b|process\.env|\$env:`)
	envName   = regexp.MustCompile(`[A-Z][A-Z0-9_]{2,}`)
)

// codeVerbHeads are the commands distinctive enough to look for inside a program,
// where a word like `pass` or `op` would mean nothing at all.
var codeVerbHeads = map[string]bool{
	"security": true, "gh": true, "git": true, "gcloud": true, "aws": true, "az": true, "kubectl": true,
	"secret-tool": true, "kwallet-query": true, "cmdkey": true, "vaultcmd": true,
}

// scanCode reads a program as text. It cannot tell code from a comment or a
// string, so it only looks for shapes that mean one thing: a credential's path,
// a command that hands one over, or an environment read of a secret's name.
func (s CredentialScope) scanCode(code, cwd string, vars map[string]string, depth int) (CredentialRead, bool) {
	tokens := codeToken.FindAllString(code, -1)
	kept := tokens[:0:0]
	for _, token := range tokens {
		if strings.Trim(token, "/+") != "" {
			kept = append(kept, token)
		}
	}
	for index, token := range kept {
		if what, found := s.pathHit(strings.TrimRight(token, ".:"), cwd, vars); found {
			return hit("the file " + what)
		}
		// `Path.home()/'.aws'/'credentials'` and `join(home, '.ssh', 'id_rsa')`
		// spell one path across several tokens.
		if strings.HasPrefix(strings.TrimLeft(token, "/"), ".") {
			joined := strings.TrimLeft(token, "/")
			for next := index + 1; next < len(kept) && next <= index+2; next++ {
				joined += "/" + strings.TrimLeft(kept[next], "/")
				if what, found := s.pathHit(joined, "", vars); found {
					return hit("the file " + what)
				}
			}
		}
	}
	words := codeWord.FindAllString(code, -1)
	for index, word := range words {
		base := commandBase(word)
		if !codeVerbHeads[base] && !strings.HasPrefix(base, "git-credential-") && !strings.HasPrefix(base, "docker-credential-") {
			continue
		}
		end := min(index+7, len(words))
		if what := credentialVerb(word, words[index+1:end]); what != "" {
			return hit(what)
		}
	}
	if envReader.MatchString(code) {
		for _, name := range envName.FindAllString(code, -1) {
			if SecretEnvName(name) {
				return hit("reading $" + name + " from the environment")
			}
		}
	}
	return CredentialRead{}, false
}

// ── the paths in one command ────────────────────────────────────────────────

func (s CredentialScope) segmentPaths(base string, rest []string, cwd string, vars map[string]string) (CredentialRead, bool) {
	if credentialUsers[base] {
		return CredentialRead{}, false
	}
	skipNext := false
	checkAll := !nameOnly[base]
	if checkAll {
		for _, word := range rest {
			if skipNext {
				skipNext = false
				continue
			}
			if base == "scp" && word == "-i" {
				skipNext = true
				continue
			}
			for _, candidate := range pathCandidates(word) {
				if what, found := s.pathHit(candidate, cwd, vars); found {
					return hit("the file " + what)
				}
			}
		}
	}
	return s.treeRead(base, rest, cwd, vars)
}

// pathCandidates are the ways one word can name a file: itself, the value of a
// `--flag=file`, curl's `@file`, and a `file://` address.
func pathCandidates(word string) []string {
	candidates := []string{word}
	if at := strings.LastIndex(word, "="); at >= 0 {
		candidates = append(candidates, word[at+1:])
	}
	out := candidates[:0:0]
	for _, candidate := range candidates {
		trimmed := strings.TrimPrefix(strings.TrimPrefix(candidate, "@"), "file://")
		if strings.HasPrefix(trimmed, "-") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// treeRead is the commands that read a folder whole. They are refused on a folder
// that holds credentials even though the folder itself is not one.
func (s CredentialScope) treeRead(base string, rest []string, cwd string, vars map[string]string) (CredentialRead, bool) {
	skipsHidden := false
	patternFirst := false
	reads := false
	switch base {
	case "grep", "egrep", "fgrep", "zgrep":
		patternFirst = true
		for _, word := range rest {
			if word == "--recursive" || word == "--dereference-recursive" || (strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.ContainsAny(word, "rR")) {
				reads = true
			}
		}
	case "rg", "ag", "ack":
		patternFirst, reads, skipsHidden = true, true, true
		for _, word := range rest {
			if word == "--hidden" || word == "-." || strings.HasPrefix(word, "-uu") || word == "--unrestricted" {
				skipsHidden = false
			}
		}
	case "find":
		reads = anyWord(rest, "-exec", "-execdir", "-ok", "-okdir")
	case "tar", "zip", "7z", "rsync", "rclone", "cpio", "scp":
		reads = true
	case "cp":
		for _, word := range rest {
			if word == "--recursive" || word == "--archive" || (strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.ContainsAny(word, "rRa")) {
				reads = true
			}
		}
	}
	if !reads {
		return CredentialRead{}, false
	}
	var targets []string
	for index := 0; index < len(rest); index++ {
		word := rest[index]
		if word == "-e" && patternFirst {
			patternFirst = false
			index++
			continue
		}
		if strings.HasPrefix(word, "-") {
			continue
		}
		if patternFirst {
			patternFirst = false
			continue
		}
		targets = append(targets, word)
	}
	if len(targets) == 0 && base != "tar" && base != "zip" && base != "find" {
		targets = []string{"."}
	}
	for _, target := range targets {
		if what, found := s.holderHit(target, cwd, vars, skipsHidden); found {
			return hit("the folder " + what + ", which holds credentials (name a narrower folder to search it)")
		}
	}
	return CredentialRead{}, false
}
