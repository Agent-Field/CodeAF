package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// writeV3Session puts one conversation on disk the way the door writes it: a
// folder named by the session id, the journal inside it, and meta.json beside
// the journal. spoke is when the person last said something, and the zero time
// is a session nobody has spoken in.
func writeV3Session(t *testing.T, bucket, id, said string, spoke time.Time) string {
	t.Helper()
	dir := filepath.Join(bucket, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lines := []string{`{"type":"session","version":1,"id":"` + id + `","timestamp":"2026-08-15T09:00:00Z"}`}
	if said != "" {
		lines = append(lines, `{"type":"message","role":"user","content":"`+said+`","timestamp":"2026-08-15T09:00:01Z"}`)
	}
	place := session.Place{Dir: dir}
	if err := os.WriteFile(place.Transcript(), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := session.Meta{ID: id, Workspace: "/w", Created: time.Now()}
	if !spoke.IsZero() {
		meta.LastUserAt = spoke
	}
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Resume follows the person, not the filesystem. A background write touching a
// file is not somebody returning to a conversation, so the launch opens the
// session with the newest last-spoken stamp even when another folder was
// modified a moment ago (docs/CHAT-V3.md, Decision 26).
func TestResumeOpensTheConversationThePersonSpokeInLast(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	stale := writeV3Session(t, bucket, "00000000000000a1", "last week", time.Now().Add(-7*24*time.Hour))
	live := writeV3Session(t, bucket, "00000000000000a2", "this morning", time.Now().Add(-time.Hour))
	// The older conversation is written to NOW — a task landing, a checkpoint,
	// anything that is not a person typing.
	now := time.Now()
	if err := os.Chtimes(stale, now, now); err != nil {
		t.Fatal(err)
	}

	found, err := v3ResolveSession("", workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	if !found.Resumed {
		t.Fatal("a project with conversations in it opened a new one")
	}
	if found.Place.Dir != live {
		t.Fatalf("resumed %s, want the one last spoken in (%s)", found.Place.Dir, live)
	}
	if found.Transcript != filepath.Join(live, "transcript.jsonl") {
		t.Fatalf("the journal is %s", found.Transcript)
	}
}

// A fresh identity cannot race an assignment of a previously empty conversation.
// Unassigned empty folders are still cleaned up instead of accumulating.
func TestALaunchMintsOneSessionAndReapsUnassignedEmpties(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"00000000000000b1", "00000000000000b2", "00000000000000b3"} {
		writeV3Session(t, bucket, id, "", time.Time{})
	}

	found, err := v3ResolveSession("", workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	if found.Resumed {
		t.Fatal("a session nobody has spoken in was announced as resumed")
	}
	for _, old := range []string{"00000000000000b1", "00000000000000b2", "00000000000000b3"} {
		if found.Place.Dir == filepath.Join(bucket, old) {
			t.Fatal("launch reused an old identity")
		}
	}
	entries, err := os.ReadDir(bucket)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the bucket holds %d folders, want the fresh conversation", len(entries))
	}
	if filepath.Join(bucket, entries[0].Name()) != found.Place.Dir {
		t.Fatalf("the surviving folder is %s, want the fresh one %s", entries[0].Name(), found.Place.Dir)
	}
}

// A conversation somebody spoke in is never reaped, whatever else the groom
// finds beside it.
func TestTheGroomLeavesEveryConversationAlone(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	kept := writeV3Session(t, bucket, "00000000000000c1", "port the picker", time.Now())
	writeV3Session(t, bucket, "00000000000000c2", "", time.Time{})

	if _, err := v3ResolveSession("", workspace, workspace, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(kept, "transcript.jsonl")); err != nil {
		t.Fatalf("the conversation was reaped: %v", err)
	}
	entries, _ := os.ReadDir(bucket)
	if len(entries) != 1 {
		t.Fatalf("the bucket holds %d folders, want only the conversation", len(entries))
	}
}

// A project with no conversations gets one, and it gets a folder named by its
// own id with meta.json already in it — so a second window sees the session
// before anybody has spoken in it.
func TestAFirstLaunchMintsAFolderThatNamesItself(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()

	found, err := v3ResolveSession("", workspace, filepath.Join(workspace, "cmd"), false)
	if err != nil {
		t.Fatal(err)
	}
	if found.Resumed {
		t.Fatal("a project that has never held a session cannot resume one")
	}
	meta, err := session.LoadMeta(found.Place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != filepath.Base(found.Place.Dir) {
		t.Fatalf("the folder is called %s and the session %s", filepath.Base(found.Place.Dir), meta.ID)
	}
	if meta.Workspace != workspace {
		t.Fatalf("the session records its workspace as %q, want %q", meta.Workspace, workspace)
	}
	if meta.LaunchDir != filepath.Join(workspace, "cmd") {
		t.Fatalf("the session records the launch directory as %q", meta.LaunchDir)
	}
	if meta.Owned {
		t.Fatal("a session opened in a project borrowed nothing")
	}
}

// A folder the person stood in deliberately is BORROWED even without a
// repository; only the home directory itself and the temporary directories own
// a workspace of their own.
func TestOnlyThePlacelessDirectoriesOwnTheirWorkspace(t *testing.T) {
	// The rule is arithmetic on the path and touches no disk, so the
	// directories here are named rather than made — which is also the only way
	// to name an ordinary directory in a test whose temp root IS one of the two
	// placeless ones.
	t.Setenv("HOME", "/home/somebody")

	if v3NoProjectPlace("/home/somebody/notes") {
		t.Fatal("a directory somebody deliberately stood in owns a workspace")
	}
	if !v3NoProjectPlace("/home/somebody") {
		t.Fatal("the home directory itself is a project")
	}
	if !v3NoProjectPlace(os.TempDir()) {
		t.Fatal("a temporary directory is a project")
	}
	if !v3NoProjectPlace(filepath.Join(os.TempDir(), "codeaf-run-1")) {
		t.Fatal("a directory under the temporary root is a project")
	}
	// And the workspace a launch resolves from one: the directory itself, with
	// nothing borrowed.
	if project, owned := v3Workspace("/home/somebody", ""); project != "/home/somebody" || !owned {
		t.Fatalf("the home directory resolved to %q, owned=%v", project, owned)
	}
	// A named workspace is somebody asking to work in a place, and asking is
	// borrowing.
	if project, owned := v3Workspace("/home/somebody", "/srv/api"); project != "/srv/api" || owned {
		t.Fatalf("a named workspace resolved to %q, owned=%v", project, owned)
	}
}

// A place folder that sits inside a repository is that folder. Naming it is
// asking to work there; climbing to the repository would hand the chat a
// wider tree than the place listed.
func TestANamedSubdirectoryDoesNotClimbToTheRepositoryAroundIt(t *testing.T) {
	if !gitHere(t) {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	command := exec.Command("git", "init")
	command.Dir = root
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	sub := filepath.Join(root, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "marker.txt"), []byte("pkg"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, owned := v3Workspace(sub, sub)
	if project != sub || owned {
		t.Fatalf("named subdirectory resolved to %q, owned=%v", project, owned)
	}
	if got := bare.ResolvePath("marker.txt", project); got != filepath.Join(sub, "marker.txt") {
		t.Fatalf("a relative tool path resolved to %s", got)
	}
	walked, _ := v3Workspace(sub, "")
	wantRoot, _ := filepath.EvalSymlinks(root)
	gotRoot, _ := filepath.EvalSymlinks(walked)
	if gotRoot != wantRoot {
		t.Fatalf("an unnamed subdirectory resolved to %q, want the repository %s", walked, root)
	}
}

// An owned session's tools root is its own work/ directory, and it is ready to
// work in before the launch hands it over.
func TestAnOwnedSessionWorksInItsOwnFolder(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	nowhere := t.TempDir()

	found, err := v3ResolveSession("", nowhere, nowhere, true)
	if err != nil {
		t.Fatal(err)
	}
	if !found.Place.Owned {
		t.Fatal("a session opened nowhere borrowed something")
	}
	if want := filepath.Join(found.Place.Dir, "work"); found.Place.Workspace != want {
		t.Fatalf("the tools root is %q, want %q", found.Place.Workspace, want)
	}
	if err := prepareOwnedWorkspace(found.Place); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(found.Place.Work()); err != nil || !info.IsDir() {
		t.Fatalf("the owned workspace is not there: %v", err)
	}
}

func TestAResumedAnchorOverridesTheLaunchWorkspace(t *testing.T) {
	anchor := t.TempDir()
	place := session.Place{Dir: t.TempDir(), Workspace: anchor}
	cfg, err := v3PointAt(session.Config{Workspace: "/the/launch/directory"}, place)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workspace != anchor || cfg.Place.Workspace != anchor {
		t.Fatalf("reopened config = %+v, want persisted anchor %s", cfg.Place, anchor)
	}
}

// A path a person named is a path they mean, and an old flat transcript opens
// as what it is: no folder, and every sidecar derived the way it always was.
func TestANamedFlatTranscriptKeepsTheLegacyLayout(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()
	flat := filepath.Join(t.TempDir(), "20260815-090102_b7c1.jsonl")
	if err := os.WriteFile(flat, []byte("{\"type\":\"session\",\"version\":1,\"id\":\"s-1\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	found, err := v3ResolveSession(flat, workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	if !found.Resumed {
		t.Fatal("a transcript that is on disk was announced as new")
	}
	if found.Transcript != flat {
		t.Fatalf("opened %s, want the file that was named", found.Transcript)
	}
	if found.Place.Dir != "" {
		t.Fatalf("a flat transcript was given the folder %s", found.Place.Dir)
	}
}

// A session folder's own journal, named on the command line, opens AS its
// folder: the sidecars and the node journals belong to it either way.
func TestANamedFolderTranscriptOpensAsItsFolder(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	dir := writeV3Session(t, bucket, "00000000000000d1", "port the picker", time.Now())

	found, err := v3ResolveSession(filepath.Join(dir, "transcript.jsonl"), workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	if found.Place.Dir != dir {
		t.Fatalf("opened the folder %q, want %q", found.Place.Dir, dir)
	}
	if found.Place.Workspace != "/w" {
		t.Fatalf("the session's workspace is %q, want the one it recorded", found.Place.Workspace)
	}
}

// THE SURFACE'S OWN LOG FILE IS STATE AND NOT WORK. `filepath.Join(profileDir,
// "chat.log")` with an empty profile — which is every launch that sets no
// CODEAF_PROFILE_DIR, meaning nearly all of them — names the file RELATIVE, so
// every repository a person opened a chat in grew an untracked chat.log and the
// surface's own repository band then counted that workspace dirty because of a
// file the surface itself had written.
func TestTheChatLogIsWrittenUnderTheStateRootAndNeverIntoTheWorkspace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("CODEAF_HOME", root)

	path := chatLogPath("")
	if !filepath.IsAbs(path) {
		t.Fatalf("chat.log is named relative to wherever the person was standing: %q", path)
	}
	if want := filepath.Join(root, "chat.log"); path != want {
		t.Fatalf("chat.log = %q, want %q — the state root, exactly as config.BudgetConfigPath falls back", path, want)
	}

	// A profile of its own still wins: a second profile is a second state root,
	// and its log belongs beside its config.
	profile := t.TempDir()
	if got, want := chatLogPath(profile), filepath.Join(profile, "chat.log"); got != want {
		t.Fatalf("with a profile set, chat.log = %q, want %q", got, want)
	}
	// And a profile that is only whitespace is no profile at all.
	if got, want := chatLogPath("   "), filepath.Join(root, "chat.log"); got != want {
		t.Fatalf("a blank profile named %q, want %q", got, want)
	}
}

// The reaper is the only rm -rf on the launch path, and this is the line it
// must not cross: a journal it could not READ is not a journal nobody spoke in.
//
// The torn folder here is the shape a lid closing mid-write leaves — a header
// and half of the person's first line — which every reader before this fix
// counted as silence, because the only question anybody asked the file was
// whether a parser found a turn in it.
func TestTheReaperKeepsAConversationItCouldNotRead(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}

	torn := filepath.Join(bucket, "00000000000000d1")
	if err := os.MkdirAll(torn, 0o700); err != nil {
		t.Fatal(err)
	}
	half := `{"type":"session","version":1,"id":"00000000000000d1","timestamp":"2026-08-15T09:00:00Z"}` + "\n" +
		`{"type":"message","role":"user","content":"what should we charge for the`
	if err := os.WriteFile(filepath.Join(torn, "transcript.jsonl"), []byte(half), 0o600); err != nil {
		t.Fatal(err)
	}
	// A folder whose journal reads cleanly and holds no turn is the litter the
	// reaper exists for, and it must still go in the same launch.
	litter := writeV3Session(t, bucket, "00000000000000d2", "", time.Time{})

	if _, err := v3ResolveSession("", workspace, workspace, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(torn, "transcript.jsonl")); err != nil {
		t.Fatalf("the torn conversation was reaped: %v", err)
	}
	if _, err := os.Stat(litter); err == nil {
		t.Fatal("a folder nobody ever spoke in survived the launch")
	}
}

// The other three ways a reader fails, each one a folder that stays. A schema
// this build does not know is the one worth naming: every line parses, so a
// parser-only test calls the file empty and deletes somebody's whole history
// the first time the journal grows a kind.
func TestTheReaperKeepsEveryFolderItCannotSettle(t *testing.T) {
	for _, journal := range []struct {
		name  string
		lines string
	}{
		{"a line past the scanner's buffer", `{"type":"message","role":"user","content":"` + strings.Repeat("x", 9<<20) + `"}`},
		{"a kind from a schema this build has never seen", `{"type":"turn","role":"user","content":"hello"}`},
		{"a header followed by rubbish", `{"type":"session","version":1,"id":"00000000000000e1"}` + "\nnot json at all\n"},
	} {
		t.Run(journal.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(journal.lines), 0o600); err != nil {
				t.Fatal(err)
			}
			if v3EmptySession(dir) {
				t.Fatalf("%s was called an empty conversation", journal.name)
			}
		})
	}
}

func TestLaunchKeepsTaskOnlyConversationFolders(t *testing.T) {
	for _, name := range []string{"plandb.db", "plandb.db.1", "tasks.json", "tasks/1/transcript.jsonl"} {
		t.Run(name, func(t *testing.T) {
			bucket := t.TempDir()
			dir := writeV3Session(t, bucket, "aaaaaaaaaaaaaaaa", "", time.Time{})
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			// Even a torn store is evidence of work, never permission to reap it.
			if err := os.WriteFile(path, []byte("partial saved work"), 0o600); err != nil {
				t.Fatal(err)
			}
			if v3EmptySession(dir) {
				t.Fatal("launch would reuse or reap saved task work")
			}
		})
	}
}

func TestTeamConversationsSurviveEmptyCleanupAndCannotBeReused(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	workspace := t.TempDir()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	manager := writeV3Session(t, bucket, "manager-empty", "", time.Time{})
	member := writeV3Session(t, bucket, "member-empty", "", time.Time{})
	unused := writeV3Session(t, bucket, "unused-empty", "", time.Time{})
	file := func(dir string) string { return session.Place{Dir: dir}.Transcript() }
	f := &teams.File{Version: teams.Version, Teams: []teams.Team{{ID: "aaaaaaaaaaaa", Name: "Interface cleanup", Manager: file(manager), Members: []teams.Member{{Key: file(manager), File: file(manager)}, {Key: file(member), File: file(member)}}}}}
	if err := teams.Save(config.ProfileDir(), f.Teams); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{manager, member} {
		if v3EmptySession(dir) {
			t.Fatal("assigned conversation considered disposable")
		}
	}
	_, empty := v3ScanBucket(bucket)
	if len(empty) != 1 || empty[0].dir != unused {
		t.Fatal("assigned empty conversation was offered for reuse")
	}
	// A stale cleanup snapshot made before membership must be rechecked.
	v3ReapEmpty([]v3Folder{{dir: manager}, {dir: member}, {dir: unused}}, "")
	for _, dir := range []string{manager, member} {
		if _, err := os.Stat(file(dir)); err != nil {
			t.Fatal("assigned transcript was removed", err)
		}
	}
	if _, err := os.Stat(unused); !os.IsNotExist(err) {
		t.Fatal("unassigned empty was no longer cleaned")
	}
	found, err := v3ResolveSession("", workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	if found.Place.Dir == manager || found.Place.Dir == member {
		t.Fatal("new launch reused a team conversation")
	}
	// A later launch and an explicit resume keep the new empty manager intact.
	if _, err := v3ResolveSession("", workspace, workspace, false); err != nil {
		t.Fatal(err)
	}
	named, err := v3NamedSession(file(manager), workspace, false)
	if err != nil || named.Transcript != file(manager) {
		t.Fatal("manager did not survive restart", err)
	}
}

func TestUnreadableTeamsNeverAuthorizeEmptyCleanupOrReuse(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	profile := t.TempDir()
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	workspace := t.TempDir()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	dir := writeV3Session(t, bucket, "keep-empty", "", time.Time{})
	if err := os.WriteFile(teams.Path(profile), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if v3EmptySession(dir) {
		t.Fatal("unknown membership treated as disposable")
	}
	_, empty := v3ScanBucket(bucket)
	if len(empty) != 0 {
		t.Fatal("unknown membership was reused")
	}
	v3ReapEmpty([]v3Folder{{dir: dir}}, "")
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("unknown membership was removed")
	}
}
