package remote

// file.go is image.go's law with the pictures taken out of it.
//
// image.go remakes ONE payload on arrival, because a picture's path means
// nothing on the machine that did not read it off a disk. Every other thing a
// person drops into a chat — a log, a CSV, a PDF, a stack trace they saved —
// has exactly that problem and had no answer at all before version 2: the path
// was typed here and named nothing there.
//
// So this file states the general form of the same bargain, which wire.go's
// [SubmitFilesArgs] writes out in one sentence: WHAT A PERSON PUTS INTO THE
// CHAT IS THE SURFACE'S TO READ AND THE ENGINE'S TO KEEP. The bytes ride the
// message, the engine writes them where that session keeps such things, and
// what reaches the journal is a path that is true on the machine that owns the
// journal.
//
// AND THE MODEL IS TOLD THE PATH, NEVER THE CONTENTS. An attached file is a
// file and the session already has a `read` tool, so a 4MB CSV stays out of the
// context window until something actually wants a row of it. That is the one
// place this differs from a picture, and it differs because a picture has no
// tool that can open it — the bytes have to be IN the message or the model
// cannot look at them at all (attach.go in internal/tui3 states the same law
// from the surface's end).
//
// [server.fetchFile] is the same frame walked backwards, and it is the door
// version 1 did not have: a byte moving from the engine machine to the one the
// person is sitting at. THE REFUSAL ON THAT DOOR IS THE ENGINE'S TO MAKE — see
// [handOver] for what this session will hand over and what it will not.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// attachmentStampFormat is the sortable head of an attachment's name, exactly as
// image.go spells its own: `ls` reads the folder in the order the files arrived.
const attachmentStampFormat = "20060102-150405"

// attachmentsDirectory is the leaf a session's attachments land under, and
// attachmentsLegacyDirectory is where a session with no folder of its own puts
// them — the flat layout's dot directory, on the same scheme every other
// dropping uses (internal/session's landing.go).
const (
	attachmentsDirectory       = "attachments"
	attachmentsLegacyDirectory = ".aforge-v3/attachments"
)

// maxFetchBytes is the most one file may weigh coming the OTHER way, and the
// number is set by the DEADLINE rather than by the frame cap.
//
// [frameCap] is 64MB and would allow far more than this. What would not allow
// it is client.go's [callDeadline]: every call on this wire has ten seconds to
// answer, and a fetch that has not crossed in ten seconds is reported to the
// person as a connection that has gone — which would be a lie about a link that
// is working and merely slow. Sixteen megabytes clears a modest link inside
// that window; a ceiling much above it would be a limit the connection failed
// before the number did.
const maxFetchBytes = 16 << 20

// ── the engine's half ───────────────────────────────────────────────────────

// submitFiles is one message carrying files, and pictures if it also carried
// those. THE WHOLE MESSAGE IS ONE CALL and therefore one turn: a person who
// dropped a log file and pasted a screenshot said one thing, and two submits
// would have opened two turns for it ([SubmitFilesArgs.Images] says the same).
//
// The files are written down BEFORE the turn opens, so the path in the sentence
// names a file that already exists — a model told to read something that is
// still being written is a tool call that fails for a reason nobody can see.
func (s *server) submitFiles(call Frame) (json.RawMessage, error) {
	args, err := arg[SubmitFilesArgs](call)
	if err != nil {
		return nil, err
	}
	agent := s.session.current()
	if agent == nil {
		return nil, errors.New("engine: no conversation is open")
	}
	// The pictures go through image.go's own door, unchanged: this method adds
	// files to a message and changes nothing about what a picture is.
	images, err := s.store(args.Images)
	if err != nil {
		return nil, err
	}
	kept, err := s.keep(args.Files)
	if err != nil {
		return nil, err
	}
	// SubmitImage with no pictures IS Submit (internal/session's image.go says
	// so in as many words), so one door answers both shapes of this message.
	return s.stream(agent.SubmitImage(context.Background(), AttachedSentence(args.Text, kept), images))
}

