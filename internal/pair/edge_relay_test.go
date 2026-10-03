package pair

// Contract 18.9, the rows about the relay: one that loses, repeats, reorders or
// changes messages, one that sits in the middle, and one that is not there, is
// too old, or has been restarted. The relay is untrusted for the bytes it
// carries and trusted for nothing.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pair/cpace"
	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// attempt is one pairing under way: A showing a code, B typing it.
type attempt struct {
	a     *screen
	offer *running
	join  <-chan joinEnd
	code  *Code
}

// startPair runs both devices at once, under one deadline each.
func startPair(t *testing.T, r *chatRig, deadline time.Duration) *attempt {
	t.Helper()
	return startPairWith(t, r, deadline, deadline)
}

// startPairWith gives the two devices deadlines of their own. A device whose
// deadline comes first takes its mailbox with it, so a test that wants the other
// one to end by its own clock gives the other the shorter deadline.
func startPairWith(t *testing.T, r *chatRig, offering, joining time.Duration) *attempt {
	t.Helper()
	a := newScreen()
	ctxA, stopA := context.WithTimeout(context.Background(), offering)
	t.Cleanup(stopA)
	att := &attempt{a: a, offer: startOffer(t, r, ctxA, Grant{Identity: r.a}, a)}
	att.code = a.nextCode(t)
	att.join = joinInBackground(bounded(t, joining), r, r.homeB, att.code.Shown(), newCountingJoin())
	return att
}

// A RELAY THAT LOSES A MESSAGE LEAVES BOTH DEVICES WAITING FOR IT, and each says
// the pairing did not finish. Nothing lands on the joining device, and no
// grant leaves the other one unless message 4 itself is the one that was lost.
func TestRelayDrops(t *testing.T) {
	for n := 1; n <= 4; n++ {
		t.Run(fmt.Sprintf("message %d", n), func(t *testing.T) {
			r, hostile := hostileRig(t)
			hostile.on(n, swallow)
			att := startPairWith(t, r, 1500*time.Millisecond, 800*time.Millisecond)

			b := joinWithin(t, att.join)
			a := att.offer.wait(t)
			for name, err := range map[string]error{"A": a.err, "B": b.err} {
				if !errors.Is(err, ErrDidNotFinish) || err.Error() != ErrDidNotFinish.Error() {
					t.Errorf("%s ended with %v, want the pairing did not finish", name, err)
				}
			}
			untouched(t, r.homeB)
			if got := burns(att.a); got != 0 {
				t.Errorf("a lost message burned %d codes", got)
			}
			if sent := len(hostile.wroteBy(pairbox.SideA)) - 1; sent > 0 && n != 4 {
				t.Errorf("a grant was sent although message %d never arrived", n)
			}
		})
	}
}

// A RELAY THAT SAYS A MESSAGE TWICE, OR HANDS AN OLD ANSWER OUT AGAIN, IS NOT
// ALLOWED TO MAKE A SECOND STEP OUT OF IT. The exchange stops at the message
// that is out of step.
func TestRelayReplays(t *testing.T) {
	t.Run("a message written twice", func(t *testing.T) {
		r, hostile := hostileRig(t)
		hostile.on(2, twice)
		b, a := finish(t, r)
		wantWrongCode(t, b)
		wantNoSuccess(t, a)
	})
	t.Run("an old answer handed to B again", func(t *testing.T) {
		r, hostile := hostileRig(t)
		hostile.replayBatches(pairbox.SideA)
		b, a := finish(t, r)
		wantWrongCode(t, b)
		wantNoSuccess(t, a)
	})
	t.Run("an old message handed to A again", func(t *testing.T) {
		r, hostile := hostileRig(t)
		hostile.replayBatches(pairbox.SideB)
		att := startPair(t, r, 20*time.Second)
		att.a.nextCode(t) // the burn is followed by a new code
		if got := burns(att.a); got != 1 {
			t.Fatalf("A was told %d codes burned, want 1", got)
		}
		att.offer.stop()
		if b := joinWithin(t, att.join); b.err == nil {
			t.Fatal("B was paired by a relay that repeated itself")
		}
		untouched(t, r.homeB)
	})
}

