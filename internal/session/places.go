package session

// WHERE A CONVERSATION IS ABOUT.
//
// place.go answers "where does this conversation keep things" and taskstands.go
// answers "where does this piece of work stand". This file answers the third
// question, which sat between them with nobody holding it: WHICH FOLDERS IS THIS
// CONVERSATION ABOUT?
//
// People open aforge in general — one window, often from a home directory — and
// then work on things that live somewhere else. The window is where you sat
// down; the conversation is about places. There are two kinds and they are not
// the same thing:
//
//   - THE STANDING PLACE is [Config.Workspace]: the tools' cwd, the prompt's
//     working directory, the gate, the project config. It is captured at launch,
//     it is one, and nothing here moves it.
//   - REFERRED PLACES are the small ordered set below — the directories this
//     conversation has turned out to be about. They accrue from evidence and
//     never from configuration, and they are remembered on the session's own
//     meta.json so that a conversation reopened tomorrow still knows them.
//
// ── HOW ONE ARRIVES ──
//
// Two roads, and [PlaceArrival] is the whole of the difference between them:
//
//  1. THE PERSON NAMES IT ([Agent.ReferPlace] — the picker, a folder dropped on
//     the window, `/attach` on a directory). Somebody's own word, and it never
//     silently expires.
//  2. THE WORK RESOLVES IT ([Agent.keepGround]). When the ground ladder answers
//     at the TOUCHED rung, or the person settles the two-roots question and the
//     next proposal comes back naming their answer, that answer is written down
//     here — so the ladder's next climb finds it at SAID and NOBODY IS ASKED THE
//     SAME QUESTION TWICE. This is the whole anti-chore mechanism: you never
//     link anything; the conversation notices and remembers.
//
// ── AND IT IS EVIDENCE, NEVER A SECOND RESOLVER ──
//
// A referred place is a SAID answer fed into the one ladder taskstands.go owns
// ([Agent.groundFromPlaces]), and that is a ruling and not an implementation
// detail: [Agent.resolveTaskGround] stays the only place the ground is decided,
// so a task's ground is answered once, by one reading, whatever put the evidence
// in front of it.
//
// Nothing here widens where work may WRITE. One writable ground per node is
// taskoutside.go's law and it is untouched: referring a place tells the ladder
// what the conversation is about, and what the guard allows is still decided by
// the one ground a node stands on.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PlaceArrival is HOW a referred place got onto the conversation, and the only
// thing that turns on it is whether the place may go stale.
type PlaceArrival string

const (
	// PlaceSaid is the person's own act: the picker, a path in their words, a
	// folder dropped on the window, `/attach` on a directory. A place somebody
	// NAMED never silently expires — said is never overruled, which is the law
	// taskstands.go already keeps about the rung it feeds.
	PlaceSaid PlaceArrival = "said"
	// PlaceKept is a ground the ladder resolved and this conversation wrote
	// down. It is a cache of an answer rather than an instruction, so it decays
	// by recency in the ranking below — though its record stays on the meta as
	// history, because what the conversation was about last week is not a lie.
	PlaceKept PlaceArrival = "kept"
)

