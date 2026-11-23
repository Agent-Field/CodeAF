package filesdoor

import (
	"context"
	"crypto/ecdh"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
)

const (
	// LostLine and BackLine are the only two things the door says on its own,
	// once each per spell of the sync service being out of reach.
	LostLine = "moving chats between your computers is paused while they cannot reach each other"
	BackLine = "moving chats between your computers works again"

	// The registration backoff doubles from a second up to ten seconds. In a
	// steady failure that is six attempts a minute, which leaves room under the
	// hosted relay's ten registrations a minute per network for the task door
	// and for a second computer behind the same address.
	registerBackoffStart = time.Second
	registerBackoffCap   = 10 * time.Second
	// registerStableAfter is how long a registration has to stay up before the
	// next failure starts the backoff over from a second.
	registerStableAfter = time.Minute
)

// ServeConfig is everything one process needs to hold its computer's files door.
type ServeConfig struct {
	// Service is the relay base URL.
	Service  string
	Identity identity.Identity
	// Device is this computer.
	Device identity.Dev
	// Roster answers, per device id, whether that device is still allowed: not
	// removed and not revoked. It is asked for every connection and again
	// before every response, so removal takes effect mid-take.
	Roster func(context.Context) (map[string]bool, error)
	// Store is the read-only source of what is served. Only Has, Locate,
	// GetFrame, GetMany and Get are ever called on it.
	Store blobstore.Store
	// LockPath is the file whose lock says which process of this home holds the
	// door, for example <home>/v3/sync/files-door.lock.
	LockPath string
	// Say is told the rare thing worth saying. It may be nil.
	Say func(string)
	// RosterFor is how long a roster answer is trusted; 0 is [DefaultRosterFor].
	RosterFor time.Duration
	// LockPoll is how often a process without the lock looks again; 0 is
	// [DefaultLockPoll].
	LockPoll time.Duration
	// Secrets answers a request for the secret files of one chat; nil answers
	// that there are none.
	Secrets SecretsFunc
	// SecretsPage is the most bytes of file one page holds; 0 is [DefaultSecretsPage].
	SecretsPage int
	// ChatTitle reads the name of one local conversation for a paired peer.
	ChatTitle func(id string) string
	// Ready, when set, is called after every successful registration, before
	// the door starts answering. It must not block.
	Ready func()
}

func (c ServeConfig) check() error {
	address, err := url.Parse(strings.TrimSpace(c.Service))
	if err != nil || address.Host == "" || (address.Scheme != "http" && address.Scheme != "https") {
		return errors.New("filesdoor: a door needs a relay address that looks like https://relay.example.com")
	}
	if c.Roster == nil || c.Store == nil || c.LockPath == "" {
		return errors.New("filesdoor: a door needs a roster, a store and a lock file")
	}
	if c.Device.Cert.Verify(c.Identity.PublicKey()) != nil {
		return errors.New("filesdoor: this device does not belong to the identity")
	}
	return nil
}

// Serve holds this computer's files door until ctx ends, and returns nil then.
// It returns an error only for a configuration that can never work.
//
// ONE PROCESS PER HOME REGISTERS. Serve first takes an exclusive lock on
// LockPath, and while it does not hold it makes no request to the relay at all.
// Several codeaf processes can run a sync for one home, and the relay allows a
// computer's network only a handful of registrations a minute; siblings that
// each tried to register would spend them against each other, and would be
// fighting over one name. The lock belongs to the process, so it is free the
// moment the holder ends, however it ends, and a waiting sibling takes over
// within one poll.
func Serve(ctx context.Context, cfg ServeConfig) error {
	if err := cfg.check(); err != nil {
		return err
	}
	key, err := DoorKey(cfg.Identity, cfg.Device.ID())
	if err != nil {
		return err
	}
	poll := cfg.LockPoll
	if poll <= 0 {
		poll = DefaultLockPoll
	}
	ttl := cfg.RosterFor
	if ttl <= 0 {
		ttl = DefaultRosterFor
	}
	lock, err := acquire(ctx, cfg.LockPath, poll)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer release(lock)

	d := &door{cfg: cfg, key: key, roster: &rosterCache{fetch: cfg.Roster, now: time.Now, ttl: ttl}}
	(&registrar{
		service:  cfg.Service,
		key:      key,
		register: relay.Register,
		hold:     d.hold,
		ready:    cfg.Ready,
		say:      cfg.Say,
		now:      time.Now,
		wait:     sleep,
	}).run(ctx)
	return nil
}

