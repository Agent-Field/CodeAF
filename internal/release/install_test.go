package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRelease struct {
	TagName     string      `json:"tag_name"`
	CreatedAt   string      `json:"created_at,omitempty"`
	PublishedAt any         `json:"published_at,omitempty"`
	Assets      []fakeAsset `json:"assets,omitempty"`
}

type fakeAsset struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type fakeTiming struct {
	created   string
	published any
}

type apiRefusal struct {
	status    int
	headers   map[string]string
	body      string
	tokenOnly bool
}

type installGitHub struct {
	server            *httptest.Server
	releases          []string
	timings           map[string]fakeTiming
	badChecksum       bool
	refuse            *apiRefusal
	failAPI           bool
	notFound          bool
	oldRepositoryOnly bool
	legacyAssetOnly   bool
	binary            []byte
	mu                sync.Mutex
	requests          []*http.Request
}

func newInstallGitHub(t *testing.T, releases ...string) *installGitHub {
	t.Helper()
	github := &installGitHub{releases: releases}
	github.server = httptest.NewServer(http.HandlerFunc(github.serve))
	t.Cleanup(github.server.Close)
	return github
}

func (github *installGitHub) serve(w http.ResponseWriter, request *http.Request) {
	github.mu.Lock()
	github.requests = append(github.requests, request.Clone(request.Context()))
	github.mu.Unlock()
	if refusal := github.refuse; refusal != nil && strings.HasPrefix(request.URL.Path, "/repos/") &&
		(!refusal.tokenOnly || request.Header.Get("Authorization") != "") {
		for name, value := range refusal.headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(refusal.status)
		_, _ = w.Write([]byte(refusal.body))
		return
	}
	if github.notFound {
		http.NotFound(w, request)
		return
	}

	path := request.URL.Path
	const prefix = "/repos/Agent-Field/codeaf/"
	const legacyPrefix = "/repos/Agent-Field/aforge-v2/" // legacy-name
	apiPrefix := ""
	switch {
	case strings.HasPrefix(path, prefix) && !github.oldRepositoryOnly:
		apiPrefix = prefix
	case strings.HasPrefix(path, legacyPrefix):
		apiPrefix = legacyPrefix
	}
	if apiPrefix != "" {
		if github.failAPI {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		switch {
		case path == apiPrefix+"releases/latest":
			if len(github.releases) == 0 {
				http.NotFound(w, request)
				return
			}
			github.writeJSON(w, fakeRelease{TagName: github.releases[0]})
			return
		case path == apiPrefix+"releases":
			var releases []fakeRelease
			for _, tag := range github.releases {
				release := fakeRelease{TagName: tag}
				if timing, ok := github.timings[tag]; ok {
					release.CreatedAt = timing.created
					release.PublishedAt = timing.published
				}
				if github.timings != nil {
					release.Assets = []fakeAsset{{
						Name:      platformAsset(),
						CreatedAt: "2999-01-01T00:00:00Z",
						UpdatedAt: "2999-01-01T00:00:00Z",
					}}
				}
				releases = append(releases, release)
			}
			github.writeJSON(w, releases)
			return
		}
	}

	const downloads = "/Agent-Field/codeaf/releases/download/"
	const legacyDownloads = "/Agent-Field/aforge-v2/releases/download/" // legacy-name
	downloadPrefix := ""
	switch {
	case strings.HasPrefix(path, downloads) && !github.oldRepositoryOnly:
		downloadPrefix = downloads
	case strings.HasPrefix(path, legacyDownloads):
		downloadPrefix = legacyDownloads
	}
	if downloadPrefix != "" {
		rest := strings.TrimPrefix(path, downloadPrefix)
		tag, name, ok := strings.Cut(rest, "/")
		if !ok || !contains(github.releases, tag) {
			http.NotFound(w, request)
			return
		}
		github.writeAsset(w, tag, name)
		return
	}
	http.NotFound(w, request)
}

func (github *installGitHub) writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (github *installGitHub) writeAsset(w http.ResponseWriter, tag, name string) {
	binary := fakeBinary(tag)
	if len(github.binary) != 0 {
		binary = github.binary
	}
	asset := platformAsset()
	if github.legacyAssetOnly {
		asset = legacyPlatformAsset()
	}
	switch name {
	case asset:
		_, _ = w.Write(binary)
	case "checksums.txt":
		digest := sha256.Sum256(binary)
		checksum := hex.EncodeToString(digest[:])
		if github.badChecksum {
			checksum = strings.Repeat("0", len(checksum))
		}
		fmt.Fprintf(w, "%s  %s\n", checksum, asset)
	default:
		http.NotFound(w, nil)
	}
}

func fakeBinary(tag string) []byte {
	return []byte("#!/bin/sh\nprintf 'codeaf " + tag + " · fake\\n'\n")
}

func platformAsset() string {
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	return "codeaf-" + runtime.GOOS + "-" + runtime.GOARCH + extension
}

func legacyPlatformAsset() string {
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	return "aforge-" + runtime.GOOS + "-" + runtime.GOARCH + extension // legacy-name
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

type installRun struct {
	code       int
	output     string
	stderr     string
	home       string
	installDir string
}

func runInstaller(t *testing.T, github *installGitHub, arguments []string, extraEnv ...string) installRun {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the installer contract is exercised through Bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	home := t.TempDir()
	installDir := filepath.Join(home, "install")
	path := minimalPath(t, true)
	command := exec.Command(bash, append([]string{filepath.Join(repositoryRoot(t), "scripts", "install.sh")}, arguments...)...)
	command.Env = append([]string{
		"HOME=" + home,
		"CODEAF_INSTALL_DIR=" + installDir,
		"CODEAF_GITHUB_API=" + github.server.URL,
		"CODEAF_GITHUB_DOWNLOAD=" + github.server.URL,
		"PATH=" + path,
		"SHELL=/bin/bash",
	}, extraEnv...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	code := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !strings.Contains(runErr.Error(), "exit status") || !asExitError(runErr, &exitErr) {
			t.Fatalf("run installer: %v", runErr)
		}
		code = exitErr.ExitCode()
	}
	return installRun{code: code, output: stdout.String() + stderr.String(), stderr: stderr.String(), home: home, installDir: installDir}
}

func asExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}

func minimalPath(t *testing.T, withCurl bool) string {
	t.Helper()
	dir := t.TempDir()
	commands := []string{"awk", "basename", "cat", "chmod", "cp", "date", "dirname", "grep", "ln", "mkdir", "mktemp", "mv", "rm", "sed", "sha256sum", "shasum", "tr", "uname", "wget"}
	if withCurl {
		commands = append(commands, "curl")
	}
	for _, name := range commands {
		target, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestInstallerGetsLatestStableAndFinishesWithVersion(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	run := runInstaller(t, github, nil, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("exit %d:\n%s", run.code, run.output)
	}
	// A normal run says what it did in checked steps, the installed binary
	// naming itself on one of them, then the guide. The tag and the GETs are
	// --verbose's to say.
	if !strings.Contains(run.output, "Installed   codeaf v1.2.3\n") {
		t.Errorf("output does not carry the receipt:\n%s", run.output)
	}
	for _, absent := range []string{"stable v1.2.3", "codeaf: installed"} {
		if strings.Contains(run.output, absent) {
			t.Errorf("a normal run should not say %q:\n%s", absent, run.output)
		}
	}
	verbose := runInstaller(t, github, []string{"--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
	for _, want := range []string{"stable v1.2.3", runtime.GOOS + "/" + runtime.GOARCH, "codeaf: installed", "Installed   codeaf v1.2.3\n"} {
		if verbose.code != 0 || !strings.Contains(verbose.output, want) {
			t.Errorf("verbose output does not contain %q (exit %d):\n%s", want, verbose.code, verbose.output)
		}
	}
	if _, err := os.Stat(filepath.Join(run.installDir, "codeaf")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.output, "export PATH=") {
		t.Fatalf("the no-modify-path run did not print the line to paste:\n%s", run.output)
	}
	if _, err := os.Stat(filepath.Join(run.home, ".bashrc")); !os.IsNotExist(err) {
		t.Fatalf("--no-modify-path edited the shell file: %v", err)
	}
}

// onlyTheReleaseRepository fails the test for any request that named a
// repository other than the one releases come from.
func onlyTheReleaseRepository(t *testing.T, github *installGitHub) {
	t.Helper()
	github.mu.Lock()
	defer github.mu.Unlock()
	if len(github.requests) == 0 {
		t.Fatal("the installer asked nothing")
	}
	for _, request := range github.requests {
		path := request.URL.Path
		if !strings.HasPrefix(path, "/repos/Agent-Field/codeaf/") && !strings.HasPrefix(path, "/Agent-Field/codeaf/") {
			t.Fatalf("the installer asked another repository: %s", path)
		}
	}
}

// THE INSTALLER ASKS ONE REPOSITORY. The fake answers under the former name
// too, as a repository squatting on it could; a 404 from the real one still
// ends the install, and nothing is asked of the other.
func TestInstallerAsksOnlyTheReleaseRepository(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	github.oldRepositoryOnly = true
	run := runInstaller(t, github, nil, "CODEAF_NO_MODIFY_PATH=1")
	if run.code == 0 {
		t.Fatalf("an install with no release in the release repository succeeded:\n%s", run.output)
	}
	if _, err := os.Stat(filepath.Join(run.installDir, "codeaf")); err == nil {
		t.Fatal("a binary was written although the release repository had none")
	}
	onlyTheReleaseRepository(t, github)
}

// H10: a release containing only the former asset spelling is installed under
// the current binary name after its checksum is verified.
func TestH10InstallerFallsBackToLegacyAssetName(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	github.legacyAssetOnly = true
	run := runInstaller(t, github, nil, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("asset fallback exit %d:\n%s", run.code, run.output)
	}
	installed, err := os.ReadFile(filepath.Join(run.installDir, "codeaf"))
	if err != nil || !strings.Contains(string(installed), "codeaf v1.2.3") {
		t.Fatalf("installed current binary = %q, %v", installed, err)
	}
}

// A release published before the rename carries the former asset name; pinning
// it still installs it, with its asset and checksum both read from the release
// repository.
func TestPinnedFormerReleaseTakesItsFormerAssetFromTheReleaseRepository(t *testing.T) {
	github := newInstallGitHub(t, "v0.1.0")
	github.legacyAssetOnly = true
	run := runInstaller(t, github, []string{"--version", "v0.1.0"}, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("pinned former release exit %d:\n%s", run.code, run.output)
	}
	github.mu.Lock()
	var asset, checksum bool
	for _, request := range github.requests {
		asset = asset || strings.Contains(request.URL.Path, "/Agent-Field/codeaf/releases/download/v0.1.0/aforge-") // legacy-name
		checksum = checksum || request.URL.Path == "/Agent-Field/codeaf/releases/download/v0.1.0/checksums.txt"
	}
	github.mu.Unlock()
	if !asset || !checksum {
		t.Fatalf("release repository asset=%v checksum=%v", asset, checksum)
	}
	onlyTheReleaseRepository(t, github)
}

func TestInstallerRunsAdoptionBeforeCreatingTheStateRoot(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	github.binary = adoptingFakeBinary("v1.2.3")
	home := t.TempDir()
	oldState := filepath.Join(home, ".aforge") // legacy-name
	kept := filepath.Join(oldState, "v3", "projects", "p1", "note.txt")
	if err := os.MkdirAll(filepath.Dir(kept), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kept, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(home, ".codeaf", "bin")
	run := runInstaller(t, github, nil, "HOME="+home, "CODEAF_INSTALL_DIR="+installDir, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("adopting install exit %d:\n%s", run.code, run.output)
	}
	adopted := filepath.Join(home, ".codeaf", "v3", "projects", "p1", "note.txt")
	if body, err := os.ReadFile(adopted); err != nil || string(body) != "kept" {
		t.Fatalf("adopted state = %q, %v", body, err)
	}
	if info, err := os.Lstat(oldState); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("former state root is not a compatibility link: %v %v", info, err)
	}
	installed := filepath.Join(installDir, "codeaf")
	command := exec.Command(installed, "doctor")
	command.Env = []string{"HOME=" + home}
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), adopted) {
		t.Fatalf("installed executable does not resolve adopted state: %v\n%s", err, output)
	}
}

func TestCustomInstallOutsideTheStateRootDoesNotRunAdoption(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	github.binary = adoptingFakeBinary("v1.2.3")
	home := t.TempDir()
	oldState := filepath.Join(home, ".aforge") // legacy-name
	if err := os.MkdirAll(oldState, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "bin")
	run := runInstaller(t, github, nil, "HOME="+home, "CODEAF_INSTALL_DIR="+outside, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 {
		t.Fatalf("custom install exit %d:\n%s", run.code, run.output)
	}
	if info, err := os.Lstat(oldState); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("custom install moved former state: %v %v", info, err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".codeaf")); !os.IsNotExist(err) {
		t.Fatalf("custom install created the state root: %v", err)
	}
}

func adoptingFakeBinary(tag string) []byte {
	return []byte("#!/bin/sh\n" +
		"if [ -z \"${CODEAF_HOME+x}\" ] && [ -z \"${AFORGE_HOME+x}\" ] && [ ! -e \"$HOME/.codeaf\" ] && [ -d \"$HOME/.aforge\" ] && [ ! -L \"$HOME/.aforge\" ]; then\n" + // legacy-name
		"  mv \"$HOME/.aforge\" \"$HOME/.codeaf\" && ln -s .codeaf \"$HOME/.aforge\"\n" + // legacy-name
		"fi\n" +
		"if [ \"${1:-}\" = doctor ]; then printf '%s\\n' \"$HOME/.codeaf/v3/projects/p1/note.txt\"; exit 0; fi\n" +
		"printf 'codeaf " + tag + " · fake\\n'\n")
}

// H10: PATH edits use the current installer marker and remain idempotent.
func TestH10InstallerWritesTheCurrentPathMarker(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	home := t.TempDir()
	run := runInstaller(t, github, nil, "HOME="+home)
	if run.code != 0 {
		t.Fatalf("install exit %d:\n%s", run.code, run.output)
	}
	body, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), "# codeaf installer") != 1 {
		t.Fatalf("PATH marker = %q", body)
	}
}

func TestInstallerReplacesTheFormerPathLineOnceAndStaysIdempotent(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	home := t.TempDir()
	rc := filepath.Join(home, ".bashrc")
	former := "export PATH=\"$HOME/.aforge/bin:$PATH\" # aforge installer\n" // legacy-name
	if err := os.WriteFile(rc, []byte(former), 0o600); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(home, ".codeaf", "bin")
	for attempt := 0; attempt < 3; attempt++ {
		run := runInstaller(t, github, nil, "HOME="+home, "CODEAF_INSTALL_DIR="+installDir)
		if run.code != 0 {
			t.Fatalf("attempt %d exit %d:\n%s", attempt+1, run.code, run.output)
		}
	}
	body, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), "# codeaf installer") != 1 || strings.Contains(string(body), "aforge") { // legacy-name
		t.Fatalf("PATH line was not repaired exactly once:\n%s", body)
	}
}

