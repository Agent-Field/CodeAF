package main

// chatv3_layout.go is where a v3 conversation lives, and which one a launch
// opens (docs/CHAT-V3.md, Decision 26).
//
// A session is a FOLDER: ~/.aforge/v3/projects/<encoded-workspace>/<id>/, with
// the transcript, the sidecars, the node journals and — for an owned session —
// the workspace itself inside it. internal/session's place.go is the arithmetic
// on that folder; this file is the three decisions a launch makes before the
// arithmetic can start:
//
//   - WHICH PROJECT. The workspace is the git root of the directory the person
//     stood in, so `aforge` typed in repo/cmd and in repo/ is the same project.
//     The encoded directory name is a bucket and not an identity — meta.json
//     carries the real path — which is what lets the encoding stay dumb.
//   - BORROWED OR OWNED. A conversation opened inside a project borrows it and
//     litters nothing; one opened nowhere at all owns a workspace of its own.
//   - WHICH CONVERSATION. The newest one the PERSON spoke in, an empty one
//     reused rather than duplicated, or a new one — and the other empties are
//     reaped on the way past.
//
// EVERY PATH GOES THROUGH internal/home, so AFORGE_HOME moves all of it.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// v3Dir is ~/.aforge/v3: the directory this surface keeps its own files in —
// the model cache, the history list, the drafts, the memory file. The
// conversations live under it in projects/ (see [v3ProjectDir]).
func v3Dir() (string, error) {
	dir := home.Join("v3")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create session directory: %w", err)
	}
	return dir, nil
}

// v3ProjectDir is one project's BUCKET: the directory holding every session
// folder this workspace has ever had, plus the project's task index. The name
// is the workspace with its separators turned to dashes — one flat level, and
// readable, because the point of keeping a person's sessions in files is that
// they can find them.
func v3ProjectDir(workspace string) (string, error) {
	dir := home.Join("v3", "projects", encodeWorkspace(workspace))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create session directory: %w", err)
	}
	return dir, nil
}

func encodeWorkspace(workspace string) string {
	encoded := strings.ReplaceAll(filepath.Clean(workspace), string(filepath.Separator), "-")
	encoded = strings.ReplaceAll(encoded, ":", "-")
	if !strings.HasPrefix(encoded, "-") {
		encoded = "-" + encoded
	}
	return encoded
}

// ── which project, and does the conversation have one ───────────────────────

// v3Workspace answers the two questions a launch asks about the directory it
// was typed in: which PROJECT this is, and whether the conversation has one at
// all.
//
// The answer's first half is the bucket key and the borrowed session's tools
// root: the git root of the directory, so every subdirectory of a repository is
// the same project, and the directory itself when there is no repository. An
// explicit workspace — the engine's --workspace, a door that has already
// resolved one — is taken as given and is always borrowed: it is a place
// somebody named, and naming a place is asking to work in it.
func v3Workspace(cwd, explicit string) (string, bool) {
	if named := strings.TrimSpace(explicit); named != "" {
		return named, false
	}
	if root, found := v3GitRoot(cwd); found {
		return root, false
	}
	return cwd, v3NoProjectPlace(cwd)
}

