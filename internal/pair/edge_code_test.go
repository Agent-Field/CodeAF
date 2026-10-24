package pair

// Contract 18.9, the rows about the code itself: typing it wrong, typing it for
// a mailbox that is not there, using it twice, racing for it, and the two
// screens that show it.

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// A WRONG CODE COSTS THE PERSON NOTHING BUT A NEW CODE, AND THEY DO NOT ASK FOR
// IT. The old mailbox is gone before the new one is made, so a second guess at
// it has nothing to land on.
func TestWrongCodeBurnsAndReminds(t *testing.T) {
	r := newChatRig(t)
	a := &burnWatch{screen: newScreen()}
	var afterBurn struct {
		sync.Mutex
		plate string
		gone  bool
	}
	a.whenBurned(func() {
		afterBurn.Lock()
		defer afterBurn.Unlock()
		afterBurn.gone = gone(r.box, afterBurn.plate)
	})
	offerAs(t, r, a)
	first := a.nextCode(t)
	afterBurn.Lock()
	afterBurn.plate = first.Plate()
	afterBurn.Unlock()

	_, err := r.join(bounded(t, 10*time.Second), r.homeB, typedWith(first, otherDigits(first)), newCountingJoin())
	if !errors.Is(err, ErrCodeDidNotWork) || err.Error() != ErrCodeDidNotWork.Error() {
		t.Fatalf("B was told %v", err)
	}
	second := a.nextCode(t) // no key was pressed on A
	if got := burns(a.screen); got != 1 {
		t.Fatalf("A was told %d codes burned, want 1", got)
	}
	if !differs(first, second) {
		t.Fatal("the code that replaced the burned one is the same code")
	}
	afterBurn.Lock()
	defer afterBurn.Unlock()
	if !afterBurn.gone {
		t.Fatal("the burned code's mailbox still answered when the burn was announced")
	}
	untouched(t, r.homeB)
}

// A NAMEPLATE NOTHING WAITS UNDER AND ONE THAT RAN OUT ARE THE SAME SENTENCE,
// because the person's next step is the same: ask for a new code.
func TestUnknownNameplate(t *testing.T) {
	t.Run("nothing waits there", func(t *testing.T) {
		r := newChatRig(t)
		_, err := r.join(bounded(t, 10*time.Second), r.homeB, "77-123-456", newCountingJoin())
		wantNothingWaiting(t, err, "77")
		untouched(t, r.homeB)
	})
	t.Run("it expired", func(t *testing.T) {
		r, clock := clockRig(t)
		a := newScreen()
		offer := offerAs(t, r, a)
		code := a.nextCode(t)
		clock.Advance(pairbox.DefaultLimits.TTL + time.Second)

		_, err := r.join(bounded(t, 10*time.Second), r.homeB, code.Shown(), newCountingJoin())
		wantNothingWaiting(t, err, code.Plate())
		if end := offer.wait(t); !errors.Is(end.err, ErrDidNotFinish) {
			t.Fatalf("A ended with %v, want the pairing did not finish", end.err)
		}
		untouched(t, r.homeB)
	})
}

func wantNothingWaiting(t *testing.T, err error, plate string) {
	t.Helper()
	if !errors.Is(err, ErrNothingWaiting) {
		t.Fatalf("got %v, want nothing waiting", err)
	}
	if want := "no pairing is waiting under " + plate; err.Error() != want {
		t.Fatalf("the sentence is %q, want %q", err, want)
	}
}

// ONE CODE PAIRS ONE DEVICE. When it has done that its mailbox is gone, a
// second device is told nothing waits there, and the offering guard is free for
// the next code.
func TestCodeSingleUse(t *testing.T) {
	r := newChatRig(t)
	a := newScreen()
	offer := offerAs(t, r, a)
	code := a.nextCode(t)
	if _, err := r.join(bounded(t, 20*time.Second), r.homeB, code.Shown(), newScreen()); err != nil {
		t.Fatal(err)
	}
	if end := offer.wait(t); end.err != nil {
		t.Fatalf("A ended with %v", end.err)
	}

	second := t.TempDir()
	_, err := r.join(bounded(t, 10*time.Second), second, code.Shown(), newCountingJoin())
	wantNothingWaiting(t, err, code.Plate())
	untouched(t, second)

	fresh := newScreen()
	offerAs(t, r, fresh)
	if next := fresh.nextCode(t); !differs(code, next) {
		t.Log("the next code happens to equal the spent one; the mailbox behind it is a new one")
	}
}

