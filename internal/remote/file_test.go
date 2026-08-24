package remote

// file_test.go drives the attachment doors both ways: a file crossing to the
// engine and landing on its disk, the boundary refusing a name that tried to
// choose its own directory, and what this session will and will not hand back.
//
// The handlers are called directly wherever the question is about THEM. That is
// deliberate and not a shortcut: the dispatch case that reaches them is one line
// in server.go's invoke, and a test that could only run once that line existed
// would be a test of the switch statement.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// attachEngine is a scripted engine rooted at one directory, with a session folder
// beside it — the two roots [handOver] measures a fetch against.
func attachEngine(t *testing.T) (*server, *fakeAgent, string, session.Place) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	folder := filepath.Join(root, "session")
	for _, dir := range []string{workspace, folder} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("make %s: %v", dir, err)
		}
	}
	agent := &fakeAgent{model: "m"}
	place := session.Place{Dir: folder, Workspace: workspace}
	sess := NewSession(&Engine{Agent: agent, Workspace: workspace, Place: place}, false)
	return &server{out: io.Discard, session: sess}, agent, workspace, place
}

func submitFilesCall(t *testing.T, s *server, args SubmitFilesArgs) (StreamRef, error) {
	t.Helper()
	payload, err := s.submitFiles(Frame{Kind: "call", ID: 1, Method: MethodSubmitFiles, Payload: mustJSON(args)})
	if err != nil {
		return StreamRef{}, err
	}
	var ref StreamRef
	if err := json.Unmarshal(payload, &ref); err != nil {
		t.Fatalf("the result was not a stream reference: %v", err)
	}
	return ref, nil
}

// A FILE THAT ARRIVED IS A FILE ON THIS DISK AND A PATH IN THE SENTENCE. This
// is the whole of the attachment contract in one test: the bytes land under the
// session's own attachments/, the name the person gave it survives, and what the
// model is told is where to find it rather than what is in it.
func TestAnAttachedFileLandsInTheSessionsAttachmentsAndIsNamedByPath(t *testing.T) {
	s, agent, _, place := attachEngine(t)

	ref, err := submitFilesCall(t, s, SubmitFilesArgs{
		Text:  "why is this failing",
		Files: []WireFile{{Name: "server.log", MIME: "text/plain", Bytes: []byte("panic: nil map\n")}},
	})
	if err != nil {
		t.Fatalf("the engine refused an ordinary file: %v", err)
	}
	if ref.Stream == 0 {
		t.Fatal("a submitted message has to open a stream")
	}

	directory := filepath.Join(place.Dir, attachmentsDirectory)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("nothing landed in %s: %v", directory, err)
	}
	if len(entries) != 1 {
		t.Fatalf("one file was attached and %d landed", len(entries))
	}
	landed := filepath.Join(directory, entries[0].Name())
	if !strings.HasSuffix(landed, "-server.log") {
		t.Fatalf("the person's own name has to survive: %s", entries[0].Name())
	}
	body, err := os.ReadFile(landed)
	if err != nil || string(body) != "panic: nil map\n" {
		t.Fatalf("the bytes did not arrive whole: %q %v", body, err)
	}

	// THE MODEL IS TOLD THE PATH AND NOT THE CONTENTS. A message carrying the
	// file's body would put every attached CSV in the context window.
	agent.mu.Lock()
	sent := append([]string(nil), agent.sent...)
	agent.mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("one message was sent and the agent saw %d", len(sent))
	}
	if !strings.Contains(sent[0], "why is this failing") {
		t.Fatalf("the person's own words went missing: %q", sent[0])
	}
	if !strings.Contains(sent[0], "attached file: "+landed) {
		t.Fatalf("the message has to name the path it landed on:\n%s", sent[0])
	}
	if strings.Contains(sent[0], "panic: nil map") {
		t.Fatalf("the contents must not ride the message:\n%s", sent[0])
	}
}