// finish runs one pairing until B has ended, and then stops A.
func finish(t *testing.T, r *chatRig) (joinEnd, offerEnd) {
	t.Helper()
	att := startPair(t, r, 20*time.Second)
	b := joinWithin(t, att.join)
	att.offer.stop()
	return b, att.offer.wait(t)
}

func wantWrongCode(t *testing.T, b joinEnd) {
	t.Helper()
	if !errors.Is(b.err, ErrCodeDidNotWork) {
		t.Fatalf("B was told %v, want that code did not work", b.err)
	}
}

// wantNoSuccess is A never saying a pairing worked when it did not.
func wantNoSuccess(t *testing.T, a offerEnd) {
	t.Helper()
	if a.err == nil {
		t.Fatalf("A said it paired %q", a.label)
	}
}

// A MESSAGE COPIED OUT OF ONE PAIRING AND PUT INTO ANOTHER MAILBOX GETS NOWHERE,
// and the reason is in the context: the nameplate is part of what both devices
// mix in before anything is said.
func TestReplayAcrossMailboxes(t *testing.T) {
	t.Run("a recorded joining device in a fresh mailbox", func(t *testing.T) {
		r, hostile := hostileRig(t)
		att := startPair(t, r, 20*time.Second)
		if b := joinWithin(t, att.join); b.err != nil {
			t.Fatal(b.err)
		}
		att.offer.wait(t)
		recorded := hostile.wroteBy(pairbox.SideB)

		if err := replayInto(t, r, att.code, recorded[:2]); !errors.Is(err, ErrWrongCode) {
			t.Fatalf("the replayed messages were read as %v, want a wrong code", err)
		}
	})
	t.Run("the nameplate is part of the context", func(t *testing.T) {
		for _, c := range []struct {
			joiner, offerer string
			opens           bool
		}{{"42", "42", true}, {"42", "43", false}, {"7", "77", false}} {
			joiner, offerer := talk(chatScheme(c.joiner), chatScheme(c.offerer), "715302")
			if opened := joiner == nil && offerer == nil; opened != c.opens {
				t.Errorf("plates %s and %s: opened %v (%v, %v), want %v", c.joiner, c.offerer, opened, joiner, offerer, c.opens)
			}
		}
	})
}

// replayInto writes a recorded joining device's messages into a new mailbox and
// lets the offering device's half read them, under the digits that made them.
func replayInto(t *testing.T, r *chatRig, spent *Code, messages [][]byte) error {
	t.Helper()
	ctx := bounded(t, 10*time.Second)
	keyA, _ := pairbox.NewKey()
	keyB, _ := pairbox.NewKey()
	made, err := r.box.Create(ctx, keyA)
	for err == nil && made.Nameplate == spent.Plate() {
		// A fresh mailbox is one that is not under the plate the messages were made for.
		_ = r.box.Delete(ctx, made.Nameplate, keyA)
		made, err = r.box.Create(ctx, keyA)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range messages {
		if _, err := r.box.Post(ctx, made.Nameplate, pairbox.SideB, keyB, m); err != nil {
			t.Fatal(err)
		}
	}
	link := &boxLink{ctx: ctx, box: r.box, plate: made.Nameplate, mine: pairbox.SideA, theirs: pairbox.SideB, key: keyA}
	_, err = answer(link, chatScheme(made.Nameplate), spent.secret())
	return err
}

// talk runs the introduction between two schemes over a pipe and says how each
// half ended. A half whose far end gave up is closed rather than left waiting.
func talk(joiner, offerer scheme, secret string) (joinErr, offerErr error) {
	near, far := net.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, offerErr = answer(streamLink{far}, offerer, secret)
		_ = far.Close()
	}()
	_, joinErr = begin(streamLink{near}, joiner, secret, []byte("laptop-0123456789abcdef"))
	_ = near.Close()
	wg.Wait()
	return joinErr, offerErr
}

