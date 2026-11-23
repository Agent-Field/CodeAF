package syncsetup

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filesdoor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

// SPEC-3. The files the seal guard keeps out of a chat's saved history (every
// dotenv file, committed templates included, and any other file that looks like
// it holds a secret) used to travel only through the key vault, which waits for
// sync storage. With storage off a chat that moved by blind pass-through arrived
// without them, and git in the new folder showed them as deleted. These tests
// are the contract that they now move with the chat, through the same blind
// pipe and nothing else, and that nothing else rides with them. Each names the
// item it comes from (V1 to V9) and is written from what a person with two
// computers sees.

const (
	// passMarker is planted inside dotenv files and a secret-looking README, so
	// a tap on the wire and a look at every file at rest can tell whether the
	// bytes of a secret ever lay anywhere but the two folders.
	passMarker = "PLAINTEXT-MARKER-IN-A-DOTENV-5c1f90"
	// passKeyShaped is a token the guard recognises by its shape.
	passKeyShaped = "sk-or-v1-0123456789abcdef0123456789abcdef"
)

// passSecret is one file of the project that the guard keeps out of the saved history.
type passSecret struct {
	path      string
	body      string
	mode      fs.FileMode
	committed bool // tracked by git, as a template or a README is
}

func passSecrets() []passSecret {
	return []passSecret{
		{".env", "API_KEY=" + passKeyShaped + "\nMARKER=" + passMarker + "\n", 0o644, false},
		{".env.local", "LOCAL_ONLY=1\nMARKER=" + passMarker + "\n", 0o600, false},
		{"web/client/app/.env.production", "# prod\nAPI_URL='https://x'\nTOKEN=" + passMarker + "\n", 0o640, false},
		{".env.example.tmpl", "API_KEY=changeme\n", 0o644, true},
		{"docs/setup.md", "Before you start, export OPENROUTER_API_KEY=" + passKeyShaped + " and the marker " + passMarker + ".\n", 0o644, true},
	}
}

// passSecretChat is liveChat for a project that holds secret files: A starts a
// chat in a git project whose tracked templates are kept out of the saved
// history by the guard, with three turns, and it is in B's directory.
func passSecretChat(t *testing.T, r *liveRig) *openChat {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	seedTree(t, r.work)
	for _, f := range passSecrets() {
		path := filepath.Join(r.work, filepath.FromSlash(f.path))
		mustNil(t, os.MkdirAll(filepath.Dir(path), 0o755))
		mustNil(t, os.WriteFile(path, []byte(f.body), f.mode))
		mustNil(t, os.Chmod(path, f.mode))
	}
	mustNil(t, os.WriteFile(filepath.Join(r.work, "marker.txt"), []byte(liveFileMarker+"\n"), 0o644))
	git(t, r.work, "init", "-q")
	tracked := []string{"README.md", "src/main.go", "bin/run.sh"}
	for _, f := range passSecrets() {
		if f.committed {
			tracked = append(tracked, f.path)
		}
	}
	git(t, r.work, append([]string{"add", "--force", "--"}, tracked...)...)
	git(t, r.work, "commit", "-qm", "the project, with its templates")

	a := r.openA()
	a.mustSay("first turn")
	a.mustSay("second turn carries " + liveTurnMarker)
	a.mustSay("third turn")
	r.durable(r.cell.ID, r.cell)
	return a
}

// withheldOnA is what the seal guard keeps out of the chat's history on A: the
// files that have to move for the other computer to have the whole project.
func (r *liveRig) withheldOnA() []string {
	r.t.Helper()
	eng := cellstore.EngineFor(r.work)
	_, paths, err := eng.Withheld(r.cell)
	mustNil(r.t, err)
	sort.Strings(paths)
	return paths
}

// assertTheGuardKeptThemOut is the precondition of every test here: if the
// guard did not keep the planted files out of the history, a pass below would
// say nothing about the files a guard does keep out.
func assertTheGuardKeptThemOut(t *testing.T, r *liveRig) []string {
	t.Helper()
	have := r.withheldOnA()
	for _, f := range passSecrets() {
		if !contains(have, f.path) {
			t.Fatalf("the seal guard on A keeps out %v; it does not keep out %s, so this test would prove nothing", have, f.path)
		}
	}
	return have
}

func contains(list []string, one string) bool {
	for _, s := range list {
		if s == one {
			return true
		}
	}
	return false
}

