//go:build linux

package sessionhost

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	chatA = "hs_aaaaaaaaaaaaaaaaaaaaaaaa"
	chatB = "hs_bbbbbbbbbbbbbbbbbbbbbbbb"
	chatC = "hs_cccccccccccccccccccccccc"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := ConfigFromEnv(func(key string) string {
		if key == "CODEAF_SESSIONHOST_SECRET" {
			return strings.Repeat("s", 32)
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Data = t.TempDir()
	return cfg
}

type fakeProcesses struct {
	live processTable
}

func (p *fakeProcesses) snapshot() (processTable, error) { return p.live, nil }

func (p *fakeProcesses) KillAll(uid int, _ time.Duration) error {
	delete(p.live, uid)
	return nil
}

type fakeLauncher struct {
	starts int
	reaps  int
}

func (l *fakeLauncher) reap(_ int, folders layout, mode reapMode) error {
	l.reaps++
	fd, err := openReapDir(folders.dir)
	if err != nil {
		return err
	}
	defer fd.Close()
	return reapAt(int(fd.Fd()), mode)
}

func (l *fakeLauncher) start(_ int, _ layout, _ []string) (*engineChild, error) {
	l.starts++
	return nil, errors.New("fixture has no child")
}

func testSupervisor(t *testing.T, cfg Config, report Report) (*supervisor, *fakeLauncher, *fakeProcesses) {
	t.Helper()
	launcher := &fakeLauncher{}
	procs := &fakeProcesses{live: make(processTable)}
	s := newTestSupervisor(t, cfg, report, launcher, procs)
	return s, launcher, procs
}

func newTestSupervisor(t *testing.T, cfg Config, report Report, launcher Launcher, procs *fakeProcesses) *supervisor {
	t.Helper()
	s, err := newSupervisor(cfg, report, supervisorDeps{
		launcher: launcher, processes: procs, now: time.Now,
		chown:   func(string, int, int) error { return nil },
		isOwner: func(os.FileInfo, int) bool { return true },
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.closeWires()
		// A hijacked connection finishes asynchronously. Join its final
		// registry write before testing removes the temporary volume.
		s.wireWG.Wait()
	})
	return s
}

func healthyReport() Report {
	return Report{Require: modeFenced, UIDSwitch: SwitchReport{OK: true}, Landlock: LandlockReport{Available: true, ABI: 7}}.decide()
}

func request(h http.Handler, method, path, secret, person, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if secret != "" {
		r.Header.Set("Authorization", "Bearer "+secret)
	}
	if person != "" {
		r.Header.Set("X-Codeaf-Person", person)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func createChat(t *testing.T, s *supervisor, person, id string) *httptest.ResponseRecorder {
	t.Helper()
	w := request(s, "POST", "/v1/sessions", s.cfg.Secret, person,
		`{"id":"`+id+`","model_url":"http://model.test/v1","model_token":"made-up-model-bearer"}`)
	if w.Code != 201 {
		t.Fatalf("create chat: %d %s", w.Code, w.Body)
	}
	return w
}

func wantError(t *testing.T, w *httptest.ResponseRecorder, status int, sentence string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"error": sentence})
	if w.Code != status || w.Body.String() != string(body)+"\n" {
		t.Fatalf("wanted %d %s, got %d %s", status, body, w.Code, w.Body)
	}
}

func TestC4_ProbeRefusesWhenForced(t *testing.T) {
	cfg := testConfig(t)
	cfg.ForceProbe = "fail"
	report := Probe(cfg)
	if report.OK || report.Mode != "refused" || report.Reason != "forced to fail by CODEAF_SESSIONHOST_FORCE_PROBE" {
		t.Fatalf("unexpected report: %+v", report)
	}
	s, launcher, _ := testSupervisor(t, cfg, report)
	w := request(s, "POST", "/v1/sessions", cfg.Secret, "alice",
		`{"id":"`+chatA+`","model_url":"http://model.test/v1","model_token":"fake"}`)
	wantError(t, w, 503, "This server cannot keep your chat apart from other people's, so it did not start one.")
	if launcher.starts != 0 || launcher.reaps != 0 {
		t.Fatal("refused create started a process")
	}
}

func TestC4_UidModeNeedsExplicitRequire(t *testing.T) {
	cfg := testConfig(t)
	report := Report{Require: cfg.Require, UIDSwitch: SwitchReport{OK: true}}
	if got := report.decide(); got.OK || got.Mode != "refused" {
		t.Fatalf("missing command restrictions passed by default: %+v", got)
	}
	cfg.Require = "uid"
	report.Require = cfg.Require
	got := report.decide()
	if !got.OK || got.Mode != "uid" {
		t.Fatalf("explicit process-only isolation failed: %+v", got)
	}
	s, _, _ := testSupervisor(t, cfg, got)
	w := createChat(t, s, "alice", chatA)
	var row struct{ Warning string }
	if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil ||
		row.Warning != "This server keeps your chat apart from other people's, but does not limit what its commands can reach." {
		t.Fatalf("missing warning: %s, %v", w.Body, err)
	}
	report.UIDSwitch = SwitchReport{Error: "could not switch identities"}
	for _, requirement := range []string{"uid", "uid+fence"} {
		report.Require = requirement
		if got := report.decide(); got.OK || got.Mode != "refused" {
			t.Fatalf("identity switch failure passed with %s: %+v", requirement, got)
		}
	}
}

func TestC10_OtherPersonGetsTheSameAnswerAsNoSuchSession(t *testing.T) {
	cfg := testConfig(t)
	s, launcher, _ := testSupervisor(t, cfg, healthyReport())
	createChat(t, s, " Alice ", chatA)
	for _, tc := range []struct{ method, suffix string }{
		{"GET", ""}, {"POST", "/stop"}, {"DELETE", ""}, {"GET", "/wire"},
	} {
		w := request(s, tc.method, "/v1/sessions/"+chatA+tc.suffix, cfg.Secret, "bob", "")
		missing := request(s, tc.method, "/v1/sessions/"+chatB+tc.suffix, cfg.Secret, "bob", "")
		wantError(t, w, 404, "There is no such chat.")
		if missing.Code != w.Code || !bytes.Equal(w.Body.Bytes(), missing.Body.Bytes()) {
			t.Fatalf("%s %s distinguishes another person's chat from a missing one", tc.method, tc.suffix)
		}
	}
	if launcher.starts != 0 || launcher.reaps != 1 {
		t.Fatal("another person triggered a helper")
	}
	w := request(s, "GET", "/v1/sessions", cfg.Secret, "bob", "")
	if w.Code != 200 || w.Body.String() != "{\"sessions\":[]}\n" {
		t.Fatalf("another person saw a session: %d %s", w.Code, w.Body)
	}
}

func TestC10_MissingOrWrongSecretIsEmpty401(t *testing.T) {
	cfg := testConfig(t)
	s, _, _ := testSupervisor(t, cfg, healthyReport())
	for _, tc := range []struct{ path, secret, person string }{
		{"/v1/sessions", "", "alice"},
		{"/v1/sessions", strings.Repeat("x", 32), "alice"},
		{"/v1/sessions", cfg.Secret, ""},
		{"/v1/sessions", cfg.Secret, "bad handle"},
		{"/v1/health", "", ""},
		{"/unknown", "bad-key", ""},
	} {
		w := request(s, "GET", tc.path, tc.secret, tc.person, "")
		if w.Code != 401 || w.Body.Len() != 0 {
			t.Fatalf("wanted an empty 401, got %d %q", w.Code, w.Body.String())
		}
	}
	w := request(s, "GET", "/v1/health", cfg.Secret, "", "")
	if w.Code != 200 || w.Body.String() != "{\"ok\":true,\"mode\":\"uid+fence\",\"reason\":\"\"}\n" {
		t.Fatalf("health refused its valid bearer: %d %s", w.Code, w.Body)
	}
}

func TestC3_EngineEnvironmentIsExactlyTheAllowList(t *testing.T) {
	cfg := testConfig(t)
	cfg.Model = "example-model"
	for _, name := range []string{"OPENROUTER_API_KEY", "GH_TOKEN", "CODEAF_SERVER_KEY", "CODEAF_SESSIONHOST_SECRET", "OPENAI_API_KEY", "TMPDIR"} {
		t.Setenv(name, "poisoned-supervisor-value")
	}
	launcher := newWireTestLauncher()
	s := newTestSupervisor(t, cfg, healthyReport(), launcher, &fakeProcesses{live: make(processTable)})
	createChat(t, s, "alice", chatA)
	creds := putTestCredentials(t, s, "updated-model-token")
	conn := dialTestWire(t, s)
	defer conn.CloseNow()

	folders, err := layoutFor(cfg.Data, "alice", chatA)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"HOME": folders.home(), "CODEAF_HOME": folders.home(), "TMPDIR": folders.tmp(),
		"PATH": "/usr/local/bin:/usr/bin:/bin", "LANG": "C.UTF-8",
		"CODEAF_TELEMETRY": "off", "DO_NOT_TRACK": "1", "CODEAF_NO_UPDATE_CHECK": "1",
		"CODEAF_BASE_URL": creds.ModelURL, "OPENAI_API_KEY": creds.ModelToken, "CODEAF_ENGINE_IDLE": "30m0s",
		"CODEAF_FENCE_REQUIRE": "1", "CODEAF_MODEL": "example-model",
		// P1 item 0c adds exactly this one name: the ports a chat's commands may connect to.
		"CODEAF_FENCE_CONNECT_PORTS": "22,80,443,9418",
	}
	env := launcher.environment(t)
	got := make(map[string]string)
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatal("environment entry has no value")
		}
		got[name] = value
	}
	if len(env) != len(want) || !reflect.DeepEqual(got, want) {
		t.Fatalf("engine environment differs from the allowlist: %v", got)
	}
}

