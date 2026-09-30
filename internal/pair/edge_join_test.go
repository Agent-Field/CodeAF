package pair

// Contract 18.9, the rows about the people at the two screens and about what
// lands on the joining computer: the words they compare, the answer that may be
// no, the chats a computer already has, and the grant that is checked whole
// before a file is touched.

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// sessionWords runs one introduction over a pipe and answers the words each
// device computed.
func sessionWords(t *testing.T, plate, secret string) (joiner, offerer string) {
	t.Helper()
	near, far := net.Pipe()
	defer func() { _ = near.Close() }()
	got := make(chan string, 1)
	go func() {
		defer func() { _ = far.Close() }()
		o, err := answer(streamLink{far}, chatScheme(plate), secret)
		if err != nil {
			got <- ""
			return
		}
		got <- o.Words
	}()
	intro, err := begin(streamLink{near}, chatScheme(plate), secret, []byte("laptop-0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return intro.Words, <-got
}

// THE WORDS ARE ONE SESSION'S, NOT ONE CODE'S. Both screens of a session show
// the same three, and a second session that has the same code, the same name
// and the same everything else shows others, which is what lets a person tell
// the device they hold from one that only knows the code.
func TestJoinWordsDifferPerSession(t *testing.T) {
	t.Run("both screens of one session agree", func(t *testing.T) {
		r := newChatRig(t)
		a, b := newScreen(), newCountingJoin()
		offerAs(t, r, a)
		code := a.nextCode(t)
		if _, err := r.join(bounded(t, 20*time.Second), r.homeB, code.Shown(), b); err != nil {
			t.Fatal(err)
		}
		if asked, shown := <-a.asked, <-b.words; asked != shown || len(strings.Fields(asked)) != wordsShown {
			t.Fatalf("the screens show %q and %q", asked, shown)
		}
	})
	t.Run("two sessions through one relay differ", func(t *testing.T) {
		seen := map[string]bool{}
		for range 2 {
			r := newChatRig(t)
			a, b := newScreen(), newCountingJoin()
			offer := offerAs(t, r, a)
			code := a.nextCode(t)
			if _, err := r.join(bounded(t, 20*time.Second), r.homeB, code.Shown(), b); err != nil {
				t.Fatal(err)
			}
			offer.wait(t)
			seen[<-a.asked] = true
		}
		if len(seen) != 2 {
			t.Fatal("two sessions showed the same words")
		}
	})
	t.Run("an attacker who knows the code differs from the real device", func(t *testing.T) {
		const secret = "715302"
		attackerJoiner, attackerOfferer := sessionWords(t, "42", secret)
		realJoiner, realOfferer := sessionWords(t, "42", secret)
		if attackerJoiner != attackerOfferer || realJoiner != realOfferer {
			t.Fatal("the two ends of one session disagree")
		}
		if attackerJoiner == realJoiner {
			t.Fatal("two sessions with the same code and name show the same words")
		}
	})
}

// A DEVICE THAT LOST THE RACE IS SHOWN NO WORDS AT ALL. Words on its screen would
// be words for a session it is not in, and a person comparing them with the
// other screen could be talked into a yes.
func TestLoserSeesNoWords(t *testing.T) {
	r := newChatRig(t)
	release := make(chan struct{})
	a := newScreen()
	a.answer = func(ctx context.Context, _, _ string) bool {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return false
	}
	offer := offerAs(t, r, a)
	code := a.nextCode(t)

	attacker := newCountingJoin()
	attackerEnd := joinInBackground(bounded(t, 30*time.Second), r, t.TempDir(), code.Shown(), attacker)
	words := <-attacker.words

	loser := newCountingJoin()
	_, err := r.join(bounded(t, 10*time.Second), r.homeB, code.Shown(), loser)
	if !errors.Is(err, ErrSomeoneElse) || err.Error() != ErrSomeoneElse.Error() {
		t.Fatalf("the loser was told %v, want someone else used that code", err)
	}
	if n := loser.n.Load(); n != 0 {
		t.Fatalf("the loser was shown words %d times", n)
	}
	if asked := <-a.asked; asked != words {
		t.Fatalf("A asks about %q while the device that got in shows %q", asked, words)
	}
	close(release)
	if end := joinWithin(t, attackerEnd); !errors.Is(end.err, ErrRefused) {
		t.Fatalf("the device that got in was told %v", end.err)
	}
	offer.wait(t)
	untouched(t, r.homeB)
}

// A NO ON THE OTHER SCREEN IS A NO HERE: nothing is written, the joining device
// says so in the sentence for it, and the offering device's mailbox is gone.
func TestRefusedByA(t *testing.T) {
	r := newChatRig(t)
	a := newScreen()
	a.answer = func(context.Context, string, string) bool { return false }
	offer := offerAs(t, r, a)
	code := a.nextCode(t)

	_, err := r.join(bounded(t, 20*time.Second), r.homeB, code.Shown(), newScreen())
	if !errors.Is(err, ErrRefused) || err.Error() != "refused on the other device" {
		t.Fatalf("B was told %v", err)
	}
	if end := offer.wait(t); !errors.Is(end.err, ErrRefused) {
		t.Fatalf("A ended with %v, want refused", end.err)
	}
	if !gone(r.box, code.Plate()) {
		t.Fatal("the mailbox of a refused pairing is still there")
	}
	untouched(t, r.homeB)
}

// SILENCE IS A NO. A question nobody answers ends as a no on A, and the joining
// device is told it was refused. The two minutes themselves are a context
// deadline on A's question, so the tests end the question the two ways such a
// deadline can: the person is gone and A's own context ends with it, or A is
// alive and its question comes back unanswered.
func TestConfirmTimeout(t *testing.T) {
	t.Run("the question comes back unanswered", func(t *testing.T) {
		r := newChatRig(t)
		unanswered := make(chan struct{})
		a := newScreen()
		a.answer = func(ctx context.Context, _, _ string) bool {
			select {
			case <-unanswered:
			case <-ctx.Done():
			}
			return false
		}
		offer := offerAs(t, r, a)
		code := a.nextCode(t)
		joined := joinInBackground(bounded(t, 20*time.Second), r, r.homeB, code.Shown(), newScreen())
		<-a.asked
		close(unanswered)

		if end := joinWithin(t, joined); !errors.Is(end.err, ErrRefused) || end.err.Error() != "refused on the other device" {
			t.Fatalf("B was told %v, want refused on the other device", end.err)
		}
		if end := offer.wait(t); !errors.Is(end.err, ErrRefused) {
			t.Fatalf("A ended with %v, want refused", end.err)
		}
		if !gone(r.box, code.Plate()) {
			t.Fatal("the mailbox of an unanswered pairing is still there")
		}
		untouched(t, r.homeB)
	})
	t.Run("A's own context ends with the question", func(t *testing.T) {
		r := newChatRig(t)
		a := newScreen()
		a.answer = func(ctx context.Context, _, _ string) bool { <-ctx.Done(); return false }
		ctx, stop := context.WithCancel(context.Background())
		offer := startOffer(t, r, ctx, Grant{Identity: r.a}, a)
		code := a.nextCode(t)
		joined := joinInBackground(bounded(t, 20*time.Second), r, r.homeB, code.Shown(), newScreen())
		<-a.asked
		stop()

		// A has gone, so B may hear the refusal or find the mailbox gone; it must
		// not be paired.
		if end := joinWithin(t, joined); !errors.Is(end.err, ErrRefused) && !errors.Is(end.err, ErrCodeDidNotWork) {
			t.Fatalf("B was told %v", end.err)
		}
		if end := offer.wait(t); !errors.Is(end.err, ErrRefused) {
			t.Fatalf("A ended with %v, want refused", end.err)
		}
		if !gone(r.box, code.Plate()) {
			t.Fatal("the mailbox of an unanswered pairing is still there")
		}
		untouched(t, r.homeB)
	})
}

// ── what a computer already has ─────────────────────────────────────────────

// pairOnce runs one whole pairing of the rig's A into a joining device.
func pairOnce(t *testing.T, r *chatRig, j Joining) (Joined, offerEnd, error) {
	t.Helper()
	a := newScreen()
	offer := offerAs(t, r, a)
	code := a.nextCode(t)
	joined, err := Join(bounded(t, 20*time.Second), r.route, j, code.Shown(), newScreen())
	return joined, offer.wait(t), err
}

func joiningAt(home string) Joining { return Joining{Home: home, Label: "laptop"} }

// A COMPUTER THAT HAS CHATS OF ITS OWN KEEPS THEM. Without --replace the pairing
// says so and changes not one byte; with it, this computer takes the other's.
func TestJoinRefusesDifferentIdentity(t *testing.T) {
	r := newChatRig(t)
	mine, err := identity.Ensure(r.homeB)
	if err != nil {
		t.Fatal(err)
	}
	before := filesIn(t, r.homeB)

	_, _, err = pairOnce(t, r, joiningAt(r.homeB))
	if !errors.Is(err, ErrDifferentChats) || err.Error() != ErrDifferentChats.Error() {
		t.Fatalf("B was told %v", err)
	}
	same(t, before, filesIn(t, r.homeB))
	held, err := identity.Load(r.homeB)
	if err != nil || held.ID() != mine.ID() || !bytes.Equal(held.CellKey(), mine.CellKey()) {
		t.Fatalf("this computer's own chats were disturbed (%v)", err)
	}

	replace := joiningAt(r.homeB)
	replace.Replace = true
	if _, _, err := pairOnce(t, r, replace); err != nil {
		t.Fatalf("with replace B was told %v", err)
	}
	if adopted, err := identity.Load(r.homeB); err != nil || adopted.ID() != r.a.ID() {
		t.Fatalf("replace did not adopt the other device's chats (%v)", err)
	}
}

// A COMPUTER THAT ALREADY HOLDS THESE CHATS IS LEFT ALONE, certificate and all.
func TestJoinSameIdentityIsNoop(t *testing.T) {
	r := newChatRig(t)
	if _, err := identity.Adopt(r.homeB, r.a, false); err != nil {
		t.Fatal(err)
	}
	before := filesIn(t, r.homeB)
	if _, ok := before[filepath.Join(r.homeB, identity.DeviceFile)]; !ok {
		t.Fatal("the fixture has no device certificate, so it proves nothing about keeping one")
	}

	joined, _, err := pairOnce(t, r, joiningAt(r.homeB))
	if err != nil || !joined.Already {
		t.Fatalf("got %+v, %v, want Already", joined, err)
	}
	same(t, before, filesIn(t, r.homeB))
}

// A VAULT KEY FROM BEFORE IDENTITIES IS CHATS OF ITS OWN. A computer with only
// that file is a different identity and its key is not overwritten.
func TestJoinLegacyVaultKey(t *testing.T) {
	r := newChatRig(t)
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 7)
	}
	legacy := filepath.Join(r.homeB, "vault.key")
	text := []byte(hex.EncodeToString(secret) + "\n")
	if err := os.WriteFile(legacy, text, 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := pairOnce(t, r, joiningAt(r.homeB))
	if !errors.Is(err, ErrDifferentChats) {
		t.Fatalf("B was told %v", err)
	}
	if now, err := os.ReadFile(legacy); err != nil || !bytes.Equal(now, text) {
		t.Fatalf("the vault key was changed (%v)", err)
	}
	if held, err := identity.Load(r.homeB); err == nil && !bytes.Equal(held.CellKey(), secret) {
		t.Fatal("the chats this computer had no longer open with its own key")
	}

	replace := joiningAt(r.homeB)
	replace.Replace = true
	if _, _, err := pairOnce(t, r, replace); err != nil {
		t.Fatalf("with replace B was told %v", err)
	}
}

// ── the grant ───────────────────────────────────────────────────────────────

// THE RELAY THE OTHER DEVICE SYNCS THROUGH IS SAVED WHEN IT IS NAMED, and not
// touched when it is not.
func TestGrantCarriesSyncURL(t *testing.T) {
	for _, c := range []struct{ name, url string }{{"named", "https://relay.example"}, {"the default", ""}} {
		t.Run(c.name, func(t *testing.T) {
			r := newChatRig(t)
			var saved []string
			j := joiningAt(r.homeB)
			j.SaveSyncURL = func(url string) error { saved = append(saved, url); return nil }

			a := newScreen()
			offer := startOffer(t, r, bounded(t, 30*time.Second), Grant{Identity: r.a, SyncURL: c.url}, a)
			code := a.nextCode(t)
			if _, err := Join(bounded(t, 20*time.Second), r.route, j, code.Shown(), newScreen()); err != nil {
				t.Fatal(err)
			}
			offer.wait(t)

			if c.url == "" && len(saved) != 0 {
				t.Fatalf("the default relay was saved as %v", saved)
			}
			if c.url != "" && (len(saved) != 1 || saved[0] != c.url) {
				t.Fatalf("saved %v, want [%s]", saved, c.url)
			}
		})
	}
}

// grantWith is a grant document with the parts a case wants to spoil.
func grantWith(version int, identityDoc, extra, syncURL string) string {
	sync := ""
	if syncURL != "" {
		sync = fmt.Sprintf(`,"sync_url":%q`, syncURL)
	}
	return fmt.Sprintf(`{"V":%d,"identity":%s%s%s}`, version, identityDoc, extra, sync)
}

// A GRANT IS CHECKED WHOLE BEFORE ANYTHING IS WRITTEN. Every shape that is not
// exactly a grant is one sentence, and the joining computer is left as it was.
func TestGrantBounds(t *testing.T) {
	r := newChatRig(t).via(pairbox.NewMemory(roomy(), nil))
	doc, err := r.a.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	good := grantWith(1, string(doc), "", "https://relay.example")

	bad := map[string][]byte{
		"oversize":          append([]byte{verdictWelcome}, bytes.Repeat([]byte(" "), maxGrant+1)...),
		"nothing after":     {verdictWelcome},
		"empty":             {},
		"unknown verdict":   append([]byte{9}, good...),
		"garbled":           append([]byte{verdictWelcome}, `{"V":1,"identity":`...),
		"not an object":     append([]byte{verdictWelcome}, `[1,2,3]`...),
		"unknown field":     append([]byte{verdictWelcome}, grantWith(1, string(doc), `,"extra":true`, "")...),
		"wrong version":     append([]byte{verdictWelcome}, grantWith(2, string(doc), "", "")...),
		"bad identity":      append([]byte{verdictWelcome}, grantWith(1, `{"V":1}`, "", "")...),
		"identity is null":  append([]byte{verdictWelcome}, grantWith(1, `null`, "", "")...),
		"bad sync scheme":   append([]byte{verdictWelcome}, grantWith(1, string(doc), "", "ftp://relay.example")...),
		"bad sync no host":  append([]byte{verdictWelcome}, grantWith(1, string(doc), "", "https://")...),
		"bad sync not url":  append([]byte{verdictWelcome}, grantWith(1, string(doc), "", "not a url")...),
		"trailing garbage":  append([]byte{verdictWelcome}, good+" x"...),
		"two documents":     append([]byte{verdictWelcome}, good+good...),
		"truncated version": append([]byte{verdictWelcome}, `{"V":`...),
	}
	t.Run("the fixture is a grant", func(t *testing.T) {
		got, err := hearGrant(append([]byte{verdictWelcome}, good...))
		if err != nil || got.Identity.ID() != r.a.ID() || got.SyncURL != "https://relay.example" {
			t.Fatalf("a good grant was read as %v", err)
		}
	})
	for name, said := range bad {
		t.Run("read "+name, func(t *testing.T) {
			if _, err := hearGrant(said); !errors.Is(err, ErrBadGrant) {
				t.Fatalf("got %v, want a grant that could not be read", err)
			}
		})
		t.Run("over the wire "+name, func(t *testing.T) {
			if len(said) > 3000 {
				t.Skip("larger than one mailbox message")
			}
			home := t.TempDir()
			_, err := joinHostileOfferer(t, r, home, said)
			if !errors.Is(err, ErrBadGrant) {
				t.Fatalf("B was told %v, want a grant that could not be read", err)
			}
			untouched(t, home)
		})
	}
}

// joinHostileOfferer has a device that is not codeaf show a code and answer the
// joining device with whatever it likes inside a perfectly good encrypted reply.
func joinHostileOfferer(t *testing.T, r *chatRig, home string, reply []byte) (Joined, error) {
	t.Helper()
	ctx := bounded(t, 20*time.Second)
	const digits = "482913"
	key, err := pairbox.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	made, err := r.box.Create(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.box.Delete(context.Background(), made.Nameplate, key) })
	go func() {
		link := &boxLink{ctx: ctx, box: r.box, plate: made.Nameplate, mine: pairbox.SideA, theirs: pairbox.SideB, key: key}
		if joiner, err := answer(link, chatScheme(made.Nameplate), digits); err == nil {
			_ = joiner.reply(reply)
		}
	}()
	typed := made.Nameplate + "-" + digits[:3] + "-" + digits[3:]
	return Join(ctx, r.route, joiningAt(home), typed, newScreen())
}