// assertSecretsArrived is V1's last clause: every file the guard keeps out is in
// the new folder, at the same path, with the same bytes and permissions, and git
// there shows exactly what git shows on A, with no deletion.
func assertSecretsArrived(t *testing.T, r *liveRig, workB string, paths []string) {
	t.Helper()
	for _, rel := range paths {
		want, err := os.ReadFile(filepath.Join(r.work, filepath.FromSlash(rel)))
		mustNil(t, err)
		got, err := os.ReadFile(filepath.Join(workB, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s did not arrive on B: %v", rel, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s on B is %d bytes that differ from A's %d", rel, len(got), len(want))
		}
		if runtime.GOOS != "windows" {
			wi, _ := os.Stat(filepath.Join(r.work, filepath.FromSlash(rel)))
			gi, _ := os.Stat(filepath.Join(workB, filepath.FromSlash(rel)))
			if wi != nil && gi != nil && wi.Mode().Perm() != gi.Mode().Perm() {
				t.Errorf("%s on B has mode %v; A's is %v", rel, gi.Mode().Perm(), wi.Mode().Perm())
			}
		}
	}
	status := gitOut(t, workB, "--no-optional-locks", "status", "--porcelain")
	if strings.Contains(status, " D ") || strings.HasPrefix(status, "D ") || strings.Contains(status, "\nD ") {
		t.Errorf("git on B shows files deleted after the move:\n%s", status)
	}
	if want := gitOut(t, r.work, "--no-optional-locks", "status", "--porcelain"); status != want {
		t.Errorf("git status on B is\n%s\nA's is\n%s", status, want)
	}
}

// storeRequests is every request the relay saw for its object store, of any
// kind: puts, gets and has.
func (w *wireLog) storeRequests() []string {
	var out []string
	for _, e := range w.snapshot() {
		if strings.HasPrefix(e.Path, "/v1/store/") {
			out = append(out, e.Method+" "+e.Path)
		}
	}
	return out
}

// V1 and V2: with storage off and blind pass-through on, every file the guard
// keeps out of a chat's history that A has is on B, byte for byte, by the time
// the take reports its files done, while the relay is asked for no object at all.
func TestPassThroughV1TheSecretFilesArriveWithTheChat(t *testing.T) {
	for _, mode := range []string{"still open on A", "A's window quit and its computer still runs"} {
		t.Run(mode, func(t *testing.T) {
			r := newLiveRig(t)
			a := passSecretChat(t, r)
			paths := assertTheGuardKeptThemOut(t, r)
			if mode != "still open on A" {
				mustNil(t, a.drive.Close(context.Background()))
			}

			got := liveTake(t, r)
			workB := workspaceOf(got.Taken.Cell.Root)
			assertSecretsArrived(t, r, workB, paths)
			assertMovedIntact(t, r, got)
			if n := r.wire.storeRequests(); len(n) != 0 {
				t.Fatalf("the relay's object store was asked %d times: %v; want never", len(n), n)
			}
		})
	}
}

// V3: the bytes of a secret never cross the relay's side of the wire, never sit
// in a request the relay logged, and never sit at rest in either computer's
// store of shared chats. The marker really is on B, so the check is not vacuous.
func TestPassThroughV3TheSecretsAreSealedOnTheWireAndStoredNowhere(t *testing.T) {
	r := newLiveRig(t)
	passSecretChat(t, r)
	got := liveTake(t, r)
	workB := workspaceOf(got.Taken.Cell.Root)

	for _, rel := range []string{".env", "web/client/app/.env.production", "docs/setup.md"} {
		have, err := os.ReadFile(filepath.Join(workB, filepath.FromSlash(rel)))
		if err != nil || !strings.Contains(string(have), passMarker) {
			t.Fatalf("%s never reached B (%v), so the checks below would prove nothing", rel, err)
		}
	}
	seen := r.tap.bytes()
	if len(seen) < 9000 {
		t.Fatalf("the tap saw %d bytes; a moved chat is more than that", len(seen))
	}
	for name, needle := range map[string][]byte{
		"the marker":           []byte(passMarker),
		"the marker, base64":   []byte(base64.StdEncoding.EncodeToString([]byte(passMarker))),
		"the marker, base64u":  []byte(base64.RawURLEncoding.EncodeToString([]byte(passMarker))),
		"the marker, hex":      []byte(hex.EncodeToString([]byte(passMarker))),
		"the key-shaped token": []byte(passKeyShaped),
	} {
		if bytes.Contains(seen, needle) {
			t.Fatalf("the relay's side of the wire held %s", name)
		}
		for _, e := range r.wire.snapshot() {
			if bytes.Contains(e.Body, needle) || strings.Contains(e.Path, string(needle)) {
				t.Fatalf("a request the relay logged (%s %s) held %s", e.Method, e.Path, name)
			}
		}
		for _, h := range []string{r.homeA, r.homeB} {
			filepath.WalkDir(liveStoreDir(h), func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if raw, _ := os.ReadFile(path); bytes.Contains(raw, needle) {
					t.Errorf("%s holds %s at rest", path, name)
				}
				return nil
			})
		}
	}
}