func TestInstallerSelectsTheNewestBuildOfEachChannel(t *testing.T) {
	github := newInstallGitHub(t,
		"v2.0.0", "dev-20260910-aaaaaaaaaaaa", "v2.1.0-rc.2",
		"staging-20260910-bbbbbbbbbbbb", "v2.1.0-rc.1")
	for _, test := range []struct{ channel, want string }{
		{"rc", "v2.1.0-rc.2"}, {"dev", "dev-20260910-aaaaaaaaaaaa"}, {"staging", "staging-20260910-bbbbbbbbbbbb"},
	} {
		t.Run(test.channel, func(t *testing.T) {
			arguments := []string{"--" + test.channel}
			environment := []string{"CODEAF_NO_MODIFY_PATH=1"}
			if test.channel == "rc" {
				arguments = nil
				environment = append(environment, "CHANNEL=rc")
			}
			run := runInstaller(t, github, arguments, environment...)
			if run.code != 0 || !strings.Contains(run.output, test.want) {
				t.Fatalf("exit %d, want %s:\n%s", run.code, test.want, run.output)
			}
		})
	}
}

// Contract: the newest published_at wins regardless of list order, created_at
// replaces a null published_at, and the same ordering applies to staging.
func TestInstallerPicksTheNewestChannelBuildWhateverTheListOrder(t *testing.T) {
	t.Run("a newer malformed tag is ignored", func(t *testing.T) {
		const (
			valid     = "dev-20260915-abcdefabcdef"
			malformed = "dev-backfill"
		)
		github := newInstallGitHub(t, valid, malformed)
		github.timings = map[string]fakeTiming{
			valid:     {published: "2026-09-15T15:00:00Z"},
			malformed: {published: "2026-09-15T16:00:00Z"},
		}
		run := runInstaller(t, github, []string{"--dev", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 || !strings.Contains(run.output, "dev "+valid) || strings.Contains(run.output, malformed) {
			t.Fatalf("exit %d, want valid tag %s:\n%s", run.code, valid, run.output)
		}
	})

	t.Run("a release candidate with leading zeroes is ignored", func(t *testing.T) {
		const (
			valid     = "v2.1.0-rc.1"
			malformed = "v02.1.0-rc.2"
		)
		github := newInstallGitHub(t, valid, malformed)
		github.timings = map[string]fakeTiming{
			valid:     {published: "2026-09-15T15:00:00Z"},
			malformed: {published: "2026-09-15T16:00:00Z"},
		}
		run := runInstaller(t, github, []string{"--rc", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 || !strings.Contains(run.output, "rc "+valid) || strings.Contains(run.output, malformed) {
			t.Fatalf("exit %d, want valid tag %s:\n%s", run.code, valid, run.output)
		}
	})

	t.Run("newest published_at", func(t *testing.T) {
		const (
			oldest = "dev-20260915-e2ae913b7d0c"
			middle = "dev-20260915-56a22c20ec53"
			newest = "dev-20260915-4b6ec83cfbfa"
		)
		github := newInstallGitHub(t, oldest, middle, newest)
		github.timings = map[string]fakeTiming{
			oldest: {published: "2026-09-15T13:43:00Z"},
			middle: {published: "2026-09-15T14:56:00Z"},
			newest: {published: "2026-09-15T15:21:00Z"},
		}
		run := runInstaller(t, github, []string{"--dev", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 {
			t.Fatalf("exit %d:\n%s", run.code, run.output)
		}
		if !strings.Contains(run.output, "dev "+newest) {
			t.Errorf("output does not name newest tag %q:\n%s", newest, run.output)
		}
		for _, older := range []string{oldest, middle} {
			if strings.Contains(run.output, older) {
				t.Errorf("output contains older tag %q:\n%s", older, run.output)
			}
		}
		installed, err := os.ReadFile(filepath.Join(run.installDir, "codeaf"))
		if err != nil || !strings.Contains(string(installed), "codeaf "+newest) {
			t.Fatalf("installed binary = %q, %v", installed, err)
		}
		github.mu.Lock()
		defer github.mu.Unlock()
		for _, request := range github.requests {
			if strings.Contains(request.URL.Path, "/releases/download/"+oldest+"/") {
				t.Fatalf("installer requested the oldest release: %s", request.URL.Path)
			}
		}
	})

	t.Run("created_at replaces null published_at", func(t *testing.T) {
		const (
			older  = "dev-20260915-111111111111"
			newest = "dev-20260915-222222222222"
		)
		github := newInstallGitHub(t, older, newest)
		github.timings = map[string]fakeTiming{
			older:  {created: "2026-09-15T13:00:00Z", published: "2026-09-15T14:00:00Z"},
			newest: {created: "2026-09-15T16:00:00Z", published: json.RawMessage("null")},
		}
		run := runInstaller(t, github, []string{"--dev", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 || !strings.Contains(run.output, "dev "+newest) {
			t.Fatalf("exit %d, want %s:\n%s", run.code, newest, run.output)
		}
		installed, err := os.ReadFile(filepath.Join(run.installDir, "codeaf"))
		if err != nil || !strings.Contains(string(installed), "codeaf "+newest) {
			t.Fatalf("installed binary = %q, %v", installed, err)
		}
	})

	t.Run("staging uses timestamps", func(t *testing.T) {
		const (
			older  = "staging-20260915-333333333333"
			newest = "staging-20260915-444444444444"
		)
		github := newInstallGitHub(t, older, newest)
		github.timings = map[string]fakeTiming{
			older:  {published: "2026-09-15T12:00:00Z"},
			newest: {published: "2026-09-15T17:00:00Z"},
		}
		run := runInstaller(t, github, []string{"--staging", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 || !strings.Contains(run.output, "staging "+newest) {
			t.Fatalf("exit %d, want %s:\n%s", run.code, newest, run.output)
		}
	})
}

func TestInstallerPinsAReleaseAndNamesAMissingOne(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3", "build-legacy")
	// The channel and tag are --verbose lines; a normal run's receipt is the
	// installed binary naming itself, which the fake does with its tag.
	run := runInstaller(t, github, []string{"--version", "v1.2.3", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 || !strings.Contains(run.output, "stable v1.2.3") {
		t.Fatalf("exit %d:\n%s", run.code, run.output)
	}
	fromEnvironment := runInstaller(t, github, nil, "VERSION=v1.2.3", "CODEAF_NO_MODIFY_PATH=1")
	if fromEnvironment.code != 0 || !strings.Contains(fromEnvironment.output, "Installed   codeaf v1.2.3\n") {
		t.Fatalf("VERSION install exit %d:\n%s", fromEnvironment.code, fromEnvironment.output)
	}
	legacy := runInstaller(t, github, []string{"--version", "build-legacy", "--verbose"}, "CODEAF_NO_MODIFY_PATH=1")
	wantLegacy := "codeaf: build-legacy for " + runtime.GOOS + "/" + runtime.GOARCH
	if legacy.code != 0 || !strings.Contains(legacy.output, wantLegacy) || strings.Contains(legacy.output, "codeaf: version ") {
		t.Fatalf("legacy-tag install exit %d:\n%s", legacy.code, legacy.output)
	}

	missing := runInstaller(t, github, []string{"--version", "v9.9.9"}, "CODEAF_NO_MODIFY_PATH=1")
	if missing.code != 1 || !strings.Contains(missing.output, "v9.9.9") || !strings.Contains(missing.output, "check the tag on the Releases page") {
		t.Fatalf("missing release: exit %d:\n%s", missing.code, missing.output)
	}
}

func TestDocumentedVersionPinReachesThePipedInstaller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the installer contract is exercised through Bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	github := newInstallGitHub(t, "v1.2.3", "v9.9.9")
	home := t.TempDir()
	installDir := filepath.Join(home, "install")
	path := minimalPath(t, true)
	if err := os.Symlink(bash, filepath.Join(path, "bash")); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repositoryRoot(t), "scripts", "install.sh")
	command := exec.Command(bash, "-c", `cat "$1" | VERSION=v1.2.3 bash`, "documented-pin", script)
	command.Env = []string{
		"HOME=" + home,
		"CODEAF_INSTALL_DIR=" + installDir,
		"CODEAF_GITHUB_API=" + github.server.URL,
		"CODEAF_GITHUB_DOWNLOAD=" + github.server.URL,
		"CODEAF_NO_MODIFY_PATH=1",
		"PATH=" + path,
		"SHELL=/bin/bash",
	}
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "Installed   codeaf v1.2.3\n") || strings.Contains(string(output), "v9.9.9") {
		t.Fatalf("documented pin failed: %v\n%s", err, output)
	}
}

func TestInstallerNamesAnEmptyChannelWithoutWriting(t *testing.T) {
	for _, test := range []struct {
		name      string
		releases  []string
		arguments []string
	}{
		{name: "stable", arguments: []string{"--stable"}},
		{name: "staging", releases: []string{"v1.2.3"}, arguments: []string{"--staging"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			github := newInstallGitHub(t, test.releases...)
			run := runInstaller(t, github, test.arguments)
			want := "codeaf: no " + test.name + " build has been published yet"
			if run.code != 1 || !strings.Contains(run.output, want) {
				t.Fatalf("exit %d:\n%s", run.code, run.output)
			}
			if _, err := os.Stat(run.installDir); !os.IsNotExist(err) {
				t.Fatalf("install directory was written on refusal: %v", err)
			}
		})
	}
}

func TestInstallerChecksBeforeReplacingAndCanRunTwice(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	badHome := t.TempDir()
	badDir := filepath.Join(badHome, "install")
	github.badChecksum = true
	run := runInstaller(t, github, nil, "CODEAF_INSTALL_DIR="+badDir, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 1 || !strings.Contains(run.output, "checksum") {
		t.Fatalf("checksum refusal: exit %d:\n%s", run.code, run.output)
	}
	if _, err := os.Stat(filepath.Join(badDir, "codeaf")); !os.IsNotExist(err) {
		t.Fatalf("a failed checksum left a binary: %v", err)
	}

	github.badChecksum = false
	home := t.TempDir()
	dir := filepath.Join(home, "install")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codeaf"), []byte("old running binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	first := runInstaller(t, github, nil, "HOME="+home, "CODEAF_INSTALL_DIR="+dir)
	second := runInstaller(t, github, nil, "HOME="+home, "CODEAF_INSTALL_DIR="+dir)
	if first.code != 0 || second.code != 0 {
		t.Fatalf("repeat exits %d and %d:\n%s\n%s", first.code, second.code, first.output, second.output)
	}
	rc, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(rc), "# codeaf installer") != 1 {
		t.Fatalf("PATH edit is not idempotent:\n%s", rc)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".codeaf.tmp.*")); len(matches) != 0 {
		t.Fatalf("atomic install left temporary files: %v", matches)
	}
	installed, err := os.ReadFile(filepath.Join(dir, "codeaf"))
	if err != nil || strings.Contains(string(installed), "old running binary") {
		t.Fatalf("the old binary was not replaced: %v %q", err, installed)
	}
}

func TestInstallerSendsTheTokenOnAPICalls(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		path      string
	}{
		{name: "latest", path: "/repos/Agent-Field/codeaf/releases/latest"},
		{name: "list", arguments: []string{"--rc"}, path: "/repos/Agent-Field/codeaf/releases"},
	} {
		t.Run(test.name, func(t *testing.T) {
			github := newInstallGitHub(t, "v1.2.3", "v1.3.0-rc.1")
			const token = "secret-installer-token"
			run := runInstaller(t, github, test.arguments, "GITHUB_TOKEN="+token, "CODEAF_NO_MODIFY_PATH=1")
			if run.code != 0 {
				t.Fatalf("token install exit %d:\n%s", run.code, run.output)
			}
			github.mu.Lock()
			defer github.mu.Unlock()
			for _, request := range github.requests {
				if request.URL.Path == test.path {
					if authorization := request.Header.Get("Authorization"); authorization != "Bearer "+token {
						t.Fatalf("API request Authorization = %q", authorization)
					}
					return
				}
			}
			t.Fatalf("API request %s was not made", test.path)
		})
	}
}

func TestInstallerNeverSendsAuthorizationOnDownloads(t *testing.T) {
	for _, token := range []string{"", "GITHUB_TOKEN=secret-installer-token"} {
		name := "without_token"
		if token != "" {
			name = "with_token"
		}
		t.Run(name, func(t *testing.T) {
			github := newInstallGitHub(t, "v1.2.3")
			environment := []string{"CODEAF_NO_MODIFY_PATH=1"}
			if token != "" {
				environment = append(environment, token)
			}
			run := runInstaller(t, github, nil, environment...)
			if run.code != 0 {
				t.Fatalf("install exit %d:\n%s", run.code, run.output)
			}
			github.mu.Lock()
			defer github.mu.Unlock()
			downloads := 0
			for _, request := range github.requests {
				if strings.Contains(request.URL.Path, "/releases/download/") {
					downloads++
					if authorization := request.Header.Get("Authorization"); authorization != "" {
						t.Errorf("download %s has Authorization %q", request.URL.Path, authorization)
					}
				}
			}
			if downloads != 2 {
				t.Fatalf("download requests = %d, want 2", downloads)
			}
		})
	}
}

func TestInstallerDoesNotPrintTheTokenWithVerboseOutput(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	const token = "secret-installer-token"
	run := runInstaller(t, github, nil, "GITHUB_TOKEN="+token, "VERBOSE=1", "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 || strings.Contains(run.output, token) {
		t.Fatalf("verbose install exit %d or leaked token:\n%s", run.code, run.output)
	}
}

func TestInstallerHelpUnknownFlagsAndAPIFailures(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	help := runInstaller(t, github, []string{"--help"})
	if help.code != 0 || !strings.Contains(help.output, "Channels:") || !strings.Contains(help.output, "Environment:") {
		t.Fatalf("help exit %d:\n%s", help.code, help.output)
	}
	unknown := runInstaller(t, github, []string{"--unknown"})
	if unknown.code != 2 || !strings.Contains(unknown.output, "Usage:") {
		t.Fatalf("unknown exit %d:\n%s", unknown.code, unknown.output)
	}
	github.failAPI = true
	failure := runInstaller(t, github, nil)
	if failure.code != 1 || !strings.Contains(failure.output, "GitHub's API rate limit for this address is used up for up to an hour; export GITHUB_TOKEN to use your own, or pin VERSION=<tag>") {
		t.Fatalf("API failure exit %d:\n%s", failure.code, failure.output)
	}
}

// C8: Anonymous refusals name the reason and retain the version escape hatch.
func TestInstallerNamesBothReasonsWhenTheAPIRefuses(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*installGitHub)
		want    string
	}{
		{
			name: "429 quota", prepare: func(github *installGitHub) { github.failAPI = true },
			want: "GitHub's API rate limit for this address is used up for up to an hour; export GITHUB_TOKEN to use your own, or pin VERSION=<tag>",
		},
		{
			name: "403 quota", prepare: func(github *installGitHub) {
				github.refuse = &apiRefusal{status: 403, headers: map[string]string{"X-RateLimit-Remaining": "0"}}
			},
			want: "GitHub's API rate limit for this address is used up for up to an hour; export GITHUB_TOKEN to use your own, or pin VERSION=<tag>",
		},
		{
			name: "private repository", prepare: func(github *installGitHub) { github.notFound = true },
			want: "GitHub's API cannot find Agent-Field/codeaf (404), which is also its answer for a repository you cannot read; export GITHUB_TOKEN, or pin VERSION=<tag>",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			github := newInstallGitHub(t, "dev-20260915-aaaaaaaaaaaa")
			test.prepare(github)
			run := runInstaller(t, github, []string{"--dev"})
			if run.code != 1 || !strings.Contains(run.stderr, "codeaf: "+test.want+"\n") {
				t.Fatalf("exit %d, want refusal %q:\n%s", run.code, test.want, run.output)
			}
			if requests := installerAPIRequests(github); len(requests) != 1 {
				t.Fatalf("anonymous API requests = %d, want 1", len(requests))
			}
		})
	}
}

func TestInstallerUsesWgetWhenCurlIsAbsent(t *testing.T) {
	if _, err := exec.LookPath("wget"); err != nil {
		t.Skip("wget is not installed")
	}
	github := newInstallGitHub(t, "v1.2.3")
	run := runInstaller(t, github, nil, "PATH="+minimalPath(t, false), "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 || !strings.Contains(run.output, "Installed   codeaf v1.2.3\n") {
		t.Fatalf("wget install exit %d:\n%s", run.code, run.output)
	}
	missing := runInstaller(t, github, []string{"--version", "v9.9.9"}, "PATH="+minimalPath(t, false), "CODEAF_NO_MODIFY_PATH=1")
	if missing.code != 1 || !strings.Contains(missing.output, "v9.9.9") || !strings.Contains(missing.output, "check the tag on the Releases page") {
		t.Fatalf("wget missing release exit %d:\n%s", missing.code, missing.output)
	}
}

func TestInstallerNamesAnUnsupportedPlatform(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	path := minimalPath(t, true)
	uname := filepath.Join(path, "uname")
	if err := os.Remove(uname); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(uname, []byte("#!/bin/sh\nprintf 'plan9\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := runInstaller(t, github, nil, "PATH="+path, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 1 || !strings.Contains(run.output, "unsupported platform: plan9") {
		t.Fatalf("unsupported install exit %d:\n%s", run.code, run.output)
	}
}

func TestInstallerKeepsTheWebsiteChannelSeam(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const seams = "CHANNEL=\"${CHANNEL:-stable}\"\nINSTALL_NAME=\"${CODEAF_INSTALL_NAME:-codeaf}\""
	if strings.Count(string(raw), seams) != 1 || strings.Count(string(raw), "INSTALL_NAME=\"${CODEAF_INSTALL_NAME:-codeaf}\"") != 1 {
		t.Fatal("the website-rewritten CHANNEL and INSTALL_NAME lines are not adjacent and unique")
	}
}

// V1: --name and CODEAF_INSTALL_NAME install the selected dev build under the
// requested file, leave codeaf untouched, and reject every invalid name before writing.
func TestV1InstallerName(t *testing.T) {
	const tag = "dev-20260921-bbbbbbbbbbbb"
	github := newInstallGitHub(t, tag)

	t.Run("flag", func(t *testing.T) {
		dir := t.TempDir()
		codeaf := filepath.Join(dir, "codeaf")
		original := []byte("stable stays here")
		if err := os.WriteFile(codeaf, original, 0o755); err != nil {
			t.Fatal(err)
		}
		run := runInstaller(t, github, []string{"--name", "devaf", "--dev"},
			"CODEAF_INSTALL_DIR="+dir, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 0 {
			t.Fatalf("exit %d:\n%s", run.code, run.output)
		}
		devaf := filepath.Join(dir, "devaf")
		if _, err := os.Stat(devaf); err != nil {
			t.Fatal(err)
		}
		kept, err := os.ReadFile(codeaf)
		if err != nil || string(kept) != string(original) {
			t.Fatalf("codeaf = %q, %v", kept, err)
		}
		// A normal run's receipt names the COMMAND the person will type first,
		// then the installed file naming its build; the last thing said is the
		// line to paste, and the path is --verbose's to say. A devaf install that
		// said "installed codeaf" sent the person to type a command this install
		// never wrote (the fresh-install check of 2026-09-25).
		if !strings.Contains(run.output, "Installed   devaf · codeaf "+tag+"\n") {
			t.Fatalf("output does not carry the receipt naming devaf:\n%s", run.output)
		}
		if strings.Contains(run.output, "Installed   codeaf") {
			t.Fatalf("a devaf install's receipt names codeaf:\n%s", run.output)
		}
		// The guide tells the person to type the name this install wrote, and
		// the PATH line stands on a line of its own so it can be pasted whole.
		if !strings.Contains(run.output, "\n     devaf\n") || strings.Contains(run.output, "\n     codeaf\n") {
			t.Fatalf("the guide does not send the person to devaf:\n%s", run.output)
		}
		pasteable := false
		for _, line := range strings.Split(run.output, "\n") {
			if strings.TrimSpace(line) == `export PATH="`+dir+`:$PATH"` {
				pasteable = true
			}
		}
		if !pasteable {
			t.Fatalf("no line is the bare PATH line to paste:\n%s", run.output)
		}
		verbose := runInstaller(t, github, []string{"--name", "devaf", "--dev", "--verbose"},
			"CODEAF_INSTALL_DIR="+dir, "CODEAF_NO_MODIFY_PATH=1")
		if verbose.code != 0 || !strings.Contains(verbose.output, "codeaf: installed "+devaf) {
			t.Fatalf("verbose output does not name %s (exit %d):\n%s", devaf, verbose.code, verbose.output)
		}
	})

	t.Run("environment and flag precedence", func(t *testing.T) {
		dir := t.TempDir()
		fromEnv := runInstaller(t, github, []string{"--dev"},
			"CODEAF_INSTALL_DIR="+dir, "CODEAF_INSTALL_NAME=devaf", "CODEAF_NO_MODIFY_PATH=1")
		if fromEnv.code != 0 {
			t.Fatalf("environment exit %d:\n%s", fromEnv.code, fromEnv.output)
		}
		if _, err := os.Stat(filepath.Join(dir, "devaf")); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(fromEnv.output, "Installed   devaf · codeaf "+tag+"\n") {
			t.Fatalf("an install named from the environment does not name devaf in its receipt:\n%s", fromEnv.output)
		}
		flag := runInstaller(t, github, []string{"--name", "mine", "--dev"},
			"CODEAF_INSTALL_DIR="+dir, "CODEAF_INSTALL_NAME=ignored", "CODEAF_NO_MODIFY_PATH=1")
		if flag.code != 0 {
			t.Fatalf("flag exit %d:\n%s", flag.code, flag.output)
		}
		if _, err := os.Stat(filepath.Join(dir, "mine")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "ignored")); !os.IsNotExist(err) {
			t.Fatalf("environment overrode flag: %v", err)
		}
	})

	for _, invalid := range []string{"../x", "-x", ""} {
		t.Run("invalid "+invalid, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "install")
			run := runInstaller(t, github, []string{"--name", invalid, "--dev"},
				"CODEAF_INSTALL_DIR="+dir, "CODEAF_NO_MODIFY_PATH=1")
			if run.code != 2 || !strings.Contains(run.output, "must match ^[A-Za-z0-9][A-Za-z0-9._-]*$") {
				t.Fatalf("exit %d:\n%s", run.code, run.output)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("invalid name wrote install directory: %v", err)
			}
		})
	}

	t.Run("invalid from the environment", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "install")
		run := runInstaller(t, github, []string{"--dev"},
			"CODEAF_INSTALL_DIR="+dir, "CODEAF_INSTALL_NAME=../x", "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 2 || !strings.Contains(run.output, "must match ^[A-Za-z0-9][A-Za-z0-9._-]*$") {
			t.Fatalf("exit %d:\n%s", run.code, run.output)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("invalid environment name wrote install directory: %v", err)
		}
	})

	t.Run("no word after the flag", func(t *testing.T) {
		run := runInstaller(t, github, []string{"--name"}, "CODEAF_NO_MODIFY_PATH=1")
		if run.code != 2 || !strings.Contains(run.output, "--name needs a word") {
			t.Fatalf("exit %d:\n%s", run.code, run.output)
		}
	})
}

// V2: The website name seam is the one exact line beneath CHANNEL, and the
// installer's help names both ways to choose it.
func TestV2InstallerNameSeamAndHelp(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const nameLine = "INSTALL_NAME=\"${CODEAF_INSTALL_NAME:-codeaf}\""
	if strings.Count(string(raw), nameLine) != 1 || !strings.Contains(string(raw), "CHANNEL=\"${CHANNEL:-stable}\"\n"+nameLine+"\n") {
		t.Fatal("the installer name seam is not unique and directly below CHANNEL")
	}
	github := newInstallGitHub(t, "v1.2.3")
	help := runInstaller(t, github, []string{"--help"})
	if help.code != 0 || !strings.Contains(help.output, "--name WORD") || !strings.Contains(help.output, "CODEAF_INSTALL_NAME") {
		t.Fatalf("help exit %d:\n%s", help.code, help.output)
	}
}

// A piped install cannot change its parent shell's PATH, so the installer links
// the command into a folder already on PATH, and the guide then has no line to
// paste. A file there that is not a link is somebody else's install and is left
// alone, and the paste line comes back.
func TestInstallerLinksIntoAFolderAlreadyOnPath(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	home := t.TempDir()
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	path := local + ":" + minimalPath(t, true)
	installDir := filepath.Join(home, ".codeaf", "bin")
	run := runInstaller(t, github, nil, "HOME="+home, "PATH="+path, "CODEAF_INSTALL_DIR="+installDir)
	if run.code != 0 {
		t.Fatalf("exit %d:\n%s", run.code, run.output)
	}
	target, err := os.Readlink(filepath.Join(local, "codeaf"))
	if err != nil || target != filepath.Join(installDir, "codeaf") {
		t.Fatalf("link = %q, %v", target, err)
	}
	if !strings.Contains(run.output, "ready in this terminal") || strings.Contains(run.output, "export PATH=") {
		t.Fatalf("a linked install still asks for a PATH line:\n%s", run.output)
	}
	// Installing again finds its own link and refreshes it.
	rerun := runInstaller(t, github, nil, "HOME="+home, "PATH="+path, "CODEAF_INSTALL_DIR="+installDir)
	if rerun.code != 0 {
		t.Fatalf("exit %d:\n%s", rerun.code, rerun.output)
	}
	if target, err := os.Readlink(filepath.Join(local, "codeaf")); err != nil || target != filepath.Join(installDir, "codeaf") || !strings.Contains(rerun.output, "ready in this terminal") {
		t.Fatalf("a second install did not keep its own link: %q, %v\n%s", target, err, rerun.output)
	}

	// A link to another build is somebody's choice too: a developer's source
	// build linked into ~/.local/bin must survive a stable install.
	other := filepath.Join(home, "src", "codeaf", "bin", "codeaf")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("#!/bin/sh\necho source build\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(local, "codeaf")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(local, "codeaf")); err != nil {
		t.Fatal(err)
	}
	linked := runInstaller(t, github, nil, "HOME="+home, "PATH="+path, "CODEAF_INSTALL_DIR="+installDir)
	if linked.code != 0 {
		t.Fatalf("exit %d:\n%s", linked.code, linked.output)
	}
	if target, err := os.Readlink(filepath.Join(local, "codeaf")); err != nil || target != other {
		t.Fatalf("the installer replaced a link to another build: %q, %v", target, err)
	}
	if !strings.Contains(linked.output, `export PATH="$HOME/.codeaf/bin:$PATH"`) || strings.Contains(linked.output, "ready in this terminal") {
		t.Fatalf("a link to another build does not leave the PATH line:\n%s", linked.output)
	}

	theirs := []byte("#!/bin/sh\necho someone else's codeaf\n")
	if err := os.Remove(filepath.Join(local, "codeaf")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "codeaf"), theirs, 0o755); err != nil {
		t.Fatal(err)
	}
	again := runInstaller(t, github, nil, "HOME="+home, "PATH="+path, "CODEAF_INSTALL_DIR="+installDir)
	if again.code != 0 {
		t.Fatalf("exit %d:\n%s", again.code, again.output)
	}
	kept, err := os.ReadFile(filepath.Join(local, "codeaf"))
	if err != nil || string(kept) != string(theirs) {
		t.Fatalf("the installer replaced a file that was not its link: %q, %v", kept, err)
	}
	if !strings.Contains(again.output, `export PATH="$HOME/.codeaf/bin:$PATH"`) {
		t.Fatalf("a shadowed install does not give the PATH line:\n%s", again.output)
	}
}

