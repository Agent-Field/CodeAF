package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ── THE FENCE: WHAT AN UNATTENDED AGENT'S COMMANDS MAY TOUCH ────────────────
//
// A factory step, a `codeaf chat --once` run and a `codeaf exec` run are an
// agent working with nobody at the keyboard. Before the fence their shell ran
// with every permission the person has: on one machine a step ran
// `brew install zig` (1.6 GB) and pulled docker images nobody asked for, and
// nothing stopped one reading ~/.ssh or writing anywhere in the home folder.
//
// The fence is the [Fenced] class's policy. It differs from [Sandboxed] in the
// one way that work needs: THE NETWORK STAYS ON, because a step fetches
// modules, clones and calls APIs. What it takes away is the filesystem beyond
// the job:
//
//   - WRITES land only in the checkout, the repository's own git folder (so a
//     worktree can commit), the job's own temp and cache folders, the device
//     sinks, and whatever the person allowed. A brew prefix, ~/Library, the
//     rest of the home folder: refused.
//   - READS are open (toolchains under /usr, /opt/homebrew, Xcode and the Go
//     root must work), EXCEPT the hidden list: keys and sign-ins (~/.ssh,
//     ~/.aws, ~/.config/gh and their kin), codeaf's own secret files, and the
//     keychain.
//   - THE DOCKER SOCKET is refused, because a container is a way out of every
//     limit above.
//   - THE REPOSITORY'S HOOKS, ITS GIT CONFIG AND ITS .codeaf FOLDER ARE
//     READ-ONLY, because each of them is something the person's own unconfined
//     git or codeaf runs later: a step that could write a pre-commit hook, a
//     core.hooksPath or a wider jail setting would leave the fence on the next
//     run without ever breaking it on this one.
//
// A FENCE THAT CANNOT BE APPLIED IS SAID ONCE AND THE WORK RUNS AS IT ALWAYS
// DID. Network-denying classes fail closed because no-network is a promise the
// result's side effect relies on; the fence makes no such promise, and a
// machine without Landlock must not fail every step it is asked to run.

// FenceOptions is what a door knows when it builds a fence.
type FenceOptions struct {
	// Root is the job's checkout or working directory: read and write.
	Root string
	// Home is the person's home folder; "" reads it from the system.
	Home string
	// CodeafHome is codeaf's own folder (CODEAF_HOME), whose secret files are
	// hidden; "" hides none of them.
	CodeafHome string
	// OtherCodeafHomes are further codeaf folders on this machine whose secret
	// files are hidden too: the person's own default folder when this run's
	// state lives in another one, as the desktop app's does, so the command
	// line's keys and sign-ins stay out of reach of the app's unattended runs.
	OtherCodeafHomes []string
	// StateDir is where the job's own temp and cache folders are made, one
	// pair per checkout. It must not be inside a hidden folder.
	StateDir string
	// Allow is the person's widening, as [ParseFenceAllow] reads it.
	Allow FenceAllow
	// Receipts are codeaf's own record folders the step writes from inside
	// the fence: a factory step's check receipts, beside its transcript in
	// codeaf's home (verification.ReceiptRoot). Each is made if missing and
	// is writable; nothing else of codeaf's home is.
	Receipts []string
	// Canvases are the desktop app's canvas folders ($WORKAF_CANVAS_ROOT, or
	// the app's default), writable when they exist and never made: the
	// `canvas` command a run types writes its canvases there, and the app
	// shows only what is there. A machine without the app has none.
	Canvases []string
}

// FenceAllow is how far a person widened the fence. Paths are absolute and
// both readable and writable, including a path the fence would hide.
type FenceAllow struct {
	Off    bool
	Docker bool
	Paths  []string
}