// A RELAY THAT SWAPS TWO MESSAGES OF ONE SIDE HAS THEM READ OUT OF STEP. What A
// reads first is not the message it waits for, so the attempt is spent.
func TestRelayReorders(t *testing.T) {
	r, hostile := hostileRig(t)
	hostile.on(1, holdBack)
	a := newScreen()
	offerAs(t, r, a)
	code := a.nextCode(t)

	digits := code.secret()
	first, _, err := cpace.Start(digits, chatScheme(code.Plate()).pake)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := pairbox.NewKey()
	for _, m := range [][]byte{first, []byte("the second message of B")} {
		if _, err := hostile.Post(context.Background(), code.Plate(), pairbox.SideB, key, m); err != nil {
			t.Fatal(err)
		}
	}
	next := a.nextCode(t)
	if got := burns(a); got != 1 || !differs(code, next) {
		t.Fatalf("out-of-order messages burned %d codes, new code differs: %v", got, differs(code, next))
	}
}

// A RELAY THAT CHANGES ONE BYTE OF ANY MESSAGE ENDS THE PAIRING ON THE WRONG-CODE
// ROAD: the joining device is told the code did not work and holds nothing, and
// the offering device never says a pairing worked.
func TestRelayAlters(t *testing.T) {
	for _, c := range []struct {
		n int
		// early messages may fail on either road, because a changed point is
		// sometimes no point at all and sometimes another one.
		also error
	}{{1, ErrStopped}, {2, nil}, {3, nil}, {4, nil}} {
		t.Run(fmt.Sprintf("message %d", c.n), func(t *testing.T) {
			r, hostile := hostileRig(t)
			hostile.on(c.n, flipped)
			att := startPair(t, r, 20*time.Second)

			b := joinWithin(t, att.join)
			if !errors.Is(b.err, ErrCodeDidNotWork) && (c.also == nil || !errors.Is(b.err, c.also)) {
				t.Fatalf("B was told %v, want that code did not work", b.err)
			}
			if c.n == 3 {
				att.a.nextCode(t) // A burned it and offered a new one
				if got := burns(att.a); got != 1 {
					t.Fatalf("A was told %d codes burned, want 1", got)
				}
			}
			att.offer.stop()
			wantNoSuccess(t, att.offer.wait(t))
			untouched(t, r.homeB)
		})
	}
}

// A CODE THAT BURNS IS NEVER FOLLOWED BY A MAILBOX UNDER THE SAME PLATE. The
// relay draws short plates at random, so the new mailbox can land on the one
// just deleted; the joining device, still waiting there for the answer to its
// guess, would then find a live empty mailbox instead of the word that the old
// one is gone, and wait on it until its own time ran out.
func TestABurnedPlateIsNotMadeAgain(t *testing.T) {
	inner := newHostile(pairbox.NewMemory(roomy(), nil))
	// Message 1 that is no point at all, so the code burns the moment it is read.
	inner.on(1, func(h *hostileBox, w write) (int, error) {
		w.msg = bytes.Repeat([]byte{0xff}, len(w.msg))
		return h.store(w)
	})
	box := &unluckyBox{Box: inner}
	r := newChatRig(t).via(box)
	att := startPair(t, r, 20*time.Second)

	b := joinWithin(t, att.join)
	if !errors.Is(b.err, ErrStopped) && !errors.Is(b.err, ErrCodeDidNotWork) {
		t.Fatalf("B was told %v, want an ending", b.err)
	}
	if next := att.a.nextCode(t); next.Plate() == att.code.Plate() {
		t.Fatalf("the code that replaced %s was made under the same plate", att.code.Plate())
	}
	att.offer.stop()
	wantNoSuccess(t, att.offer.wait(t))
}

// unluckyBox is a relay whose second mailbox lands on the plate of the first,
// the draw a short plate space gives about one time in a hundred.
type unluckyBox struct {
	pairbox.Box
	mu    sync.Mutex
	made  int
	first string
}

func (u *unluckyBox) Create(ctx context.Context, key pairbox.Key) (pairbox.Created, error) {
	u.mu.Lock()
	u.made++
	n, first := u.made, u.first
	u.mu.Unlock()
	for {
		made, err := u.Box.Create(ctx, key)
		if err != nil || n != 2 || made.Nameplate == first {
			if err == nil && n == 1 {
				u.mu.Lock()
				u.first = made.Nameplate
				u.mu.Unlock()
			}
			return made, err
		}
		_ = u.Box.Delete(ctx, made.Nameplate, key)
	}
}