// V4: only the person's own computers are given secret files. A computer of
// another person is refused before a byte is sent; a computer that was removed
// while it was reading gets nothing more.
func TestPassThroughV4OnlyYourOwnComputersGetTheSecrets(t *testing.T) {
	r := newLiveRig(t)
	passSecretChat(t, r)
	ctx := context.Background()
	chat := r.cell.ID

	own := doorFor(r, r.b.Identity, r.b.Device)
	t.Cleanup(func() { _ = own.Close() })
	page, err := own.Secrets(ctx, chat, "")
	if err != nil || !page.Found || len(page.Files) == 0 {
		t.Fatalf("one of your own computers was given %+v, %v", page, err)
	}

	strangerHome := t.TempDir()
	strangerID, err := identity.Ensure(strangerHome)
	mustNil(t, err)
	strangerDev, err := identity.Device(strangerHome)
	mustNil(t, err)
	stranger := doorFor(r, strangerID, strangerDev)
	t.Cleanup(func() { _ = stranger.Close() })
	if page, err := stranger.Secrets(ctx, chat, ""); err == nil || len(page.Files) != 0 {
		t.Fatalf("a computer of another person was given %+v, %v", page, err)
	}
	if st := stranger.Stats(); st.BytesDown != 0 {
		t.Fatalf("a stranger was sent %d bytes", st.BytesDown)
	}

	// A page holds one file, so removal can come between two of them.
	liveSecretsPage = 1
	t.Cleanup(func() { liveSecretsPage = 0 })
	r.a.restartFilesDoor(t)
	mid := doorFor(r, r.b.Identity, r.b.Device)
	t.Cleanup(func() { _ = mid.Close() })
	first, err := mid.Secrets(ctx, chat, "")
	if err != nil || len(first.Files) != 1 || !first.More {
		t.Fatalf("the first page was %+v, %v; want one file and more to come", first, err)
	}
	mustNil(t, r.a.Dir.Revoke(ctx, r.b.Device.ID()))
	time.Sleep(3 * liveDoorRosterFor)
	before := mid.Stats().BytesDown
	_, err = mid.Secrets(ctx, chat, first.Files[0].Path)
	var refused *filesdoor.RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("the next page after removal = %v; want a refusal", err)
	}
	if sent := mid.Stats().BytesDown - before; sent > 4096 {
		t.Fatalf("a removed computer was sent %d more bytes", sent)
	}
}

// restartFilesDoor ends this computer's door and starts it again, so a changed
// setting of the door takes hold.
func (s *Sync) restartFilesDoor(t *testing.T) {
	t.Helper()
	stopFilesDoor(s.Home)
	s.ensureFilesDoor()
}

// V5: with the computer that has the chat off, the take fails as it always did
// and puts no secret file on B, not in the folder and not beside it; and the
// same take brings them when A runs again.
func TestPassThroughV5TheHolderBeingOffInstallsNoSecretFile(t *testing.T) {
	r := newLiveRig(t)
	a := passSecretChat(t, r)
	paths := assertTheGuardKeptThemOut(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	stopFilesDoor(r.homeA)

	err := failedTake(t, r)
	if !errors.Is(err, syncwords.ErrHolderOffline) {
		t.Fatalf("the take failed with %v; want the holder-offline refusal", err)
	}
	assertNothingOnB(t, r)
	filepath.WalkDir(r.rootsB, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), ".env") {
			t.Errorf("%s is on B after a take that did not happen", path)
		}
		return nil
	})

	r.a.ensureFilesDoor()
	got := liveTake(t, r)
	assertSecretsArrived(t, r, workspaceOf(got.Taken.Cell.Root), paths)
}

