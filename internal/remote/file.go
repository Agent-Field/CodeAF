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
	said := AttachedSentence(args.Text, kept)
	events, err := agent.SubmitImage(context.Background(), said, images)
	return s.stream(MethodSubmitFiles, said, events, err)
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

// depositFile keeps ONE arriving file and says where it went, and the whole of
// what makes it a different door from [server.submitFiles] is what it does NOT
// do afterwards.
//
// A DEPOSIT IS A FACT ON DISK AND NOT A THING ANYBODY SAID. It is the browse
// page's drag-drop lane (internal/filedoor, reached through tui3's hostSource),
// and a file dropped on a web page is not a sentence: no turn opens, no event
// is sent, nothing is written to the transcript. The conversation learns of the
// file when a person mentions it, which is what /attach — the same landing
// place with the person's own words on it — has always been for.
//
// THE BYTES GO WHERE AN ATTACHMENT GOES AND NOWHERE ELSE. [server.keep] is the
// implementation entire, so the name law ([attachmentName]) and the naming
// ([writeAttachment]) are the ones the message lane already obeys rather than a
// second spelling of them that could drift — which matters more here than
// anywhere, because this door is reached from a web page on a machine the
// engine cannot see.
func (s *server) depositFile(call Frame) (json.RawMessage, error) {
	file, err := arg[WireFile](call)
	if err != nil {
		return nil, err
	}
	// The name is judged before the weight, though [server.keep] will judge it
	// again, so that the sentence about the weight can NAME the file: a refusal
	// is printed in a browser tab beside the row it is about, and the one thing
	// that may not be echoed there is a string that was never a file name.
	name, err := attachmentName(file.Name)
	if err != nil {
		return nil, err
	}
	// THE CEILING IS CHECKED HERE THOUGH THE DOOR ALSO CHECKS IT. The browse
	// page refuses an oversized drop on the surface's side so the person hears
	// it before the bytes are spent (filedoor's maxCrossBytes), and a boundary
	// that trusts a check made on the other machine is not a boundary. The
	// number and the sentence are [server.fetchFile]'s, because a file is the
	// same weight in both directions.
	if len(file.Bytes) > maxFetchBytes {
		return nil, fmt.Errorf("engine: %s is %dMB and the most one file may cross this connection is %dMB", name, len(file.Bytes)>>20, maxFetchBytes>>20)
	}
	kept, err := s.keep([]WireFile{file})
	if err != nil {
		return nil, err
	}
	return json.Marshal(DepositedFile{Path: kept[0]})
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
	//
	// AND SO IS THE DIGEST, which is what turns a fetch into something a surface
	// can decide not to do. It is the sha256 of the bytes that actually crossed
	// — never of what the stat above said was there — because a file that grew
	// between the two would otherwise be handed over under the name of a
	// content it no longer has. It is the digest internal/cas keys on, so a
	// surface holding the blob already can answer the next click without asking
	// this machine anything.
	sum := sha256.Sum256(data)
	return json.Marshal(FetchedFile{
		Name:  filepath.Base(path),
		MIME:  fileMIME(path),
		Size:  int64(len(data)),
		Hash:  hex.EncodeToString(sum[:]),
		Bytes: data,
	})
}

// listDirMax is the most rows one listing carries, and the ceiling is on the
// FRAME rather than on the person. A directory holding a hundred thousand
// generated files is an ordinary thing on a machine that has been working, and
// a listing of it would be megabytes of JSON crossing a call that has ten
// seconds to answer (client.go's callDeadline) for a screen that draws a page
// at a time. What is cut is the TAIL, and [DirListing.Truncated] says so —
// a listing that quietly stopped early would be this engine lying about that
// machine's disk, which is the one thing a browse view may never do.
//
// IT IS A VAR SO A TEST CAN LOWER IT. Proving the cut works by writing two
// thousand files is seconds of somebody's disk for a fact three files can
// establish just as well.
var listDirMax = 2000

// statPathsMax is the most paths one stat call may ask about.
//
// The batch exists so a screen full of new words is ONE round trip and not one
// per word, and the ceiling exists because a batch that size is already every
// word on a screen — past it, something is asking about a machine rather than
// about a view. Over it the call is REFUSED and not trimmed: a surface told
// about the first 64 of its 200 candidates would draw 136 doors that silently
// did not exist, and a wrong answer arriving quietly is worse than no answer
// arriving at all.
const statPathsMax = 64

