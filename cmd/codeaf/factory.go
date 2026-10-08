package main

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// factoryFixtureEnv is the one switch that puts a still fixture on the factory
// page in place of the person's own floor.
const factoryFixtureEnv = "CODEAF_FACTORY_FIXTURE"

// v3FactoryRoot is where the factory's items are kept: the v3 factory folder
// under the codeaf home, one file per item. The manual names it in words
// (internal/manual/chat/factory.md), because "where is it saved" is asked.
func v3FactoryRoot() string { return home.Join("v3", "factory") }

// v3Factory opens the factory store once for this process, or answers nil.
//
// NIL IS THE FACTORY OFF, and every caller reads it that way: no page floor, no
// `factory_add` on the belt. A folder that cannot be made is exactly that case
// — a capability that cannot work is absent, not broken — so the error is
// swallowed here rather than failing somebody's launch over a folder, and
// nothing is written to the screen, which the surface has not taken yet.
func v3Factory() *store.Store {
	st, err := store.Open(v3FactoryRoot())
	if err != nil {
		return nil
	}
	return st
}

// factoryDoor is the chat's door onto the same store, as the session takes it.
//
// A NIL STORE IS A NIL DOOR, SPELLED OUT. A typed-nil *store.Store inside the
// interface would read as non-nil to [session.Config]'s belt predicate and put
// `factory_add` on the belt with nothing behind it, so the nil is returned as
// the interface's own zero value and never as a wrapped pointer.
func factoryDoor(st *store.Store) session.FactoryDoor {
	if st == nil {
		return nil
	}
	return st
}

// factorySeam is the factory page's seam for this launch.
//
// THE PERSON'S OWN FLOOR IS THE ORDINARY ANSWER: [factory.LocalSeam] over the
// store this process opened, which holds the items made from chat and from `n`
// on the floor, and whose every engine door is nil, so the page offers no
// launch it cannot do. A nil store is the zero seam, whose page is the one dim
// line saying nothing is connected yet.
//
// Two switches stand in front of it, and both win over the store when they are
// on. The moving mock exists only in a -tags factorymock build with
// CODEAF_FACTORY_MOCK=1 (factorymock.go); CODEAF_FACTORY_FIXTURE=1 hands the
// page a still fixture ([factory.FixtureSeam]) whose every verb is nil.
func factorySeam(st *store.Store) factory.Seam {
	// The mock is asked first. The default build has a stub that always
	// answers false, so the shipped binary carries none of it
	// (internal/factory/mock/REMOVING.md).
	if seam, ok := factoryMockSeam(); ok {
		return seam
	}
	if strings.TrimSpace(env.Get(factoryFixtureEnv)) == "1" {
		return factory.FixtureSeam(time.Now())
	}
	if st == nil {
		return factory.Seam{}
	}
	return factory.LocalSeam(st, time.Now())
}

// factoryHere names the repository new work lands on when the floor itself
// cannot: a floor with no items has no repos yet, so the page's `n` asks for
// one with nothing to offer, and the store rightly refuses work on no
// repository. The answer is this window's workspace, by its folder's name,
// which is the same name `factory_add` is told to use when the person names
// none (internal/session's tools_factory.go). A floor that already has repos
// passes its own, and a seam with no `new` door is handed back untouched.
func factoryHere(seam factory.Seam, workspace string) factory.Seam {
	here := strings.TrimSpace(workspace)
	if seam.New == nil || here == "" {
		return seam
	}
	here = filepath.Base(filepath.Clean(here))
	if here == "." || here == string(filepath.Separator) {
		return seam
	}
	made := seam.New
	seam.New = func(repo, words string) (int, error) {
		if strings.TrimSpace(repo) == "" {
			repo = here
		}
		return made(repo, words)
	}
	return seam
}
