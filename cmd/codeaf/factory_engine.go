package main

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// engineFactoryHere says whether this hello is a person's window on this
// machine, which is the one window the factory door is handed to. The local
// dial is the only caller that fills [remote.Hello.Launch] (chatv3_local.go's
// [v3LaunchShape]), and it marks the shape interactive for a screen and not for
// a --once probe; every remote surface sends nil.
func engineFactoryHere(hello remote.Hello) bool {
	return !hello.Headless && hello.Launch != nil && hello.Launch.Interactive
}

// factoryConfigured says this machine has a factory set up: it is attached to
// one, serves one, or watches a repository on its own floor. A folder that
// holds none of these is a machine that has never used the factory, and the
// engine leaves its belt, its prompt and its processes alone
// ([bootEngine]). It reads the folder and writes nothing.
func factoryConfigured() bool {
	root := v3FactoryRoot()
	for _, name := range []string{"attach.json", "serve.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return true
		}
	}
	if _, err := os.Stat(root); err != nil {
		return false
	}
	st, err := store.Open(root)
	if err != nil {
		return false
	}
	repos, err := st.Repos()
	return err == nil && len(repos) > 0
}

// engineFactoryStore is the factory store this engine process opened, once,
// for every conversation it serves. NIL IS THE FACTORY OFF ([v3Factory]); it is
// opened on the first hello that asks for it and never for a process that only
// ever serves remote windows.
var engineFactoryStore struct {
	once  sync.Once
	store *store.Store
}

// factoryBackgroundHere says this process may start the factory's own
// background work (the worker loop, the GitHub poll, triage, and a service of
// its own) just because a window opened the floor. THE NATIVE APP NEVER DOES:
// it serves its factory only when a person asks (factory.local_start, or the
// quiet resume of that choice) and starts a runner only on an explicit
// factory.local_run (appfactory_setup.go). [config.AppMode] is the app
// process's own marker, installed before anything else in runApp.
func factoryBackgroundHere() bool { return !config.AppMode() }

func engineFactory(profileDir string) *store.Store {
	engineFactoryStore.once.Do(func() {
		engineFactoryStore.store = v3Factory()
		if !factoryBackgroundHere() {
			return
		}
		// The engine polls the floor it opened for a person's window, and so
		// does that window: a window never counts on the engine, which may be
		// an older build with no poll. The two take turns through the floor's
		// locks ([startFactoryPoll]).
		startFactoryPoll(engineFactoryStore.store, profileDir)
	})
	return engineFactoryStore.store
}