// v3NoProjectPlace reports whether a directory is somewhere the person stood by
// accident rather than on purpose — the two places a conversation has no
// project to be about.
//
// A PLAIN DIRECTORY IS STILL BORROWED. Somebody who runs aforge in ~/notes
// means ~/notes: it is not a repository, but it is where their work is, and a
// session that quietly worked in a hidden folder of its own instead would be
// answering questions about the wrong directory. What is left is the home
// directory ITSELF — where a terminal opens, which nobody chose — and the
// temporary directories, which are where a launcher, a script or an editor
// starts a process that was never about a place at all. Those own a workspace,
// and a session whose workspace was temporary is litter the sweep may reap.
func v3NoProjectPlace(dir string) bool {
	dir = filepath.Clean(dir)
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == dir {
		return true
	}
	for _, temporary := range []string{os.TempDir(), "/tmp"} {
		temporary = filepath.Clean(temporary)
		if dir == temporary || strings.HasPrefix(dir, temporary+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// v3GitRoot is `git rev-parse --show-toplevel` in one directory. A machine with
// no git, a directory that is not in a repository, and a repository this build
// cannot read all answer the same thing — no root — because all three mean the
// same to the caller: there is nothing here to call a project.
func v3GitRoot(dir string) (string, bool) {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	command.Dir = dir
	out, err := command.Output()
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false
	}
	return root, true
}

// ── which conversation ──────────────────────────────────────────────────────

// v3Session is one launch's answer: the folder, the journal inside it, and
// whether the conversation was found rather than made.
type v3Session struct {
	Place      session.Place
	Transcript string
	Resumed    bool
	// Bucket is the project directory the folder sits in, which the surface
	// needs after the launch for its list of recent conversations.
	Bucket string
}

// v3ResolveSession decides which conversation this launch opens.
//
// THE ORDER IS THE LAW, and every step of it is Decision 26's:
//
//  1. An explicit --session is taken as given. A path a person named is a path
//     they mean, existing or not, and an old flat transcript still opens as
//     itself — with no Place, which is what the legacy layout is spelled as.
//  2. Otherwise the newest conversation the PERSON last spoke in, read from
//     meta.json rather than from file mtime: a background write touching a file
//     is not somebody returning to a conversation.
//  3. Otherwise an empty untitled session is REUSED rather than duplicated. The
//     flat layout minted one per launch and left nineteen dead ones behind on
//     the author's own machine, which is this rule's whole case.
//  4. Otherwise a new one.
//
// And the other empties are reaped on the way past.
func v3ResolveSession(explicit, workspace, launchDir string, owned bool) (v3Session, error) {
	if explicit != "" {
		return v3NamedSession(explicit, workspace, owned)
	}
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		return v3Session{}, err
	}
	spoken, empty := v3ScanBucket(bucket)

	if len(spoken) > 0 {
		folder := spoken[0]
		place := v3PlaceFor(folder.dir, workspace, owned || folder.meta.Owned)
		v3ReapEmpty(empty, "")
		return v3Session{Place: place, Transcript: place.Transcript(), Resumed: true, Bucket: bucket}, nil
	}
	if folder, found := v3FreeEmpty(empty); found {
		place := v3PlaceFor(folder.dir, workspace, owned)
		v3ReapEmpty(empty, folder.dir)
		// Reused, not resumed: there is nothing in it to come back to, and a
		// surface that said "resumed" about a folder with no words in it would be
		// naming a conversation that never happened.
		return v3Session{Place: place, Transcript: place.Transcript(), Bucket: bucket}, nil
	}
	place, err := v3MintSession(bucket, workspace, launchDir, owned)
	if err != nil {
		return v3Session{}, err
	}
	return v3Session{Place: place, Transcript: place.Transcript(), Bucket: bucket}, nil
}

// v3NamedSession opens the path a person put on the command line.
//
// A folder's transcript is opened AS a folder — the sidecars, the node journals
// and the droppings all belong to it, and a session opened by its own path is
// still that session. Anything else is the flat layout, which keeps working
// exactly as it did: a zero Place, and every sidecar derived from the file's
// stem.
func v3NamedSession(explicit, workspace string, owned bool) (v3Session, error) {
	path, err := expandHome(explicit)
	if err != nil {
		return v3Session{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return v3Session{}, fmt.Errorf("create session directory: %w", err)
	}
	_, statErr := os.Stat(path)
	found := v3Session{Transcript: path, Resumed: statErr == nil, Bucket: filepath.Dir(filepath.Dir(path))}
	if filepath.Base(path) != v3TranscriptName {
		bucket, err := v3ProjectDir(workspace)
		if err != nil {
			return v3Session{}, err
		}
		found.Bucket = bucket
		return found, nil
	}
	// The folder's own account of itself wins over the launch's: a session
	// carried in from another directory is the session it was, borrowed or
	// owned, whatever the terminal it is being opened from looks like. A folder
	// with no meta.json yet takes the launch's posture.
	found.Place = v3PlaceOf(path, workspace)
	if meta, _ := session.LoadMeta(filepath.Dir(path)); strings.TrimSpace(meta.ID) == "" && owned {
		found.Place = v3PlaceFor(filepath.Dir(path), workspace, true)
	}
	return found, nil
}

// v3PlaceOf reads which session folder a transcript belongs to: the folder when
// the path is one's transcript.jsonl, and the legacy zero Place for a flat file
// — which is how a caller says "derive the sidecars the old way" (place.go).
//
// The folder's own meta.json answers whether it owns its workspace, because
// that is a fact about the conversation and not about the window opening it.
func v3PlaceOf(transcript, workspace string) session.Place {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" || filepath.Base(transcript) != v3TranscriptName {
		return session.Place{}
	}
	dir := filepath.Dir(transcript)
	meta, _ := session.LoadMeta(dir)
	root := strings.TrimSpace(meta.Workspace)
	if root == "" {
		root = workspace
	}
	return v3PlaceFor(dir, root, meta.Owned)
}

// v3PointAt aims one launch's config at a session folder: the journal inside
// it, the folder itself, and — for an owned session — the tools root that comes
// with it. It is the ONE place a config changes conversations, so /new, a
// contended resume and the picker cannot drift apart.
func v3PointAt(cfg session.Config, place session.Place) (session.Config, error) {
	cfg.Place = place
	cfg.SessionFile = place.Transcript()
	if !place.Owned {
		return cfg, nil
	}
	if err := prepareOwnedWorkspace(place); err != nil {
		return cfg, err
	}
	cfg.Workspace = place.Work()
	return cfg, nil
}

// v3Reopen aims a config at a transcript a person picked. It is [v3PointAt] for
// a path rather than a folder: a session folder's journal reopens as its
// folder, and a flat one reopens as the legacy layout it was written in.
func v3Reopen(cfg session.Config, transcript, workspace string) (session.Config, error) {
	place := v3PlaceOf(transcript, workspace)
	if place.Dir == "" {
		cfg.Place = session.Place{}
		cfg.SessionFile = transcript
		return cfg, nil
	}
	return v3PointAt(cfg, place)
}

// v3TranscriptName is the journal's name inside a session folder. It is
// [session.Place.Transcript]'s last element, repeated here because this side
// has to RECOGNIZE one — a path a person typed — where the other side only ever
// builds them.
const v3TranscriptName = "transcript.jsonl"

// v3PlaceFor builds the Place for one session folder. The workspace follows
// from the posture and never from a caller's opinion: an owned session's tools
// root is its own work/, and a borrowed one's is the project it borrowed.
func v3PlaceFor(dir, workspace string, owned bool) session.Place {
	place := session.Place{Dir: dir, Owned: owned}
	if owned {
		place.Workspace = place.Work()
		return place
	}
	place.Workspace = workspace
	return place
}

// v3MintSession makes one session folder: a fresh id, the directory named by
// it, and meta.json written BEFORE the first line of the journal — so a picker
// in another window sees the conversation from the moment it exists rather than
// from the moment somebody speaks in it.
func v3MintSession(bucket, workspace, launchDir string, owned bool) (session.Place, error) {
	id := session.NewSessionID()
	dir := filepath.Join(bucket, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return session.Place{}, fmt.Errorf("create session directory: %w", err)
	}
	place := v3PlaceFor(dir, workspace, owned)
	// The write is not checked for the reason place.go gives: meta.json is a
	// citation and not the record, and a session that would not open because a
	// lookup file could not be written would be the wrong trade twice over.
	_ = session.SaveMeta(dir, session.Meta{
		ID:        id,
		Workspace: place.Workspace,
		LaunchDir: launchDir,
		Owned:     owned,
		Created:   time.Now(),
	})
	return place, nil
}

// v3NextSession mints a sibling of the session a launch is already on: the same
// bucket, the same borrowed-or-owned posture, a new id. It is what /new and a
// contended resume both need — a second conversation about the same project —
// and it falls back to the workspace's own bucket for a launch that has no
// folder at all (an explicit flat --session).
func v3NextSession(current session.Place, workspace string) (session.Place, error) {
	if dir := strings.TrimSpace(current.Dir); dir != "" {
		root := strings.TrimSpace(current.Workspace)
		if current.Owned {
			// An owned session's workspace is its OWN work/, so the sibling's
			// cannot be inherited: it is minted with the folder, below.
			root = workspace
		}
		return v3MintSession(filepath.Dir(dir), root,
			v3StampLaunchDir(v3LaunchDir(), root), current.Owned)
	}
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		return session.Place{}, err
	}
	return v3MintSession(bucket, workspace,
		v3StampLaunchDir(v3LaunchDir(), workspace), false)
}