// Fence is one job's confinement, built by [NewFence]. Every path is canonical.
type Fence struct {
	Root     string
	Tmp      string
	Cache    string
	Writable []string // beyond Root, Tmp and Cache
	ReadOnly []string // under a writable folder, kept read-only
	Hidden   []string // neither read nor written
	Sockets  []string // container sockets refused
	// Blank is an empty file nobody may read, mounted over a hidden file on Linux.
	Blank string
	// Env points the conventional temp and cache variables into Tmp and Cache.
	Env []string
	// ReadGrants is, on Linux, what Landlock alone grants for reading: every
	// path beside the hidden ones ([avoiding]). It is worked out once, when the
	// fence is built, because it lists the folders on the way to each hidden
	// path, and a command should not pay for that walk.
	ReadGrants []string
	// Require makes the fence a promise and not a courtesy: where this
	// computer can apply none of it, a command is REFUSED with
	// [ErrFenceRequired] instead of running as the person's own process. Only
	// a door that has something beneath the fence worth protecting sets it (the
	// hosted engine, engine_hosted.go in cmd/codeaf); every other door keeps
	// the old behaviour on purpose, and the field is left out of the helper's
	// spec when it is false so a spec is byte-identical to what it always was.
	Require bool `json:"Require,omitempty"`
	// ConnectPorts is the TCP ports a command of a REQUIRED fence may connect
	// to, on Linux where Landlock (ABI 4, Linux 6.7) can say so: every other
	// port is refused by the kernel. Landlock filters by port and not by
	// address, so this keeps a chat's commands off the services an operator runs
	// on other ports of the same private network (an agent's node API, a model
	// stub); it does not make a service on an allowed port safe to reach. Empty
	// means no limit, which is what every fence that is not a hosted chat's has
	// always had, and the field is left out of the helper's spec when it is
	// empty so a spec is byte-identical to what it always was. Only a required
	// fence honours it: a laptop's own commands keep the whole network.
	ConnectPorts []int `json:"ConnectPorts,omitempty"`
}

// MaxConnectPorts is the most ports one fence may name: a list longer than a
// person could read is a mistake, and each port is a rule the kernel keeps.
const MaxConnectPorts = 64

// ParseConnectPorts reads a port list as an operator writes it: decimal TCP
// ports separated by commas or spaces. It answers them sorted and without
// repeats, an empty list for empty text, and an error that names the entry
// at fault and never anything else. A list is refused whole rather than
// trimmed: a typo that silently dropped a port would turn a working chat's
// git into a refusal, and one that silently added one would widen the fence.
func ParseConnectPorts(text string) ([]int, error) {
	var ports []int
	seen := map[int]bool{}
	for _, word := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		n, err := strconv.Atoi(word)
		if err != nil || n < 1 || n > 65535 || word != strconv.Itoa(n) {
			return nil, errors.New(strconv.Quote(word) + " is not a TCP port from 1 to 65535")
		}
		if !seen[n] {
			seen[n] = true
			ports = append(ports, n)
		}
	}
	if len(ports) > MaxConnectPorts {
		return nil, errors.New("more than " + strconv.Itoa(MaxConnectPorts) + " ports")
	}
	sort.Ints(ports)
	return ports, nil
}

// limitsPorts is whether this fence holds commands to a list of TCP ports.
func (f *Fence) limitsPorts() bool {
	return f != nil && f.Require && len(f.ConnectPorts) > 0
}

// FenceAllowWords is how a widening is spelled, in a project file, the
// profile or the environment: `docker`, `off`, or a path (`~/` is the home
// folder). A word that is none of these is ignored by [ParseFenceAllow] and
// named by [FenceAllowRejects].
const FenceAllowWords = "docker, off, or an absolute path such as ~/.cache/ms-playwright"

// ParseFenceAllow reads widening words. Relative paths are not paths: a
// widening that meant something different depending on where the run stood
// would widen something nobody named.
func ParseFenceAllow(home string, words ...string) FenceAllow {
	var out FenceAllow
	for _, word := range splitAllow(words) {
		switch strings.ToLower(word) {
		case "off", "none", "unconfined":
			out.Off = true
		case "docker":
			out.Docker = true
		default:
			if p, ok := allowPath(home, word); ok {
				out.Paths = append(out.Paths, p)
			}
		}
	}
	return out
}

// FenceAllowRejects is the words [ParseFenceAllow] could not use.
func FenceAllowRejects(home string, words ...string) []string {
	var bad []string
	for _, word := range splitAllow(words) {
		switch strings.ToLower(word) {
		case "off", "none", "unconfined", "docker":
			continue
		}
		if _, ok := allowPath(home, word); !ok {
			bad = append(bad, word)
		}
	}
	return bad
}

