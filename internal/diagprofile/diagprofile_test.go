package diagprofile

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func mapLookup(m map[string]string) Lookup {
	return func(name string) (string, bool) { v, ok := m[name]; return v, ok }
}

// privateDir is a fresh 0700 directory; t.TempDir's leaf is 0700 already but
// the chmod makes the test independent of the platform's temp policy.
func privateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "profiles")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func listing(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// burn gives the CPU profiler something to sample.
func burn(d time.Duration) {
	end := time.Now().Add(d)
	x := 1
	for time.Now().Before(end) {
		for i := 0; i < 1e5; i++ {
			x = x*31 + i
		}
	}
	if x == 42 {
		os.Stdout.Write(nil)
	}
}

func TestDisabledHasNoSideEffects(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "never")
	before := runtime.NumGoroutine()
	// Everything but the directory variable is set: still off.
	s, err := Start(context.Background(), mapLookup(map[string]string{EnvSeconds: "banana", EnvHeap: "1"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Active() || runtime.NumGoroutine() != before {
		t.Fatal("a disabled session must not start a goroutine")
	}
	s.Stop()
	s.Stop()
	var nilSession *Session
	nilSession.Stop()
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("a disabled session must not touch the filesystem")
	}
	// Empty is off, too.
	if s, err := Start(context.Background(), mapLookup(map[string]string{EnvDir: ""}), nil); err != nil || s.Active() {
		t.Fatalf("empty directory variable: active=%v err=%v", s.Active(), err)
	}
}

func TestBadEnvironmentRefusesWithoutEchoingValues(t *testing.T) {
	const secret = "sk-secret-value-123"
	good := privateDir(t)
	cases := map[string]map[string]string{
		"relative":    {EnvDir: "relative/" + secret},
		"unclean":     {EnvDir: good + "/../" + secret},
		"zero":        {EnvDir: good, EnvSeconds: "0"},
		"too long":    {EnvDir: good, EnvSeconds: "61"},
		"negative":    {EnvDir: good, EnvSeconds: "-5"},
		"unit":        {EnvDir: good, EnvSeconds: "5s"},
		"space":       {EnvDir: good, EnvSeconds: " 5"},
		"secret":      {EnvDir: good, EnvSeconds: secret},
		"heap secret": {EnvDir: good, EnvHeap: secret},
	}
	for name, env := range cases {
		before := listing(t, good)
		s, err := Start(context.Background(), mapLookup(env), nil)
		if err == nil || s != nil {
			t.Fatalf("%s: want refusal, got session=%v err=%v", name, s, err)
		}
		var refusal *Error
		if !errors.As(err, &refusal) || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), good) {
			t.Fatalf("%s: refusal must be fixed text: %q", name, err)
		}
		if got := listing(t, good); len(got) != len(before) {
			t.Fatalf("%s: a refusal left files: %v", name, got)
		}
	}
	for _, ok := range []string{"1", "30", "60"} {
		if _, err := ParseConfig(mapLookup(map[string]string{EnvDir: good, EnvSeconds: ok})); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
}

func TestDefaultsAreBounded(t *testing.T) {
	cfg, err := ParseConfig(mapLookup(map[string]string{EnvDir: privateDir(t)}))
	if err != nil || cfg.Duration != 30*time.Second || cfg.Heap {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
}

func TestDirectoryPermissionsAreEnforced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix modes")
	}
	open := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(open, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(open, 0o755)
	if _, err := Start(context.Background(), mapLookup(map[string]string{EnvDir: open}), nil); err == nil {
		t.Fatal("a group/world-accessible directory must be refused")
	}
	if info, _ := os.Stat(open); info.Mode().Perm() != 0o755 {
		t.Fatal("an existing directory must not be chmod'ed")
	}
	target := privateDir(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), mapLookup(map[string]string{EnvDir: link}), nil); err == nil {
		t.Fatal("a symlinked directory must be refused")
	}
	// A missing directory is created private.
	fresh := filepath.Join(t.TempDir(), "fresh")
	s, err := Start(context.Background(), mapLookup(map[string]string{EnvDir: fresh}), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Stop()
	if info, _ := os.Stat(fresh); info.Mode().Perm() != 0o700 {
		t.Fatalf("created directory mode %v", info.Mode().Perm())
	}
}

func TestExistingFileAndSymlinkAreNeverTouched(t *testing.T) {
	dir := privateDir(t)
	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openNew(existing); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing file: %v", err)
	}
	victim := filepath.Join(dir, "victim")
	os.WriteFile(victim, []byte("keep"), 0o600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(victim, link); err != nil {
		t.Skip("no symlinks")
	}
	if _, err := openNew(link); err == nil {
		t.Fatal("a symlink must not be followed")
	}
	dangling := filepath.Join(dir, "dangling")
	os.Symlink(filepath.Join(dir, "created-through-link"), dangling)
	if _, err := openNew(dangling); err == nil {
		t.Fatal("a dangling symlink must not be followed")
	}
	for _, p := range []string{existing, victim} {
		if b, _ := os.ReadFile(p); string(b) != "keep" {
			t.Fatalf("%s was modified", p)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "created-through-link")); err == nil {
		t.Fatal("the link target was created")
	}
}

func TestRunsGetUniqueFilesWith0600(t *testing.T) {
	dir := privateDir(t)
	cfg := Config{Dir: dir, Duration: time.Minute, Heap: true}
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		s, err := start(context.Background(), cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		s.Stop()
	}
	names := listing(t, dir)
	if len(names) != 6 {
		t.Fatalf("want 3 cpu + 3 heap files, got %v", names)
	}
	for _, n := range names {
		if seen[n] || !strings.HasPrefix(n, FilePrefix) || !strings.HasSuffix(n, FileSuffix) {
			t.Fatalf("bad or duplicate name %q", n)
		}
		seen[n] = true
		info, _ := os.Lstat(filepath.Join(dir, n))
		if runtime.GOOS != "windows" && info.Mode() != 0o600 {
			t.Fatalf("%s mode %v", n, info.Mode())
		}
	}
}

func TestIntervalEndsProfileAndStopIsIdempotent(t *testing.T) {
	dir := privateDir(t)
	var warned []string
	var mu sync.Mutex
	s, err := start(context.Background(), Config{Dir: dir, Duration: 30 * time.Millisecond, Heap: true}, func(l string) { mu.Lock(); warned = append(warned, l); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	burn(60 * time.Millisecond)
	<-s.done // the timer, not Stop, finished it
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.Stop() }()
	}
	wg.Wait()
	s.Stop()
	if len(listing(t, dir)) != 2 || len(warned) != 0 {
		t.Fatalf("files=%v warned=%v", listing(t, dir), warned)
	}
}

func TestCancellationAndStopFinishEarly(t *testing.T) {
	for _, via := range []string{"context", "stop"} {
		dir := privateDir(t)
		ctx, cancel := context.WithCancel(context.Background())
		s, err := start(ctx, Config{Dir: dir, Duration: time.Hour}, nil)
		if err != nil {
			t.Fatal(err)
		}
		burn(30 * time.Millisecond)
		start := time.Now()
		if via == "context" {
			cancel()
		}
		s.Stop()
		cancel()
		if time.Since(start) > 10*time.Second {
			t.Fatalf("%s: did not finish early", via)
		}
		if got := listing(t, dir); len(got) != 1 || !strings.Contains(got[0], "-cpu-") {
			t.Fatalf("%s: %v (heap must stay off)", via, got)
		}
	}
}

func TestSecondProfilerInProcessIsRefusedAndLeavesNothing(t *testing.T) {
	dir := privateDir(t)
	first, err := start(context.Background(), Config{Dir: dir, Duration: time.Hour}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Stop()
	if _, err := start(context.Background(), Config{Dir: dir, Duration: time.Hour}, nil); err == nil {
		t.Fatal("want refusal")
	}
	if len(listing(t, dir)) != 1 {
		t.Fatalf("refused start left a file: %v", listing(t, dir))
	}
}

func TestNeverWritesToStdout(t *testing.T) {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	s, err := start(context.Background(), Config{Dir: privateDir(t), Duration: 10 * time.Millisecond, Heap: true}, nil)
	if err == nil {
		s.Stop()
	}
	os.Stdout = old
	w.Close()
	if b, _ := io.ReadAll(r); len(b) != 0 || err != nil {
		t.Fatalf("stdout=%q err=%v", b, err)
	}
}

// TestGeneratedProfilesParse is the smoke: the files are real gzip pprof
// protos that the Go toolchain's own reader accepts, and a non-obfuscated
// build names its functions readably.
func TestGeneratedProfilesParse(t *testing.T) {
	dir := privateDir(t)
	s, err := start(context.Background(), Config{Dir: dir, Duration: time.Hour, Heap: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	burn(400 * time.Millisecond)
	s.Stop()
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goTool); err != nil {
		t.Skip("no go toolchain to read the profile with")
	}
	for _, n := range listing(t, dir) {
		path := filepath.Join(dir, n)
		raw, _ := os.ReadFile(path)
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s is not gzip: %v", n, err)
		}
		if _, err := io.Copy(io.Discard, zr); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		out, err := exec.Command(goTool, "tool", "pprof", "-raw", path).CombinedOutput()
		if err != nil {
			t.Fatalf("pprof rejected %s: %v\n%s", n, err, out)
		}
		if strings.Contains(n, "-cpu-") && !strings.Contains(string(out), "diagprofile.burn") {
			t.Fatalf("cpu profile does not name the hot function readably:\n%s", out)
		}
	}
}