// v3LaunchDir is where the person is standing, and "" when even that cannot be
// read — which is a field of meta.json left empty rather than a launch that
// fails.
//
// IT IS CAPTURED ONCE, on the first call, and every later call reads the same
// answer back. The reason is that a process can now open several conversations
// and one of them may be about another project entirely: the launch directory is
// a fact about the WINDOW — where the person was standing when they typed
// `aforge` — and re-reading it per conversation would only be a way for it to
// come back different. Nothing in this process changes directory; `aforge
// engine` is the one door that does and it does so before it opens anything
// (engine.go), so its first call captures the workspace it moved into, exactly
// as the per-call read did.
var v3LaunchWhere struct {
	once sync.Once
	dir  string
}

func v3LaunchDir() string {
	v3LaunchWhere.once.Do(func() {
		if dir, err := os.Getwd(); err == nil {
			v3LaunchWhere.dir = dir
		}
	})
	return v3LaunchWhere.dir
}

// v3StampLaunchDir is what a new session folder RECORDS as the launch directory,
// and the one case where the honest answer is nothing.
//
// [session.Meta.LaunchDir] is "where the person actually stood when the session
// opened", and it is read by two things: [projectPath] names the project of an
// owned session from it, and the idle sweep marks a session disposable if EITHER
// its workspace or its launch directory is under a temp directory
// (internal/session's sweep.go). The second is the trap. A process started in
// /tmp — by a script, by an editor, by a test harness — that then opens a
// conversation in ~/work/repo would stamp that conversation as litter and have
// it reaped once it went idle, in a project the person very much meant.
//
// SO THE LAUNCH DIRECTORY TRAVELS ONLY WHILE IT AGREES WITH THE WORKSPACE ABOUT
// BEING TEMPORARY. When it does not, the field is left empty, which the sweep
// reads as not-temporary and meta.json omits. A conversation opened in /tmp is
// still litter, because its workspace says so on its own.
func v3StampLaunchDir(launchDir, workspace string) string {
	if strings.TrimSpace(launchDir) == "" {
		return ""
	}
	if v3TempPath(launchDir) && !v3TempPath(workspace) {
		return ""
	}
	return launchDir
}