// A DEVICE THAT QUITS BEFORE THE JOINING ONE HAS BEEN ANSWERED TAKES ITS MAILBOX
// WITH IT. The joining device's poll finds it gone and says the other device
// stopped, and one that comes later finds nothing waiting.
func TestAQuitsMidPair(t *testing.T) {
	t.Run("B is waiting to be answered", func(t *testing.T) {
		r, hostile := hostileRig(t)
		hostile.on(1, swallow) // A never hears B, so B is left waiting for the answer
		a := newScreen()
		ctx, stop := context.WithCancel(context.Background())
		offer := startOffer(t, r, ctx, Grant{Identity: r.a}, a)
		code := a.nextCode(t)

		joined := joinInBackground(bounded(t, 20*time.Second), r, r.homeB, code.Shown(), newCountingJoin())
		<-hostile.reachedMessage(1)
		stop()

		if end := joinWithin(t, joined); !errors.Is(end.err, ErrStopped) {
			t.Fatalf("B was told %v, want the other device stopped pairing", end.err)
		}
		if end := offer.wait(t); !errors.Is(end.err, context.Canceled) {
			t.Fatalf("A ended with %v", end.err)
		}
		if !gone(r.box, code.Plate()) {
			t.Fatal("A left its mailbox behind")
		}
		untouched(t, r.homeB)
	})
	t.Run("B comes after", func(t *testing.T) {
		r := newChatRig(t)
		a := newScreen()
		ctx, stop := context.WithCancel(context.Background())
		offer := startOffer(t, r, ctx, Grant{Identity: r.a}, a)
		code := a.nextCode(t)
		stop()
		offer.wait(t)

		_, err := r.join(bounded(t, 10*time.Second), r.homeB, code.Shown(), newCountingJoin())
		wantNothingWaiting(t, err, code.Plate())
		untouched(t, r.homeB)
	})
}

// TWO DEVICES RACE FOR ONE CODE AND THE MAILBOX GIVES IT TO THE FIRST. The
// other is told someone else used it. A holds its answer until the loser has
// come back, so the winner cannot finish and delete the mailbox first and turn
// the loser's answer into "nothing is waiting".
func TestTwoJoinersOneWins(t *testing.T) {
	r := newChatRig(t)
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

	homes := []string{t.TempDir(), t.TempDir()}
	results := make(chan joinEnd, 2)
	start := make(chan struct{})
	ctx := bounded(t, 30*time.Second)
	for _, home := range homes {
		go func() {
			<-start
			joined, err := r.join(ctx, home, code.Shown(), newScreen())
			results <- joinEnd{joined, err}
		}()
	}
	close(start)

	loser := joinWithin(t, results)
	if !errors.Is(loser.err, ErrSomeoneElse) || loser.err.Error() != ErrSomeoneElse.Error() {
		t.Fatalf("the first to come back was told %v, want someone else used that code", loser.err)
	}
	close(release)
	if winner := joinWithin(t, results); winner.err != nil {
		t.Fatalf("the winner was told %v", winner.err)
	}
	if end := offer.wait(t); end.err != nil {
		t.Fatalf("A ended with %v", end.err)
	}
	if held := paired(t, r.a.ID(), homes...); held != 1 {
		t.Fatalf("%d of the two computers hold the chats, want exactly 1", held)
	}
}

// A STRANGER WHO WRITES ANYTHING AT ALL AS THE FIRST MESSAGE SPENDS THE ONE
// ATTEMPT. That costs the person a new code and nothing else, and the new code
// works.
func TestGarbageClaimBurns(t *testing.T) {
	random := make([]byte, 300)
	_, _ = rand.Read(random)
	kinds := map[string][]byte{
		"one byte":    {0x00},
		"empty":       {},
		"zeros":       make([]byte, 32),
		"random":      random,
		"looks right": []byte(strings.Repeat("A", 32)),
	}
	for name, garbage := range kinds {
		t.Run(name, func(t *testing.T) {
			r := newChatRig(t)
			a := newScreen()
			offerAs(t, r, a)
			first := a.nextCode(t)

			stranger, err := pairbox.NewKey()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.box.Post(context.Background(), first.Plate(), pairbox.SideB, stranger, garbage); err != nil {
				t.Fatal(err)
			}
			second := a.nextCode(t)
			if got := burns(a); got != 1 {
				t.Fatalf("A was told %d codes burned, want 1", got)
			}
			if !differs(first, second) {
				t.Fatal("the new code is the burned one")
			}
			if _, err := r.join(bounded(t, 20*time.Second), r.homeB, second.Shown(), newScreen()); err != nil {
				t.Fatalf("the new code did not work: %v", err)
			}
		})
	}
}

// ── the shapes of a typed code ──────────────────────────────────────────────