// ── a relay that sits in the middle ─────────────────────────────────────────

// mitmBox is a relay that runs a device of its own against each real one: a
// joining device against the real offering device, and an offering device
// against the real joining one. It does not know the digits, so it guesses.
//
// The two real devices reach it through the usual Box; it keeps one mailbox per
// direction, each recording everything written to it.
type mitmBox struct {
	ctx         context.Context
	left, right *hostileBox
	guess       string

	mu    sync.Mutex
	plate string
	other string
	once  sync.Once
}

func newMitm(ctx context.Context) *mitmBox {
	return &mitmBox{ctx: ctx, left: newHostile(pairbox.NewMemory(roomy(), nil)),
		right: newHostile(pairbox.NewMemory(roomy(), nil)), guess: "123456"}
}

var _ pairbox.Box = (*mitmBox)(nil)

// toward is the mailbox a call is really made against: the second one for what
// the joining device does, and the first for everything else.
func (m *mitmBox) toward(plate string, joiningDevice bool) (*hostileBox, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if joiningDevice && plate == m.plate {
		return m.right, m.other
	}
	return m.left, plate
}

func (m *mitmBox) Limits(ctx context.Context) (pairbox.Limits, error) { return m.left.Limits(ctx) }

func (m *mitmBox) Create(ctx context.Context, key pairbox.Key) (pairbox.Created, error) {
	made, err := m.left.Create(ctx, key)
	if err == nil {
		m.once.Do(func() { m.impersonate(made.Nameplate) })
	}
	return made, err
}

func (m *mitmBox) Post(ctx context.Context, plate string, side pairbox.Side, key pairbox.Key, msg []byte) (int, error) {
	box, at := m.toward(plate, side == pairbox.SideB)
	return box.Post(ctx, at, side, key, msg)
}

func (m *mitmBox) Poll(ctx context.Context, plate string, side pairbox.Side, after int, wait time.Duration) (pairbox.Batch, error) {
	box, at := m.toward(plate, side == pairbox.SideA)
	return box.Poll(ctx, at, side, after, wait)
}

func (m *mitmBox) Delete(ctx context.Context, plate string, key pairbox.Key) error {
	return m.left.Delete(ctx, plate, key)
}

// impersonate starts the relay's own two devices for the first mailbox made.
func (m *mitmBox) impersonate(plate string) {
	key, _ := pairbox.NewKey()
	made, err := m.right.Create(m.ctx, key)
	if err != nil {
		return
	}
	m.mu.Lock()
	m.plate, m.other = plate, made.Nameplate
	m.mu.Unlock()
	go m.asOfferer(made.Nameplate, plate, key)
	go m.asJoiner(plate)
}

// asJoiner is the relay's joining device, facing the real offering device.
func (m *mitmBox) asJoiner(plate string) {
	key, _ := pairbox.NewKey()
	link := &boxLink{ctx: m.ctx, box: m.left, plate: plate, mine: pairbox.SideB, theirs: pairbox.SideA, key: key}
	offer, _ := joinOffer("relay")
	_, _ = begin(link, chatScheme(plate), m.guess, offer)
}

// asOfferer is the relay's offering device, facing the real joining device. It
// cannot open what arrives, and ends the pairing as a device that did.
func (m *mitmBox) asOfferer(at, plate string, key pairbox.Key) {
	link := &boxLink{ctx: m.ctx, box: m.right, plate: at, mine: pairbox.SideA, theirs: pairbox.SideB, key: key}
	_, _ = answer(link, chatScheme(plate), m.guess)
	_ = m.right.Delete(m.ctx, at, key)
}

func (m *mitmBox) recorded() []byte {
	return append(m.left.everything(), m.right.everything()...)
}

