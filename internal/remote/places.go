package remote

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE ENGINE HALF OF THE THREE LATE PLACES, AND THE SURFACE HALF ──────────
//
// wire_places.go says what these doors are and why they are additive; this is
// what answers them and what asks them. They are in a file of their own rather
// than in server.go's switch and client.go's run of methods for one reason that
// is about people and not about code: every lane of the host-parity wave edits
// those two files, and nine methods threaded through both of them is nine
// conflicts for whoever merges.
//
// EVERY DOOR HERE REFUSES RATHER THAN INVENTS. A nil closure is a capability
// this engine does not have — memory turned off, a build with no index, a world
// with no ledger — and it is answered as an error so that the surface can tell
// "there is nothing there" from "nobody asked". That is [Engine.World]'s own
// law, restated once here and not per method.

// engineOffWord leads every refusal in this file. It names the MACHINE and not
// the connection, because that is the fact the surface turns into a sentence: a
// person reading `memory is off on that machine` has learned something true
// about the far end, where `memory is off for this session` would have been a
// claim about a setting nobody consulted (internal/tui3's host.go).
const engineOffWord = "engine: "

// placesCall answers the three late places' methods, and says whether the method
// was one of them at all. A false hands the call back to server.go's refusal,
// which is what an engine that predates these doors answers for all nine.
func (s *server) placesCall(call Frame) (json.RawMessage, bool, error) {
	sess := s.session
	sess.mu.Lock()
	engine := sess.engine
	sess.mu.Unlock()

	switch call.Method {
	case MethodPlacesArchive:
		args, err := arg[ArchiveArgs](call)
		if err != nil {
			return nil, true, err
		}
		root, dir := filepath.Clean(engine.PlacesRoot), filepath.Clean(args.Dir)
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, true, errors.New(engineOffWord + "that conversation is outside this machine's places")
		}
		if engine.Archive == nil {
			return nil, true, errors.New(engineOffWord + "this engine cannot put conversations away")
		}
		return nil, true, engine.Archive(dir, args.Archived)
	case MethodPlacesLedger:
		args, err := arg[LedgerArgs](call)
		if err != nil {
			return nil, true, err
		}
		if engine.Ledger == nil {
			return nil, true, errors.New(engineOffWord + "this engine cannot read what it has spent")
		}
		payload, err := json.Marshal(engine.Ledger(args.Since))
		return payload, true, err

	case MethodPlacesRefer:
		args, err := arg[ReferArgs](call)
		if err != nil {
			return nil, true, err
		}
		door, ok := sess.current().(placeKeeper)
		if !ok {
			return nil, true, errors.New(engineOffWord + foldersOffWord)
		}
		arrival := args.Arrival
		if arrival == "" {
			// A CALLER WHO NAMED NO ROAD NAMED THE PERSON'S. Every surface that
			// reaches this door does so because somebody chose a folder, and the
			// other road — a ground the work resolved — is written from inside the
			// engine and never crosses a wire.
			arrival = session.PlaceSaid
		}
		ref, err := door.ReferPlace(args.Path, arrival)
		if err != nil {
			return nil, true, err
		}
		// AND EVERY SURFACE IS TOLD, not only the one that chose. The set rides
		// the fact push (wire_places.go says why it is not a reading of its own),
		// so this is what puts the new folder in front of the other window on this
		// conversation — and in front of THIS one, over the top of whatever it
		// assumed while the call was in flight.
		s.session.announce()
		payload, err := json.Marshal(ref)
		return payload, true, err

	case MethodPlacesRemove:
		path, err := arg[string](call)
		if err != nil {
			return nil, true, err
		}
		door, ok := sess.current().(placeKeeper)
		if !ok {
			return nil, true, errors.New(engineOffWord + foldersOffWord)
		}
		if err := door.RemovePlace(path); err != nil {
			return nil, true, err
		}
		s.session.announce()
		return nil, true, nil

	case MethodPlacesSearch:
		args, err := arg[SearchArgs](call)
		if err != nil {
			return nil, true, err
		}
		if engine.Search == nil {
			return nil, true, errors.New(engineOffWord + "this engine is not keeping what was said")
		}
		hits, err := engine.Search(args.Terms, args.Limit)
		if err != nil {
			return nil, true, err
		}
		payload, err := json.Marshal(hits)
		return payload, true, err

	case MethodMemorySnapshot, MethodMemoryChanged, MethodMemoryList,
		MethodMemoryUpdate, MethodMemoryForget, MethodMemoryRestore,
		MethodMemoryProvenance:
		if engine.Memory == nil {
			// ONE REFUSAL FOR ALL SEVEN, because the store is one thing: an engine
			// whose memory row is off has no snapshot to answer AND no line to
			// forget, and seven different sentences about one absence would be
			// seven chances for two screens to say it differently.
			return nil, true, errors.New(engineOffWord + memoryOffWord)
		}
		payload, err := memoryCall(engine.Memory, call)
		return payload, true, err
	}
	return nil, false, nil
}