func splitAllow(words []string) []string {
	var out []string
	for _, w := range words {
		for _, part := range strings.FieldsFunc(w, func(r rune) bool { return r == ',' || r == '\n' }) {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func allowPath(home, word string) (string, bool) {
	if word == "~" || strings.HasPrefix(word, "~/") {
		if home == "" {
			return "", false
		}
		word = filepath.Join(home, strings.TrimPrefix(word, "~"))
	}
	if !filepath.IsAbs(word) {
		return "", false
	}
	return filepath.Clean(word), true
}

// Merge is both widenings together.
func (a FenceAllow) Merge(b FenceAllow) FenceAllow {
	return FenceAllow{Off: a.Off || b.Off, Docker: a.Docker || b.Docker, Paths: append(append([]string(nil), a.Paths...), b.Paths...)}
}

// secretHomePaths are the folders and files under the home folder that hold
// keys and sign-ins. They are hidden whether or not they exist yet on macOS,
// and when they exist on Linux (a mount needs something to cover).
var secretHomePaths = []string{
	".ssh", ".aws", ".config/gh", ".gnupg", ".netrc", ".git-credentials",
	".config/git/credentials", ".docker/config.json", ".kube", ".config/gcloud",
	".azure", ".pypirc", ".password-store", ".local/share/keyrings",
	".config/hub", ".npmrc", ".gem/credentials", ".cargo/credentials",
	".cargo/credentials.toml", ".terraform.d/credentials.tfrc.json",
	"Library/Keychains",
}

// codeafSecretFile names the files in codeaf's own folder that hold a key, a
// sign-in or an identity: the profile (its api_key row), its backups, the
// credential store, the vault and the device identity.
var codeafSecretFile = regexp.MustCompile(`^(config\.json.*|credentials\.json.*|identity\.json|device\.json|vault\.enc.*|provider-keys\.ledger|settings-backup-.*|.*\.key|.*\.pem)$`)

// containerSockets are the sockets a docker or podman client reaches.
func containerSockets(home string) []string {
	uid := os.Getuid()
	paths := []string{"/var/run/docker.sock", "/run/docker.sock", "/run/podman/podman.sock",
		filepath.Join("/run/user", itoa(uid), "docker.sock"), filepath.Join("/run/user", itoa(uid), "podman", "podman.sock")}
	if home != "" {
		paths = append(paths, filepath.Join(home, ".docker/run/docker.sock"), filepath.Join(home, ".docker/desktop/docker.sock"),
			filepath.Join(home, ".orbstack/run/docker.sock"), filepath.Join(home, ".rd/docker.sock"),
			filepath.Join(home, ".colima/default/docker.sock"), filepath.Join(home, ".colima/docker.sock"),
			filepath.Join(home, ".lima/default/sock/docker.sock"))
	}
	return paths
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// NewFence builds the fence for one job, making its temp and cache folders. It
// answers nil when the person turned the fence off.
func NewFence(opts FenceOptions) (*Fence, error) {
	if opts.Allow.Off {
		return nil, nil
	}
	home := opts.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	root, err := canonicalPath(opts.Root)
	if err != nil {
		return nil, err
	}
	stateRoot := opts.StateDir
	if stateRoot == "" {
		stateRoot = filepath.Join(os.TempDir(), "codeaf-fence-"+itoa(os.Getuid()))
	}
	state := filepath.Join(stateRoot, fenceDigest(root))
	f := &Fence{Root: root, Tmp: filepath.Join(state, "tmp"), Cache: filepath.Join(state, "cache"), Blank: filepath.Join(state, "blank")}
	for _, dir := range []string{f.Tmp, f.Cache} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	if err := writeBlank(f.Blank); err != nil {
		return nil, err
	}
	// A FENCE BUILT IS A JOB IN USE: the door's prune reads this folder's time,
	// so a job that runs again is never mistaken for one that ended.
	now := time.Now()
	_ = os.Chtimes(f.Tmp, now, now)
	if f.Tmp, err = canonicalPath(f.Tmp); err != nil {
		return nil, err
	}
	if f.Cache, err = canonicalPath(f.Cache); err != nil {
		return nil, err
	}
	if f.Blank, err = canonicalPath(f.Blank); err != nil {
		return nil, err
	}

	// THE REPOSITORY'S GIT FOLDER: a worktree's objects, refs and index live in
	// the repository it was cut from, so a commit writes there. Its hooks and
	// its config are what the person's own git runs, so they stay read-only.
	gitDir, common := gitDirs(root)
	for _, dir := range []string{gitDir, common} {
		if dir != "" && !within(dir, root) {
			f.Writable = append(f.Writable, dir)
		}
	}
	if common != "" {
		f.ReadOnly = append(f.ReadOnly, filepath.Join(common, "hooks"), filepath.Join(common, "config"))
	}
	f.ReadOnly = append(f.ReadOnly, filepath.Join(root, ".codeaf"))
	for _, p := range opts.Allow.Paths {
		if c, err := canonicalPath(p); err == nil {
			f.Writable = append(f.Writable, c)
		}
	}
	// CODEAF'S OWN RECORD OF THE STEP IS THE STEP'S TO WRITE. A check's
	// receipt is made from inside the fenced shell, so its folder must exist
	// before the fence is drawn (a bind or a rule needs a real path) and be
	// writable; a fence that refused it failed every check with exit 125.
	for _, p := range opts.Receipts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if err := os.MkdirAll(p, 0o700); err != nil {
			return nil, err
		}
		if c, err := canonicalPath(p); err == nil {
			f.Writable = append(f.Writable, c)
		}
	}
	// THE APP'S CANVASES ARE A RUN'S TO DRAW. A crew member asked for a canvas
	// types `canvas new`, which writes under the app's canvas folder; the
	// fence refused it (it is under ~/Library on a Mac), so the canvas was
	// drawn in the run's own folder where the app never shows it. Only a
	// folder that exists is granted: the app makes it, and a fence that made
	// one would leave an app's folder on a machine with no app.
	for _, p := range opts.Canvases {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			continue
		}
		if c, err := canonicalPath(p); err == nil {
			f.Writable = append(f.Writable, c)
		}
	}
	// CARGO KEEPS THE PERSON'S HOME, because codeaf's shell finds cargo itself
	// through $CARGO_HOME/bin (internal/shell's cargo.go): pointing it into the
	// job would lose a rustup install. Only its download caches are written;
	// its bin stays read-only, so `cargo install` is refused like any install.
	cargo := strings.TrimSpace(os.Getenv("CARGO_HOME"))
	if cargo == "" && home != "" {
		cargo = filepath.Join(home, ".cargo")
	}
	if cargo != "" {
		for _, sub := range []string{"registry", "git"} {
			if c, err := canonicalPath(filepath.Join(cargo, sub)); err == nil {
				f.Writable = append(f.Writable, c)
			}
		}
	}

	var hidden []string
	if home != "" {
		for _, rel := range secretHomePaths {
			hidden = append(hidden, filepath.Join(home, rel))
		}
	}
	for _, dir := range append([]string{opts.CodeafHome}, opts.OtherCodeafHomes...) {
		if dir == "" {
			continue
		}
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if codeafSecretFile.MatchString(e.Name()) {
					hidden = append(hidden, filepath.Join(dir, e.Name()))
				}
			}
		}
	}
	for _, p := range hidden {
		c, err := canonicalPath(p)
		if err != nil || allowedBy(c, opts.Allow.Paths) || within(root, c) {
			continue
		}
		f.Hidden = append(f.Hidden, c)
	}
	if !opts.Allow.Docker {
		for _, s := range containerSockets(home) {
			if c, err := canonicalPath(s); err == nil {
				f.Sockets = append(f.Sockets, uniqueAppend(nil, s, c)...)
			}
		}
	}
	f.Writable, f.ReadOnly, f.Hidden, f.Sockets = dedupe(f.Writable), dedupe(f.ReadOnly), dedupe(f.Hidden), dedupe(f.Sockets)
	f.Env = fenceEnv(f, home)
	if runtime.GOOS == "linux" {
		var present []string
		for _, p := range append(append([]string(nil), f.Hidden...), f.Sockets...) {
			if _, err := os.Lstat(p); err == nil {
				present = append(present, p)
			}
		}
		for p := range avoiding("/", present) {
			f.ReadGrants = append(f.ReadGrants, p)
		}
		sort.Strings(f.ReadGrants)
	}
	return f, nil
}

