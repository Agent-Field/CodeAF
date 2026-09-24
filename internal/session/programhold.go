package session

// THE HOLD A PROGRAM'S RUN HAS ON ITS FOLDER, and everything that asks it.
//
// A program works in the person's folder itself (programfolder.go), so while
// it runs that folder is its. The hold is how every other road in codeaf knows:
// a flock on a file under the state root named for the folder, which dies with
// the process that took it however that process dies, and which holds between
// two windows, two conversations in one engine and a shell alike, because a
// flock is per open file.
//
// A FOLDER IS BUSY WHEN A HELD FOLDER IS IT, HOLDS IT, OR IS INSIDE IT. A run
// on a plain folder of projects checkpoints everything under it and, once it
// has submitted, puts back whatever changed there and removes whatever was
// added; a second run in one of those projects had its edits reverted and its
// new files deleted under it, while both held their own exact path and each
// was sure it was alone. So the question is asked of the tree: the folders
// above a path by name, and every held folder below it by reading what each
// hold says it holds.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
)

// programHold is one live hold on a folder, the two things a refusal names:
// the folder, resolved, and whose run holds it (`senior-dev, task 4 (Fix the
// parser)`, or `senior-dev, a run started at a shell`).
type programHold struct {
	dir    string
	holder string
}

// programHoldWait is how long a run goes on asking for a hold it found taken,
// and programHoldPause how far apart it asks. A door that only wants to know
// whether a folder is busy takes the hold's file for the instant of asking
// ([programHoldAt]), and a run that asks at that same instant would read that
// as a run holding it; a real run holds its folder for minutes, so a tenth of
// a second costs a refused run nothing it would notice.
const (
	programHoldWait  = 100 * time.Millisecond
	programHoldPause = 20 * time.Millisecond
)

// claimProgramFolder takes the hold on one folder for a program's run and
// writes into it whose run it is and which folder, so a second run is told.
// It answers the held lock; or, when the folder is busy — held itself, or
// inside or around a folder held — the hold in the way; or a nil lock and no
// hold, which is a filesystem that takes no locks, and the run goes ahead
// unheld, which is what every run did before the hold existed.
//
// THE FOLDER'S OWN HOLD IS TAKEN BEFORE THE TREE AROUND IT IS READ, and kept
// while it is. Two runs readying a parent and a child at the same moment each
// hold their own and then look for the other, so at least one of them finds
// the other and refuses: they can both be refused, and never both start.
func claimProgramFolder(key, holder string) (*os.File, programHold, bool) {
	directory := home.Join("v3", programFolderDir)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, programHold{}, false
	}
	path := programHoldFile(key)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, programHold{}, false
	}
	for asked := time.Now(); ; {
		err = filelock.Lock(file, true, true)
		if err == nil || !isLockHeld(err) || time.Since(asked) >= programHoldWait {
			break
		}
		time.Sleep(programHoldPause)
	}
	if err != nil {
		_ = file.Close()
		if !isLockHeld(err) {
			return nil, programHold{}, false
		}
		return nil, readProgramHold(path, key), true
	}
	_ = file.Truncate(0)
	_, _ = file.WriteAt([]byte(strings.TrimSpace(holder)+"\n"+key), 0)
	if near, busy := programHoldNear(key, path); busy {
		_ = filelock.Unlock(file)
		_ = file.Close()
		return nil, near, true
	}
	return file, programHold{}, false
}

// programFolderHolder is who holds a folder now, or a folder around it or
// inside it, "" when nobody does, for a door that only wants to know.
func programFolderHolder(key string) string {
	hold, busy := programHoldNear(key, "")
	if !busy {
		return ""
	}
	return hold.holder
}