// acquire takes the exclusive lock by asking without waiting, every poll, so
// a cancelled context is noticed at once and a waiting process holds nothing
// but an open file.
func acquire(ctx context.Context, path string, poll time.Duration) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err := filelock.Lock(file, true, true)
		if err == nil {
			return file, nil
		}
		if !filelock.IsBusy(err) {
			_ = file.Close()
			return nil, err
		}
		if err := sleep(ctx, poll); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
}

func release(file *os.File) {
	_ = filelock.Unlock(file)
	_ = file.Close()
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// registrar keeps the door registered under its name. It deliberately never
// gives up while the context lives: a name still held by the dead connection of
// a process that just ended, a rate limit, an unreachable relay and a relay
// being redeployed are all things that pass, and a door that quit on one would
// stay off until the next launch.
type registrar struct {
	service  string
	key      *ecdh.PrivateKey
	register func(context.Context, string, *ecdh.PrivateKey) (*relay.Registration, error)
	hold     func(context.Context, *relay.Registration)
	ready    func()
	say      func(string)
	now      func() time.Time
	wait     func(context.Context, time.Duration) error
}

func (r *registrar) tell(line string) {
	if r.say != nil {
		r.say(line)
	}
}

func (r *registrar) run(ctx context.Context) {
	wait := registerBackoffStart
	announced, told := false, false
	for ctx.Err() == nil {
		registration, err := r.register(ctx, r.service, r.key)
		if err == nil {
			announced = true
			if told {
				r.tell(BackLine)
				told = false
			}
			if r.ready != nil {
				r.ready()
			}
			since := r.now()
			r.hold(ctx, registration)
			// The backoff starts over only when the registration stayed up for a
			// meaningful stretch; one that drops right after it was accepted
			// keeps backing off instead of hammering the relay.
			if r.now().Sub(since) >= registerStableAfter {
				wait = registerBackoffStart
			}
		} else if announced && !told && ctx.Err() == nil {
			r.tell(LostLine)
			told = true
		}
		if r.wait(ctx, wait) != nil {
			return
		}
		if wait *= 2; wait > registerBackoffCap {
			wait = registerBackoffCap
		}
	}
}

// door answers the streams that reach this computer's registration.
type door struct {
	cfg    ServeConfig
	key    *ecdh.PrivateKey
	roster *rosterCache
}

// hold owns accepted streams until the registration ends, and waits for every
// connection to finish before it returns, so nothing touches the store once
// Serve has returned.
func (d *door) hold(ctx context.Context, registration *relay.Registration) {
	stop := context.AfterFunc(ctx, func() { _ = registration.Close() })
	defer stop()
	var connections sync.WaitGroup
	defer connections.Wait()
	defer registration.Close()
	for {
		stream, err := registration.Accept()
		if err != nil {
			return
		}
		connections.Add(1)
		go func() {
			defer connections.Done()
			d.answer(ctx, stream)
		}()
	}
}

// answer runs one connection: tunnel, hello, then requests one at a time.
func (d *door) answer(ctx context.Context, stream io.ReadWriteCloser) {
	defer stream.Close()
	defer context.AfterFunc(ctx, func() { _ = stream.Close() })()
	guard := newWatch(handshakeWithin, func() { _ = stream.Close() })
	defer guard.stop()

	tunnel, err := pair.AcceptWithKeyPrologue(stream, d.key, FilesTunnelPrologue)
	if err != nil {
		return
	}
	in, out := kickReader{tunnel, guard}, kickWriter{tunnel, guard}
	device, err := d.acceptHello(ctx, in, out, tunnel)
	if err != nil {
		return
	}
	// From here the same timer is the idle bound, moved forward by every byte.
	guard.after = idleAfter
	guard.kick()
	for {
		// A request has to arrive whole within the idle bound, so a peer that
		// sends one byte at a time cannot hold the stream either.
		guard.kick()
		typ, body, _, err := readMessage(in, MaxRequestBytes)
		if err != nil {
			return
		}
		// THE ROSTER IS ASKED BEFORE EVERY RESPONSE, and before the store is
		// touched for it. A device removed in the middle of a take gets a
		// refusal for its next request and not one more object.
		allowed, err := d.roster.allows(ctx, device)
		if err != nil {
			// The door cannot tell whether this device is still allowed, so it
			// answers nothing. It ends the connection without a refusal: a refusal
			// is final for the taker, and a directory that was unreachable for a
			// moment should cost it one redial and not the take. The redial's own
			// hello asks the roster again and is refused if it still cannot say.
			return
		}
		if !allowed {
			refuse(out, RemovedLine)
			return
		}
		reply, fatal := d.respond(ctx, typ, body)
		guard.kick()
		if _, err := writeMessage(out, reply.typ, reply.parts...); err != nil || fatal {
			return
		}
	}
}

// reply is one response before it is written.
type reply struct {
	typ   byte
	parts [][]byte
}

func errorReply(code byte) reply { return reply{typ: typeError, parts: [][]byte{{code}}} }

// codeFor maps what a store said onto the code the client turns back into the
// same sentinel. An error the door cannot name is internal, and says nothing
// about the store's insides.
func codeFor(err error) byte {
	switch {
	case errors.Is(err, blobstore.ErrBadRID):
		return codeBadRID
	case errors.Is(err, blobstore.ErrTooMany):
		return codeTooMany
	case errors.Is(err, blobstore.ErrDamaged):
		return codeDamaged
	}
	return codeInternal
}

// respond answers one request from the store. The second result is true when
// the request was not understood, which ends the connection after the answer.
func (d *door) respond(ctx context.Context, typ byte, body []byte) (reply, bool) {
	switch typ {
	case typeHas:
		return d.has(ctx, body)
	case typeLocate:
		return d.locate(ctx, body)
	case typeGetMany:
		return d.getMany(ctx, body)
	case typeGetFrame:
		return d.getFrame(ctx, body)
	case typeSecrets:
		return d.secrets(ctx, body)
	case typeChatTitles:
		return d.titles(ctx, body)
	}
	return errorReply(codeBadRequest), true
}

// ids reads the ids of a request. Every one is checked with ValidRID before it
// can touch the store, where on a disk store an id becomes part of a path.
func ids(body []byte, limit int) ([]string, *reply, bool) {
	rids, err := parseRids(body, limit)
	switch {
	case errors.Is(err, errTooManyRids):
		r := errorReply(codeTooMany)
		return nil, &r, false
	case err != nil:
		r := errorReply(codeBadRequest)
		return nil, &r, true
	}
	for _, rid := range rids {
		if !blobstore.ValidRID(rid) {
			r := errorReply(codeBadRID)
			return nil, &r, false
		}
	}
	return rids, nil, false
}

func (d *door) has(ctx context.Context, body []byte) (reply, bool) {
	rids, bad, fatal := ids(body, MaxRidsPerRequest)
	if bad != nil {
		return *bad, fatal
	}
	have, err := d.cfg.Store.Has(ctx, rids)
	if err != nil || len(have) != len(rids) {
		return errorReply(codeFor(err)), false
	}
	answer := make([]byte, 2, 2+len(have))
	binary.BigEndian.PutUint16(answer, uint16(len(have)))
	for _, held := range have {
		b := byte(0)
		if held {
			b = 1
		}
		answer = append(answer, b)
	}
	return reply{typ: typeHas | answerBit, parts: [][]byte{answer}}, false
}

func (d *door) locate(ctx context.Context, body []byte) (reply, bool) {
	rids, bad, fatal := ids(body, blobstore.MaxHas)
	if bad != nil {
		return *bad, fatal
	}
	at, err := d.cfg.Store.Locate(ctx, rids)
	if err != nil {
		return errorReply(codeFor(err)), false
	}
	// Each entry names the request position it answers, so the ids are not sent
	// back. A rid asked twice is answered once, at its first position.
	var entries []byte
	count := 0
	seen := make(map[string]bool, len(at))
	for i, rid := range rids {
		loc, ok := at[rid]
		if !ok || seen[rid] || !blobstore.ValidRID(loc.Frame) {
			continue
		}
		seen[rid] = true
		frame, _ := hex.DecodeString(loc.Frame)
		entries = binary.BigEndian.AppendUint16(entries, uint16(i))
		entries = append(entries, frame...)
		entries = binary.BigEndian.AppendUint64(entries, loc.Off)
		entries = binary.BigEndian.AppendUint32(entries, loc.Len)
		count++
	}
	head := binary.BigEndian.AppendUint16(nil, uint16(count))
	return reply{typ: typeLocate | answerBit, parts: [][]byte{head, entries}}, false
}

// getMany answers the store's own prefix, cut shorter when that is what it
// takes to stay inside one response. A taker already asks again for what it
// did not get, so a shorter prefix costs a request and breaks nothing. An
// object that alone is bigger than a response is named, not sent, and the
// client reads it by ranges of its frame instead.
func (d *door) getMany(ctx context.Context, body []byte) (reply, bool) {
	rids, bad, fatal := ids(body, MaxRidsPerRequest)
	if bad != nil {
		return *bad, fatal
	}
	objects, err := d.cfg.Store.GetMany(ctx, rids)
	if errors.Is(err, blobstore.ErrNotFound) {
		return reply{typ: typeGetMany | answerBit, parts: [][]byte{{manyAbsent, 0, 0}}}, false
	}
	if err != nil {
		return errorReply(codeFor(err)), false
	}
	if len(objects) > len(rids) || (len(objects) == 0 && len(rids) > 0) {
		return errorReply(codeInternal), false
	}
	total, keep := 0, 0
	for i, object := range objects {
		if object.RID != rids[i] {
			return errorReply(codeInternal), false
		}
		if total+len(object.Bytes) > MaxResponseBytes {
			break
		}
		total += len(object.Bytes)
		keep = i + 1
	}
	if keep == 0 && len(objects) > 0 {
		return reply{typ: typeGetMany | answerBit, parts: [][]byte{{manyTooLarge, 0, 0}}}, false
	}
	parts := make([][]byte, 0, 1+2*keep)
	parts = append(parts, binary.BigEndian.AppendUint16([]byte{manyObjects}, uint16(keep)))
	for _, object := range objects[:keep] {
		parts = append(parts, binary.BigEndian.AppendUint32(nil, uint32(len(object.Bytes))), object.Bytes)
	}
	return reply{typ: typeGetMany | answerBit, parts: parts}, false
}

// getFrame answers one chunk of one frame: where it starts, how long the whole
// frame is, and at most one response's worth of bytes from there. A frame of
// the biggest size moves as a few bounded responses, each asked for after the
// last was read.
func (d *door) getFrame(ctx context.Context, body []byte) (reply, bool) {
	frame, off, n, err := parseFrameRequest(body)
	if err != nil || !blobstore.ValidRID(frame) {
		return errorReply(codeBadRequest), true
	}
	data, err := d.cfg.Store.GetFrame(ctx, frame)
	if errors.Is(err, blobstore.ErrNotFound) {
		return reply{typ: typeGetFrame | answerBit, parts: [][]byte{append([]byte{frameAbsent}, make([]byte, 16)...)}}, false
	}
	if err != nil {
		return errorReply(codeFor(err)), false
	}
	total := uint64(len(data))
	if total > blobstore.MaxFrame {
		// A store that holds a frame over its own limit is broken; say so
		// rather than send a response the client would have to refuse.
		return errorReply(codeInternal), false
	}
	if off > total {
		return errorReply(codeBadRequest), true
	}
	size := uint64(n)
	if size == 0 || size > MaxResponseBytes {
		size = MaxResponseBytes
	}
	end := min(off+size, total)
	head := binary.BigEndian.AppendUint64([]byte{frameFound}, total)
	head = binary.BigEndian.AppendUint64(head, off)
	return reply{typ: typeGetFrame | answerBit, parts: [][]byte{head, data[off:end]}}, false
}