// V6: with the switch off, nothing about secret files changes: the chat does not
// move by this road, the door of A gives no secret file to a computer that
// asks, and B has none.
func TestPassThroughV6TheOffSwitchKeepsTheSecretFilesWhereTheyWere(t *testing.T) {
	r := newLiveRig(t)
	passSecretChat(t, r)
	own := doorFor(r, r.b.Identity, r.b.Device)
	t.Cleanup(func() { _ = own.Close() })
	// With the switch on, A's door does give them: without this the test would
	// pass for a door that gives nothing at all.
	if page, err := own.Secrets(context.Background(), r.cell.ID, ""); err != nil || len(page.Files) == 0 {
		t.Fatalf("with the switch on, A's door gave %d secret files, %v; the test needs it to", len(page.Files), err)
	}
	// A's door is up already; the person now says off.
	t.Setenv(LiveTransferVar, "off")
	if page, _ := own.Secrets(context.Background(), r.cell.ID, ""); len(page.Files) != 0 {
		t.Fatalf("with the switch off, A's door gave %d secret files", len(page.Files))
	}
	_, err := r.continuerB().Take(context.Background(), r.cell.ID)
	if !errors.Is(err, syncwords.ErrLiveTransferOff) {
		t.Fatalf("the take with the switch off = %v; want the switch-off refusal", err)
	}
	assertNothingOnB(t, r)
	filepath.WalkDir(r.rootsB, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), ".env") {
			t.Errorf("%s is on B with the switch off", path)
		}
		return nil
	})
}