func TestC3_CredentialsNeverTouchDisk(t *testing.T) {
	cfg := testConfig(t)
	s, _, _ := testSupervisor(t, cfg, healthyReport())
	w := createChat(t, s, "alice", chatA)
	if strings.Contains(w.Body.String(), "made-up-model-bearer") {
		t.Fatal("create echoed model credentials")
	}
	putTestCredentials(t, s, "second-made-up-model-bearer")
	err := filepath.WalkDir(cfg.Data, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if bytes.Contains(data, []byte("made-up-model-bearer")) {
			t.Errorf("model credentials were stored in %s", entry.Name())
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func putTestCredentials(t *testing.T, s *supervisor, token string) credentials {
	t.Helper()
	const modelURL = "http://model.test/other"
	w := request(s, "PUT", "/v1/sessions/"+chatA+"/credentials", s.cfg.Secret, "alice",
		`{"model_url":"`+modelURL+`","model_token":"`+token+`"}`)
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("update credentials: %d %s", w.Code, w.Body)
	}
	return credentials{ModelURL: modelURL, ModelToken: token}
}

func TestC7_LimitsAnswerWithASentence(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxRunning, cfg.MaxPerPerson = 1, 1
	s, launcher, proc := testSupervisor(t, cfg, healthyReport())
	createChat(t, s, "alice", chatA)
	createChat(t, s, "alice", chatB)
	createChat(t, s, "bob", chatC)
	proc.live[cfg.UIDBase] = []int{4321}
	w := request(s, "GET", "/v1/sessions/"+chatC+"/wire", cfg.Secret, "bob", "")
	wantError(t, w, 429, "Waiting for a free slot (1 of 1 in use).")
	s.cfg.MaxRunning = 2
	w = request(s, "GET", "/v1/sessions/"+chatB+"/wire", cfg.Secret, "alice", "")
	wantError(t, w, 429, "You already have 1 chats running; stop one first.")
	if launcher.starts != 0 {
		t.Fatal("a refused attach started an engine")
	}
}

func TestC7_StateIsDerivedFromProcesses(t *testing.T) {
	cfg := testConfig(t)
	s, _, proc := testSupervisor(t, cfg, healthyReport())
	createChat(t, s, "alice", chatA)
	state := func() string {
		w := request(s, "GET", "/v1/sessions/"+chatA, cfg.Secret, "alice", "")
		var row struct{ State string }
		if err := json.Unmarshal(w.Body.Bytes(), &row); w.Code != 200 || err != nil {
			t.Fatalf("get chat: %d %s", w.Code, w.Body)
		}
		return row.State
	}
	if got := state(); got != "suspended" {
		t.Fatalf("empty process table gave %q", got)
	}
	proc.live[cfg.UIDBase] = []int{4321}
	if got := state(); got != "running" {
		t.Fatalf("live process gave %q", got)
	}
	delete(proc.live, cfg.UIDBase)
	if got := state(); got != "suspended" {
		t.Fatalf("process exit gave %q", got)
	}
}

func TestC2_LayoutModesAndTraversal(t *testing.T) {
	cfg := testConfig(t)
	s, _, _ := testSupervisor(t, cfg, healthyReport())
	createChat(t, s, "alice", chatA)
	l, err := layoutFor(cfg.Data, "alice", chatA)
	if err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{
		filepath.Join(cfg.Data, "people"): 0o755, filepath.Dir(l.dir): 0o755,
		l.dir: 0o700, l.home(): 0o700, l.work(): 0o700, l.tmp(): 0o700,
	} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode().Perm() != mode || !info.IsDir() {
			t.Fatalf("wanted directory mode %o for %s, got %v, %v", mode, filepath.Base(path), info, err)
		}
	}
	for _, id := range []string{"..", "../" + chatB, "bad", strings.ToUpper(chatB)} {
		w := request(s, "POST", "/v1/sessions", cfg.Secret, "alice",
			`{"id":"`+id+`","model_url":"http://model.test/v1","model_token":"fake"}`)
		wantError(t, w, 400, "This chat has an invalid identifier.")
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(cfg.Data, "people", personDir("bob"))); err != nil {
		t.Fatal(err)
	}
	w := request(s, "POST", "/v1/sessions", cfg.Secret, "bob",
		`{"id":"`+chatB+`","model_url":"http://model.test/v1","model_token":"fake"}`)
	wantError(t, w, 500, "This chat could not be created.")
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("creation crossed the symlink: %v, %v", entries, err)
	}
}