// PlaceRef is one referred place: where it is, how it got here, when it was
// last referred to, what the person said about working in it, and the one thing
// the machine learned cheaply on the way in.
//
// IT IS SMALL AND IT IS A CITATION. Everything in it is either the person's own
// act or recoverable by looking at the disk again, which is why meta.json can
// carry it (place.go's [Meta]) and why a field nobody wrote is simply absent.
type PlaceRef struct {
	// Path is the absolute, canonical directory — the repository ROOT when the
	// place is inside one, because a ground is cut from a repository and not
	// from a directory inside it ([groundRoot] makes that same snap).
	Path string `json:"path"`
	// Arrival is which of the two roads above brought it.
	Arrival PlaceArrival `json:"arrival"`
	// Referred is when the conversation last named or resolved this place. It
	// is what "lately" means in the ranking, and the set is held newest first
	// so that the answer is usually the head.
	Referred time.Time `json:"referred,omitempty"`
	// Mode is the person's own word about how work happens HERE — today only
	// "in place", meaning the work happens in the folder itself with nothing
	// isolating it ([TaskModeInPlace]). It is NEVER GUESSED: absent means the
	// mode falls out of the deliverable, exactly as [groundMode] decides it for
	// every other ground.
	Mode string `json:"mode,omitempty"`
	// Chose is the directory THE PERSON ACTUALLY POINTED AT, and it is set only
	// when the snap above moved it — so it is empty for every place chosen at its
	// own root, which is most of them.
	//
	// THE SNAP IS REAL AND MUST NOT BE SILENT. A branch is cut from a repository
	// and not from a directory inside it, so a person who picks
	// `…/internal/session` gains the project; but a record that kept only the
	// project would have quietly widened what they said, and neither the surface
	// nor the model could tell them what actually happened. This is the one field
	// that keeps the two honest with each other.
	Chose string `json:"chose,omitempty"`
	// Repository records whether git knew this directory when it was referred.
	// It is the cheap fact a picker draws and a caller reads without paying for
	// a `rev-parse` per row; the ladder asks git itself where it must be right.
	Repository bool `json:"repository,omitempty"`
}

// placesRemembered is how many referred places one conversation's meta.json
// carries.
//
// It is a bound and not a policy: places dedupe by path, so the set grows by
// DISTINCT FOLDER and a conversation that touched sixteen of them has said
// something unusual. What is dropped when the bound is reached is the oldest
// KEPT place — a resolved answer nobody has come back to — and a place the
// person named is dropped only when there is nothing else left to drop.
const placesRemembered = 16

// Places is the set this conversation is about, newest first. A surface draws
// it; nothing here changes when it is read.
func (a *Agent) Places() []PlaceRef { return a.referredPlaces() }

// ReferPlace is THE DOOR A SURFACE CALLS when a person names a folder — the
// picker, a directory dropped on the window, `/attach` on one, a path they
// typed. There is exactly one so that every road in leaves the same record.
//
// It validates against the disk rather than trusting the caller: a place is a
// directory that IS THERE, because the whole value of the set is that the
// ladder can hand a ground to work without stopping to wonder. A relative path
// or a `~` is read against the conversation's own workspace, which is where the
// person is standing when they say it.
//
// THE PATH IS SNAPPED TO THE REPOSITORY ROOT when it sits inside one, for
// [groundRoot]'s reason: a branch is cut from a repository and not from a
// directory inside it, so a person pointing at `…/internal/session` and a person
// pointing at the project itself must not become two different places.
func (a *Agent) ReferPlace(path string, arrival PlaceArrival) (PlaceRef, error) {
	dir, err := a.placePath(path)
	if err != nil {
		return PlaceRef{}, err
	}
	if arrival != PlaceSaid && arrival != PlaceKept {
		return PlaceRef{}, fmt.Errorf("a place arrives said or kept, not %q", arrival)
	}
	root, repository := repositoryRoot(dir)
	chose := ""
	if repository && root != dir {
		// WHAT THEY POINTED AT IS KEPT BESIDE WHAT THEY GAINED, for
		// [PlaceRef.Chose]'s stated reason.
		chose, dir = dir, root
	}
	ref := PlaceRef{Path: dir, Chose: chose, Arrival: arrival, Referred: time.Now(), Repository: repository}
	a.refer(ref)
	return ref, nil
}