func installerAPIRequests(github *installGitHub) []*http.Request {
	github.mu.Lock()
	defer github.mu.Unlock()
	var requests []*http.Request
	for _, request := range github.requests {
		if strings.HasPrefix(request.URL.Path, "/repos/") {
			requests = append(requests, request)
		}
	}
	return requests
}

func assertInstallerAnonymousRetry(t *testing.T, github *installGitHub, token, path string) {
	t.Helper()
	requests := installerAPIRequests(github)
	if len(requests) != 2 {
		t.Fatalf("API requests = %d, want one token attempt and one anonymous retry", len(requests))
	}
	for i, wantAuth := range []string{"Bearer " + token, ""} {
		if requests[i].URL.RequestURI() != path || requests[i].Header.Get("Authorization") != wantAuth {
			t.Errorf("request %d: %s, Authorization %q; want %s, %q", i,
				requests[i].URL.RequestURI(), requests[i].Header.Get("Authorization"), path, wantAuth)
		}
	}
}

// C1: Both discovery paths and both transports can use the free anonymous bucket.
func TestInstallerC1RetriesSpentTokenQuotaAnonymously(t *testing.T) {
	reset := time.Now().Unix() + 60
	for _, transport := range []string{"curl", "wget"} {
		t.Run(transport, func(t *testing.T) {
			if _, err := exec.LookPath(transport); err != nil {
				t.Skip(transport + " is not installed")
			}
			for _, channel := range []struct {
				name, tag, path string
				arguments       []string
			}{
				{"stable", "v1.2.3", "/repos/Agent-Field/codeaf/releases/latest", nil},
				{"dev", "dev-20260915-aaaaaaaaaaaa", "/repos/Agent-Field/codeaf/releases?per_page=100", []string{"--dev"}},
			} {
				t.Run(channel.name, func(t *testing.T) {
					github := newInstallGitHub(t, channel.tag)
					github.refuse = &apiRefusal{
						status: 403, tokenOnly: true,
						headers: map[string]string{
							"X-RateLimit-Remaining": "0", "X-RateLimit-Limit": "5000",
							"X-RateLimit-Reset": fmt.Sprint(reset), "X-OAuth-Scopes": "repo, workflow",
						},
						body: `{"message":"API rate limit exceeded for user ID 1."}`,
					}
					const token = "spent-installer-token"
					run := runInstaller(t, github, channel.arguments,
						"GITHUB_TOKEN="+token, "PATH="+minimalPath(t, transport == "curl"), "CODEAF_NO_MODIFY_PATH=1")
					if run.code != 0 || !strings.Contains(run.output, "Installed   codeaf "+channel.tag+"\n") {
						t.Fatalf("exit %d without installed receipt:\n%s", run.code, run.output)
					}
					if _, err := os.Stat(filepath.Join(run.installDir, "codeaf")); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(run.stderr, "codeaf: GitHub refused GITHUB_TOKEN (its rate limit is used up until ") ||
						!strings.Contains(run.stderr, "); continuing without it\n") || strings.Count(run.stderr, "GitHub refused GITHUB_TOKEN") != 1 {
						t.Fatalf("missing recovery note:\n%s", run.output)
					}
					assertInstallerAnonymousRetry(t, github, token, channel.path)
					for _, request := range github.requests {
						if strings.Contains(request.URL.Path, "/releases/download/") && request.Header.Get("Authorization") != "" {
							t.Errorf("download carries Authorization: %s", request.URL.Path)
						}
					}
				})
			}
		})
	}
}