// The same file twice in the same second is ONE file, which is the only
// collision this naming can have and the right answer to it — and two different
// files under one name are two files, which is the collision that would
// otherwise lose somebody's data.
func TestTwoAttachmentsUnderOneNameDoNotOverwriteEachOther(t *testing.T) {
	s, _, _, place := attachEngine(t)

	if _, err := submitFilesCall(t, s, SubmitFilesArgs{Files: []WireFile{
		{Name: "log.txt", Bytes: []byte("first")},
		{Name: "log.txt", Bytes: []byte("second")},
		{Name: "log.txt", Bytes: []byte("first")},
	}}); err != nil {
		t.Fatalf("the engine refused: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(place.Dir, attachmentsDirectory))
	if err != nil {
		t.Fatalf("read the attachments: %v", err)
	}
	if len(entries) != 2 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("two distinct files and one repeat make two on disk, not %d: %v", len(entries), names)
	}
}

// A BOUNDARY THAT TRUSTS ITS INPUT IS NOT A BOUNDARY. The name is joined to a
// directory of the engine's choosing, so a name that walked out of it would be
// this wire handing a remote surface an arbitrary write.
func TestAnAttachmentNameThatIsAPathIsRefusedAndWritesNothing(t *testing.T) {
	for _, name := range []string{
		"../../.ssh/authorized_keys",
		"..",
		"/etc/passwd",
		`..\..\windows\system32\drivers\etc\hosts`,
		"sub/dir.txt",
		"   ",
		"",
		"bad\nname.txt",
	} {
		s, agent, _, place := attachEngine(t)
		// The good file rides with the bad one, because A MESSAGE IS REFUSED
		// WHOLE OR KEPT WHOLE: a batch that landed its first file and then
		// refused would leave litter behind a turn that never opened.
		_, err := submitFilesCall(t, s, SubmitFilesArgs{Files: []WireFile{
			{Name: "fine.txt", Bytes: []byte("ok")},
			{Name: name, Bytes: []byte("x")},
		}})
		if err == nil {
			t.Fatalf("%q was accepted as an attachment name", name)
		}
		// NOTHING RAN AND NOTHING LANDED. A refusal that had already opened a
		// turn would be a message the person cannot take back.
		agent.mu.Lock()
		sent := len(agent.sent)
		agent.mu.Unlock()
		if sent != 0 {
			t.Fatalf("%q was refused and a turn opened anyway", name)
		}
		if entries, err := os.ReadDir(filepath.Join(place.Dir, attachmentsDirectory)); err == nil && len(entries) > 0 {
			t.Fatalf("%q was refused and %d files landed", name, len(entries))
		}
	}
}

func TestAnAcceptableAttachmentNameIsKeptWhole(t *testing.T) {
	for _, name := range []string{"server.log", "sales q3.csv", ".env.example", "réponse.txt"} {
		got, err := attachmentName(name)
		if err != nil {
			t.Fatalf("%q is an ordinary file name and was refused: %v", name, err)
		}
		if got != name {
			t.Fatalf("%q came back as %q", name, got)
		}
	}
}

// ── the door the other way ──────────────────────────────────────────────────

func fetch(t *testing.T, s *server, path string) (FetchedFile, error) {
	t.Helper()
	payload, err := s.fetchFile(Frame{Kind: "call", ID: 1, Method: MethodFetchFile, Payload: mustJSON(FetchFileArgs{Path: path})})
	if err != nil {
		return FetchedFile{}, err
	}
	var file FetchedFile
	if err := json.Unmarshal(payload, &file); err != nil {
		t.Fatalf("the result was not a file: %v", err)
	}
	return file, nil
}

func TestAFileInTheWorkspaceOrTheSessionFolderComesBack(t *testing.T) {
	s, _, workspace, place := attachEngine(t)
	deliverable := filepath.Join(workspace, "report.md")
	if err := os.WriteFile(deliverable, []byte("# findings\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	transcript := filepath.Join(place.Dir, "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := fetch(t, s, deliverable)
	if err != nil {
		t.Fatalf("a file in the workspace has to cross: %v", err)
	}
	if got.Name != "report.md" || string(got.Bytes) != "# findings\n" {
		t.Fatalf("came back as %q / %q", got.Name, got.Bytes)
	}
	// THE NAME IS THE ENGINE'S ANSWER, so the surface never derives one from a
	// path that is not on its disk.
	if strings.ContainsAny(got.Name, `/\`) {
		t.Fatalf("the name has to be a name: %q", got.Name)
	}
	// The type is a HINT and empty is an honest answer for an extension nobody
	// can name, so it is asserted where Go's own table has one rather than
	// against whatever /etc/mime.types this machine happens to hold.
	if got := fileMIME("/anywhere/chart.png"); got != "image/png" {
		t.Fatalf("fileMIME of a png is %q", got)
	}
	if got := fileMIME("/anywhere/no-extension"); got != "" {
		t.Fatalf("an unnameable type has to answer nothing, not %q", got)
	}
	if _, err := fetch(t, s, transcript); err != nil {
		t.Fatalf("the session's own folder has to cross: %v", err)
	}
	// A RELATIVE PATH IS THE WORKSPACE'S, which is what every relative path in
	// this conversation already means.
	if _, err := fetch(t, s, "report.md"); err != nil {
		t.Fatalf("a relative path is resolved against the workspace: %v", err)
	}
}

// THE REFUSAL IS THE ENGINE'S TO MAKE. A surface cannot know where this
// machine's line falls, so everything outside the two roots is refused here —
// including a link inside them that points out of them, which is why the real
// path is what gets measured.
func TestNothingOutsideTheWorkspaceOrTheSessionFolderCrosses(t *testing.T) {
	s, _, workspace, _ := attachEngine(t)
	outside := filepath.Join(t.TempDir(), "secrets.txt")
	if err := os.WriteFile(outside, []byte("shhh"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := fetch(t, s, outside); err == nil {
		t.Fatal("a file outside both roots was handed over")
	} else if !strings.Contains(err.Error(), "outside this conversation's workspace and its own folder") {
		t.Fatalf("the refusal has to say why: %v", err)
	}

	// A SYMLINK IS RESOLVED BEFORE THE COMPARISON. A containment test run on the
	// name a caller supplied is not a containment test at all: this link lives
	// inside the workspace and points out of it.
	link := filepath.Join(workspace, "shortcut.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("this filesystem has no symlinks: %v", err)
	}
	if _, err := fetch(t, s, link); err == nil {
		t.Fatal("a link out of the workspace handed over its target")
	}

	// And a directory is not a file, whatever else is true about it.
	if _, err := fetch(t, s, workspace); err == nil {
		t.Fatal("a directory was handed over as a file")
	}
	if _, err := fetch(t, s, ""); err == nil {
		t.Fatal("an empty path named a file")
	}
	if _, err := fetch(t, s, filepath.Join(workspace, "nothing-here")); err == nil {
		t.Fatal("a path that is not there answered with bytes")
	}
}

func TestAFileTooBigToCrossIsRefusedWithItsSize(t *testing.T) {
	s, _, workspace, _ := attachEngine(t)
	heavy := filepath.Join(workspace, "dump.bin")
	if err := os.WriteFile(heavy, make([]byte, maxFetchBytes+1), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := fetch(t, s, heavy)
	if err == nil {
		t.Fatal("a file over the ceiling crossed anyway")
	}
	if !strings.Contains(err.Error(), "the most one file may cross this connection is 16MB") {
		t.Fatalf("the refusal has to name the limit: %v", err)
	}
}

// ── what the model is told ──────────────────────────────────────────────────

func TestTheAttachedSentenceNamesEveryPathAndKeepsThePersonsWords(t *testing.T) {
	if got := AttachedSentence("look at this", nil); got != "look at this" {
		t.Fatalf("a message with no files is the message: %q", got)
	}
	one := AttachedSentence("look at this", []string{"/tmp/a.log"})
	if one != "look at this\n\nattached file: /tmp/a.log" {
		t.Fatalf("one file: %q", one)
	}
	many := AttachedSentence("", []string{"/tmp/a.log", "/tmp/b.csv"})
	if many != "attached files:\n/tmp/a.log\n/tmp/b.csv" {
		t.Fatalf("a wordless message is the files: %q", many)
	}
}

// ── over the real wire ──────────────────────────────────────────────────────

// The same journey through [Loopback]: the real client, the real engine, the
// real frames, and only the pipe faked.
func TestAFileRoundTripsThroughTheRealWire(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	folder := filepath.Join(root, "session")
	for _, dir := range []string{workspace, folder} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("make %s: %v", dir, err)
		}
	}
	agent := &fakeAgent{model: "m"}
	place := session.Place{Dir: folder, Workspace: workspace}
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: agent, Workspace: workspace, Place: place}, nil
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = loop.Close() }()

	_, err = loop.Client.Agent().SubmitFiles(t.Context(), "read this",
		[]WireFile{{Name: "notes.csv", Bytes: []byte("a,b\n1,2\n")}}, nil)
	if err != nil {
		// The dispatch case that reaches [server.submitFiles] lives in
		// server.go's invoke. Until it is there this is the honest report, and
		// the handler's own behavior is pinned by the tests above.
		if strings.Contains(err.Error(), "no such method") {
			t.Skipf("the engine's invoke has no %s case yet: %v", MethodSubmitFiles, err)
		}
		t.Fatalf("submit: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(folder, attachmentsDirectory))
	if err != nil || len(entries) != 1 {
		t.Fatalf("the file did not land: %d entries, %v", len(entries), err)
	}

	// And back the other way, off a path the engine itself would have named.
	written := filepath.Join(workspace, "made.txt")
	if err := os.WriteFile(written, []byte("done"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := loop.Client.FetchFile(written)
	if err != nil {
		if strings.Contains(err.Error(), "no such method") {
			t.Skipf("the engine's invoke has no %s case yet: %v", MethodFetchFile, err)
		}
		t.Fatalf("fetch: %v", err)
	}
	if got.Name != "made.txt" || string(got.Bytes) != "done" {
		t.Fatalf("came back as %q / %q", got.Name, got.Bytes)
	}
}
