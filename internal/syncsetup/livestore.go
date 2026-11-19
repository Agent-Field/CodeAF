package syncsetup

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/filesdoor"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// liveStore is the store one chat's sync uses while live transfer carries it.
//
// WHAT IT WRITES AND WHAT IT READS ARE DIFFERENT QUESTIONS. A publish goes to
// this computer's own live store (the relay would refuse it, and must not be
// asked): PutFrame and Has answer for that store, which is where the next
// computer will come looking. A fetch reads from everywhere that might hold
// the object, in order: the live store here, the computers that have the chat
// (through their files doors), and last the relay's store, which holds whatever
// was stored there before the account's storage closed. Once storage is
// approved, publishes go to the relay again and the relay is read first.
//
// EVERY SOURCE IS PARTIAL. A computer may hold a chat's newest frames and not
// its oldest, so a miss in one source goes on to the next for that object, never
// ends the fetch.
//
// ONE TUNNEL PER COMPUTER FOR THE LIFE OF THE STORE. The relay lets a network
// dial only a few times a second; every door is a [filesdoor.Source], which
// dials on its first request and keeps the tunnel. A computer whose door does
// not answer within the retry window is left alone for the rest of the take,
// so a chat whose holder is off fails at once for the rest of its objects.
type liveStore struct {
	sync  *Sync
	chat  string
	disk  *blobstore.Disk
	relay blobstore.Store

	mu       sync.Mutex
	resolved bool
	sources  []*liveSource
	closed   bool
	// holder is the computer the take saw holding the chat before it acquired
	// it. After the acquire the directory names this computer, so the record
	// read later cannot say who to ask.
	holder string
}

// liveSource is one other computer's door and what became of it.
type liveSource struct {
	id, name string
	source   *filesdoor.Source
	off      error // why it is left alone: not answering, or refusing
}

// errUnanswered is internal: no computer's door held what was asked.
var errUnanswered = errors.New("blind pass-through: no other computer had it")

func (s *Sync) newLiveStore(chat string, relay blobstore.Store) (*liveStore, error) {
	disk, err := s.liveDisk()
	if err != nil {
		return nil, err
	}
	return &liveStore{sync: s, chat: chat, disk: disk, relay: relay}, nil
}

// relayFirst is true once the account's storage is approved: the relay is then
// the authority and the doors only fill what it still lacks.
func (l *liveStore) relayFirst() bool { return l.sync.StorageAllowed() }

// target is where a publish goes.
func (l *liveStore) target() blobstore.Store {
	if l.relayFirst() {
		return l.relay
	}
	return l.disk
}

// others resolves, once, the computers worth asking and in what order: the one
// the directory names as having the chat first, then every other computer of
// this identity that is online now. A computer that is not online is not
// dialled, because every dial spends part of the network's budget.
func (l *liveStore) others(ctx context.Context) []*liveSource {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.resolved || l.closed {
		return l.sources
	}
	l.resolved = true
	self := l.sync.Device.ID()
	seen := map[string]bool{self: true, "": true}
	add := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		name := l.sync.deviceName(id)
		src := filesdoor.NewSource(filesdoor.SourceConfig{
			Service:         l.sync.Relay,
			Identity:        l.sync.Identity,
			Device:          l.sync.Device,
			TargetID:        id,
			TargetName:      name,
			RetryWithin:     liveRetryWithin,
			NoMachineWithin: liveNoMachineWithin,
		})
		if liveSourceHook != nil {
			liveSourceHook(self, id, src)
		}
		l.sources = append(l.sources, &liveSource{id: id, name: name, source: src})
	}
	if l.holder != "" {
		add(l.holder)
	} else if view, err := l.sync.Dir.Cell(ctx, l.chat); err == nil {
		add(view.Cell.Lease.Device)
	}
	if presence, err := l.sync.Dir.Presence(ctx); err == nil {
		var online []string
		for id, p := range presence.Devices {
			if p.Online {
				online = append(online, id)
			}
		}
		sort.Strings(online)
		for _, id := range online {
			add(id)
		}
	}
	if len(l.sources) == 0 {
		// Nobody is named and nobody is known to be online: the chat is held
		// here already (a restart of its files), or its holder has just left.
		// Every other computer of this identity is worth one try then, which
		// costs one dial each, once.
		if roster, err := l.sync.Dir.List(ctx); err == nil {
			var rest []string
			for id, d := range roster.Devices {
				if !d.Revoked {
					rest = append(rest, id)
				}
			}
			sort.Strings(rest)
			for _, id := range rest {
				add(id)
			}
		}
	}
	return l.sources
}

// Hint names the computer that held the chat when the take read its record, the
// first one worth asking.
func (l *liveStore) Hint(device string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.resolved {
		l.holder = device
	}
}