// v3TempPath reports whether a path sits inside a directory the machine itself
// considers disposable.
//
// IT IS THE SAME RULE internal/session's sweep applies to the two fields of
// meta.json, spelled here because that one is unexported and because this side
// has to answer the question BEFORE the file is written rather than after. Both
// the configured temp directory and /tmp are asked, because TMPDIR moves the
// first without making the second any less of a temp directory to the person who
// typed it. The pair is pinned by a test; if the sweep's rule ever widens, this
// one widens with it or a conversation gets reaped under somebody.
func v3TempPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	path = filepath.Clean(path)
	for _, temporary := range []string{os.TempDir(), "/tmp"} {
		temporary = filepath.Clean(strings.TrimSpace(temporary))
		if temporary == "" || temporary == string(filepath.Separator) {
			continue
		}
		if path == temporary || strings.HasPrefix(path, temporary+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ── the bucket, read and groomed ────────────────────────────────────────────

// v3Folder is one session folder as the resume law weighs it.
type v3Folder struct {
	dir  string
	meta session.Meta
	// at is what the folder is ordered by: when the person last spoke in it,
	// and the folder's own modification time for one whose meta.json is missing
	// or was written before this field existed.
	at time.Time
}

// v3ScanBucket sorts a project's session folders into the two piles the resume
// law needs, each newest first: the ones somebody has SPOKEN in, and the empty
// ones.
//
// Emptiness is read from the transcript rather than from meta.json, because the
// transcript is the record and meta.json is a citation: [session.Peek] answers
// false for a file nobody ever said anything in, which is exactly the folder
// this groom may reuse or remove. A folder holding work, worktrees or
// deliverables is never empty whatever its journal says — those are somebody's
// content, and no groom of a bookkeeping file may take them.
func v3ScanBucket(bucket string) (spoken, empty []v3Folder) {
	entries, err := os.ReadDir(bucket)
	if err != nil {
		return nil, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(bucket, entry.Name())
		place := session.Place{Dir: dir}
		if _, err := os.Stat(place.Transcript()); err != nil {
			continue
		}
		folder := v3Folder{dir: dir}
		folder.meta, _ = session.LoadMeta(dir)
		folder.at = folder.meta.LastUserAt
		if folder.at.IsZero() {
			if info, err := entry.Info(); err == nil {
				folder.at = info.ModTime()
			}
		}
		if v3EmptySession(dir) {
			empty = append(empty, folder)
			continue
		}
		spoken = append(spoken, folder)
	}
	sort.SliceStable(spoken, func(i, j int) bool { return spoken[i].at.After(spoken[j].at) })
	sort.SliceStable(empty, func(i, j int) bool { return empty[i].at.After(empty[j].at) })
	return spoken, empty
}

// v3EmptySession reports whether a session folder is one nobody ever said
// anything in and nothing was made in.
func v3EmptySession(dir string) bool {
	place := session.Place{Dir: dir, Owned: true}
	if _, spoken := session.Peek(place.Transcript()); spoken {
		return false
	}
	for _, kept := range []string{place.Work(), place.Trees(), place.Artifacts()} {
		if _, err := os.Stat(kept); err == nil {
			return false
		}
	}
	return true
}

// v3FreeEmpty is the newest empty session nobody else is sitting at. A window
// already open on an empty conversation is a person at a prompt, and handing
// their journal to a second window would only produce the lock notice one step
// later.
func v3FreeEmpty(empty []v3Folder) (v3Folder, bool) {
	for _, folder := range empty {
		if !session.InUse(session.Place{Dir: folder.dir}.Transcript()) {
			return folder, true
		}
	}
	return v3Folder{}, false
}

// v3ReapEmpty removes the empty session folders a launch left behind, except
// the one it is about to open.
//
// It is `rm -rf` and it is safe to be: an empty folder has no worktrees to
// unregister and no work directory to lose, which is what [v3EmptySession]
// checks before a folder is ever called one. A folder another window is holding
// open is skipped — a live empty session is somebody sitting at a prompt.
func v3ReapEmpty(empty []v3Folder, keep string) {
	for _, folder := range empty {
		if folder.dir == keep {
			continue
		}
		if session.InUse(session.Place{Dir: folder.dir}.Transcript()) {
			continue
		}
		_ = os.RemoveAll(folder.dir)
	}
}