// keep writes every arriving file into this session's attachments and answers
// with the paths, in the order they arrived.
//
// EVERY NAME IS JUDGED BEFORE ANY FILE IS WRITTEN. A message is refused whole or
// kept whole: a batch that failed on its third name having already landed its
// first two would leave litter in a folder nobody asked to litter, for a message
// that never opened a turn.
func (s *server) keep(files []WireFile) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	for _, file := range files {
		if _, err := attachmentName(file.Name); err != nil {
			return nil, err
		}
	}
	workspace, place := s.session.folder()

	out := make([]string, 0, len(files))
	for _, file := range files {
		path, err := writeAttachment(place, workspace, file)
		if err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, nil
}

// AttachmentsDir is where a file a person attached lands on the engine machine.
//
// IT IS BESIDE THE TRANSCRIPT AND NOT AMONG THE DELIVERABLES, and that is the
// one place it parts from [session.ImagesDir]. A deliverable is something the
// harness MADE and somebody may want back, so it lands where the person will
// look for it (internal/session's landing.go states that law). An attachment is
// the opposite claim: it is the person's own INPUT, they already have it, and a
// borrowed session that copied every log file somebody dropped into the repo it
// was lent would be littering. So it lands in the session's own folder, which
// is where everything a session holds ABOUT ITSELF lives — one folder per
// session is one gesture to delete them (docs/CHAT-V3.md, Decision 26).
//
// It is NOT under [session.Place.Logs], which is the other thing in that folder
// that is not a deliverable, because the droppings there carry the sweep's
// 7-day TTL. A journal that references an attachment by path must go on being
// readable long after that, so the file may not be swept.
//
// A session with no folder at all — the legacy flat layout — puts them under
// the workspace's own dot directory, the same fallback every other landing
// takes.
func AttachmentsDir(place session.Place, workspace string) string {
	if dir := strings.TrimSpace(place.Dir); dir != "" {
		return filepath.Join(dir, attachmentsDirectory)
	}
	return filepath.Join(workspace, filepath.FromSlash(attachmentsLegacyDirectory))
}

// writeAttachment puts one arriving file on disk under a name that cannot
// collide and cannot escape: the moment it arrived, the head of its own digest,
// and then the name the surface gave it, sanitized.
//
// The digest is what makes the name deterministic the way [writeImage]'s is —
// the same bytes attached twice in the same second are one file, which is the
// right answer to the only collision this naming can have — and the person's
// own name is kept on the end because THE MODEL IS TOLD THIS PATH: a file
// called `20260824-141233-a1b2c3d4` tells it nothing, and one called
// `…-sales-q3.csv` tells it what it is holding before it opens anything.
func writeAttachment(place session.Place, workspace string, file WireFile) (string, error) {
	name, err := attachmentName(file.Name)
	if err != nil {
		return "", err
	}
	directory := AttachmentsDir(place, workspace)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("engine: create the attachments directory: %w", err)
	}
	sum := sha256.Sum256(file.Bytes)
	stamped := time.Now().Format(attachmentStampFormat) + "-" + hex.EncodeToString(sum[:4]) + "-" + name
	path := filepath.Join(directory, stamped)
	if err := os.WriteFile(path, file.Bytes, 0o600); err != nil {
		return "", fmt.Errorf("engine: save the attached file: %w", err)
	}
	return path, nil
}