// A RELAY THAT RUNS A DEVICE OF ITS OWN AGAINST EACH REAL ONE, WITHOUT THE
// DIGITS, GETS NOTHING: both real devices stop on the wrong-code road, and
// nothing the relay ever held contains the identity or a secret of it.
func TestMitmRelay(t *testing.T) {
	ctx := bounded(t, 30*time.Second)
	mitm := newMitm(ctx)
	r := newChatRig(t).via(mitm)
	a := newScreen()
	offer := offerAs(t, r, a)
	code := a.nextCode(t)

	_, err := r.join(ctx, r.homeB, code.Shown(), newCountingJoin())
	if !errors.Is(err, ErrCodeDidNotWork) {
		t.Fatalf("B was told %v, want that code did not work", err)
	}
	a.nextCode(t) // A burned the code and offered another
	if got := burns(a); got != 1 {
		t.Fatalf("A was told %d codes burned, want 1", got)
	}
	offer.stop()
	wantNoSuccess(t, offer.wait(t))
	untouched(t, r.homeB)

	if len(mitm.left.wroteBy(pairbox.SideB)) == 0 || len(mitm.right.wroteBy(pairbox.SideA)) == 0 {
		t.Fatal("the relay's own devices never spoke, so this test proved nothing")
	}
	secrets := secretsOf(t, r.a)
	recorded := mitm.recorded()
	if n := leaks(recorded, secrets); n != 0 {
		t.Fatalf("%d secrets of the identity appear in what the relay held", n)
	}
	if leaks(append(recorded, secrets[0]...), secrets) == 0 {
		t.Fatal("the search cannot see a secret that is there")
	}
}

// AN HONEST RUN LEAVES THE IDENTITY OUT OF WHAT THE RELAY HELD TOO. The grant
// crosses it, and only as ciphertext.
func TestRelayNeverHoldsTheIdentity(t *testing.T) {
	r, hostile := hostileRig(t)
	att := startPair(t, r, 20*time.Second)
	if b := joinWithin(t, att.join); b.err != nil {
		t.Fatal(b.err)
	}
	att.offer.wait(t)
	if len(hostile.wroteBy(pairbox.SideA)) < 2 {
		t.Fatal("no grant crossed the relay")
	}
	if n := leaks(hostile.everything(), secretsOf(t, r.a)); n != 0 {
		t.Fatalf("%d secrets of the identity appear in what the relay held", n)
	}
	if bytes.Contains(hostile.everything(), []byte("signing_seed")) {
		t.Fatal("the grant crossed the relay as plain text")
	}
}

// ── a relay that is not there ───────────────────────────────────────────────

// A RELAY FROM BEFORE PAIRING IS SAID TO BE TOO OLD BY BOTH DEVICES, and neither
// makes a mailbox or shows a code.
func TestRelayTooOld(t *testing.T) {
	r, hostile := hostileRig(t)
	hostile.failLimits(pairbox.ErrTooOld)

	a := newScreen()
	_, err := Offer(bounded(t, 5*time.Second), r.route, Grant{Identity: r.a}, a)
	if !errors.Is(err, ErrRelayTooOld) || err.Error() != ErrRelayTooOld.Error() {
		t.Fatalf("A was told %v", err)
	}
	_, err = r.join(bounded(t, 5*time.Second), r.homeB, "42-715-302", newCountingJoin())
	if !errors.Is(err, ErrRelayTooOld) {
		t.Fatalf("B was told %v", err)
	}
	if n := hostile.calledTimes("Create"); n != 0 {
		t.Fatalf("%d mailboxes were made on a relay that cannot serve them", n)
	}
	if len(a.codes) != 0 {
		t.Fatal("a code was shown that no relay could serve")
	}
	untouched(t, r.homeB)
}

// A RELAY THAT DOES NOT ANSWER IS NAMED BY ITS HOST, and the offering device
// shows no code it could not serve.
func TestRelayUnreachable(t *testing.T) {
	const want = "cannot reach relay.test \u2014 check your internet connection and try again"
	t.Run("at the first question", func(t *testing.T) {
		r, hostile := hostileRig(t)
		hostile.failLimits(unreachable())
		a := newScreen()
		_, err := Offer(bounded(t, 5*time.Second), r.route, Grant{Identity: r.a}, a)
		wantSentence(t, err, ErrCannotReach, want)
		_, err = r.join(bounded(t, 5*time.Second), r.homeB, "42-715-302", newCountingJoin())
		wantSentence(t, err, ErrCannotReach, want)
		if len(a.codes) != 0 || hostile.calledTimes("Create") != 0 {
			t.Fatal("a code was made for a relay that cannot be reached")
		}
	})
	t.Run("when the mailbox is made", func(t *testing.T) {
		r, hostile := hostileRig(t)
		hostile.failCreate(unreachable())
		a := newScreen()
		_, err := Offer(bounded(t, 5*time.Second), r.route, Grant{Identity: r.a}, a)
		wantSentence(t, err, ErrCannotReach, want)
		if len(a.codes) != 0 {
			t.Fatal("a code was shown that no mailbox stood behind")
		}
	})
}

