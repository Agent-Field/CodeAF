package pairbox

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// Memory is the mailbox service: the real thing on a self-hosted relay and the
// fake every test builds against, because a fake with other semantics would
// prove nothing about the real one.
type Memory struct {
	limits Limits
	now    func() time.Time

	mu    sync.Mutex
	boxes map[string]*mailbox
	bytes int
	peers map[string]*budget
}

// NewMemory builds a mailbox service under limits and a clock; a nil clock is
// the wall clock.
func NewMemory(limits Limits, now func() time.Time) *Memory {
	if now == nil {
		now = time.Now
	}
	return &Memory{limits: limits, now: now, boxes: map[string]*mailbox{}, peers: map[string]*budget{}}
}

// mailbox is one pairing: two sides, each claimed by its first write and owned
// by the digest of the key given with it.
type mailbox struct {
	expires time.Time
	digest  [2][32]byte
	claimed [2]bool
	msgs    [2][][]byte
	// changed is closed and replaced whenever something a poll waits on moves.
	changed chan struct{}
}

func newMailbox(expires time.Time) *mailbox {
	return &mailbox{expires: expires, changed: make(chan struct{})}
}

// wake releases every poll waiting on this mailbox to look again.
func (b *mailbox) wake() {
	close(b.changed)
	b.changed = make(chan struct{})
}

// claim gives side i to key, and is the only place a side changes hands.
func (b *mailbox) claim(i int, k Key) {
	b.digest[i], b.claimed[i] = k.digest(), true
}

// holds reports whether k is the key side i was claimed with.
func (b *mailbox) holds(i int, k Key) bool {
	d := k.digest()
	return b.claimed[i] && subtle.ConstantTimeCompare(b.digest[i][:], d[:]) == 1
}

// authorise lets k write to side i: the first writer claims an unclaimed side,
// and after that only the same key gets in.
func (b *mailbox) authorise(i int, k Key) error {
	if !b.claimed[i] {
		b.claim(i, k)
		return nil
	}
	if !b.holds(i, k) {
		return ErrForbidden
	}
	return nil
}

func (b *mailbox) size() int {
	n := 0
	for _, side := range b.msgs {
		for _, m := range side {
			n += len(m)
		}
	}
	return n
}

// Limits answers the numbers this service enforces.
func (m *Memory) Limits(context.Context) (Limits, error) { return m.limits, nil }

// Create opens a mailbox for the network in ctx.
func (m *Memory) Create(ctx context.Context, key Key) (Created, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.budget(ctx).allowCreate(m.now(), m.limits); err != nil {
		return Created{}, err
	}
	m.sweep()
	if len(m.boxes) >= m.limits.MaxBoxes {
		return Created{}, ErrRelayFull
	}
	plate, err := m.allocate()
	if err != nil {
		return Created{}, err
	}
	box := newMailbox(m.now().Add(m.limits.TTL))
	box.claim(SideA.index(), key)
	m.boxes[plate] = box
	return Created{Nameplate: plate, ExpiresIn: m.limits.TTL}, nil
}

// Post appends a message to one side.
func (m *Memory) Post(ctx context.Context, plate string, side Side, key Key, msg []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.budget(ctx).allowWrite(m.now(), m.limits); err != nil {
		return 0, err
	}
	box, err := m.live(plate)
	if err != nil {
		return 0, err
	}
	if len(msg) > m.limits.MaxMsg {
		return 0, ErrTooBig
	}
	return m.append(box, side, key, msg)
}

// append is the part of Post that changes the mailbox, after every refusal that
// needs no state has been made.
func (m *Memory) append(box *mailbox, side Side, key Key, msg []byte) (int, error) {
	i := side.index()
	if err := box.authorise(i, key); err != nil {
		return 0, err
	}
	if len(box.msgs[i]) >= m.limits.MaxMsgsPerSide {
		return 0, ErrSideFull
	}
	if m.bytes+len(msg) > m.limits.MaxBytes {
		return 0, ErrRelayFull
	}
	box.msgs[i] = append(box.msgs[i], append([]byte(nil), msg...))
	m.bytes += len(msg)
	box.wake()
	return len(box.msgs[i]) - 1, nil
}

