package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/jsrun"
	"github.com/Agent-Field/aforge-v2/internal/substore"
)

// THE ONE PLACE A BUNDLE ON DISK BECOMES SOMETHING THAT RUNS.
//
// internal/substore is the layout — the directories, the version records, the
// memory and the run notes — and it says of itself that it does not know what
// JavaScript is. internal/jsrun is the engine, and it says of itself that it has
// no filesystem. The two are deliberately unacquainted, and [substore.Build] is
// the signature where they meet: a store hands over a [substore.Bundle], and
// what comes back is an [exec.Runner] the registry cannot tell from a compiled-in
// Go one. This file is that function and nothing else, so a change to either
// side's shape is a change to one adapter rather than to every surface.

// subharnessBuild is the builder both surfaces hand to [substore.Store.Source].
//
// The look is the surface's, because it is the only half of a bundle that is a
// fact about WHERE THE PROGRAM IS RUNNING rather than about the program: which
// directory a file guard is asking about, and which belt a tool guard is asking
// about. Everything else comes off the disk.
func subharnessBuild(look jsrun.Look) substore.Build {
	return func(bundle substore.Bundle) (exec.Runner, error) {
		built := jsrun.Bundle{
			Manifest: bundle.Manifest,
			Program:  string(bundle.Program),
			// The name a compile error is reported against, and it is the FULL
			// PATH rather than "program.js" for one reason: a person reading
			// `weekly-brief v3 program.js:12:4` still has to go and find which
			// weekly-brief. The file's own name comes from the store's constant,
			// never retyped here, because the layout is that package's to state.
			Source:  filepath.Join(bundle.Dir, substore.ProgramFile),
			Prompts: subharnessPrompts(bundle.Prompts),
			Look:    look,
			// FUEL IS LEFT AT ITS ZERO VALUE ON PURPOSE. jsrun applies its own
			// step ceiling and takes the wall clock from the manifest's own cost
			// shape, which is the one spelling of a budget in this wave — a
			// number restated here would be the one that drifts.
		}
		// A nil *substore.Memory would satisfy the interface and panic at the
		// first remember(), so the door is only opened when the store actually
		// opened it. jsrun states what an absent one means: remember() and
		// recall() fall through to the Env's own doors, which is the honest
		// fallback rather than a silent no-op.
		if bundle.Memory != nil {
			built.Memory = bundle.Memory
		}
		runner, err := jsrun.New(built)
		if err != nil {
			// Returned as a NIL INTERFACE and not as a nil *jsrun.Runner inside a
			// live one, because [substore.Store.Source] checks the runner for nil
			// as well as the error, and a typed nil would sail past that check
			// and be dispatched to.
			return nil, err
		}
		return runner, nil
	}
}

// subharnessPrompts is the store's prompt map in the runtime's spelling.
//
// THE TWO PACKAGES KEY THE SAME ASSET DIFFERENTLY AND BOTH ARE RIGHT. The store
// keeps what is on disk, so its key is the FILE NAME — `summarise.md`. The
// runtime is looked up by what an ai() call site wrote, so its key is the BARE
// NAME — `summarise` — and its own resolver strips a `prompts/` prefix and a
// `.md` suffix off a ref before it looks. Handing the file names straight over
// would put every asset one suffix away from every lookup, and a program would be
// told the prompt it shipped with does not exist. Every file the store reads ends
// in `.md` (its readPrompts takes no other), so nothing can collide here.
func subharnessPrompts(prompts map[string][]byte) map[string]string {
	if len(prompts) == 0 {
		return nil
	}
	text := make(map[string]string, len(prompts))
	for name, body := range prompts {
		text[strings.TrimSuffix(name, ".md")] = string(body)
	}
	return text
}

// bundleLook is the free look at the world a guard takes before a run: is this
// file here, is this tool on the belt. Both are cheap by law — no model call, no
// tool call, no spend — which is what keeps a guard a guard rather than the first
// step of the work.
type bundleLook struct {
	// workspace is the directory a relative path in a guard is about. It is the
	// surface's working directory rather than the bundle's own, because a guard
	// asking "is there a package.json" is asking about the PERSON'S project and
	// never about the directory the program happens to be stored in.
	workspace string
	// belt answers whether a tool is really available here. Nil answers false —
	// see [beltWatch] for why that is the safe direction.
	belt func(name string) bool
}