// C2 and C3: A second refusal explains both attempts and gives token-specific advice.
func TestInstallerC2C3NamesRejectedCredentialsAndAccess(t *testing.T) {
	for _, test := range []struct {
		name, message, want, absent string
		status                      int
	}{
		{
			name: "C2 credentials", status: 401, message: "Bad credentials",
			want: "GitHub rejected GITHUB_TOKEN (401: Bad credentials), and the request without it failed too (401: Bad credentials); replace or unset GITHUB_TOKEN, or pin VERSION=<tag>",
		},
		{
			name: "C3 access", status: 403, message: "Resource not accessible by personal access token", absent: "rate limit",
			want: "GitHub refused GITHUB_TOKEN access to Agent-Field/codeaf (403: Resource not accessible by personal access token), and the request without it failed too (403: Resource not accessible by personal access token); check what GITHUB_TOKEN may read, or pin VERSION=<tag>",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			github := newInstallGitHub(t, "v1.2.3")
			body, err := json.Marshal(map[string]string{"message": test.message})
			if err != nil {
				t.Fatal(err)
			}
			github.refuse = &apiRefusal{status: test.status, body: string(body)}
			const token = "refused-installer-token"
			run := runInstaller(t, github, nil, "GITHUB_TOKEN="+token)
			if run.code != 1 || !strings.Contains(run.stderr, "codeaf: "+test.want+"\n") {
				t.Fatalf("exit %d, want %q:\n%s", run.code, test.want, run.output)
			}
			if strings.Contains(run.output, "export GITHUB_TOKEN") || (test.absent != "" && strings.Contains(run.output, test.absent)) {
				t.Fatalf("misleading recovery advice:\n%s", run.output)
			}
			assertInstallerAnonymousRetry(t, github, token, "/repos/Agent-Field/codeaf/releases/latest")
		})
	}
}