// placeKeeper is the slice of an engine's agent that remembers which folders a
// conversation is about (internal/session's places.go).
//
// IT IS ASSERTED RATHER THAN REQUIRED OF [WrappedAgent], on tasklane.go's terms:
// a scripted engine in a test and any shape of agent that keeps no places
// honestly lack it, and a method on the interface would make each of them a
// compile error for a capability they have no answer to. *session.Agent
// satisfies it, which is the case that matters — cmd/aforge's engine.go pins
// that by construction.
type placeKeeper interface {
	ReferPlace(path string, arrival session.PlaceArrival) (session.PlaceRef, error)
	Places() []session.PlaceRef
	RemovePlace(path string) error
}

// keepsFolders is whether one engine's agent can hold a folder at all, and it is
// the ONE reading of that question: [Welcome.Folders] is filled from it and the
// two doors above refuse on it, so a surface cannot be told yes at the door and
// refused on the call.
func keepsFolders(agent any) bool {
	_, ok := agent.(placeKeeper)
	return ok
}

// foldersOffWord is an engine that cannot hold a folder at all. It is the third
// refusal in this file and it is said in the machine's own terms for
// [engineOffWord]'s reason.
const foldersOffWord = "this engine cannot keep the folders a conversation is about"

// memoryOffWord is the far machine's memory row, off. It is spelled here and
// matched on the surface ([Client.MemoryOff]) because the surface has to turn it
// into a sentence of its own — `memory is off on that machine` — rather than
// draw an engine's error text, which is machinery vocabulary.
const memoryOffWord = "memory is off on this machine"

// MemoryOff reports whether an error from one of the memory doors is the far
// machine saying it is not remembering, rather than a store that would not
// answer.
//
// IT IS A STRING MATCH AND THAT IS DELIBERATE. The alternative is an error type
// crossing the wire, and errors do not survive encoding/json — which is the same
// reason [EventWire] exists. The word is a constant in this package, both halves
// are built from one tree, and the fallback if it ever stops matching is the
// OTHER honest sentence rather than a wrong one.
func MemoryOff(err error) bool {
	return err != nil && len(err.Error()) >= len(memoryOffWord) &&
		err.Error()[len(err.Error())-len(memoryOffWord):] == memoryOffWord
}

// ── THE SURFACE HALF OF THE FOLDERS ─────────────────────────────────────────
//
// These three are what internal/tui3 type-asserts for when somebody picks a
// folder, and they are on [Agent] rather than on [Client] because that is the
// handle the surface holds: the local door and the ssh door hand the same
// *remote.Agent to the same picker, and a capability that lived on the client
// would be one the picker could not reach.

// KeepsFolders answers FOR THE MACHINE AT THE OTHER END, off what it said at the
// door ([Welcome.Folders]) — the fact a surface needs BEFORE it opens a picker,
// and the one its own type assertion cannot give it: this type always has the
// three methods below, whatever is behind the pipe.
//
// AND IT IS RE-READ RATHER THAN REMEMBERED, exactly as [Agent.SteerRepeatKnown]
// is: /new, /resume and a reconnect all replace the welcome, and the engine
// behind it can change with them.
func (a *Agent) KeepsFolders() bool { return a.c.Welcome().Folders }