// listDir is one directory of the engine's, as rows rather than as bytes: what
// the browse view and a file picker over a connection read.
//
// IT IS [handOver]'S LAW WITH THE FILE CLAUSE TURNED AROUND — the same two
// roots, the same symlinks resolved before anything is compared, and then the
// target has to BE a directory rather than not be one. Nothing else about what
// a surface may see changes, which is the whole point: the browse view is a
// view of this conversation's places and not of somebody's machine.
//
// A SYMLINKED ENTRY IS DESCRIBED, NEVER FOLLOWED OUT OF THE ROOTS. A link whose
// target stays inside them is described BY that target, because it names a
// place this conversation can reach anyway and drawing a directory as a
// zero-byte file would be the listing being unhelpful about its own workspace.
// A link that leaves them is listed as a plain file of no size: the row admits
// the name is there and says nothing that would invite a fetch [handOver] is
// about to refuse. That is the simplest rule that stays honest in both
// directions, and it is stated here because a surface cannot infer it.
func (s *server) listDir(call Frame) (json.RawMessage, error) {
	args, err := arg[ListDirArgs](call)
	if err != nil {
		return nil, err
	}
	workspace, place := s.session.folder()

	path, info, err := reachable(place, workspace, args.Path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("engine: %s is not a directory", args.Path)
	}
	found, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("engine: could not read %s", filepath.Base(path))
	}

	// os.ReadDir answers sorted by name, so splitting it in two leaves each
	// half sorted and the join is exactly the order [DirListing] promises.
	dirs := make([]DirEntry, 0, len(found))
	files := make([]DirEntry, 0, len(found))
	for _, entry := range found {
		row, ok := describe(place, workspace, filepath.Join(path, entry.Name()), entry.Name())
		if !ok {
			// Something that went away between the read and the stat, or that
			// this engine may not stat at all, is LEFT OUT rather than drawn as
			// a row with nothing in it — a name with no facts beside it is a
			// row a person would click.
			continue
		}
		if row.Dir {
			dirs = append(dirs, row)
		} else {
			files = append(files, row)
		}
	}
	// The cut happens after the ordering, so what is dropped is the tail of the
	// listing the surface would have drawn rather than an arbitrary slice of
	// the directory.
	entries := append(dirs, files...)
	listing := DirListing{Path: path, Entries: entries}
	if len(entries) > listDirMax {
		listing.Entries, listing.Truncated = entries[:listDirMax], true
	}
	return json.Marshal(listing)
}

// describe is one row of a listing, or false for an entry this engine cannot
// see at all. The symlink rule it carries out is stated on [server.listDir].
func describe(place session.Place, workspace, path, name string) (DirEntry, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return DirEntry{}, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(path)
		if err != nil || (!within(workspace, target) && !within(place.Dir, target)) {
			return DirEntry{Name: name, ModTime: info.ModTime().Unix()}, true
		}
		reached, err := os.Stat(target)
		if err != nil {
			return DirEntry{Name: name, ModTime: info.ModTime().Unix()}, true
		}
		info = reached
	}
	row := DirEntry{Name: name, Dir: info.IsDir(), ModTime: info.ModTime().Unix()}
	if !row.Dir {
		// A DIRECTORY'S OWN BYTE COUNT IS A FILESYSTEM ARTIFACT and not a fact
		// about what is in it, so it is left empty and the surface draws
		// nothing for it, by the emptiness law. The type is a hint and empty is
		// an honest answer for an extension nobody can name.
		row.Size, row.MIME = info.Size(), fileMIME(name)
	}
	return row, true
}

// statPaths is the engine's word on a batch of candidate paths, in the order
// they were asked about — a surface pairs the answers with its own rows by
// position, so the order is part of the contract and not a convenience.
//
// A PATH THIS SESSION WOULD REFUSE REPORTS THAT IT IS NOT THERE, which
// wire.go's [PathFact] states as the law it is: to a surface deciding whether
// to draw a door, a file that will refuse to open IS absent. So a path outside
// the two roots, a link that leaves them, and a name with nothing behind it all
// answer the same way — the alternative is a link that exists, invites a click,
// and answers a refusal, which is worse than a word that was never a link.
//
// AND IT ANSWERS WITH THE FILE'S OWN NUMBERS, which is what makes it the
// freshness question as well as the existence one. The stat that decides
// Exists already holds the size and the modification time, so carrying them
// costs this machine nothing and saves the surface a whole transfer: a cache
// that can ask "is this still the file I fetched" in one small frame is a cache
// that can be trusted to keep bytes at all (wire.go's [PathFact]).
func (s *server) statPaths(call Frame) (json.RawMessage, error) {
	args, err := arg[StatPathsArgs](call)
	if err != nil {
		return nil, err
	}
	if len(args.Paths) > statPathsMax {
		return nil, fmt.Errorf("engine: stat asks for %d paths and the most one call may ask is %d", len(args.Paths), statPathsMax)
	}
	workspace, place := s.session.folder()

	facts := make([]PathFact, 0, len(args.Paths))
	for _, asked := range args.Paths {
		fact := PathFact{Path: asked}
		if _, info, err := reachable(place, workspace, asked); err == nil {
			// THE NUMBERS COME OFF THE SAME STAT AS THE EXISTENCE, so there is
			// no window between the two in which the file could change and no
			// second syscall to pay for. A directory reports its own size,
			// which is a filesystem artifact and not a fact about what is in
			// it — the surface reads these for files.
			fact.Exists, fact.Dir = true, info.IsDir()
			fact.Size, fact.ModTime = info.Size(), info.ModTime().Unix()
		}
		facts = append(facts, fact)
	}
	return json.Marshal(facts)
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
	real, info, err := reachable(place, workspace, asked)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("engine: %s is not a file", asked)
	}
	return real, info, nil
}

// reachable is the part of that law that has nothing to do with bytes: a name
// this session may look at at all, resolved, and what it turns out to be.
//
// IT IS ONE FUNCTION BECAUSE THE THREE DOORS MUST NOT BE ABLE TO DISAGREE. A
// fetch, a listing and a stat each answer a different question about the same
// boundary, and a boundary written out three times is a boundary that will
// drift on the day one of them is changed — which is the day something outside
// the roots becomes visible through the door nobody re-read.
func reachable(place session.Place, workspace, asked string) (string, os.FileInfo, error) {
	path := strings.TrimSpace(asked)
	if path == "" {
		return "", nil, errors.New("engine: nothing was named")
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