// programHoldNear answers the live hold on key, on a folder above it, or on a
// folder inside it, leaving the hold file skip out (the asker's own). It takes
// no hold of its own for longer than it takes to ask.
func programHoldNear(key, skip string) (programHold, bool) {
	key = filepath.Clean(strings.TrimSpace(key))
	if key == "" || key == "." {
		return programHold{}, false
	}
	if hold, busy := programHoldOver(key, skip); busy {
		return hold, true
	}
	files, _ := filepath.Glob(filepath.Join(home.Join("v3", programFolderDir), "*.lock"))
	for _, path := range files {
		if path == skip {
			continue
		}
		// THE FILE IS READ BEFORE IT IS ASKED, so a hold on a folder that has
		// nothing to do with this one is never touched at all.
		if held := readProgramHold(path, ""); held.dir == "" || !strictlyInside(held.dir, key) {
			continue
		}
		if hold, busy := programHoldAt(path, ""); busy {
			return hold, true
		}
	}
	return programHold{}, false
}

// programHoldOver answers the live hold on path or on a folder above it,
// leaving the hold file skip out. It asks by name — one file for each folder
// on the way up — so it is cheap enough to ask before every write a tool
// makes ([programHoldGuard]).
func programHoldOver(path, skip string) (programHold, bool) {
	for dir := filepath.Clean(path); ; {
		if file := programHoldFile(dir); file != skip {
			if hold, busy := programHoldAt(file, dir); busy {
				return hold, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return programHold{}, false
		}
		dir = parent
	}
}

// programHoldAt asks one hold file whether a run holds it, by taking it,
// shared, for the instant of asking: a run's hold is exclusive, so a shared
// one is refused exactly when a run has it. key is the folder the file is
// named for, when the asker knows it.
func programHoldAt(path, key string) (programHold, bool) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return programHold{}, false
	}
	defer file.Close()
	if err := filelock.Lock(file, false, true); err != nil {
		if !isLockHeld(err) {
			return programHold{}, false
		}
		return readProgramHold(path, key), true
	}
	_ = filelock.Unlock(file)
	return programHold{}, false
}

// readProgramHold is what a hold file says: whose run, and which folder. A
// file written before it said the folder is read by the run's record beside
// it ([programFolderRecord]), and failing that by key.
func readProgramHold(path, key string) programHold {
	body, _ := os.ReadFile(path)
	holder, dir, _ := strings.Cut(string(body), "\n")
	hold := programHold{dir: strings.TrimSpace(dir), holder: strings.TrimSpace(holder)}
	if hold.dir == "" {
		if record, ok := readProgramFolderAt(strings.TrimSuffix(path, ".lock") + ".json"); ok {
			hold.dir = record.key
		} else {
			hold.dir = key
		}
	}
	if hold.holder == "" {
		hold.holder = "another program run"
	}
	return hold
}

// programHoldFile is the file one folder's hold is taken on.
func programHoldFile(key string) string {
	return filepath.Join(home.Join("v3", programFolderDir), programFolderName(key)+".lock")
}

// programFolderBusy is the refusal for a folder a program's run cannot have
// because another run holds it, or holds a folder around it or inside it.
func programFolderBusy(dir string, hold programHold) string {
	return dir + " is busy: " + hold.holder + ", is working in " + hold.where(dir) +
		", and one folder takes one program run at a time; ask again when that run has ended"
}

// where is the held folder as a sentence about dir names it: "it" when it is
// dir, and the folder itself, with how the two stand, when it is not.
func (h programHold) where(dir string) string {
	switch {
	case h.dir == "" || h.dir == canonicalPath(dir):
		return "it"
	case strictlyInside(h.dir, canonicalPath(dir)):
		return h.dir + ", which is inside it"
	}
	return h.dir + ", which holds it"
}

// ── nothing else of codeaf's writes in a held folder ────────────────────────
//
// WITHOUT THE COPY, A PROGRAM WORKS IN THE PERSON'S LIVE FOLDER, and what
// anything else writes there meanwhile becomes the program's to act on. Once
// senior-dev has submitted it checks its frozen tree, and a tree that moved is
// put back — `checkout --force`, `reset`, `clean -fd` — which reverts a file
// the chat edited and deletes one it wrote, with no copy kept; an ordinary
// task merged into it mid-run is undone the same way while its row says it
// landed; and whatever survives is swept into the run's finishing commit as
// the program's work. So while a program holds a folder, from any conversation,
// window or shell:
//
//   - the chat's own file tools refuse a path inside it ([programHoldGuard]),
//     and reading stays open;
//   - an ordinary task — a proposal's card, a typed `/task`, a quick task, a
//     node starting on the session's own graph or on the run road — whose
//     folder is inside it, or holds it, is refused before it starts
//     ([programHoldRefusal]);
//   - a task that was already running lands beside it rather than into it: its
//     branch kept, its copy not laid, a `/land` refused.
//
// `bash` IS NOT FENCED, and neither is the person's own editor: a command's
// effects are whatever it did, and a guard that pattern-matched commands would
// promise what it cannot keep ([treeClaimGuard] says the same). The manual says
// so plainly (senior-dev.md), and that edits made there join the run's work.