// ReferPlace attaches one folder to the conversation on the ENGINE machine.
//
// IT IS A ROUND TRIP AND IT HAS TO BE. The engine is what stats the path, snaps
// it to its repository root, writes it onto the conversation's meta.json and
// puts it in front of the model — none of which this end can do or check, and
// all of which is the difference between a folder attached and a line drawn.
// The [session.PlaceRef] that comes back is THE ENGINE'S ANSWER, root-snapped and
// canonical, so a caller reporting what was attached reports what landed rather
// than what it asked for.
func (a *Agent) ReferPlace(path string, arrival session.PlaceArrival) (session.PlaceRef, error) {
	payload, err := a.c.call(nil, MethodPlacesRefer, ReferArgs{Path: path, Arrival: arrival})
	if err != nil {
		return session.PlaceRef{}, err
	}
	var ref session.PlaceRef
	if err := json.Unmarshal(payload, &ref); err != nil {
		return session.PlaceRef{}, err
	}
	a.c.facts.referPlace(ref)
	return ref, nil
}

// Places is the folders this conversation is about, newest first — A MEMORY READ
// THAT NEVER TOUCHES THE WIRE.
//
// The set rides the fact push for replica.go's stated reason: the folder
// indicator is drawn on a frame, and a frame is not allowed to wait on a
// network. What is answered is what the engine last said, which for a connection
// that has dropped is the last true picture rather than a list that emptied
// itself because a pipe closed.
func (a *Agent) Places() []session.PlaceRef { return a.c.facts.read().Places }

// RemovePlace takes one folder back off the conversation on the engine machine,
// and carries the engine's own refusal back for a folder it is not about.
func (a *Agent) RemovePlace(path string) error {
	if _, err := a.c.call(nil, MethodPlacesRemove, path); err != nil {
		return err
	}
	a.c.facts.removePlace(path)
	return nil
}

// memoryCall is the seven doors, dispatched. It is split out of [placesCall] so
// that the nil check above happens exactly once for all of them.
func memoryCall(mem EngineMemory, call Frame) (json.RawMessage, error) {
	switch call.Method {
	case MethodMemorySnapshot:
		limit, err := arg[int](call)
		if err != nil {
			return nil, err
		}
		shelves, err := mem.Snapshot(limit)
		if err != nil {
			return nil, err
		}
		return json.Marshal(shelves)

	case MethodMemoryChanged:
		at, err := arg[time.Time](call)
		if err != nil {
			return nil, err
		}
		learned, letGo, err := mem.ChangedSince(at)
		if err != nil {
			return nil, err
		}
		return json.Marshal(MemoryChange{Learned: learned, LetGo: letGo})

	case MethodMemoryList:
		args, err := arg[MemoryListArgs](call)
		if err != nil {
			return nil, err
		}
		found, err := mem.ListMemories(args.Scope, args.Limit)
		if err != nil {
			return nil, err
		}
		return json.Marshal(found)

	case MethodMemoryUpdate:
		args, err := arg[MemoryUpdateArgs](call)
		if err != nil {
			return nil, err
		}
		return nil, mem.UpdateMemory(args.ID, args.Title, args.Text, args.Tags)

	case MethodMemoryForget:
		id, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		return nil, mem.ForgetMemory(id)

	case MethodMemoryRestore:
		id, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		return nil, mem.RestoreMemory(id)

	case MethodMemoryProvenance:
		id, err := arg[string](call)
		if err != nil {
			return nil, err
		}
		where, title, at, err := mem.MemoryProvenance(id)
		if err != nil {
			return nil, err
		}
		return json.Marshal(MemoryOrigin{Session: where, Title: title, At: at})
	}
	return nil, errors.New(engineOffWord + "no such memory door")
}