// Close lets go of every tunnel. The take calls it when it is done with the
// chat, which is after its last file has landed.
func (l *liveStore) Close() {
	l.mu.Lock()
	l.closed = true
	sources := l.sources
	l.mu.Unlock()
	for _, one := range sources {
		_ = one.source.Close()
	}
}

// ask runs call against each other computer in turn until one answers it. An
// absent object moves on to the next computer; a computer that is not on, or
// that refuses, is left alone from then on; any other failure ends the ask.
func (l *liveStore) ask(ctx context.Context, call func(blobstore.Store) error) error {
	for _, one := range l.others(ctx) {
		l.mu.Lock()
		skip := one.off != nil
		l.mu.Unlock()
		if skip {
			continue
		}
		err := call(one.source)
		var off *filesdoor.OffError
		var refused *filesdoor.RefusedError
		switch {
		case err == nil:
			return nil
		case errors.Is(err, blobstore.ErrNotFound):
		case errors.As(err, &off), errors.As(err, &refused):
			l.mu.Lock()
			one.off = err
			l.mu.Unlock()
		default:
			return err
		}
	}
	return errUnanswered
}

// absent is the answer when no source holds an object. When the computer that
// has the chat could not be reached, that is the reason, in words that name it;
// otherwise the object really is nowhere.
func (l *liveStore) absent() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, one := range l.sources {
		var off *filesdoor.OffError
		if errors.As(one.off, &off) {
			return &syncwords.HolderOfflineError{Name: one.name}
		}
	}
	return blobstore.ErrNotFound
}

// secrets walks the secret files of a chat, a page at a time, from the first
// computer that has the chat, handing each page to use. A computer that is off
// or refuses is left alone, as for objects; one that does not have the chat is
// asked past. It is silent when no computer has the chat, and says the computer
// is off only when that is the reason nothing could be asked. Once a computer
// has started, its pages are the only ones read: a walk never mixes two
// computers' files.
func (l *liveStore) secrets(ctx context.Context, chat string, use func(filesdoor.SecretPage) error) error {
	for _, one := range l.others(ctx) {
		l.mu.Lock()
		skip := one.off != nil
		l.mu.Unlock()
		if skip {
			continue
		}
		found, err := l.walkSecrets(ctx, one, chat, use)
		if found || (err != nil && !l.isOff(one, err)) {
			return err
		}
	}
	if err := l.absent(); !errors.Is(err, blobstore.ErrNotFound) {
		return err
	}
	return nil
}

// walkSecrets reads every page one computer holds for chat. found is whether it
// had the chat at all.
func (l *liveStore) walkSecrets(ctx context.Context, one *liveSource, chat string, use func(filesdoor.SecretPage) error) (found bool, err error) {
	after := ""
	for {
		page, err := one.source.Secrets(ctx, chat, after)
		if err != nil {
			return found, err
		}
		if !page.Found {
			return found, nil
		}
		found = true
		if err := use(page); err != nil || !page.More || len(page.Files) == 0 {
			return found, err
		}
		after = page.Files[len(page.Files)-1].Path
	}
}

// isOff notes that a computer is not answering, or refuses, and says whether
// that is what err was.
func (l *liveStore) isOff(one *liveSource, err error) bool {
	var off *filesdoor.OffError
	var refused *filesdoor.RefusedError
	if !errors.As(err, &off) && !errors.As(err, &refused) {
		return false
	}
	l.mu.Lock()
	one.off = err
	l.mu.Unlock()
	return true
}

// keepObjects writes what a peer sent into this computer's live store, so this
// computer can serve the chat in turn. It is best effort: a take does not fail
// because its copy could not be kept.
func (l *liveStore) keepObjects(ctx context.Context, objects []blobstore.Object) {
	if len(objects) == 0 {
		return
	}
	frame, err := blobstore.Encode(l.sync.Identity.CellKeyID(), objects)
	if err != nil {
		return
	}
	l.keepFrame(ctx, frame)
}

func (l *liveStore) keepFrame(ctx context.Context, frame []byte) {
	if _, err := l.disk.PutFrame(ctx, frame); err == nil {
		markLivePending(l.sync.Home)
	}
}

// missing is a local read that found nothing usable, which is a reason to ask
// elsewhere and not a reason to stop.
func missing(err error) bool {
	return errors.Is(err, blobstore.ErrNotFound) || errors.Is(err, blobstore.ErrDamaged)
}

func (l *liveStore) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	id, err := l.target().PutFrame(ctx, frame)
	if err == nil && !l.relayFirst() {
		markLivePending(l.sync.Home)
	}
	return id, err
}

// Has answers for where a publish puts things, so the publisher sends exactly
// what the next computer will not otherwise find.
func (l *liveStore) Has(ctx context.Context, rids []string) ([]bool, error) {
	return l.target().Has(ctx, rids)
}