// ── the name a device gives itself ──────────────────────────────────────────

// noControls is whether a label is fit to sit in a list on a screen.
func noControls(label string) bool {
	return strings.IndexFunc(label, unicode.IsControl) < 0 && utf8.RuneCountInString(label) <= 32
}

// A DEVICE CHOOSES ITS OWN NAME AND DOES NOT GET TO CHOOSE HOW IT IS DRAWN. What
// reaches the other device's question is at most 32 letters with no control
// codes in them, whichever end tidied it.
func TestJoinerLabelSanitised(t *testing.T) {
	long := strings.Repeat("x", 300)
	labels := map[string]string{
		"control codes":      "lap\x00top\x1b[31m\x07\r\ndone",
		"three hundred":      long,
		"eight-bit controls": "lap\u009b31mtop\u0085x",
		"delete":             "lap\x7ftop",
		"only controls":      "\x00\x01\x02",
	}
	for name, label := range labels {
		t.Run("through join "+name, func(t *testing.T) {
			got := labelAsAsked(t, func(r *chatRig, code *Code) {
				j := joiningAt(t.TempDir())
				j.Label = label
				_, _ = Join(bounded(t, 20*time.Second), r.route, j, code.Shown(), newScreen())
			})
			if !noControls(got) || got == "" {
				t.Fatalf("A was asked about %q", got)
			}
		})
		t.Run("from a device that skips the tidying "+name, func(t *testing.T) {
			got := labelAsAsked(t, func(r *chatRig, code *Code) {
				offer := append([]byte(label), make([]byte, nonceSize)...)
				link := &boxLink{ctx: bounded(t, 20*time.Second), box: r.box, plate: code.Plate(), mine: pairbox.SideB, theirs: pairbox.SideA}
				link.key, _ = pairbox.NewKey()
				_, _ = begin(link, chatScheme(code.Plate()), code.secret(), offer)
			})
			if !noControls(got) || got == "" {
				t.Fatalf("A was asked about %q", got)
			}
		})
	}
	t.Run("a short offer is a device", func(t *testing.T) {
		got := labelAsAsked(t, func(r *chatRig, code *Code) {
			link := &boxLink{ctx: bounded(t, 20*time.Second), box: r.box, plate: code.Plate(), mine: pairbox.SideB, theirs: pairbox.SideA}
			link.key, _ = pairbox.NewKey()
			_, _ = begin(link, chatScheme(code.Plate()), code.secret(), []byte("short"))
		})
		if got != "a device" {
			t.Fatalf("A was asked about %q", got)
		}
	})
}

// labelAsAsked runs a joining device against a fresh offer and answers the name
// A's question carried.
func labelAsAsked(t *testing.T, joiner func(r *chatRig, code *Code)) string {
	t.Helper()
	r := newChatRig(t)
	labels := make(chan string, 1)
	a := newScreen()
	a.answer = func(_ context.Context, label, _ string) bool { labels <- label; return false }
	offer := offerAs(t, r, a)
	joiner(r, a.nextCode(t))
	select {
	case label := <-labels:
		offer.stop() // a device that skips the tidying never sends the word that it read the answer
		offer.wait(t)
		return label
	case <-bounded(t, 20*time.Second).Done():
		t.Fatal("A was never asked")
		return ""
	}
}