// fenceDigest names one checkout's state folder.
func fenceDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

func writeBlank(path string) error {
	if info, err := os.Stat(path); err == nil && info.Mode().Perm() == 0 {
		return nil
	}
	_ = os.Remove(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0)
	if err != nil {
		return err
	}
	return file.Close()
}

// canonicalPath is an absolute path with links resolved. A path that does not
// exist yet has its nearest existing folder resolved and the rest kept, so a
// file about to be written under /var on macOS (a link to /private/var) is
// compared with the fence's folders in the same spelling they have.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real, nil
	}
	rest := ""
	for dir := abs; ; {
		parent := filepath.Dir(dir)
		rest = filepath.Join(filepath.Base(dir), rest)
		if parent == dir {
			return abs, nil
		}
		if real, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(real, rest), nil
		}
		dir = parent
	}
}

// within reports p is dir or beneath it.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func allowedBy(p string, allowed []string) bool {
	for _, a := range allowed {
		if c, err := canonicalPath(a); err == nil && (within(p, c) || within(c, p)) {
			return true
		}
	}
	return false
}

func uniqueAppend(list []string, items ...string) []string {
	for _, it := range items {
		if !contains(list, it) {
			list = append(list, it)
		}
	}
	return list
}

func contains(list []string, s string) bool {
	for _, it := range list {
		if it == s {
			return true
		}
	}
	return false
}

