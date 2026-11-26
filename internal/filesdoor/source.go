package filesdoor

import (
	"context"
	"crypto/ecdh"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
)

var (
	// ErrReadOnly answers a write to a source. A files door only ever reads.
	ErrReadOnly = errors.New("filesdoor: a files door is read-only")
	// ErrSourceOff matches an [OffError]: the computer could not be reached
	// within the retry window.
	ErrSourceOff = errors.New("filesdoor: that computer's files door is not answering")
	// ErrClosed answers a request to a source that was closed.
	ErrClosed = errors.New("filesdoor: the source is closed")
)

// OffError is a computer whose door did not answer within the retry window,
// which in practice means codeaf is not running there right now.
type OffError struct {
	DeviceID string
	// Name is the person-facing name of the computer.
	Name string
	// Err is the last thing that went wrong on the way to it.
	Err error
}

func (e *OffError) Error() string { return e.computer() + " is not on right now" }
func (e *OffError) Unwrap() error { return e.Err }
func (e *OffError) Is(target error) bool {
	return target == ErrSourceOff
}
func (e *OffError) computer() string {
	if e.Name != "" {
		return e.Name
	}
	return e.DeviceID
}

// RefusedError is the door answering and saying no: this device is not in the
// roster, was removed, or the two computers run versions that cannot talk. It
// is never retried, because asking again cannot change the answer.
type RefusedError struct{ Reason string }

func (e *RefusedError) Error() string { return e.Reason }

// Stats counts what one source did. The bounds the design promises can be read
// off it: no more than one request on the tunnel at a time, a small request,
// and a response that never exceeds one bounded response.
type Stats struct {
	// Dials counts attempts to reach the door, failed ones included, because the
	// relay limits dials per network whether or not they succeed.
	Dials        int
	DialFailures int
	// Requests counts requests sent on a tunnel, the hello excluded.
	Requests  int
	BytesDown int64
	BytesUp   int64
	// MaxRequestBytes and MaxResponseBytes are the largest single message seen
	// in each direction, length prefix included.
	MaxRequestBytes  int
	MaxResponseBytes int
	// MaxInFlight is the most requests ever outstanding on the tunnel at once.
	MaxInFlight int
}

// SourceConfig says which computer's door a source reads from.
type SourceConfig struct {
	Service  string
	Identity identity.Identity
	// Device is this computer, the dialer.
	Device identity.Dev
	// TargetID is the device id whose door to dial.
	TargetID string
	// TargetName is the person-facing name of that computer, used in errors.
	TargetName string
	// RetryWithin is how long a door that is not there is redialled; 0 is
	// [DefaultRetryWithin].
	RetryWithin time.Duration
	// NoMachineWithin is the shorter window used while every answer has been
	// that nothing is registered under the door's name; 0 is
	// [DefaultNoMachineWithin]. It never lengthens RetryWithin.
	NoMachineWithin time.Duration
	// Sleep waits between dial attempts. Nil is a real timer.
	Sleep func(context.Context, time.Duration) error
}

// Source is a read-only [blobstore.Store] over one tunnel to one computer's
// files door.
//
// THE TUNNEL IS DIALLED ON THE FIRST REQUEST AND KEPT. A take of a chat with
// thousands of files makes thousands of requests, and the relay lets a network
// dial only a few times a second; one dial per request would hit that limit
// long before the chat had moved. Requests are serialised, so there is never
// more than one outstanding on the stream: the relay's pipe has no flow
// control, and that is what bounds what it buffers for the transfer.
type Source struct {
	cfg  SourceConfig
	life context.Context
	end  context.CancelFunc

	// request is the one place on the tunnel, held for a whole request and its
	// answer. It is a channel rather than a mutex so a caller waiting for its
	// turn can still be cancelled.
	request chan struct{}

	// held guards the tunnel, which Close has to reach while a request that
	// holds request is still waiting on it.
	held sync.Mutex
	link *pair.Tunnel

	counts   sync.Mutex
	stats    Stats
	inFlight int
}