// programHoldGuard refuses a write by one of codeaf's own file tools into a
// folder a program's run holds. It is a pre-action citizen for the reason
// every guard here is one: [Agent.executeTool] is the single door every call
// passes through (hooks.go).
//
// IT BINDS EVERY HAND THAT PUTS A FILE ON THE PERSON'S DISK AT A PATH THE CALL
// NAMES ([savingPath]): write and edit, edit_video's writing actions, and the
// generated picture, music, video and speech a path was given for. A
// generation that names no path lands in this session's own folders, which no
// program holds.
type programHoldGuard struct{ agent *Agent }

func (programHoldGuard) Name() string { return "program-hold" }

func (g programHoldGuard) PreAction(_ context.Context, _ *episode, _ *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	path, shown, ok := g.agent.savingPath(call)
	if !ok {
		return call, toolResult{}, true
	}
	hold, busy := programHoldOver(canonicalPath(path), "")
	if !busy {
		return call, toolResult{}, true
	}
	return call, toolResult{text: programHoldWriteRefusal(shown, hold), isError: true}, false
}

// programHoldWriteRefusal is what the model reads instead of a write into a
// held folder: the file, the folder, whose run holds it, that nothing was
// written, and the two things that work.
func programHoldWriteRefusal(shown string, hold programHold) string {
	return shown + " is in " + hold.dir + ", where " + hold.holder +
		", is working, so nothing was written; wait for that run to end, or stop it, then write there"
}

// savingPath is the absolute path one call is about to put a file at, and the
// same path as a person reads it, for every hand in [savingTools] whose call
// both writes ([producedAFile]) and names where. It resolves a relative path
// against the workspace the way [Agent.mutatingPath] does, which answers for
// the hands it knows.
func (a *Agent) savingPath(call ai.ToolCall) (string, string, bool) {
	if path, shown, ok := a.mutatingPath(call); ok {
		return path, shown, true
	}
	name := call.Function.Name
	if _, known := mutatingTools[name]; known || !producedAFile(name, call.Function.Arguments) {
		return "", "", false
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := decodeToolArguments(json.RawMessage(call.Function.Arguments), &args); err != nil {
		return "", "", false
	}
	path, workspace := strings.TrimSpace(args.Path), strings.TrimSpace(a.config.Workspace)
	if path == "" || (workspace == "" && !filepath.IsAbs(path)) {
		return "", "", false
	}
	if !filepath.IsAbs(path) {
		return filepath.Clean(filepath.Join(workspace, path)), filepath.ToSlash(filepath.Clean(path)), true
	}
	return filepath.Clean(path), filepath.Clean(path), true
}

// programHoldRefusal is why an ordinary task may not work in dir now — a
// program's run holds it, a folder around it, or a folder inside it — in one
// sentence naming the run and saying what to do; "" when nothing holds it.
func programHoldRefusal(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	hold, busy := programHoldNear(canonicalPath(dir), "")
	if !busy {
		return ""
	}
	return dir + " is busy: " + hold.holder + ", is working in " + hold.where(dir) +
		", and nothing else of codeaf's works there until that run has ended; wait for it, or stop it, then ask again"
}

// standHeldRefusal is [programHoldRefusal] for where a task would stand, and ""
// for a stand that only reads its ground: a REFERENCE works in a folder of its
// own and writes nothing where it looks.
func standHeldRefusal(stand taskStand, workspace string) string {
	if stand.mode == TaskModeReference {
		return ""
	}
	dir := stand.dir
	if strings.TrimSpace(dir) == "" {
		dir = workspace
	}
	return programHoldRefusal(dir)
}