func dedupe(list []string) []string {
	out := uniqueAppend(nil, list...)
	sort.Strings(out)
	return out
}

// gitDirs finds root's git folder and the repository's common folder, read
// from the files git leaves rather than by running git: a worktree's `.git` is
// a file naming its folder, whose `commondir` names the repository's.
func gitDirs(root string) (gitDir, common string) {
	dotgit := filepath.Join(root, ".git")
	info, err := os.Lstat(dotgit)
	switch {
	case err != nil:
		return "", ""
	case info.IsDir():
		gitDir = dotgit
	default:
		data, err := os.ReadFile(dotgit)
		if err != nil {
			return "", ""
		}
		line, _, _ := strings.Cut(string(data), "\n")
		named, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
		if !ok {
			return "", ""
		}
		gitDir = strings.TrimSpace(named)
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(root, gitDir)
		}
	}
	common = gitDir
	if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		named := strings.TrimSpace(string(data))
		if !filepath.IsAbs(named) {
			named = filepath.Join(gitDir, named)
		}
		common = named
	}
	g, err1 := canonicalPath(gitDir)
	c, err2 := canonicalPath(common)
	if err1 != nil || err2 != nil {
		return "", ""
	}
	return g, c
}

// RepositoryOf is the folder whose .codeaf/config.json speaks for root: the
// checkout a worktree was cut from, or root itself.
func RepositoryOf(root string) string {
	_, common := gitDirs(root)
	if common != "" && filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	if c, err := canonicalPath(root); err == nil {
		return c
	}
	return root
}

// fenceEnv points every conventional temp and cache variable into the job's
// own folders, so a build that would have written ~/.cache, ~/go or
// ~/Library/Caches writes beside the job instead. Go's module downloads are
// first looked for in the person's own module cache, read-only, so a job does
// not fetch again what this machine already has.
func fenceEnv(f *Fence, home string) []string {
	c := func(name string) string { return filepath.Join(f.Cache, name) }
	env := []string{
		"TMPDIR=" + f.Tmp, "TMP=" + f.Tmp, "TEMP=" + f.Tmp, "GOTMPDIR=" + f.Tmp,
		"XDG_CACHE_HOME=" + c("xdg"),
		"GOCACHE=" + c("go-build"), "GOPATH=" + c("gopath"), "GOMODCACHE=" + c("gopath/pkg/mod"),
		"npm_config_cache=" + c("npm"), "npm_config_store_dir=" + c("pnpm-store"),
		"YARN_CACHE_FOLDER=" + c("yarn"), "PIP_CACHE_DIR=" + c("pip"), "UV_CACHE_DIR=" + c("uv"),
		"GRADLE_USER_HOME=" + c("gradle"),
		"BUN_INSTALL_CACHE_DIR=" + c("bun"), "DENO_DIR=" + c("deno"),
		"CODEAF_FENCED=1",
	}
	// THE DOCKER CLIENT IS POINTED AT A SOCKET IT CANNOT OPEN. Where the jail
	// also refuses the real socket (macOS, and Linux with namespaces) this is
	// only a clearer refusal; on Linux with Landlock alone it is the whole of
	// it, because Landlock does not govern connecting to a socket.
	if len(f.Sockets) > 0 {
		env = append(env, "DOCKER_HOST=unix://"+f.Blank, "CONTAINER_HOST=unix://"+f.Blank)
	}
	flags := strings.TrimSpace(os.Getenv("GOFLAGS"))
	if !strings.Contains(flags, "-modcacherw") {
		flags = strings.TrimSpace(flags + " -modcacherw")
	}
	env = append(env, "GOFLAGS="+flags)
	if proxy := goProxyThrough(home); proxy != "" {
		env = append(env, "GOPROXY="+proxy)
	}
	return env
}