func TestReadCodeShapes(t *testing.T) {
	t.Run("no request is made for a bad code", badCodeMakesNoRequest)
	for _, bad := range []string{"12345", "715302", "ab-715-302", "42-abc-302", "42-715-30x", "12345678901", "", "   ", "-"} {
		if _, err := ReadJoinCode(bad); !errors.Is(err, ErrCodeShape) {
			t.Errorf("%q gave %v, want the shape of a code", bad, err)
		}
	}
	accepted := map[string]JoinCode{
		"42-715-302":   {"42", "715302"},
		"42 715 302":   {"42", "715302"},
		"42715302":     {"42", "715302"},
		"7-715-302":    {"7", "715302"},
		"1234-715-302": {"1234", "715302"},
		" 42-715-302 ": {"42", "715302"},
	}
	for typed, want := range accepted {
		if got, err := ReadJoinCode(typed); err != nil || got != want {
			t.Errorf("%q gave %v, %v, want %v", typed, got, err, want)
		}
	}
}

// A CODE THAT CANNOT BE A CODE NEVER REACHES THE NETWORK. Not a limits check,
// not a create, nothing: the joining device stops on its own keyboard.
func badCodeMakesNoRequest(t *testing.T) {
	r, hostile := hostileRig(t)
	for _, bad := range []string{"12345", "ab-715-302", "12345678901", ""} {
		_, err := r.join(bounded(t, 5*time.Second), r.homeB, bad, newCountingJoin())
		if !errors.Is(err, ErrCodeShape) {
			t.Errorf("%q gave %v", bad, err)
		}
	}
	if n := hostile.requests(); n != 0 {
		t.Fatalf("a bad code made %d requests", n)
	}
	untouched(t, r.homeB)
}

// ── the two screens that show a code ────────────────────────────────────────

// BOTH SCREENS ARE TWO LINES OF ONE LAYOUT, AND EACH NAMES WHAT ITS CODE GRANTS.
// A person who learned one has learned the other.
func TestLinesNameTheirGrant(t *testing.T) {
	serve, err := NewCode(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	chat := serve.WithPlate("42")
	valid := "(valid 10 minutes)"

	machine := strings.Split(strings.TrimSuffix(Lines("otter-lamp-42", serve), "\n"), "\n")
	chats := strings.Split(strings.TrimSuffix(ChatLines(chat, ""), "\n"), "\n")
	relayed := strings.Split(strings.TrimSuffix(ChatLines(chat, "https://relay.example"), "\n"), "\n")

	for name, lines := range map[string][]string{"serve": machine, "chat": chats, "chat with a relay": relayed} {
		if len(lines) != 2 {
			t.Fatalf("%s is %d lines, want 2", name, len(lines))
		}
		for _, line := range lines {
			if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
				t.Errorf("%s: %q is not indented by two spaces", name, line)
			}
		}
		if !strings.HasSuffix(lines[1], "   "+valid) {
			t.Errorf("%s: the second line %q does not end with its lifetime", name, lines[1])
		}
	}
	if !strings.Contains(strings.Join(machine, "\n"), "use this machine") || !strings.Contains(machine[1], serve.Shown()) {
		t.Errorf("the serve lines do not name their grant and code: %q", machine)
	}
	if !strings.Contains(strings.Join(chats, "\n"), "your chats") || !strings.Contains(chats[1], chat.Shown()) {
		t.Errorf("the chat lines do not name their grant and code: %q", chats)
	}
	if strings.Contains(strings.Join(chats, "\n"), "--via") {
		t.Errorf("the default relay was named: %q", chats)
	}
	if !strings.Contains(relayed[1], chat.Shown()+" --via https://relay.example"+"   "+valid) {
		t.Errorf("the command does not end with the relay: %q", relayed[1])
	}
}

// ONE CODE SHOWS AT A TIME. A second offer while one is live is refused, and
// one is welcome the moment the first is over.
func TestOnlyOneCodeShowsAtATime(t *testing.T) {
	r := newChatRig(t)
	a := newScreen()
	ctx, stop := context.WithCancel(context.Background())
	first := startOffer(t, r, ctx, Grant{Identity: r.a}, a)
	a.nextCode(t)

	_, err := Offer(context.Background(), r.route, Grant{Identity: r.a}, newScreen())
	if !errors.Is(err, ErrOffering) {
		t.Fatalf("a second offer got %v", err)
	}
	stop()
	first.wait(t)

	b := newScreen()
	offerAs(t, r, b)
	b.nextCode(t)
}

// paired is how many of the homes hold the chats of the identity with this id.
func paired(t *testing.T, id string, homes ...string) int {
	t.Helper()
	held := 0
	for _, home := range homes {
		if got, err := identity.Load(home); err == nil && got.ID() == id {
			held++
		}
	}
	return held
}