// attachmentName is the boundary, and A BOUNDARY THAT TRUSTS ITS INPUT IS NOT A
// BOUNDARY. [WireFile.Name] is documented as a name and never a path, but it
// arrives from another machine and is joined to a directory of this machine's
// choosing — so a "name" that walked out of that directory would be this wire
// handing a remote surface an arbitrary write on the engine's disk, which is
// the whole of the attack this function exists to refuse.
//
// It refuses rather than repairs. `filepath.Base("../../.ssh/authorized_keys")`
// is a perfectly good file name and quietly turns an attempt to escape into a
// file called `authorized_keys` landing somewhere harmless — which is safe and
// dishonest, because the surface asked for something and got something else
// under the same name. Nothing legitimate on this wire sends a separator, so
// the refusal costs nobody anything and names exactly what was wrong.
//
// BOTH SEPARATORS ARE REFUSED, not just this machine's. A backslash is a
// separator on the surface's machine and an ordinary character in a file name
// on the engine's, and the engine must not be the machine that finds that out.
func attachmentName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("engine: an attached file arrived with no name")
	}
	if strings.ContainsAny(name, `/\`) || filepath.IsAbs(name) || strings.Contains(name, ":") {
		return "", fmt.Errorf("engine: %q is a path and not a name — an attachment names itself and the engine chooses where it goes", raw)
	}
	if name == "." || name == ".." {
		return "", fmt.Errorf("engine: %q is not a file name", raw)
	}
	// A control character in a name is either a mistake or somebody writing an
	// escape sequence into a path a terminal will later print.
	if strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", fmt.Errorf("engine: %q is not a file name", raw)
	}
	// A name longer than most filesystems accept fails at the write with an
	// errno nobody can read, so it is cut here and the digest in front of it
	// keeps the result unique regardless.
	if len(name) > 120 {
		name = name[:120]
	}
	return name, nil
}

// fetchFile is the reverse door: the bytes of a file the ENGINE holds, asked
// for by a path on the engine's disk. It is what lets `/export` land on the
// machine the person is sitting at and a deliverable be brought here at all.
func (s *server) fetchFile(call Frame) (json.RawMessage, error) {
	args, err := arg[FetchFileArgs](call)
	if err != nil {
		return nil, err
	}
	workspace, place := s.session.folder()

	path, info, err := handOver(place, workspace, args.Path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFetchBytes {
		return nil, fmt.Errorf("engine: %s is %dMB and the most one file may cross this connection is %dMB", filepath.Base(path), info.Size()>>20, maxFetchBytes>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("engine: could not read %s", filepath.Base(path))
	}
	// Checked again, because the file could have grown between the stat and the
	// read — internal/session's own reasoning about its own guard, and the
	// surface's in internal/tui3's readAttachments.
	if len(data) > maxFetchBytes {
		return nil, fmt.Errorf("engine: %s is %dMB and the most one file may cross this connection is %dMB", filepath.Base(path), len(data)>>20, maxFetchBytes>>20)
	}
	// THE NAME IS THE ENGINE'S ANSWER, for the reason [FetchedFile.Name] gives
	// in the other direction: the surface is about to write this down and must
	// not have to derive a name from a path that is not on its disk.
	return json.Marshal(FetchedFile{Name: filepath.Base(path), MIME: fileMIME(path), Bytes: data})
}

// handOver decides what this session will hand over, and it is the whole of the
// refusal wire.go's [FetchFileArgs] says belongs here.
//
// TWO ROOTS AND NOTHING ELSE: the workspace this conversation is working in,
// and the session's own folder. Between them they hold everything the engine
// could honestly have shown the person — a deliverable, an export, a file the
// session wrote, the transcript itself — and outside them is the rest of a
// machine somebody else owns. A surface cannot know where that line falls, so a
// check on the surface's side would be a permission decision taken on the wrong
// machine.
//
// SYMLINKS ARE RESOLVED BEFORE THE COMPARISON, on the path AND on the roots. A
// containment test run on the name a caller supplied is not a containment test:
// a link inside the workspace pointing at `/etc/shadow` passes it and hands over
// the target. So the real path is what is measured, and a link that leaves the
// roots leaves them.
//
// And what comes back is a REGULAR FILE or nothing. A directory has no bytes, a
// fifo would block this engine's only reader forever, and a device would answer
// until the frame cap stopped it.
func handOver(place session.Place, workspace, asked string) (string, os.FileInfo, error) {
	path := strings.TrimSpace(asked)
	if path == "" {
		return "", nil, errors.New("engine: no file was named")
	}
	// A relative path is resolved against the workspace, which is the directory
	// every relative path in this conversation already means.
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, fmt.Errorf("engine: no such file: %s", asked)
	}
	if !within(workspace, real) && !within(place.Dir, real) {
		return "", nil, fmt.Errorf("engine: %s is outside this conversation's workspace and its own folder, and nothing outside those two crosses this connection", asked)
	}
	info, err := os.Lstat(real)
	if err != nil {
		return "", nil, fmt.Errorf("engine: no such file: %s", asked)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("engine: %s is not a file", asked)
	}
	return real, info, nil
}

// within reports whether a resolved path sits under a resolved root. The root
// is resolved here rather than by the caller so that the two sides of the
// comparison have had the same thing done to them — a real path measured
// against a root full of symlinks is the same false negative as the reverse.
func within(root, path string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(resolved, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// fileMIME is what the surface should call this thing, or "" when nobody can
// say. Empty is the honest answer and the surface draws nothing for it (the
// emptiness law); a guessed type on a downloaded file is how a `.csv` opens in
// the wrong program.
func fileMIME(path string) string {
	kind := mime.TypeByExtension(filepath.Ext(path))
	if kind == "" {
		return ""
	}
	if cut, _, found := strings.Cut(kind, ";"); found {
		return strings.TrimSpace(cut)
	}
	return kind
}

// AttachedSentence is what the MODEL is told about the files on a message, and
// it is a PATH rather than a payload — see this file's header for why.
//
// IT IS EXPORTED BECAUSE THE LOCAL SURFACE COMPOSES THE SAME SENTENCE. Over a
// connection the engine writes the files down and says this; on a local session
// there is nothing to write down — the file is already on the machine the
// session runs on — and the surface says it instead (internal/tui3's attach.go).
// The two must be one sentence, because a model that met a different phrasing
// depending on which machine it was running on would have learned two things.
//
// The words are plain on purpose. This is not a person's line; it is the part
// of the message that tells a model where something is, and anything decorative
// in it is a thing the model has to decide whether to repeat.
func AttachedSentence(text string, paths []string) string {
	if len(paths) == 0 {
		return text
	}
	var block string
	if len(paths) == 1 {
		block = "attached file: " + paths[0]
	} else {
		block = "attached files:\n" + strings.Join(paths, "\n")
	}
	if strings.TrimSpace(text) == "" {
		return block
	}
	return strings.TrimRight(text, " \t\n") + "\n\n" + block
}

// ── the surface's half ──────────────────────────────────────────────────────

// SubmitFiles is Submit with files attached, and pictures with them where the
// message carried both.
//
// THE BYTES ARE READ ON THE MACHINE THE PERSON IS SITTING AT, which is the only
// machine the path they typed means anything on — the same fact [Agent.SubmitImage]
// turns on, and the reason this method takes bytes it did not open a file for:
// the surface reads them at the moment enter is pressed, so the message is
// assembled from what was on disk when the person sent it.
//
// The name is reduced to a name HERE, with this machine's own idea of what a
// separator is. A surface on Windows holding `C:\logs\run.txt` knows that
// `run.txt` is the name of it and the engine, which may be a Unix box where a
// backslash is an ordinary character, does not. The engine refuses a path
// regardless ([attachmentName]) — that is the boundary and it stays one — but
// the refusal it would make is not a thing anybody should have to see for a
// path this side could read correctly.
func (a *Agent) SubmitFiles(ctx context.Context, text string, files []WireFile, images []session.Image) (<-chan session.Event, error) {
	loaded, err := loadImages(images)
	if err != nil {
		return nil, err
	}
	named := make([]WireFile, 0, len(files))
	for _, file := range files {
		file.Name = filepath.Base(strings.TrimSpace(file.Name))
		named = append(named, file)
	}
	return a.open(ctx, MethodSubmitFiles, SubmitFilesArgs{Text: text, Files: named, Images: loaded})
}

// FetchFile asks the engine for the bytes of a file it holds, by a path on the
// ENGINE's disk. The path is never resolved here — it came off something the
// engine already said, and this side has no directory to resolve it against.
//
// THE ERROR IS THE ENGINE'S SENTENCE AND NOTHING SOFTENS IT. A file this
// session will not hand over is a decision taken on the machine that owns the
// file, and a surface that redrew that refusal in its own words would be
// guessing at somebody else's boundary (the same bargain [Client.SaveStanding]
// makes with a store that refused a write).
func (c *Client) FetchFile(path string) (FetchedFile, error) {
	payload, err := c.call(nil, MethodFetchFile, FetchFileArgs{Path: path})
	if err != nil {
		return FetchedFile{}, err
	}
	var file FetchedFile
	if err := json.Unmarshal(payload, &file); err != nil {
		return FetchedFile{}, err
	}
	return file, nil
}