// Poll answers what a side has written after a position, holding the call open
// while there is nothing.
func (m *Memory) Poll(ctx context.Context, plate string, side Side, after int, wait time.Duration) (Batch, error) {
	release, err := m.enterPoll(ctx)
	if err != nil {
		return Batch{}, err
	}
	defer release()
	after = max(after, 0)
	timer := time.NewTimer(min(wait, MaxWait))
	defer timer.Stop()
	for {
		batch, changed, err := m.read(plate, side, after)
		if err != nil || len(batch.Msgs) > 0 {
			return batch, err
		}
		select {
		case <-changed:
		case <-timer.C:
			return batch, nil
		case <-ctx.Done():
			return Batch{}, ctx.Err()
		}
	}
}

// read is one look at a side, with the channel that says when to look again.
func (m *Memory) read(plate string, side Side, after int) (Batch, <-chan struct{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	box, err := m.live(plate)
	if err != nil {
		return Batch{}, nil, err
	}
	have := box.msgs[side.index()]
	if after >= len(have) {
		return Batch{Next: after}, box.changed, nil
	}
	fresh := make([][]byte, 0, len(have)-after)
	for _, msg := range have[after:] {
		fresh = append(fresh, append([]byte(nil), msg...))
	}
	return Batch{Msgs: fresh, Next: len(have)}, box.changed, nil
}

// Delete removes a mailbox for the holder of either side's key.
func (m *Memory) Delete(_ context.Context, plate string, key Key) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	box, err := m.live(plate)
	if err != nil {
		return err
	}
	if !box.holds(SideA.index(), key) && !box.holds(SideB.index(), key) {
		return ErrForbidden
	}
	m.drop(plate)
	return nil
}

// live is the mailbox under a nameplate, or ErrGone once it has expired. An
// expired mailbox is removed here, so reading it is what frees it.
func (m *Memory) live(plate string) (*mailbox, error) {
	box, ok := m.boxes[plate]
	if !ok {
		return nil, ErrGone
	}
	if !m.now().Before(box.expires) {
		m.drop(plate)
		return nil, ErrGone
	}
	return box, nil
}

// drop removes a mailbox, gives its bytes back and wakes its polls to a 404.
func (m *Memory) drop(plate string) {
	box := m.boxes[plate]
	m.bytes -= box.size()
	delete(m.boxes, plate)
	box.wake()
}

// sweep drops every expired mailbox, so an abandoned pairing costs nothing
// once the next one is made.
func (m *Memory) sweep() {
	for plate := range m.boxes {
		_, _ = m.live(plate)
	}
	for peer, b := range m.peers {
		if b.idle(m.now()) {
			delete(m.peers, peer)
		}
	}
}

// allocate draws an unused nameplate with as few digits as keep the live
// mailboxes under a tenth of the space, so a guess at one rarely hits a box.
func (m *Memory) allocate() (string, error) {
	digits := plateDigits(len(m.boxes) + 1)
	space := big.NewInt(1)
	for range digits {
		space.Mul(space, big.NewInt(10))
	}
	for range 64 {
		n, err := rand.Int(rand.Reader, space)
		if err != nil {
			return "", err
		}
		if plate := fmt.Sprintf("%0*d", digits, n); m.boxes[plate] == nil {
			return plate, nil
		}
	}
	return "", ErrRelayFull
}

// plateDigits is the smallest width, from two to four, whose space is ten
// times the live count.
func plateDigits(live int) int {
	for digits, space := 2, 100; digits < 4; digits, space = digits+1, space*10 {
		if live*10 <= space {
			return digits
		}
	}
	return 4
}