// RemovePlace takes one folder back off the conversation: off the set, off
// meta.json, and out of what the next request tells the model
// (placescontext.go).
//
// IT IS THE OTHER HALF OF [Agent.ReferPlace] AND THE SURFACE'S ONE DOOR OUT.
// A folder indicator a person can see and cannot dismiss is a mistake they have
// to open a new conversation to correct, and a conversation still carrying a
// folder somebody removed from their screen would be the surface and the model
// disagreeing about what this is about.
//
// IT WORKS ON A FOLDER THAT IS NO LONGER THERE. The set is a history and keeps a
// record whose directory has been deleted (see [loadPlaces]), so a remove that
// insisted on stat'ing first would leave exactly those records unremovable — the
// path is read the ordinary way when the disk can answer, and taken as written
// when it cannot.
//
// A FOLDER THIS CONVERSATION IS NOT ABOUT IS REFUSED RATHER THAN IGNORED, which
// is [Agent.SetPlaceMode]'s reading: a caller told "done" about a path that was
// never on the set has been told something false about which folders are
// attached.
func (a *Agent) RemovePlace(path string) error {
	want := strings.TrimSpace(path)
	if want == "" {
		return fmt.Errorf("a place is a folder · this one has no path")
	}
	if dir, err := a.placePath(want); err == nil {
		if root, ok := repositoryRoot(dir); ok {
			dir = root
		}
		want = dir
	} else if filepath.IsAbs(want) {
		want = canonicalPath(filepath.Clean(want))
	} else {
		// A relative path this process cannot resolve names nothing at all, and
		// the refusal [Agent.placePath] already wrote is the true one.
		return err
	}
	// THE SET IS REPLACED AND NEVER EDITED IN PLACE, for [Agent.SetPlaceMode]'s
	// stated reason: a stamp hands the live slice to the marshaller.
	a.mu.Lock()
	kept := make([]PlaceRef, 0, len(a.places))
	for _, place := range a.places {
		if place.Path != want {
			kept = append(kept, place)
		}
	}
	found := len(kept) != len(a.places)
	if found {
		a.places = kept
	}
	a.mu.Unlock()
	if !found {
		return fmt.Errorf("this conversation is not about %s", want)
	}
	a.stampPlaces()
	a.keepAttached()
	return nil
}

// SetPlaceMode records the person's own word about how work happens in one
// place — "here", "directly", "in place", which all mean the same thing: the
// work happens in that folder itself rather than in a copy of it.
//
// IT IS SET AND NEVER GUESSED, and it is cleared by the same door with an empty
// word, because a person who said "in place" once and changed their mind has to
// have a way back. A place this conversation does not refer to is refused
// rather than invented: the mode is a fact ABOUT a place, so there has to be
// one.
func (a *Agent) SetPlaceMode(path, word string) error {
	dir, err := a.placePath(path)
	if err != nil {
		return err
	}
	if root, ok := repositoryRoot(dir); ok {
		dir = root
	}
	mode, ok := placeModeWord(word)
	if !ok {
		return fmt.Errorf("%q is not a way of working in a place · say \"in place\"", word)
	}
	// THE SET IS REPLACED AND NEVER EDITED IN PLACE. A stamp hands the live slice
	// to the marshaller and writes the file after the lock is released
	// (placemeta.go), so a field changed under an older reader's feet would be one
	// hand writing what another is reading.
	a.mu.Lock()
	found := false
	places := make([]PlaceRef, len(a.places))
	copy(places, a.places)
	for index := range places {
		if places[index].Path != dir {
			continue
		}
		found, places[index].Mode = true, mode
		break
	}
	if found {
		a.places = places
	}
	a.mu.Unlock()
	if !found {
		return fmt.Errorf("this conversation is not about %s", dir)
	}
	a.stampPlaces()
	return nil
}

// placeModeWord reads the person's word for working in the folder itself. The
// answer is [TaskModeInPlace]'s own spelling rather than a second vocabulary,
// so the mode a place carries and the mode a task stands in are one word.
func placeModeWord(word string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "":
		return "", true
	case "here", "directly", "in place":
		return string(TaskModeInPlace), true
	}
	return "", false
}