// NewSource makes a source. It dials nothing until the first request.
func NewSource(cfg SourceConfig) *Source {
	if cfg.RetryWithin <= 0 {
		cfg.RetryWithin = DefaultRetryWithin
	}
	if cfg.NoMachineWithin <= 0 {
		cfg.NoMachineWithin = DefaultNoMachineWithin
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleep
	}
	life, end := context.WithCancel(context.Background())
	return &Source{cfg: cfg, life: life, end: end, request: make(chan struct{}, 1)}
}

// Close drops the tunnel and ends any request waiting on it.
func (s *Source) Close() error {
	s.end()
	s.held.Lock()
	link := s.link
	s.link = nil
	s.held.Unlock()
	if link != nil {
		_ = link.Close()
	}
	return nil
}

// Stats is a snapshot of what the source has done so far.
func (s *Source) Stats() Stats {
	s.counts.Lock()
	defer s.counts.Unlock()
	return s.stats
}

func (s *Source) note(change func(*Stats)) {
	s.counts.Lock()
	defer s.counts.Unlock()
	change(&s.stats)
}

// PutFrame refuses: a source only reads.
func (s *Source) PutFrame(context.Context, []byte) (blobstore.FrameID, error) {
	return "", ErrReadOnly
}

// checkRids is the one place the argument rules live, in the order a local
// store applies them: too many ids is a caller bug, then an id that could never
// be stored. Refusing here is free, where a round trip is not.
func checkRids(rids []string, limit int, distinct bool) error {
	if len(rids) > limit {
		return fmt.Errorf("%w: %d ids, the limit is %d", blobstore.ErrTooMany, len(rids), limit)
	}
	seen := make(map[string]bool, len(rids))
	for _, rid := range rids {
		if !blobstore.ValidRID(rid) {
			return fmt.Errorf("%w: %q", blobstore.ErrBadRID, rid)
		}
		if distinct && seen[rid] {
			return fmt.Errorf("%w: %q asked for twice", blobstore.ErrBadRID, rid)
		}
		seen[rid] = true
	}
	return nil
}

