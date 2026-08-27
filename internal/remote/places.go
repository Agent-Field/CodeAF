package remote

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"
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