// placePath turns what somebody typed into the absolute directory it names, or
// says plainly why it is not one.
//
// THE EXPANSION IS [resolveTaskWhere]'S, not a second reading of `~` and of a
// relative path against the workspace — that function is what the `where`
// argument already goes through, and it owns the refusal for a path that names
// a file. What is added here is EXISTENCE: `where` may name a folder still to be
// made, because somebody saying where work should go is not making a claim about
// the disk, while a place is somewhere the conversation is ALREADY about.
func (a *Agent) placePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("a place is a folder · this one has no path")
	}
	dir, err := resolveTaskWhere(path, canonicalPath(strings.TrimSpace(a.config.Workspace)))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s is not there", dir)
		}
		return "", err
	}
	return canonicalPath(dir), nil
}

// keepGround writes a ground the ladder just resolved back onto the
// conversation, and it is the second half of the anti-chore mechanism this file
// exists for: the person is asked which project the work is about AT MOST ONCE.
//
// IT RUNS WHERE THE STAND IS CONSUMED AND NEVER INSIDE THE LADDER. Resolving a
// ground is a reading and must stay one — a rung with a side effect would write
// down an answer that a refusal or a question later threw away, and the two
// doors that ask ([Agent.proposeTask]) and the one that cannot ([Agent.
// taskGroundOrStandingIn]) would each be keeping a different thing.
//
// TWO RUNGS ARE KEPT AND THE OTHERS ARE NOT. SAID is the answer somebody gave —
// the model's own `ground`, or the person's answer to the two-roots question
// arriving as the next proposal's argument. TOUCHED is the reading of the
// evidence that this file is meant to stop repeating. STANDING IN and NOTHING
// are the workspace itself, which is not a referred place but the place the
// conversation is standing in, and writing it down here would put the window's
// own folder in a set that exists to name everywhere else.
func (a *Agent) keepGround(stand taskStand) {
	if stand.dir == "" || stand.ask != "" || stand.refusal != "" {
		return
	}
	if stand.rung != taskGroundSaid && stand.rung != taskGroundTouched {
		return
	}
	if stand.dir == canonicalPath(strings.TrimSpace(a.config.Workspace)) {
		return
	}
	a.refer(PlaceRef{Path: stand.dir, Arrival: PlaceKept, Referred: time.Now()})
}

// refer folds one place into the set and writes the file when — and only when —
// something about the set actually changed.
//
// A PLACE ALREADY AT THE HEAD COSTS NOTHING. A ground is resolved once per
// proposal, and a conversation working steadily in one project would otherwise
// pay a read and a rename of meta.json for an answer identical to the one
// already on disk. The recency stamp alone is not a change worth a write: the
// set is held newest first, so a place that is already the head is already the
// most recent thing in it.
//
// AND AN ARRIVAL ONLY EVER GOES UP. A place the conversation kept and the person
// then named becomes theirs and stops decaying; a place they named is never
// quietly demoted to a resolved answer by the next proposal that lands on it.
func (a *Agent) refer(ref PlaceRef) {
	ref.Path = strings.TrimSpace(ref.Path)
	if ref.Path == "" {
		return
	}
	if ref.Referred.IsZero() {
		ref.Referred = time.Now()
	}
	known, settled := a.knownPlace(ref.Path)
	// AND WHAT THE PERSON POINTED AT SURVIVES A GROUND LANDING ON THE SAME PLACE,
	// but is never carried over a person's own act. A resolved ground makes no
	// claim about what anybody pointed at, so it inherits the record's; somebody
	// choosing the project itself is SAYING they pointed at the root, and
	// resurrecting last week's subdirectory under them would be this file
	// remembering an intention they have just replaced. IT IS SETTLED BEFORE THE
	// EARLY RETURN BELOW, because a person re-picking the same project at a
	// different depth has something new to say about a place already at the head.
	if ref.Arrival == PlaceKept && ref.Chose == "" {
		ref.Chose = known.Chose
	}
	// The head, with nothing new to say about it. An arrival only ever goes up,
	// so a ground resolved onto a place the person already named is a place the
	// set already reads correctly.
	if settled && (known.Arrival == ref.Arrival || ref.Arrival == PlaceKept) && known.Chose == ref.Chose {
		return
	}
	if ref.Mode == "" {
		ref.Mode = known.Mode
	}
	if known.Arrival == PlaceSaid {
		ref.Arrival = PlaceSaid
	}
	// WHAT THE MACHINE LEARNED, asked once per place and not once per proposal:
	// the record above already carries it for a place the conversation has, and
	// git is only asked about one it has never seen. It is asked OUTSIDE the lock
	// because it is a subprocess, and the turn this proposal belongs to is
	// waiting on that lock.
	if !ref.Repository {
		if known.Path != "" {
			ref.Repository = known.Repository
		} else {
			_, ref.Repository = repositoryRoot(ref.Path)
		}
	}
	a.mu.Lock()
	kept := make([]PlaceRef, 0, len(a.places)+1)
	for _, place := range a.places {
		if place.Path != ref.Path {
			kept = append(kept, place)
		}
	}
	a.places = trimPlaces(append([]PlaceRef{ref}, kept...))
	a.mu.Unlock()
	a.stampPlaces()
	// AND THE MODEL IS TOLD, which is the whole difference between a folder this
	// process remembers and a folder this conversation is about. The block is
	// composed from the SAID rows alone and compares itself before it writes, so
	// a ground the ladder resolved reaches this line and changes nothing
	// (placescontext.go).
	a.keepAttached()
}