// C4: GitHub's reset epoch is rendered in the machine's local zone for either bucket.
func TestInstallerC4NamesQuotaResetInLocalTime(t *testing.T) {
	const reset = int64(1800000000)
	wait := "until " + time.Unix(reset, 0).In(time.FixedZone("IST", 19800)).Format("15:04") + " IST"
	for _, withToken := range []bool{false, true} {
		t.Run(fmt.Sprint("token=", withToken), func(t *testing.T) {
			github := newInstallGitHub(t, "v1.2.3")
			github.refuse = &apiRefusal{status: 403, headers: map[string]string{
				"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(reset),
			}, body: `{"message":"API rate limit exceeded"}`}
			env := []string{"TZ=IST-5:30"}
			want := "GitHub's API rate limit for this address is used up " + wait + "; export GITHUB_TOKEN to use your own, or pin VERSION=<tag>"
			if withToken {
				env = append(env, "GITHUB_TOKEN=spent-installer-token")
				want = "GitHub's API rate limit for GITHUB_TOKEN is used up " + wait + ", and the request without it failed too (this address's rate limit is used up " + wait + "); wait, or pin VERSION=<tag>"
			}
			run := runInstaller(t, github, nil, env...)
			if run.code != 1 || !strings.Contains(run.stderr, "codeaf: "+want+"\n") {
				t.Fatalf("exit %d, want %q:\n%s", run.code, want, run.output)
			}
			if withToken && strings.Contains(run.output, "export GITHUB_TOKEN") {
				t.Fatalf("a configured token was answered with export advice:\n%s", run.output)
			}
		})
	}
}