// goProxyThrough is GOPROXY with the person's module download cache in front,
// or "" when there is none or the person turned the proxy off.
func goProxyThrough(home string) string {
	cache := strings.TrimSpace(os.Getenv("GOMODCACHE"))
	if cache == "" {
		gopath := strings.TrimSpace(os.Getenv("GOPATH"))
		if first, _, _ := strings.Cut(gopath, string(os.PathListSeparator)); first != "" {
			cache = filepath.Join(first, "pkg", "mod")
		} else if home != "" {
			cache = filepath.Join(home, "go", "pkg", "mod")
		}
	}
	download := filepath.Join(cache, "cache", "download")
	if info, err := os.Stat(download); err != nil || !info.IsDir() {
		return ""
	}
	upstream := strings.TrimSpace(os.Getenv("GOPROXY"))
	if upstream == "off" {
		return ""
	}
	if upstream == "" {
		upstream = "https://proxy.golang.org,direct"
	}
	return "file://" + filepath.ToSlash(download) + "," + upstream
}

// ── WHAT THE PERSON AND THE MODEL READ ─────────────────────────────────────

// FenceLine is the one sentence added to a command's output when the fence
// stopped it. It says what happened and why, and where the widening lives.
const FenceLine = "codeaf blocked this: a run with nobody at the keyboard may change files only in its checkout and its own temp and cache folders, and may not read keys and sign-ins or use docker; to allow more, add \"jail.allow\" to the repository's .codeaf/config.json."

// FenceConnectLine is the one sentence added to a failed command of a chat
// whose fence limits the ports it may connect to ([Fence.ConnectPorts]) when
// its output reads as a refusal. It stands in for [FenceLine], whose advice
// (a project's jail.allow) belongs to a person's own machine and is no help in
// a chat: it says what a refused command can still do, and nothing about how
// the limit is made.
const FenceConnectLine = "codeaf blocked this: commands in this chat may change files only in the chat's own folder, and may reach other computers only on the ports the organization allows."

// noteLine is the sentence a refusal under this fence is explained with.
func (f *Fence) noteLine() string {
	if f.limitsPorts() {
		return FenceConnectLine
	}
	return FenceLine
}

// FenceRequiredLine is what a command is refused with when the fence is a
// requirement ([Fence.Require]) and this computer can apply none of it. It is
// written for the person reading a chat, so it names no mechanism: only what
// did not happen and why.
const FenceRequiredLine = "This computer cannot keep commands inside this chat's folder, so no command was run."

// ErrFenceRequired is the error a required fence's command fails with when the
// fence cannot be applied. Its text is [FenceRequiredLine], so every door that
// prints an executor error to a person or a model says it in plain words.
var ErrFenceRequired = errors.New(FenceRequiredLine)

// FenceDegradedLine is said once when this system cannot apply the fence.
const FenceDegradedLine = "This system cannot confine commands, so this run's commands have your full permissions."

// FencePartialLine is said once when the fence applies only in part: on Linux
// without user namespaces, writes and private files are protected but the
// docker socket is only steered away, and the checkout's own git hooks stay
// writable.
const FencePartialLine = "This system limits this run's commands only in part: writes and private files are protected, but docker and the checkout's own git hooks are not fully closed off."

// refusalMarks are how a refused open, write or connect reads in a tool's own
// words: EACCES and EPERM from the kernel, seatbelt's EPERM, a read-only bind,
// and the sentence brew and npm print for a folder they cannot write.
var refusalMarks = regexp.MustCompile(`(?i)operation not permitted|permission denied|read-only file system|is not writable|EACCES|EPERM|EROFS`)