func (l *liveStore) Get(ctx context.Context, rid string) ([]byte, error) {
	if l.relayFirst() {
		b, err := l.relay.Get(ctx, rid)
		if err == nil || !errors.Is(err, blobstore.ErrNotFound) {
			return b, err
		}
	}
	if b, err := l.disk.Get(ctx, rid); err == nil {
		return b, nil
	} else if !missing(err) {
		return nil, err
	}
	var got []byte
	err := l.ask(ctx, func(s blobstore.Store) error {
		b, err := s.Get(ctx, rid)
		got = b
		return err
	})
	if err == nil {
		l.keepObjects(ctx, []blobstore.Object{{RID: rid, Bytes: got}})
		return got, nil
	}
	if !errors.Is(err, errUnanswered) {
		return nil, err
	}
	if !l.relayFirst() {
		b, relayErr := l.relay.Get(ctx, rid)
		if relayErr == nil || !errors.Is(relayErr, blobstore.ErrNotFound) {
			return b, relayErr
		}
	}
	return nil, l.absent()
}

func (l *liveStore) GetMany(ctx context.Context, rids []string) ([]blobstore.Object, error) {
	if l.relayFirst() {
		objects, err := l.relay.GetMany(ctx, rids)
		if err == nil || !errors.Is(err, blobstore.ErrNotFound) {
			return objects, err
		}
	}
	if objects, err := l.disk.GetMany(ctx, rids); err == nil {
		return objects, nil
	} else if !missing(err) {
		return nil, err
	}
	var got []blobstore.Object
	err := l.ask(ctx, func(s blobstore.Store) error {
		objects, err := s.GetMany(ctx, rids)
		got = objects
		return err
	})
	if err == nil {
		l.keepObjects(ctx, got)
		return got, nil
	}
	if !errors.Is(err, errUnanswered) {
		return nil, err
	}
	if !l.relayFirst() {
		objects, relayErr := l.relay.GetMany(ctx, rids)
		if relayErr == nil || !errors.Is(relayErr, blobstore.ErrNotFound) {
			return objects, relayErr
		}
	}
	return nil, l.absent()
}

func (l *liveStore) GetFrame(ctx context.Context, frame string) ([]byte, error) {
	if l.relayFirst() {
		b, err := l.relay.GetFrame(ctx, frame)
		if err == nil || !errors.Is(err, blobstore.ErrNotFound) {
			return b, err
		}
	}
	if b, err := l.disk.GetFrame(ctx, frame); err == nil {
		return b, nil
	} else if !missing(err) {
		return nil, err
	}
	var got []byte
	err := l.ask(ctx, func(s blobstore.Store) error {
		b, err := s.GetFrame(ctx, frame)
		got = b
		return err
	})
	if err == nil {
		l.keepFrame(ctx, got)
		return got, nil
	}
	if !errors.Is(err, errUnanswered) {
		return nil, err
	}
	if !l.relayFirst() {
		b, relayErr := l.relay.GetFrame(ctx, frame)
		if relayErr == nil || !errors.Is(relayErr, blobstore.ErrNotFound) {
			return b, relayErr
		}
	}
	return nil, l.absent()
}

// Locate asks each source for what the earlier ones did not hold, and merges
// the answers: a frame id from any of them is a frame GetFrame can find.
func (l *liveStore) Locate(ctx context.Context, rids []string) (map[string]blobstore.Location, error) {
	found := make(map[string]blobstore.Location, len(rids))
	pending := rids
	narrow := func(store blobstore.Store) error {
		if len(pending) == 0 {
			return nil
		}
		got, err := store.Locate(ctx, pending)
		if err != nil {
			return err
		}
		var rest []string
		for _, rid := range pending {
			if loc, ok := got[rid]; ok {
				found[rid] = loc
			} else {
				rest = append(rest, rid)
			}
		}
		pending = rest
		return nil
	}
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if l.relayFirst() {
		note(narrow(l.relay))
	}
	note(narrow(l.disk))
	for _, one := range l.others(ctx) {
		if len(pending) == 0 {
			break
		}
		l.mu.Lock()
		skip := one.off != nil
		l.mu.Unlock()
		if skip {
			continue
		}
		err := narrow(one.source)
		var off *filesdoor.OffError
		var refused *filesdoor.RefusedError
		if errors.As(err, &off) || errors.As(err, &refused) {
			l.mu.Lock()
			one.off = err
			l.mu.Unlock()
			continue
		}
		note(err)
	}
	if !l.relayFirst() && len(pending) > 0 {
		note(narrow(l.relay))
	}
	if len(found) == 0 && firstErr != nil {
		if abs := l.absent(); !errors.Is(abs, blobstore.ErrNotFound) {
			return nil, abs
		}
		return nil, firstErr
	}
	return found, nil
}

var _ blobstore.Store = (*liveStore)(nil)