// C5 and C6: The supplying variable is named and its value stays private, even on recovery.
func TestInstallerC5C6NamesTheTokenVariableWithoutPrintingItsValue(t *testing.T) {
	for _, test := range []struct {
		name, variable string
		env            []string
	}{
		{"GH_TOKEN alone", "GH_TOKEN", nil},
		{"empty GITHUB_TOKEN", "GH_TOKEN", []string{"GITHUB_TOKEN="}},
		{"GITHUB_TOKEN wins", "GITHUB_TOKEN", []string{"GH_TOKEN=unused-token"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, recover := range []bool{false, true} {
				t.Run(fmt.Sprint("recover=", recover), func(t *testing.T) {
					github := newInstallGitHub(t, "v1.2.3")
					github.refuse = &apiRefusal{status: 401, body: `{"message":"Bad credentials"}`, tokenOnly: recover}
					const token = "private-refused-installer-token"
					env := append([]string{"VERBOSE=1", "CODEAF_NO_MODIFY_PATH=1"}, test.env...)
					env = append(env, test.variable+"="+token)
					run := runInstaller(t, github, nil, env...)
					wantCode := 1
					want := "GitHub rejected " + test.variable + " (401: Bad credentials)"
					if recover {
						wantCode = 0
						want = "GitHub refused " + test.variable + " (401: Bad credentials); continuing without it"
					}
					if run.code != wantCode || !strings.Contains(run.stderr, want) {
						t.Fatalf("exit %d, want %d and %q:\n%s", run.code, wantCode, want, run.output)
					}
					if strings.Contains(run.output, token) || strings.Contains(run.output, "unused-token") || strings.Contains(run.output, "export GITHUB_TOKEN") ||
						(test.variable == "GH_TOKEN" && strings.Contains(run.output, "GITHUB_TOKEN")) {
						t.Fatalf("token leaked or the wrong variable was named:\n%s", run.output)
					}
					assertInstallerAnonymousRetry(t, github, token, "/repos/Agent-Field/codeaf/releases/latest")
				})
			}
		})
	}
}