func TestC9_PlainWords(t *testing.T) {
	for _, sentence := range append(refusalText[:len(refusalText):len(refusalText)], warningUID) {
		for _, term := range []string{"uid", "sandbox", "sessionhost", "fence", "landlock", "daemon", "setuid"} {
			if strings.Contains(strings.ToLower(sentence), term) {
				t.Errorf("%q contains %q", sentence, term)
			}
		}
	}
}

// C2: abstract Unix sockets belong to no user, so a fenced chat is only kept apart from
// the others where the kernel can scope them (Landlock ABI 6, Linux 6.12). Below that the
// machine refuses to start chats unless the operator chose process-only isolation.
func TestC2_FencedModeNeedsAKernelThatScopesAbstractSockets(t *testing.T) {
	report := func(abi int, require string) Report {
		return Report{Require: require, UIDSwitch: SwitchReport{OK: true}, Landlock: LandlockReport{Available: abi > 0, ABI: abi}}
	}
	if got := report(5, modeFenced).decide(); got.OK || got.Mode != modeRefused ||
		!strings.Contains(got.Reason, "CODEAF_SESSIONHOST_REQUIRE=uid") || !strings.Contains(got.Reason, "Linux 6.12") {
		t.Fatalf("an old kernel passed or was unexplained: %+v", got)
	}
	if got := report(6, modeFenced).decide(); !got.OK || got.Mode != modeFenced {
		t.Fatalf("a kernel that can scope was refused: %+v", got)
	}
	if got := report(5, modeUID).decide(); !got.OK || got.Mode != modeUID {
		t.Fatalf("the operator's explicit process-only choice was refused: %+v", got)
	}
}