// knownPlace is what the set already holds about one path, and whether it is
// sitting at the head of it — which is the one shape that may cost nothing at
// all. A path the conversation has never referred to answers a zero [PlaceRef].
func (a *Agent) knownPlace(path string) (PlaceRef, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for index, place := range a.places {
		if place.Path == path {
			return place, index == 0
		}
	}
	return PlaceRef{}, false
}

// trimPlaces holds the set to [placesRemembered], dropping the oldest KEPT
// place first for the reason that constant states.
func trimPlaces(places []PlaceRef) []PlaceRef {
	for len(places) > placesRemembered {
		drop := -1
		for index := len(places) - 1; index > 0; index-- {
			if places[index].Arrival == PlaceKept {
				drop = index
				break
			}
		}
		if drop < 0 {
			drop = len(places) - 1
		}
		places = append(places[:drop], places[drop+1:]...)
	}
	return places
}

// referredPlaces is a copy of the set, so a caller weighing it — the ladder,
// most of all — never holds the agent's lock while it stats the disk or asks
// git anything.
func (a *Agent) referredPlaces() []PlaceRef {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.places) == 0 {
		return nil
	}
	out := make([]PlaceRef, len(a.places))
	copy(out, a.places)
	return out
}

// loadPlaces reads a session folder's referred places at open.
//
// A MISSING FIELD IS A CONVERSATION WITH NO PLACES, which is every conversation
// written before this existed and every one that has not accrued one yet — so
// absence is answered with nothing and never with an error, exactly as
// [LoadMeta] answers a missing file. A record naming a folder that is no longer
// there is kept as it stands: the ladder stats before it weighs anything
// ([Agent.groundFromPlaces]), and a set that quietly rewrote itself every time a
// disk was unplugged would be a history that forgets.
func loadPlaces(dir string) []PlaceRef {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		return nil
	}
	return metaPlaces(meta)
}

// metaPlaces is one meta.json's referred places, read the same way for the
// conversation that owns them and for a surface that is merely LOOKING at the
// conversation ([readSessionRow], world.go). It is one function because a home
// row drawing a different set from the one the ladder weighs would be the
// screen and the work disagreeing about what a conversation is about.
func metaPlaces(meta Meta) []PlaceRef {
	var out []PlaceRef
	for _, place := range meta.Places {
		if path := strings.TrimSpace(place.Path); path != "" && filepath.IsAbs(path) {
			place.Path = path
			out = append(out, place)
		}
	}
	return trimPlaces(out)
}