// C7: Only 401, 403 and 429 spend the single anonymous retry.
func TestInstallerC7DoesNotRetryOtherTokenFailures(t *testing.T) {
	for _, status := range []int{404, 500, 502, 302, 400, 418} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			github := newInstallGitHub(t, "v1.2.3")
			github.refuse = &apiRefusal{status: status, body: `{"message":"A response"}`}
			run := runInstaller(t, github, []string{"--dev"}, "GITHUB_TOKEN=installer-token")
			want := fmt.Sprintf("GitHub's API answered %d (A response); pin VERSION=<tag>", status)
			if status == 404 {
				want = "GitHub's API cannot find Agent-Field/codeaf with GITHUB_TOKEN (404), which is also its answer for a repository the token cannot read; check what GITHUB_TOKEN may read, or pin VERSION=<tag>"
			} else if status >= 500 {
				want = fmt.Sprintf("GitHub's API answered %d and may be having trouble; try again in a minute, or pin VERSION=<tag>", status)
			}
			if run.code != 1 || !strings.Contains(run.stderr, "codeaf: "+want+"\n") || strings.Contains(run.output, "export GITHUB_TOKEN") {
				t.Fatalf("exit %d, want %q:\n%s", run.code, want, run.output)
			}
			if requests := installerAPIRequests(github); len(requests) != 1 || requests[0].Header.Get("Authorization") != "Bearer installer-token" {
				t.Fatalf("API requests = %v, want one token request", requests)
			}
		})
	}
}

func TestInstallerC7NamesTheUnreachableAPIHost(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	github := newInstallGitHub(t, "v1.2.3")
	for _, token := range []string{"", "GITHUB_TOKEN=installer-token"} {
		run := runInstaller(t, github, nil, "CODEAF_GITHUB_API=http://"+host+"/api", token)
		want := "GitHub's API could not be reached at " + host + "; check the connection, or pin VERSION=<tag>"
		if run.code != 1 || !strings.Contains(run.stderr, want) || strings.Contains(run.output, "export GITHUB_TOKEN") {
			t.Fatalf("exit %d, want %q:\n%s", run.code, want, run.output)
		}
	}
}

// D12: A retry's 404 describes both attempts, while a first 404 keeps the stable answer.
func TestInstallerKeepsStable404UnlessTheTokenWasRefused(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			github := newInstallGitHub(t)
			github.refuse = &apiRefusal{status: status, tokenOnly: true}
			run := runInstaller(t, github, nil, "GITHUB_TOKEN=installer-token")
			if run.code != 1 || !strings.Contains(run.stderr, "and the request without it failed too (404);") ||
				strings.Contains(run.output, "no stable build") || strings.Contains(run.output, "export GITHUB_TOKEN") {
				t.Fatalf("anonymous 404 lost the token refusal (exit %d):\n%s", run.code, run.output)
			}
			assertInstallerAnonymousRetry(t, github, "installer-token", "/repos/Agent-Field/codeaf/releases/latest")
		})
	}
	github := newInstallGitHub(t)
	run := runInstaller(t, github, nil, "GITHUB_TOKEN=installer-token")
	if run.code != 1 || !strings.Contains(run.stderr, "codeaf: no stable build has been published yet\n") || len(installerAPIRequests(github)) != 1 {
		t.Fatalf("first stable 404 changed (exit %d):\n%s", run.code, run.output)
	}
}

func TestInstallerNamesAnAnswerWithoutAReleaseTag(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	github.refuse = &apiRefusal{status: 200, body: `{}`}
	run := runInstaller(t, github, nil)
	if run.code != 1 || !strings.Contains(run.stderr, "codeaf: GitHub's API answered without a release tag; pin VERSION=<tag>\n") {
		t.Fatalf("exit %d without the missing-tag answer:\n%s", run.code, run.output)
	}
}

func installerPureFunctions(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const begin = "# BEGIN api refusal: pure functions, read by internal/release/install_test.go\n"
	const end = "# END api refusal\n"
	_, rest, ok := strings.Cut(string(raw), begin)
	if !ok || strings.Count(string(raw), begin) != 1 || strings.Count(string(raw), end) != 1 {
		t.Fatal("the installer must expose exactly one pure refusal block")
	}
	block, _, ok := strings.Cut(rest, end)
	if !ok {
		t.Fatal("the pure refusal block has no end")
	}
	path := filepath.Join(t.TempDir(), "api-functions.sh")
	if err := os.WriteFile(path, []byte(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runInstallerPureFunction(t *testing.T, block, function string, arguments []string, env ...string) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	command := exec.Command(bash, append([]string{"--noprofile", "--norc", "-c", `set -eu; source "$1"; shift; "$@"`, "api-test", block, function}, arguments...)...)
	command.Env = append([]string{"PATH=" + minimalPath(t, true), "TZ=IST-5:30"}, env...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", function, err, output)
	}
	return string(output)
}

func installerResponseFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// C9: Classification depends only on the status, the final header block and the body.
func TestInstallerC9APIReason(t *testing.T) {
	block := installerPureFunctions(t)
	for _, test := range []struct{ name, status, headers, body, want string }{
		{"401 before quota", "401", "x-ratelimit-remaining: 0\n", `{"message":"rate limit"}`, "rejected"},
		{"spent quota", "403", "x-ratelimit-remaining: 0\n", "", "quota"},
		{"body quota", "403", "", `{"message":"API rate limit exceeded"}`, "quota"},
		{"secondary quota", "403", "", `{"message":"You have exceeded a secondary rate limit"}`, "quota"},
		{"case insensitive body", "403", "", `{"message":"RATE LIMIT"}`, "quota"},
		{"429", "429", "", "", "quota"},
		{"quota still available", "403", "x-ratelimit-remaining: 4999\n", "", "access"},
		{"no quota evidence", "403", "", "", "access"},
		{"404", "404", "", "", "missing"},
		{"500", "500", "", "", "server"},
		{"503", "503", "", "", "server"},
		{"no status", "", "", "", "unreachable"},
		{"418", "418", "", "", "other"},
		{"mixed case", "403", "X-RateLimit-Remaining: 0\n", "", "quota"},
		{"CRLF and wget indentation", "403", "  HTTP/1.1 403 Forbidden\r\n  x-RATELIMIT-remaining: 0\r\n\r\n", "", "quota"},
		{"last block spent", "403", "HTTP/1.1 301 Moved\r\nx-ratelimit-remaining: 4999\r\n\r\nHTTP/2 403\r\nX-RateLimit-Remaining: 0\r\n\r\n", "", "quota"},
		{"last block available", "403", "HTTP/1.1 301 Moved\nx-ratelimit-remaining: 0\n\nHTTP/1.1 403 Forbidden\nx-ratelimit-remaining: 4999\n\n", "", "access"},
		{"last block absent", "403", "HTTP/1.1 301 Moved\nx-ratelimit-remaining: 0\n\nHTTP/1.1 403 Forbidden\n\n", "", "access"},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := installerResponseFile(t, "headers", test.headers)
			body := installerResponseFile(t, "body", test.body)
			if got := runInstallerPureFunction(t, block, "api_reason", []string{test.status, headers, body}); got != test.want {
				t.Errorf("api_reason = %q, want %q", got, test.want)
			}
		})
	}
}

func TestInstallerC9APIWait(t *testing.T) {
	block := installerPureFunctions(t)
	const reset = int64(1800000000)
	local := time.Unix(reset, 0).In(time.FixedZone("IST", 19800)).Format("15:04")
	for _, test := range []struct{ name, headers, want string }{
		{"local reset", fmt.Sprintf("x-ratelimit-reset: %d\nRetry-After: 600\n", reset), "until " + local + " IST"},
		{"30 seconds", "retry-after: 30\n", "for about a minute"},
		{"600 seconds", "retry-after: 600\n", "for about 10 minutes"},
		{"61 seconds", "retry-after: 61\n", "for about a minute"},
		{"90 seconds", "retry-after: 90\n", "for about a minute"},
		{"91 seconds", "retry-after: 91\n", "for about 2 minutes"},
		{"zero seconds", "retry-after: 0\n", "for about a minute"},
		{"leading zeros", "retry-after: 0091\n", "for about 2 minutes"},
		{"neither", "", "for up to an hour"},
		{"invalid reset", "x-ratelimit-reset: tomorrow\n", "for up to an hour"},
		{"invalid reset with retry", "x-ratelimit-reset: invalid\nretry-after: 600\n", "for about 10 minutes"},
		{"invalid retry", "retry-after: 2 minutes\n", "for up to an hour"},
		{"oversized reset", "x-ratelimit-reset: " + strings.Repeat("9", 38) + "\n", "for up to an hour"},
		{"oversized reset with retry", "x-ratelimit-reset: " + strings.Repeat("9", 38) + "\nretry-after: 600\n", "for about 10 minutes"},
		{"oversized retry", "retry-after: " + strings.Repeat("9", 38) + "\n", "for up to an hour"},
		{"largest retry", "retry-after: 999999\n", "for about 16667 minutes"},
		{"final block only", "HTTP/1.1 301 Moved\nx-ratelimit-reset: 1800000000\n\n  HTTP/1.1 403 Forbidden\r\n  Retry-After: 91\r\n", "for about 2 minutes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := installerResponseFile(t, "headers", test.headers)
			if got := runInstallerPureFunction(t, block, "api_wait", []string{headers}); got != test.want {
				t.Errorf("api_wait = %q, want %q", got, test.want)
			}
		})
	}
}