func TestP1_6_ModelBearerFingerprintOnRows(t *testing.T) {
	s, _, _ := testSupervisor(t, testConfig(t), healthyReport())
	create := createChat(t, s, "alice", chatA)
	if strings.Contains(create.Body.String(), "model_bearer") {
		t.Fatalf("create response changed: %s", create.Body)
	}
	check := func(token string) {
		t.Helper()
		sum := sha256.Sum256([]byte(token))
		want := hex.EncodeToString(sum[:8])
		if token == "" {
			want = ""
		}
		checkRow := func(raw []byte) {
			t.Helper()
			var row map[string]json.RawMessage
			if err := json.Unmarshal(raw, &row); err != nil || string(row["model_bearer"]) != `"`+want+`"` {
				t.Fatalf("model_bearer = %s, want %q: %v", row["model_bearer"], want, err)
			}
		}
		w := request(s, "GET", "/v1/sessions/"+chatA, s.cfg.Secret, "alice", "")
		if w.Code != 200 {
			t.Fatalf("get: %d %s", w.Code, w.Body)
		}
		checkRow(w.Body.Bytes())
		w = request(s, "GET", "/v1/sessions", s.cfg.Secret, "alice", "")
		var list struct{ Sessions []json.RawMessage }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Sessions) != 1 {
			t.Fatalf("list: %d %s", w.Code, w.Body)
		}
		checkRow(list.Sessions[0])
	}
	check("made-up-model-bearer")
	putTestCredentials(t, s, "updated-model-token")
	check("updated-model-token")
	if w := request(s, "POST", "/v1/sessions/"+chatA+"/stop", s.cfg.Secret, "alice", ""); w.Code != 204 {
		t.Fatalf("stop: %d %s", w.Code, w.Body)
	}
	check("")
}