// FenceRefused reports output that reads as a fence refusal.
func FenceRefused(output []byte) bool {
	return refusalMarks.Match(output)
}

// WithFenceLine is output with [FenceLine] added once when it reads as a
// refusal, and output unchanged otherwise.
func WithFenceLine(output string) string { return withNote(output, FenceLine) }

// WithNote is [WithFenceLine] with the sentence THIS fence explains its
// refusals with: a chat whose commands are limited to some ports is told what
// it may reach, and not to edit a repository's jail.allow, which a chat cannot
// do and which names a mechanism a person reading a chat never meets.
func (f *Fence) WithNote(output string) string {
	if f == nil {
		return WithFenceLine(output)
	}
	return withNote(output, f.noteLine())
}

func withNote(output, line string) string {
	if !FenceRefused([]byte(output)) || strings.Contains(output, FenceLine) || strings.Contains(output, FenceConnectLine) {
		return output
	}
	if output != "" && !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	return output + line
}

// Allows reports whether the fence lets an in-process file tool read (write
// false) or write path. It is the same policy the jail applies to a process,
// asked by the session's own read, write and edit tools, which run inside
// codeaf and so outside any jail.
func (f *Fence) Allows(path string, write bool) bool {
	if f == nil {
		return true
	}
	p, err := canonicalPath(path)
	if err != nil {
		return false
	}
	for _, h := range append(append([]string(nil), f.Hidden...), f.Sockets...) {
		if within(p, h) {
			return false
		}
	}
	if !write {
		return true
	}
	for _, ro := range f.ReadOnly {
		if within(p, ro) {
			return false
		}
	}
	for _, w := range append([]string{f.Root, f.Tmp, f.Cache}, f.Writable...) {
		if within(p, w) {
			return true
		}
	}
	return runtime.GOOS == "darwin" && (within(p, "/private/tmp") || within(p, "/private/var/folders"))
}

// avoiding is the set of paths that, granted together, cover everything
// beneath grant except the avoided paths: each folder on the way down to an
// avoided path is replaced by its entries, minus the one on the way. The
// folders on the way are left out themselves, because a grant on a folder
// reaches everything beneath it. An entry that is a link into an avoided path
// is left out too, since a grant follows the link. It is how a policy that can
// only grant (Landlock) keeps something out.
func avoiding(grant string, avoid []string) map[string]bool {
	out := map[string]bool{grant: true}
	for _, a := range avoid {
		for g := range out {
			if !within(a, g) {
				continue
			}
			delete(out, g)
			for dir := g; dir != a; {
				next := nextOnPath(dir, a)
				// A folder this user may pass through but not list (a hosted
				// chat's machine keeps others' folders that way) cannot say what
				// is beside the path, so none of it is granted, and the walk
				// goes on below it. Stopping here instead dropped every grant
				// beneath, and the chat's own files were then refused to its
				// own commands.
				if entries, err := os.ReadDir(dir); err == nil {
					for _, e := range entries {
						p := filepath.Join(dir, e.Name())
						if p == next {
							continue
						}
						if real, err := filepath.EvalSymlinks(p); err == nil && real != p && pathInAny(real, avoid) {
							continue
						}
						out[p] = true
					}
				}
				dir = next
			}
			break
		}
	}
	return out
}

// nextOnPath is the child of dir on the way to target.
func nextOnPath(dir, target string) string {
	rel, _ := filepath.Rel(dir, target)
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	return filepath.Join(dir, first)
}

func pathInAny(p string, list []string) bool {
	for _, l := range list {
		if within(p, l) {
			return true
		}
	}
	return false
}

// FenceLimitLine is said once in a conversation whose commands run fenced on a
// system that cannot apply all of the fence, and is "" where it can.
func FenceLimitLine() string { return fenceLimit() }

// canonicalAll is each path made canonical, the ones that cannot be dropped.
func canonicalAll(paths []string) []string {
	var out []string
	for _, p := range paths {
		if c, err := canonicalPath(p); err == nil && filepath.IsAbs(c) {
			out = append(out, c)
		}
	}
	return out
}