// FileExists says whether the path a file guard names is there.
//
// A DIRECTORY ANSWERS YES, because the guard's question is whether the path is
// there and not what kind of thing it is. An absolute path is taken as written;
// anything else is resolved against the workspace, which is the only reading that
// makes a guard portable between one person's checkout and another's.
func (l bundleLook) FileExists(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		if l.workspace == "" {
			// Nowhere to resolve it against is "I could not tell", and jsrun
			// states what that has to mean: a guard that cannot be checked does
			// not pass.
			return false
		}
		path = filepath.Join(l.workspace, path)
	}
	_, err := os.Stat(path)
	return err == nil
}

// ToolOnBelt says whether the tool a tool guard names is really available.
func (l bundleLook) ToolOnBelt(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || l.belt == nil {
		return false
	}
	return l.belt(name)
}

// beltWatch is the one indirection between a registry and a belt that does not
// exist yet.
//
// THE ORDER IS THE PROBLEM AND IT CANNOT BE REORDERED AWAY. A surface builds its
// registry BEFORE it builds the thing that holds the belt — the chat surface's
// belt belongs to a [session.Agent] that is constructed out of the very config
// the registry is being put into — so a Look built at registry time has nothing
// to ask. Threading the belt in later through the bundles would mean rebuilding
// every runner; this is one pointer the Look closes over instead, filled the
// moment the belt exists.
//
// AN UNFILLED WATCH ANSWERS FALSE, AND FALSE IS THE SAFE DIRECTION. jsrun states
// the law on the Look door itself: a guard that cannot be checked does not pass,
// because the safe answer to "I could not tell" is the long way and never the
// fast path. So a run that happens before the belt is attached falls back and
// gets its work done by the generalist — slower, and never wrong.
//
// It locks because the two sides sit on different goroutines: the launch fills
// it, and a task node's run reads it.
type beltWatch struct {
	mutex sync.RWMutex
	ask   func(name string) bool
}

// watch attaches the belt. A nil receiver is a surface that wired no watch at
// all, which is not an error — it is a surface whose tool guards take the long
// way, for as long as that is true.
func (w *beltWatch) watch(ask func(name string) bool) {
	if w == nil {
		return
	}
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.ask = ask
}

// on is what [bundleLook.ToolOnBelt] is pointed at. It is a method value so the
// Look can be built before anything is watching.
func (w *beltWatch) on(name string) bool {
	if w == nil {
		return false
	}
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	if w.ask == nil {
		return false
	}
	return w.ask(name)
}

// subharnessRunRecorder is the ONE PLACE A RUN NOTE IS WRITTEN, whichever door
// the run came through. The conversation and the headless command record the
// same facts about the same names into the same store, because "when did this
// last run" is a question about the machine and not about which surface asked.
//
// THE VERSION IS READ AT THE MOMENT OF WRITING and its error is dropped on
// purpose: a Go-native program compiled into this binary has no bundle on disk
// and therefore no head, and zero is the honest answer for one rather than a
// reason to record nothing at all.
//
// A note that cannot be written is dropped in silence. The run already happened
// and nobody is waiting on this sentence; the worst a full disk costs is a row
// that draws nothing, which is exactly what a row with no history draws anyway.
func subharnessRunRecorder(store *substore.Store) func(name string, note substore.RunNote) {
	if store == nil {
		return nil
	}
	return func(name string, note substore.RunNote) {
		version, _ := store.Head(name)
		note.Version = version
		_ = store.RecordRun(name, note)
	}
}

// toolboxBelt answers a tool guard from exec's own toolbox — the bare set a
// surface with no session belt has. It reads the toolbox's own definitions rather
// than a list written here, so a tool that arrives in or leaves internal/exec
// changes this answer without changing this file, which is the same reading
// [headlessEnv.present] takes of the same object a moment later.
func toolboxBelt(tools *exec.Toolbox) func(string) bool {
	if tools == nil {
		return nil
	}
	return func(name string) bool {
		for _, definition := range tools.Definitions() {
			if definition.Function.Name == name {
				return true
			}
		}
		return false
	}
}