// V7: with storage approved the files travel through the key vault as before:
// they arrive, and A's door is never asked for them.
func TestPassThroughV7ApprovedStorageCarriesTheSecretsByTheVault(t *testing.T) {
	r := newLiveRig(t)
	t.Setenv(home.EnvVar, r.homeA)
	r.engine.Guard.Home = r.homeA
	r.approveStorage()
	var asked int
	var mu sync.Mutex
	old := liveSecretsHook
	liveSecretsHook = func(string, string) { mu.Lock(); asked++; mu.Unlock() }
	t.Cleanup(func() { liveSecretsHook = old })

	a := passSecretChat(t, r)
	paths := assertTheGuardKeptThemOut(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	t.Setenv(home.EnvVar, r.homeB)
	r.engB.Guard.Home = r.homeB
	got := liveTake(t, r)
	assertSecretsArrived(t, r, workspaceOf(got.Taken.Cell.Root), paths)
	mu.Lock()
	defer mu.Unlock()
	if asked != 0 {
		t.Fatalf("A's door was asked for secret files %d times with storage approved; the vault carries them", asked)
	}
	if r.wire.storeWrites() == 0 {
		t.Fatal("nothing was stored on the relay, so this was not the approved-storage road")
	}
}

// V8: a secret file edited on B arrives back on A when A takes the chat back,
// and the files nobody touched are as they were.
func TestPassThroughV8ASecretFileEditedOnBComesBackToA(t *testing.T) {
	r := newLiveRig(t)
	a := passSecretChat(t, r)
	paths := assertTheGuardKeptThemOut(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	ctx := context.Background()

	took := liveTake(t, r)
	workB := workspaceOf(took.Taken.Cell.Root)
	assertSecretsArrived(t, r, workB, paths)

	edited := "API_KEY=" + passKeyShaped + "\nMARKER=" + passMarker + "\nADDED_ON_B=1\n"
	envB := filepath.Join(workB, ".env")
	mustNil(t, os.WriteFile(envB, []byte(edited), 0o644))
	later := time.Now().Add(time.Minute)
	mustNil(t, os.Chtimes(envB, later, later))
	untouched, err := os.ReadFile(filepath.Join(r.work, ".env.local"))
	mustNil(t, err)

	b := r.openOn(r.b, r.engB, took.Taken.Cell, workB, nameB)
	b.mustSay("a turn on B")
	r.durable(r.cell.ID, took.Taken.Cell)
	mustNil(t, b.drive.Close(ctx))

	back, err := r.continuerA().Take(ctx, r.cell.ID)
	if err != nil {
		t.Fatalf("A could not take the chat back: %v", err)
	}
	if back.Wait != nil {
		mustNil(t, back.Wait(ctx))
	}
	if got, _ := os.ReadFile(filepath.Join(r.work, ".env")); string(got) != edited {
		t.Fatalf(".env on A = %q; want B's edit", got)
	}
	if got, _ := os.ReadFile(filepath.Join(r.work, ".env.local")); !bytes.Equal(got, untouched) {
		t.Fatalf(".env.local on A = %q; nobody touched it", got)
	}
}

// V4 and V8 together: a computer that took a chat can pass its secret files on
// at once, before it has sealed a turn of its own, because it kept out of its
// history the files it received. Without that, a third computer taking the chat
// from it while the first is off would arrive without them.
func TestPassThroughV8AComputerThatTookTheChatCanPassItsSecretFilesOn(t *testing.T) {
	r := newLiveRig(t)
	a := passSecretChat(t, r)
	paths := assertTheGuardKeptThemOut(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	liveTake(t, r)

	asA := filesdoor.NewSource(filesdoor.SourceConfig{
		Service: r.a.Relay, Identity: r.a.Identity, Device: r.a.Device,
		TargetID: r.b.Device.ID(), TargetName: nameB, RetryWithin: 300 * time.Millisecond,
	})
	t.Cleanup(func() { _ = asA.Close() })
	var offered []string
	after := ""
	for {
		page, err := asA.Secrets(context.Background(), r.cell.ID, after)
		mustNil(t, err)
		for _, f := range page.Files {
			offered = append(offered, f.Path)
			after = f.Path
		}
		if !page.More {
			break
		}
	}
	sort.Strings(offered)
	if strings.Join(offered, "|") != strings.Join(paths, "|") {
		t.Fatalf("B offers %v; the chat's seal-guarded files are %v", offered, paths)
	}
}

// V9: only the chat's own seal-guarded files move this way. Saved provider keys,
// connections, preferences and a chat nobody asked for stay where they are, and
// a request for a chat A does not have is answered with nothing.
func TestPassThroughV9OnlyTheChatsSecretFilesMove(t *testing.T) {
	r := newLiveRig(t)
	const connections = `{"mail":{"key":"FAKE-connection-key-not-a-real-one"}}`
	const providerKey = "FAKE-provider-key-not-a-real-one"
	profileA := r.a.profile()
	mustNil(t, os.WriteFile(filepath.Join(profileA, vaultsync.CredentialsFile), []byte(connections), 0o600))
	mustNil(t, os.WriteFile(config.BudgetConfigPath(profileA), []byte(`{"api_key":"`+providerKey+`"}`), 0o600))
	passSecretChat(t, r)
	paths := assertTheGuardKeptThemOut(t, r)
	got := liveTake(t, r)

	ctx := context.Background()
	own := doorFor(r, r.b.Identity, r.b.Device)
	t.Cleanup(func() { _ = own.Close() })
	var offered []string
	after := ""
	for {
		page, err := own.Secrets(ctx, r.cell.ID, after)
		mustNil(t, err)
		for _, f := range page.Files {
			offered = append(offered, f.Path)
			after = f.Path
		}
		if !page.More {
			break
		}
	}
	sort.Strings(offered)
	if strings.Join(offered, "|") != strings.Join(paths, "|") {
		t.Fatalf("A's door offers %v; the chat's seal-guarded files are %v", offered, paths)
	}
	if page, err := own.Secrets(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", ""); err != nil || page.Found || len(page.Files) != 0 {
		t.Fatalf("a chat A does not have was answered with %+v, %v", page, err)
	}

	profileB := r.b.profile()
	if _, err := os.Stat(filepath.Join(profileB, vaultsync.CredentialsFile)); err == nil {
		t.Fatal("A's connections file arrived on B by blind pass-through")
	}
	if raw, _ := os.ReadFile(config.BudgetConfigPath(profileB)); strings.Contains(string(raw), providerKey) {
		t.Fatal("A's saved provider key arrived on B by blind pass-through")
	}
	for _, needle := range []string{"FAKE-connection-key-not-a-real-one", providerKey} {
		if bytes.Contains(r.tap.bytes(), []byte(needle)) {
			t.Fatalf("%q crossed the relay", needle)
		}
	}
	_ = got
}

// The door finds a chat in the places a home keeps one, the same three the
// take looks in when it is asked for a chat by its id, and finds nothing for an
// id that is not a chat's or a chat that is not there. The rig replaces the
// finder, so this is the only test of the one a person's computer runs.
func TestTheDoorFindsAChatWhereAHomeKeepsIt(t *testing.T) {
	layouts := map[string]func(home string) string{
		"a project's bucket": func(home string) string { return filepath.Join(home, "v3", "projects", "some-project") },
		"a moving copy":      func(home string) string { return filepath.Join(home, "v3", "projects", "some-project", ".moving") },
		"the home's cells":   func(home string) string { return cell.Dir(home) },
	}
	for name, parent := range layouts {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			dir := parent(home)
			mustNil(t, os.MkdirAll(dir, 0o755))
			made, err := cell.CreateIn(dir, cell.Options{Class: cell.FilesOnly})
			mustNil(t, err)
			got, _, ok := findChatHere(home, made.ID)
			if !ok || got.Root != made.Root {
				t.Fatalf("findChatHere = %q, %v; want the chat at %s", got.Root, ok, made.Root)
			}
			if _, _, ok := findChatHere(home, "01M7T60C9QSY5VNAB64T4FXH4C"); ok {
				t.Fatal("found a chat that is not on this computer")
			}
		})
	}
	for _, id := range []string{"", "*", "../cells", "a/b", "[x]"} {
		if _, _, ok := findChatHere(t.TempDir(), id); ok {
			t.Errorf("found a chat for the id %q, which is no chat's", id)
		}
	}
}