func wantSentence(t *testing.T, err, is error, text string) {
	t.Helper()
	if !errors.Is(err, is) || err == nil || err.Error() != text {
		t.Fatalf("got %v, want %q", err, text)
	}
}

// A RELAY THAT WAS RESTARTED HAS FORGOTTEN EVERY PAIRING. The joining device
// that was waiting to be answered finds the mailbox gone and says the other
// device stopped; the offering device says the pairing did not finish.
func TestRelayRestart(t *testing.T) {
	t.Run("B is waiting to be answered", func(t *testing.T) {
		r, hostile := hostileRig(t)
		hostile.on(1, swallow)
		a := newScreen()
		offer := offerAs(t, r, a)
		code := a.nextCode(t)
		joined := joinInBackground(bounded(t, 20*time.Second), r, r.homeB, code.Shown(), newCountingJoin())
		<-hostile.reachedMessage(1)
		hostile.restart(pairbox.NewMemory(roomy(), nil))

		if end := joinWithin(t, joined); !errors.Is(end.err, ErrStopped) {
			t.Fatalf("B was told %v, want the other device stopped pairing", end.err)
		}
		if end := offer.wait(t); !errors.Is(end.err, ErrDidNotFinish) {
			t.Fatalf("A ended with %v, want the pairing did not finish", end.err)
		}
		untouched(t, r.homeB)
	})
	t.Run("B is waiting for a person", func(t *testing.T) {
		r, hostile := hostileRig(t)
		release := make(chan struct{})
		a := newScreen()
		a.answer = func(ctx context.Context, _, _ string) bool {
			select {
			case <-release:
				return true
			case <-ctx.Done():
				return false
			}
		}
		offer := offerAs(t, r, a)
		code := a.nextCode(t)
		b := newCountingJoin()
		joined := joinInBackground(bounded(t, 20*time.Second), r, r.homeB, code.Shown(), b)
		<-b.words // B shows its words while A's person decides
		hostile.restart(pairbox.NewMemory(roomy(), nil))
		close(release)

		if end := joinWithin(t, joined); !errors.Is(end.err, ErrCodeDidNotWork) {
			t.Fatalf("B was told %v", end.err)
		}
		if end := offer.wait(t); !errors.Is(end.err, ErrDidNotFinish) {
			t.Fatalf("A ended with %v, want the pairing did not finish", end.err)
		}
		untouched(t, r.homeB)
	})
}

// A NETWORK THAT ASKS FOR TOO MANY CODES IS TOLD HOW LONG TO WAIT, in minutes.
func TestOfferRateLimited(t *testing.T) {
	t.Run("the relay says seven minutes", func(t *testing.T) {
		r, hostile := hostileRig(t)
		hostile.failCreate(pairbox.RateLimited{RetryAfter: 7 * time.Minute})
		a := newScreen()
		_, err := Offer(bounded(t, 5*time.Second), r.route, Grant{Identity: r.a}, a)
		wantSentence(t, err, ErrTooManyPairings, "too many pairings from this network; wait 7 min")
		if len(a.codes) != 0 {
			t.Fatal("a code was shown after the relay said no")
		}
	})
	t.Run("a real service with a budget of one", func(t *testing.T) {
		limits := roomy()
		limits.CreatePerHour = 1
		r := newChatRig(t).via(pairbox.NewMemory(limits, nil))
		first := newScreen()
		ctx, stop := context.WithCancel(context.Background())
		offer := startOffer(t, r, ctx, Grant{Identity: r.a}, first)
		first.nextCode(t)
		stop()
		offer.wait(t)

		_, err := Offer(bounded(t, 5*time.Second), r.route, Grant{Identity: r.a}, newScreen())
		if !errors.Is(err, ErrTooManyPairings) {
			t.Fatalf("the second code got %v", err)
		}
	})
}