func TestInstallerC9APIMessage(t *testing.T) {
	block := installerPureFunctions(t)
	for _, test := range []struct{ name, body, want string }{
		{"message", `{"message":"Bad credentials"}`, "Bad credentials"},
		{"escaped quotes", `{"message":"Say \"no\", then \\ and \/"}`, `Say "no", then \ and /`},
		{"control bytes", "{\"message\":\"a\x01b\x1b[31m\tc\rd\ne\"}", "ab[31mcde"},
		{"JSON controls and unicode", `{"message":"a\nb\rc\td\u0000e\u0021é\u00e9"}`, "abcde!"},
		{"collapse spaces", `{"message":"Bad    credentials"}`, "Bad credentials"},
		{"cap", `{"message":"` + strings.Repeat("a", 101) + `"}`, strings.Repeat("a", 100) + "..."},
		{"at cap", `{"message":"` + strings.Repeat("a", 100) + `"}`, strings.Repeat("a", 100)},
		{"first message", `{"message":"first","other":{"message":"second"}}`, "first"},
		{"missing", `{}`, ""},
		{"empty", `{"message":""}`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := installerResponseFile(t, "body", test.body)
			if got := runInstallerPureFunction(t, block, "api_message", []string{body}); got != test.want {
				t.Errorf("api_message = %q, want %q", got, test.want)
			}
		})
	}
}

// The remaining anonymous sentences distinguish access, credentials and server failures.
func TestInstallerNamesAnonymousFailuresExactly(t *testing.T) {
	for _, test := range []struct {
		name, body, want string
		status           int
	}{
		{"access", `{"message":"Access denied"}`, "GitHub's API refused access to Agent-Field/codeaf (403: Access denied); pin VERSION=<tag>", 403},
		{"access without message", `{}`, "GitHub's API refused access to Agent-Field/codeaf (403); pin VERSION=<tag>", 403},
		{"credentials", `{"message":"Bad credentials"}`, "GitHub's API refused the request (401: Bad credentials); pin VERSION=<tag>", 401},
		{"server", `{}`, "GitHub's API answered 503 and may be having trouble; try again in a minute, or pin VERSION=<tag>", 503},
		{"other", `{"message":"A teapot"}`, "GitHub's API answered 418 (A teapot); pin VERSION=<tag>", 418},
		{"other without message", `{}`, "GitHub's API answered 418; pin VERSION=<tag>", 418},
	} {
		t.Run(test.name, func(t *testing.T) {
			github := newInstallGitHub(t, "v1.2.3")
			github.refuse = &apiRefusal{status: test.status, body: test.body}
			run := runInstaller(t, github, nil)
			if run.code != 1 || run.stderr != "codeaf: "+test.want+"\n" {
				t.Fatalf("exit %d, stderr %q; want %q", run.code, run.stderr, test.want)
			}
			if len(installerAPIRequests(github)) != 1 {
				t.Fatal("an anonymous failure was retried")
			}
		})
	}
}

// Wget's error body is kept where --content-on-error is supported.
func TestInstallerWgetKeepsTheAccessRefusalBody(t *testing.T) {
	wget, err := exec.LookPath("wget")
	if err != nil {
		t.Skip("wget is not installed")
	}
	help, _ := exec.Command(wget, "--help").CombinedOutput()
	if !strings.Contains(string(help), "--content-on-error") {
		t.Skip("wget cannot retain error bodies")
	}
	github := newInstallGitHub(t, "v1.2.3")
	github.refuse = &apiRefusal{status: 403, body: `{"message":"Resource not accessible by personal access token"}`}
	run := runInstaller(t, github, nil, "GITHUB_TOKEN=installer-token", "PATH="+minimalPath(t, false))
	const want = "GitHub refused GITHUB_TOKEN access to Agent-Field/codeaf (403: Resource not accessible by personal access token), and the request without it failed too (403: Resource not accessible by personal access token); check what GITHUB_TOKEN may read, or pin VERSION=<tag>"
	if run.code != 1 || run.stderr != "codeaf: "+want+"\n" {
		t.Fatalf("exit %d, want %q:\n%s", run.code, want, run.output)
	}
	assertInstallerAnonymousRetry(t, github, "installer-token", "/repos/Agent-Field/codeaf/releases/latest")
}

// D3: A wget without the advertised option must never be given that option.
func TestInstallerUsesWgetWithoutContentOnErrorSupport(t *testing.T) {
	wget, err := exec.LookPath("wget")
	if err != nil {
		t.Skip("wget is not installed")
	}
	path := minimalPath(t, false)
	wrapper := filepath.Join(path, "wget")
	if err := os.Remove(wrapper); err != nil {
		t.Fatal(err)
	}
	// The wrapper rejects the optional flag and otherwise uses a real transport.
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = --help ]; then printf '%s\\n' 'minimal wget'; exit 0; fi\n" +
		"for arg do if [ \"$arg\" = --content-on-error ]; then exit 64; fi; done\n" +
		"exec '" + strings.ReplaceAll(wget, "'", "'\\''") + "' \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	github := newInstallGitHub(t, "v1.2.3")
	github.refuse = &apiRefusal{status: 403, tokenOnly: true, headers: map[string]string{"X-RateLimit-Remaining": "0"}}
	run := runInstaller(t, github, nil, "GITHUB_TOKEN=installer-token", "PATH="+path, "CODEAF_NO_MODIFY_PATH=1")
	const want = "GitHub refused GITHUB_TOKEN (its rate limit is used up for up to an hour); continuing without it"
	if run.code != 0 || !strings.Contains(run.stderr, want) || !strings.Contains(run.output, "Installed   codeaf v1.2.3\n") {
		t.Fatalf("exit %d without the wget fallback:\n%s", run.code, run.output)
	}
	assertInstallerAnonymousRetry(t, github, "installer-token", "/repos/Agent-Field/codeaf/releases/latest")
}

// D7: Failed or unparseable GNU output gives BSD date its turn; the zone may be empty.
func TestInstallerEpochRenderingTriesHostDateSyntaxInOrder(t *testing.T) {
	block := installerPureFunctions(t)
	for _, test := range []struct{ name, date, want, calls string }{
		{"GNU", "printf '12:34 GNU\\n'", "12:34 GNU", "-d\n"},
		{"BSD after failure", "if [ \"$1\" = -d ]; then exit 1; fi\nprintf '12:35 BSD\\n'", "12:35 BSD", "-d\n-r\n"},
		{"BSD after invalid output", "if [ \"$1\" = -d ]; then printf 'invalid\\n'; else printf '12:36 BSD\\n'; fi", "12:36 BSD", "-d\n-r\n"},
		{"empty zone", "printf '12:37 \\n'", "12:37", "-d\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := t.TempDir()
			log := filepath.Join(t.TempDir(), "date-calls")
			script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$DATE_CALLS\"\n" + test.date + "\n"
			if err := os.WriteFile(filepath.Join(path, "date"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			if got := runInstallerPureFunction(t, block, "api_epoch", []string{"1800000000"}, "PATH="+path, "DATE_CALLS="+log); got != test.want {
				t.Errorf("api_epoch = %q, want %q", got, test.want)
			}
			calls, err := os.ReadFile(log)
			if err != nil || string(calls) != test.calls {
				t.Errorf("date calls = %q, %v; want %q", calls, err, test.calls)
			}
		})
	}
}

func TestInstallerRunsWhenDateIsAbsent(t *testing.T) {
	github := newInstallGitHub(t, "v1.2.3")
	path := minimalPath(t, true)
	if err := os.Remove(filepath.Join(path, "date")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	github.refuse = &apiRefusal{status: 429, tokenOnly: true, headers: map[string]string{"X-RateLimit-Reset": "1800000000"}}
	run := runInstaller(t, github, nil, "GITHUB_TOKEN=installer-token", "PATH="+path, "CODEAF_NO_MODIFY_PATH=1")
	if run.code != 0 || !strings.Contains(run.stderr, "its rate limit is used up for up to an hour") {
		t.Fatalf("a missing optional date broke the install (exit %d):\n%s", run.code, run.output)
	}
}

func TestInstallerEpochRenderingFallsBackToBashWhenDateCannotRender(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	probe := exec.Command(bash, "--noprofile", "--norc", "-c", `printf '%(%H:%M %Z)T' 1800000000`)
	if _, err := probe.Output(); err != nil {
		t.Skip("bash predates the time-format printf fallback")
	}
	block := installerPureFunctions(t)
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "date"), []byte("#!/bin/sh\nprintf 'cannot render\\n' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := time.Unix(1800000000, 0).In(time.FixedZone("IST", 19800)).Format("15:04") + " IST"
	if got := runInstallerPureFunction(t, block, "api_epoch", []string{"1800000000"}, "PATH="+path); got != want {
		t.Errorf("api_epoch = %q, want %q", got, want)
	}
}