// Has answers, per id, whether the computer holds it. A list longer than one
// request may carry is asked in several, up to the store's own bound.
func (s *Source) Has(ctx context.Context, rids []string) ([]bool, error) {
	if err := checkRids(rids, blobstore.MaxHas, false); err != nil {
		return nil, err
	}
	have := make([]bool, 0, len(rids))
	for start := 0; start < len(rids); start += MaxRidsPerRequest {
		batch := rids[start:min(start+MaxRidsPerRequest, len(rids))]
		body, err := ridsBody(batch)
		if err != nil {
			return nil, err
		}
		err = s.exchange(ctx, typeHas, body, func(b []byte) error {
			if len(b) != 2+len(batch) || int(binary.BigEndian.Uint16(b)) != len(batch) {
				return protocolf("a has answer for %d ids is not that long", len(batch))
			}
			for _, held := range b[2:] {
				if held > 1 {
					return protocolf("a has answer holds %d", held)
				}
				have = append(have, held == 1)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return have, nil
}

// Locate answers where each held object lies. An id the computer does not hold
// is absent from the map, as with a local store.
func (s *Source) Locate(ctx context.Context, rids []string) (map[string]blobstore.Location, error) {
	if err := checkRids(rids, blobstore.MaxHas, false); err != nil {
		return nil, err
	}
	at := make(map[string]blobstore.Location, len(rids))
	if len(rids) == 0 {
		return at, nil
	}
	body, err := ridsBody(rids)
	if err != nil {
		return nil, err
	}
	err = s.exchange(ctx, typeLocate, body, func(b []byte) error {
		if len(b) < 2 {
			return protocolf("a locate answer without a count")
		}
		count := int(binary.BigEndian.Uint16(b))
		const entry = 2 + 32 + 8 + 4
		if len(b) != 2+count*entry || count > len(rids) {
			return protocolf("a locate answer of %d entries is not that long", count)
		}
		for i := 0; i < count; i++ {
			e := b[2+i*entry : 2+(i+1)*entry]
			index := int(binary.BigEndian.Uint16(e))
			off, length := binary.BigEndian.Uint64(e[34:42]), binary.BigEndian.Uint32(e[42:46])
			if index >= len(rids) || length > blobstore.MaxFrame || off > blobstore.MaxFrame {
				return protocolf("a locate answer places an object outside any frame")
			}
			at[rids[index]] = blobstore.Location{Frame: hex.EncodeToString(e[2:34]), Off: off, Len: length}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return at, nil
}

// GetMany answers the longest prefix of rids the door could send in one
// response, at least one object, stopping at the first it does not hold;
// ErrNotFound only when the first is absent. It is the answer a local store
// gives, except that the prefix may be shorter when objects are large, which
// the caller already handles by asking again for the rest.
func (s *Source) GetMany(ctx context.Context, rids []string) ([]blobstore.Object, error) {
	if len(rids) == 0 {
		return nil, nil
	}
	if err := checkRids(rids, MaxRidsPerRequest, true); err != nil {
		return nil, err
	}
	body, err := ridsBody(rids)
	if err != nil {
		return nil, err
	}
	var objects []blobstore.Object
	status := manyObjects
	err = s.exchange(ctx, typeGetMany, body, func(b []byte) error {
		if len(b) < 3 {
			return protocolf("a get-many answer without a status and count")
		}
		status = b[0]
		count := int(binary.BigEndian.Uint16(b[1:3]))
		rest := b[3:]
		switch status {
		case manyAbsent, manyTooLarge:
			if count != 0 || len(rest) != 0 {
				return protocolf("a get-many answer that holds objects and says it has none")
			}
			return nil
		case manyObjects:
		default:
			return protocolf("a get-many status of %d", status)
		}
		if count < 1 || count > len(rids) {
			return protocolf("a get-many answer of %d objects for %d ids", count, len(rids))
		}
		for i := 0; i < count; i++ {
			if len(rest) < 4 {
				return protocolf("a get-many answer cut short")
			}
			n := int(binary.BigEndian.Uint32(rest))
			if n > len(rest)-4 {
				return protocolf("a get-many answer cut short")
			}
			objects = append(objects, blobstore.Object{RID: rids[i], Bytes: rest[4 : 4+n : 4+n]})
			rest = rest[4+n:]
		}
		if len(rest) != 0 {
			return protocolf("a get-many answer with bytes left over")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	switch status {
	case manyAbsent:
		return nil, blobstore.ErrNotFound
	case manyTooLarge:
		// The one object is bigger than a response, so it is read by ranges of
		// the frame that holds it and cut out.
		bytes, err := s.large(ctx, rids[0])
		if err != nil {
			return nil, err
		}
		return []blobstore.Object{{RID: rids[0], Bytes: bytes}}, nil
	}
	return objects, nil
}

// Get answers one object. It is a get-many of one, so an object bigger than a
// response is read the same way.
func (s *Source) Get(ctx context.Context, rid string) ([]byte, error) {
	objects, err := s.GetMany(ctx, []string{rid})
	if err != nil {
		return nil, err
	}
	return objects[0].Bytes, nil
}

// large reads one object that is bigger than a response: where it lies, then
// its bytes as bounded ranges of the frame, each asked for after the last was
// read.
func (s *Source) large(ctx context.Context, rid string) ([]byte, error) {
	at, err := s.Locate(ctx, []string{rid})
	if err != nil {
		return nil, err
	}
	loc, ok := at[rid]
	if !ok {
		return nil, blobstore.ErrNotFound
	}
	if loc.Len == 0 || uint64(loc.Len) > blobstore.MaxFrame {
		return nil, fmt.Errorf("%w: the door placed an object of %d bytes", blobstore.ErrUnreachable, loc.Len)
	}
	object := make([]byte, 0, loc.Len)
	for uint32(len(object)) < loc.Len {
		want := min(uint64(loc.Len)-uint64(len(object)), MaxResponseBytes)
		total, chunk, err := s.chunk(ctx, loc.Frame, loc.Off+uint64(len(object)), uint32(want))
		if err != nil {
			return nil, err
		}
		if loc.Off+uint64(loc.Len) > total {
			return nil, fmt.Errorf("%w: an object runs past the end of its frame", blobstore.ErrUnreachable)
		}
		object = append(object, chunk...)
	}
	return object, nil
}

// GetFrame answers one whole frame, read as bounded chunks. The id is the hash
// of the bytes, so the assembled frame is checked against it: a wrong answer
// degrades like a missing one instead of failing a take at import.
func (s *Source) GetFrame(ctx context.Context, frame string) ([]byte, error) {
	if !blobstore.ValidRID(frame) {
		return nil, fmt.Errorf("%w: %q", blobstore.ErrBadRID, frame)
	}
	var data []byte
	for {
		total, chunk, err := s.chunk(ctx, frame, uint64(len(data)), 0)
		if err != nil {
			return nil, err
		}
		if data == nil {
			data = make([]byte, 0, total)
		}
		data = append(data, chunk...)
		if uint64(len(data)) == total {
			break
		}
	}
	if blobstore.IDOf(data) != frame {
		return nil, fmt.Errorf("%w: frame %s answered bytes that are not it", blobstore.ErrDamaged, frame)
	}
	return data, nil
}

// chunk reads n bytes of a frame from off (0 for as much as one response
// takes), and says how long the frame is. The door must answer exactly what was
// asked, so a short or long answer is the door breaking the rules.
func (s *Source) chunk(ctx context.Context, frame string, off uint64, n uint32) (uint64, []byte, error) {
	body, err := frameRequest(frame, off, n)
	if err != nil {
		return 0, nil, err
	}
	var total uint64
	var data []byte
	absent := false
	err = s.exchange(ctx, typeGetFrame, body, func(b []byte) error {
		if len(b) < getFrameFixed {
			return protocolf("a get-frame answer without its header")
		}
		switch b[0] {
		case frameAbsent:
			if len(b) != getFrameFixed {
				return protocolf("a get-frame answer says absent and sends bytes")
			}
			absent = true
			return nil
		case frameFound:
		default:
			return protocolf("a get-frame status of %d", b[0])
		}
		total = binary.BigEndian.Uint64(b[1:9])
		at := binary.BigEndian.Uint64(b[9:17])
		data = b[getFrameFixed:]
		if total == 0 || total > blobstore.MaxFrame || at != off || off > total {
			return protocolf("a chunk at %d of a frame of %d bytes, asked for %d", at, total, off)
		}
		want := uint64(n)
		if want == 0 || want > MaxResponseBytes {
			want = MaxResponseBytes
		}
		if uint64(len(data)) != min(want, total-off) {
			return protocolf("a chunk of %d bytes where %d were asked for", len(data), min(want, total-off))
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	if absent {
		return 0, nil, blobstore.ErrNotFound
	}
	return total, data, nil
}

// exchange sends one request and hands the answer's body to parse, all while
// holding the only place on the tunnel. parse runs inside that place so the
// answer can be checked before the next request is sent.
//
// A tunnel that breaks under a request is dialled again ONCE, through the
// same retry as the first dial, and the request is sent again. Every request
// here is a read, so sending one twice cannot do harm; a door that changed
// process between two requests of a take is the case this is for.
func (s *Source) exchange(ctx context.Context, typ byte, body []byte, parse func([]byte) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(s.life, cancel)()
	if ctx.Err() != nil {
		return s.settle(ctx, ctx.Err())
	}
	select {
	case s.request <- struct{}{}:
		defer func() { <-s.request }()
	case <-ctx.Done():
		return s.settle(ctx, ctx.Err())
	}

	for attempt := 0; ; attempt++ {
		tunnel, err := s.connect(ctx)
		if err != nil {
			return s.settle(ctx, err)
		}
		answer, answered, err := s.roundTrip(ctx, tunnel, typ, body)
		if err != nil {
			s.drop(tunnel)
			switch {
			case ctx.Err() != nil:
				return s.settle(ctx, ctx.Err())
			case errors.Is(err, errProtocol):
				return fmt.Errorf("%w: %v", blobstore.ErrUnreachable, err)
			case attempt == 0:
				continue
			}
			return fmt.Errorf("%w: %v", blobstore.ErrUnreachable, err)
		}
		if err := s.interpret(tunnel, typ, answered, answer, parse); err != nil {
			return err
		}
		return nil
	}
}

// settle turns an error that came from the context into the right one: a
// closed source says so, and anything else is the caller's own cancellation.
func (s *Source) settle(ctx context.Context, err error) error {
	if s.life.Err() != nil {
		return ErrClosed
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// interpret reads what the door answered. A refusal and a broken answer both
// end the tunnel; an error code is the door answering normally with a no.
func (s *Source) interpret(tunnel *pair.Tunnel, request, answered byte, body []byte, parse func([]byte) error) error {
	switch answered {
	case typeRefused:
		s.drop(tunnel)
		var no refusal
		if json.Unmarshal(body, &no) != nil || no.Reason == "" {
			no.Reason = couldNotConfirm
		}
		return &RefusedError{Reason: no.Reason}
	case typeError:
		if len(body) != 1 {
			s.drop(tunnel)
			return fmt.Errorf("%w: an error answer of %d bytes", blobstore.ErrUnreachable, len(body))
		}
		switch body[0] {
		case codeBadRID:
			return blobstore.ErrBadRID
		case codeTooMany:
			return blobstore.ErrTooMany
		case codeDamaged:
			return blobstore.ErrDamaged
		case codeInternal:
			return blobstore.ErrAnswered
		}
		s.drop(tunnel)
		return fmt.Errorf("%w: the door did not understand the request", blobstore.ErrUnreachable)
	case request | answerBit:
		if err := parse(body); err != nil {
			s.drop(tunnel)
			return fmt.Errorf("%w: %v", blobstore.ErrUnreachable, err)
		}
		return nil
	}
	s.drop(tunnel)
	return fmt.Errorf("%w: an answer of type %d to a request of type %d", blobstore.ErrUnreachable, answered, request)
}

// roundTrip sends one request and reads the whole answer. The in-flight count
// rises when the request is written and falls when the answer has been read in
// full, so it measures exactly what the tunnel was asked to carry at once.
func (s *Source) roundTrip(ctx context.Context, tunnel *pair.Tunnel, typ byte, body []byte) (answer []byte, answered byte, err error) {
	s.note(func(st *Stats) {
		s.inFlight++
		st.MaxInFlight = max(st.MaxInFlight, s.inFlight)
		st.Requests++
	})
	defer s.note(func(*Stats) { s.inFlight-- })

	stop := context.AfterFunc(ctx, func() { _ = tunnel.Close() })
	defer stop()
	guard := newWatch(silentAfter, func() { _ = tunnel.Close() })
	defer guard.stop()

	// A take shows how far it has got, and gives up on a move whose bytes have
	// stopped arriving, from what the traffic watch in ctx hears.
	traffic := blobstore.TrafficFrom(ctx)
	var in io.Reader = kickReader{tunnel, guard}
	if traffic != nil {
		traffic.Begin()
		defer traffic.End()
		in = trafficRead{r: in, w: traffic}
	}

	sent, err := writeMessage(kickWriter{tunnel, guard}, typ, body)
	if traffic != nil {
		traffic.Moved(int64(sent), 0)
	}
	s.note(func(st *Stats) {
		st.BytesUp += int64(sent)
		st.MaxRequestBytes = max(st.MaxRequestBytes, sent)
	})
	if err != nil {
		return nil, 0, err
	}
	answered, answer, got, err := readMessage(in, maxResponseMessage)
	s.note(func(st *Stats) {
		st.BytesDown += int64(got)
		st.MaxResponseBytes = max(st.MaxResponseBytes, got)
	})
	if err != nil {
		return nil, 0, err
	}
	return answer, answered, nil
}

// trafficRead tells the watch about every read, as bytes that arrived.
type trafficRead struct {
	r io.Reader
	w blobstore.TrafficWatch
}

func (t trafficRead) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.w.Moved(0, int64(n))
	}
	return n, err
}

func (s *Source) current() *pair.Tunnel {
	s.held.Lock()
	defer s.held.Unlock()
	return s.link
}

func (s *Source) drop(tunnel *pair.Tunnel) {
	s.held.Lock()
	if s.link == tunnel {
		s.link = nil
	}
	s.held.Unlock()
	_ = tunnel.Close()
}

// connect returns the live tunnel, dialling one when there is none. A door
// that is not there is redialled with a growing pause until RetryWithin has
// passed, and then the computer is called off.
//
// THE RELAY'S ANSWER TO A NAME NOBODY HOLDS IS NOT A FAULT. While every failure
// so far has been exactly that answer, the window is the shorter
// NoMachineWithin: a computer that is off says nothing else, and the person is
// waiting to be told so. The first failure of any other kind (a busy or
// unreachable relay, a handshake that broke) puts the longer window back, so a
// flaky network is still given its time.
func (s *Source) connect(ctx context.Context) (*pair.Tunnel, error) {
	if tunnel := s.current(); tunnel != nil {
		return tunnel, nil
	}
	start := time.Now()
	var planned time.Duration
	backoff := 500 * time.Millisecond
	onlyNoMachine := true
	for {
		tunnel, err := s.dialOnce(ctx)
		if err == nil {
			s.held.Lock()
			defer s.held.Unlock()
			if s.life.Err() != nil {
				_ = tunnel.Close()
				return nil, ErrClosed
			}
			s.link = tunnel
			return tunnel, nil
		}
		var no *RefusedError
		if errors.As(err, &no) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !errors.Is(err, relay.ErrNoMachine) {
			onlyNoMachine = false
		}
		window := s.cfg.RetryWithin
		if onlyNoMachine {
			window = min(window, s.cfg.NoMachineWithin)
		}
		// Elapsed time counts the pauses it was told to take as well as the
		// clock, so a test that replaces the sleep still ends the window.
		remaining := window - max(time.Since(start), planned)
		if remaining <= 0 {
			return nil, &OffError{DeviceID: s.cfg.TargetID, Name: s.cfg.TargetName, Err: err}
		}
		pause := min(backoff, remaining)
		if err := s.cfg.Sleep(ctx, pause); err != nil {
			return nil, err
		}
		planned += pause
		backoff = min(backoff*2, 2*time.Second)
	}
}

func (s *Source) dialOnce(ctx context.Context) (*pair.Tunnel, error) {
	s.note(func(st *Stats) { st.Dials++ })
	tunnel, err := s.handshake(ctx)
	if err != nil {
		s.note(func(st *Stats) { st.DialFailures++ })
	}
	return tunnel, err
}

// handshake is one attempt, bounded as a whole: the relay, the tunnel and the
// hello. It succeeds only for the computer asked for, as that computer's own
// certificate says, so a name that landed on some other machine ends here.
func (s *Source) handshake(ctx context.Context) (*pair.Tunnel, error) {
	attempt, cancel := context.WithTimeout(ctx, dialWithin)
	defer cancel()
	mine, err := DoorKey(s.cfg.Identity, s.cfg.Device.ID())
	if err != nil {
		return nil, err
	}
	theirs, err := DoorKey(s.cfg.Identity, s.cfg.TargetID)
	if err != nil {
		return nil, err
	}
	stream, err := relay.DialDoor(attempt, s.cfg.Service, relay.NameFor(theirs.PublicKey().Bytes()), relay.DoorMove)
	if err != nil {
		return nil, err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = stream.Close()
		}
	}()
	stop := context.AfterFunc(attempt, func() { _ = stream.Close() })
	defer stop()

	tunnel, err := pair.ConnectWithKeyPrologue(stream, theirs.PublicKey().Bytes(), mine, FilesTunnelPrologue)
	if err != nil {
		return nil, err
	}
	sent, err := sendHello(tunnel, tunnel, s.cfg.Device)
	s.note(func(st *Stats) {
		st.BytesUp += int64(sent)
		st.MaxRequestBytes = max(st.MaxRequestBytes, sent)
	})
	if err != nil {
		return nil, err
	}
	typ, body, got, err := readMessage(tunnel, MaxRequestBytes)
	s.note(func(st *Stats) {
		st.BytesDown += int64(got)
		st.MaxResponseBytes = max(st.MaxResponseBytes, got)
	})
	if err != nil {
		return nil, err
	}
	if err := s.checkAnswer(tunnel, typ, body); err != nil {
		return nil, err
	}
	// A callback that has already run won the race with the timeout, and a
	// tunnel on a closed stream is no tunnel.
	if !stop() || attempt.Err() != nil {
		return nil, errors.New("filesdoor: the dial ran out of time")
	}
	succeeded = true
	return tunnel, nil
}

// checkAnswer reads the door's first message. A refusal is the door's answer
// and is final; an answer from some other computer, or one that is not a hello
// at all, is a door that cannot be trusted with the request.
func (s *Source) checkAnswer(tunnel *pair.Tunnel, typ byte, body []byte) error {
	return checkAnswer(s.cfg.Identity, tunnel, s.cfg.TargetID, s.person(), typ, body)
}

// DialLive opens an authenticated live-view tunnel to host on its files-door
// registration using the live dial class, intent and prologue.
func DialLive(ctx context.Context, service string, id identity.Identity, dev identity.Dev, host string) (io.ReadWriteCloser, error) {
	attempt, cancel := context.WithTimeout(ctx, dialWithin)
	defer cancel()
	keys, err := liveKeys(id, dev, host)
	if err != nil {
		return nil, err
	}
	name, err := DoorName(id, host)
	if err != nil {
		return nil, err
	}
	stream, err := relay.DialDoor(attempt, service, name, relay.DoorLive)
	if err != nil {
		return nil, err
	}
	return sayLive(attempt, stream, id, dev, host, keys, pair.IntentLive, LiveTunnelPrologue)
}

// DialDirect finishes a live-view tunnel over a stream that already exists: the
// byte stream of an open peer-to-peer data channel. It is DialLive for a
// carrier the caller built, with the direct intent and prologue, the same
// door-key handshake against the host's pinned key and the same device hello,
// and it returns a READY tunnel, which is the contract a link's Dial has. The
// stream is closed when the handshake fails, and the dial ends with
// [dialWithin].
func DialDirect(ctx context.Context, stream io.ReadWriteCloser, id identity.Identity, dev identity.Dev, host string) (io.ReadWriteCloser, error) {
	attempt, cancel := context.WithTimeout(ctx, dialWithin)
	defer cancel()
	keys, err := liveKeys(id, dev, host)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	return sayLive(attempt, stream, id, dev, host, keys, pair.IntentDirect, DirectTunnelPrologue)
}

// liveKeysOf are the two door keys a live dial uses: this device's own, and the
// host's, whose public half the handshake pins.
type liveKeysOf struct{ mine, theirs *ecdh.PrivateKey }

func liveKeys(id identity.Identity, dev identity.Dev, host string) (liveKeysOf, error) {
	mine, err := DoorKey(id, dev.ID())
	if err != nil {
		return liveKeysOf{}, err
	}
	theirs, err := DoorKey(id, host)
	if err != nil {
		return liveKeysOf{}, err
	}
	return liveKeysOf{mine: mine, theirs: theirs}, nil
}

// sayLive is the handshake and hello every live dial runs over a stream it has
// been handed, whichever road made the stream. It takes the stream's ownership:
// a failed or timed-out dial closes it.
func sayLive(attempt context.Context, stream io.ReadWriteCloser, id identity.Identity, dev identity.Dev, host string, keys liveKeysOf, intent byte, prologue string) (io.ReadWriteCloser, error) {
	succeeded := false
	defer func() {
		if !succeeded {
			_ = stream.Close()
		}
	}()
	stop := context.AfterFunc(attempt, func() { _ = stream.Close() })
	defer stop()

	tunnel, err := pair.ConnectWithKeyIntent(stream, keys.theirs.PublicKey().Bytes(), keys.mine, intent, prologue)
	if err != nil {
		return nil, err
	}
	if err := SayHello(tunnel, id, dev, host); err != nil {
		return nil, err
	}
	if !stop() || attempt.Err() != nil {
		return nil, errors.New("filesdoor: the dial ran out of time")
	}
	succeeded = true
	return tunnel, nil
}

func (s *Source) person() string {
	if s.cfg.TargetName != "" {
		return s.cfg.TargetName
	}
	return s.cfg.TargetID
}

var _ blobstore.Store = (*Source)(nil)
var _ io.Closer = (*Source)(nil)