func TestP1_6_GitSettingsReachTheEngineWithoutOtherChanges(t *testing.T) {
	cfg := testConfig(t)
	cfg.Model = "example-model"
	// The connect-port list has its own test (item 0c); here it is off so this one
	// proves the git settings add exactly their two names and change nothing else.
	cfg.LimitPorts = false
	launcher := newWireTestLauncher()
	s := newTestSupervisor(t, cfg, healthyReport(), launcher, &fakeProcesses{live: make(processTable)})
	const model = `"model_url":"http://model.test/v1","model_token":"made-up-model-bearer"`
	const git = `"git_url":"https://org.test/v1/hosted/git","git_hosts":["https://github.com","http://git.test:8891"]`
	w := request(s, "POST", "/v1/sessions", cfg.Secret, "alice", `{"id":"`+chatA+`",`+model+`,`+git+`}`)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	const updated = `"git_url":"http://org.test/v1/hosted/git","git_hosts":["http://updated.test:8891"]`
	w = request(s, "PUT", "/v1/sessions/"+chatA+"/credentials", cfg.Secret, "alice", `{`+model+`,`+updated+`}`)
	if w.Code != 204 {
		t.Fatalf("credentials: %d %s", w.Code, w.Body)
	}

	folders, err := layoutFor(cfg.Data, "alice", chatA)
	if err != nil {
		t.Fatal(err)
	}
	original := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1",
		"HOME=" + folders.home(), "CODEAF_HOME=" + folders.home(), "TMPDIR=" + folders.tmp(),
		"CODEAF_NO_UPDATE_CHECK=1", "CODEAF_BASE_URL=http://model.test/v1", "OPENAI_API_KEY=made-up-model-bearer",
		"CODEAF_ENGINE_IDLE=30m0s", "CODEAF_FENCE_REQUIRE=1", "CODEAF_MODEL=example-model",
	}
	creds := credentials{ModelURL: "http://model.test/v1", ModelToken: "made-up-model-bearer"}
	if got := engineEnv(cfg, folders, creds); !reflect.DeepEqual(got, original) {
		t.Fatalf("default engine environment changed: %v", got)
	}
	conn := dialTestWire(t, s)
	defer conn.CloseNow()
	want := append(original, "CODEAF_HOSTED_GIT_URL=http://org.test/v1/hosted/git", "CODEAF_HOSTED_GIT_HOSTS=http://updated.test:8891")
	if got := launcher.environment(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("engine environment = %v, want %v", got, want)
	}
}

func TestP1_6_InvalidGitSettingsRefuseTheWholeCredentialsBody(t *testing.T) {
	s, _, _ := testSupervisor(t, testConfig(t), healthyReport())
	const model = `"model_url":"http://model.test/v1","model_token":"fake"`
	for _, optional := range []string{
		`"git_url":"git://org.test/v1/hosted/git"`,
		`"git_url":"http:///v1/hosted/git"`,
		`"git_url":"https://user@org.test/v1/hosted/git"`,
		`"git_url":"https://org.test/v1/hosted/git/"`,
		`"git_url":"https://org.test/v1/hosted/git?x=1"`,
		`"git_hosts":["ftp://git.test"]`,
		`"git_hosts":["http://:8891"]`,
		`"git_hosts":["https://user@git.test"]`,
		`"git_hosts":["https://git.test/path"]`,
		`"git_hosts":["https://git.test?"]`,
		`"git_hosts":["https://git.test#fragment"]`,
		`"git_hosts":["https://git.test","https://` + strings.Repeat("x", 193) + `"]`,
		`"git_hosts":[` + strings.Repeat(`"https://git.test",`, 16) + `"https://git.test"]`,
	} {
		w := request(s, "POST", "/v1/sessions", s.cfg.Secret, "alice", `{"id":"`+chatA+`",`+model+`,`+optional+`}`)
		if w.Code != 400 {
			t.Errorf("create with %s: %d %s", optional, w.Code, w.Body)
		}
	}
	createChat(t, s, "alice", chatB)
	w := request(s, "PUT", "/v1/sessions/"+chatB+"/credentials", s.cfg.Secret, "alice",
		`{`+model+`,"git_hosts":["https://git.test/path"]}`)
	if w.Code != 400 {
		t.Fatalf("update with invalid Git origin: %d %s", w.Code, w.Body)
	}
}

// P1-0b: on a kernel older than 6.12, and on one with no Landlock at all, the
// default stays closed and the operator is told the exact setting that accepts
// the gap; typing it starts chats in the process-only mode.
func TestP1_0b_RefusalsNameTheSettingExactly(t *testing.T) {
	for name, landlock := range map[string]LandlockReport{
		"kernel 6.8 (ABI 4)": {Available: true, ABI: 4},
		"no landlock":        {},
	} {
		refused := Report{Require: modeFenced, UIDSwitch: SwitchReport{OK: true}, Landlock: landlock}.decide()
		if refused.OK || refused.Mode != modeRefused || !strings.Contains(refused.Reason, "CODEAF_SESSIONHOST_REQUIRE=uid") {
			t.Errorf("%s: default did not refuse with the setting named: %+v", name, refused)
		}
		accepted := Report{Require: modeUID, UIDSwitch: SwitchReport{OK: true}, Landlock: landlock}.decide()
		if !accepted.OK || accepted.Mode != modeUID {
			t.Errorf("%s: the named setting did not start chats: %+v", name, accepted)
		}
	}
}

// TestP1_0_EveryFolderAboveAChatCanBeOpenedForReadingByItsUser is item 0: the
// first command of every new hosted chat said "calls are not being sealed ...
// Permission denied" because the saved-history helper syncs every folder above
// a new file, and the folders above a chat allowed passing through but not
// opening. Each folder from the data folder down to the chat's own must now be
// readable by any chat's user, while a chat's own folder stays closed to all
// but its user.
func TestP1_0_EveryFolderAboveAChatCanBeOpenedForReadingByItsUser(t *testing.T) {
	cfg := testConfig(t)
	s, _, _ := testSupervisor(t, cfg, healthyReport())
	createChat(t, s, "alice", chatA)
	l, err := layoutFor(cfg.Data, "alice", chatA)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{cfg.Data, filepath.Join(cfg.Data, "people"), filepath.Dir(l.dir)} {
		info, err := os.Lstat(dir)
		if err != nil || info.Mode().Perm()&0o055 != 0o055 {
			t.Errorf("a chat's user could not open %s for reading: %v, %v", dir, info, err)
		}
		if info.Mode().Perm()&0o022 != 0 {
			t.Errorf("%s can be changed by someone other than the supervisor: %v", dir, info.Mode())
		}
	}
	if info, err := os.Lstat(l.dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the chat's own folder is no longer closed to other chats: %v, %v", info, err)
	}
}

// TestP1_0_FoldersTheFirstStageMadeAreOpenedUpOnStart: a machine that already
// holds chats made with the first stage's 0711 folders must not need them
// recreated for the first command of an existing chat to stop saying so.
func TestP1_0_FoldersTheFirstStageMadeAreOpenedUpOnStart(t *testing.T) {
	cfg := testConfig(t)
	people := filepath.Join(cfg.Data, "people")
	person := filepath.Join(people, personDir("alice"))
	for _, dir := range []string{people, person} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o711); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(cfg.Data, 0o711); err != nil {
		t.Fatal(err)
	}
	testSupervisor(t, cfg, healthyReport())
	for _, dir := range []string{cfg.Data, people, person} {
		info, err := os.Lstat(dir)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("%s was not opened up on start: %v, %v", dir, info, err)
		}
	}
	// Anything else is still refused rather than guessed at.
	if err := os.Chmod(person, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := openAncestor(person); err == nil {
		t.Error("a folder the supervisor did not make this way was accepted")
	}
}
