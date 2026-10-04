package pair

// Sharing your chats: the device that already has them shows a code, and hands
// them to the device that types it.
//
// ONE CODE, ONE ATTEMPT. The device draws six digits, opens a mailbox and shows
// `42-715-302`. The first message it reads from the joining side is the only
// guess that code will ever take: right, and a person looks at three words on
// two screens; wrong, and the mailbox is deleted, the screen says a code was
// burned, and a new code is on it before anyone presses a key.

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// OfferUI is what a person sees and answers on the device that shows the code.
// A screen and a terminal each implement it; nothing here draws.
type OfferUI interface {
	// Show puts a fresh code on screen, with the lines that say what to do with it.
	Show(code *Code, lines string)
	// Burned says a wrong code was typed and the code on screen is gone. The next
	// Show follows at once.
	Burned()
	// Ask puts the joining device's name and the three words to a person and
	// answers whether they said yes. It returns false when ctx ends, which is how
	// silence becomes a no.
	Ask(ctx context.Context, label, words string) bool
}

// offering is the one live code of this process, so two screens cannot each
// hold a code and double the guesses against the same chats.
var offering atomic.Bool

// Offer shares a grant with the next device that types the code, and answers
// when a pairing has finished, been refused, or run out of time. A wrong code
// is not an ending: it burns the code and the loop offers another.
//
// It returns the name the joining device gave itself when it was let in.
func Offer(ctx context.Context, route Mailbox, grant Grant, ui OfferUI) (string, error) {
	if !offering.CompareAndSwap(false, true) {
		return "", ErrOffering
	}
	defer offering.Store(false)
	if err := checkRelay(ctx, route); err != nil {
		return "", err
	}
	for burned := ""; ; {
		label, plate, err := offerOnce(ctx, route, grant, ui, burned)
		if !errors.Is(err, ErrWrongCode) {
			return label, err
		}
		burned = plate
		ui.Burned()
	}
}

// checkRelay asks the relay for its numbers before anything is shown, so a
// relay that cannot serve a code is never allowed to display one.
func checkRelay(ctx context.Context, route Mailbox) error {
	if _, err := route.Box.Limits(ctx); err != nil {
		return phraseBox(err, route.Host)
	}
	return nil
}

// offerOnce is one code's whole life: made, shown, spent by the first message
// that arrives, and taken away again whatever happened. It answers the nameplate
// it used, so the next code can be made under another one. burned is the plate
// of the code this one replaces, or empty for the first.
func offerOnce(ctx context.Context, route Mailbox, grant Grant, ui OfferUI, burned string) (string, string, error) {
	code, key, err := drawCode()
	if err != nil {
		return "", "", err
	}
	made, err := createApart(ctx, route.Box, key, burned)
	if err != nil {
		return "", "", phraseBox(err, route.Host)
	}
	code = code.WithPlate(made.Nameplate)
	defer discard(route.Box, made.Nameplate, key)
	ui.Show(code, ChatLines(code, route.Shown))

	// The relay's own reckoning of the lifetime, and not this device's clock.
	ctx, stop := context.WithTimeout(ctx, made.ExpiresIn)
	defer stop()
	link := &boxLink{ctx: ctx, box: route.Box, plate: made.Nameplate, mine: pairbox.SideA, theirs: pairbox.SideB,
		key: key, gone: map[stage]error{awaitStart: ErrDidNotFinish, awaitOffer: ErrDidNotFinish}, sendGone: ErrDidNotFinish}
	joiner, err := answer(link, chatScheme(made.Nameplate), code.secret())
	if err != nil {
		return "", made.Nameplate, phraseBox(err, route.Host)
	}
	label, err := conclude(ctx, joiner, grant, ui)
	return label, made.Nameplate, err
}

// createApart opens a mailbox under a nameplate other than burned. Nameplates
// are short and the relay draws them at random, so the next mailbox can land on
// the one just deleted, and the joining device whose code burned is still
// waiting on that plate: it would find a live, empty mailbox where it expected
// the word that the old one is gone, and wait there for an answer that cannot
// come. A relay that keeps handing the same plate back is as good as full.
func createApart(ctx context.Context, box pairbox.Box, key pairbox.Key, burned string) (pairbox.Created, error) {
	for range 5 {
		made, err := box.Create(ctx, key)
		if err != nil || made.Nameplate != burned {
			return made, err
		}
		discard(box, made.Nameplate, key)
	}
	return pairbox.Created{}, pairbox.ErrRelayFull
}

// conclude puts the joining device to a person and sends the answer: the grant
// for a yes, a refusal for a no or for silence.
func conclude(ctx context.Context, joiner *offered, grant Grant, ui OfferUI) (string, error) {
	label := joinerLabel(joiner.Offer)
	asked, stop := context.WithTimeout(ctx, ConfirmWithin)
	defer stop()
	if !ui.Ask(asked, label, joiner.Words) {
		_ = joiner.reply(sayVerdict("refused"))
		return "", ErrRefused
	}
	reply, err := grant.verdictOf()
	if err != nil {
		return "", err
	}
	if err := joiner.reply(reply); err != nil {
		return "", phraseBox(err, "")
	}
	return label, nil
}

// joinerLabel is the joining device's name as this screen shows it: what it
// called itself, without the random bytes that keep two sessions apart.
func joinerLabel(offer []byte) string {
	if len(offer) < nonceSize {
		return readableLabel("")
	}
	return readableLabel(string(offer[:len(offer)-nonceSize]))
}

// nonceSize is the random tail of a joining device's offer. It makes the offer
// of two sessions differ even when the name is the same.
const nonceSize = 16

// drawCode makes the six digits and the side key from the system's random
// source, before any mailbox exists.
func drawCode() (*Code, pairbox.Key, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return nil, pairbox.Key{}, err
	}
	key, err := pairbox.NewKey()
	code := &Code{digits: fmt.Sprintf("%06d", n.Int64()), born: time.Now(), left: CodeAttempts}
	return code, key, err
}

// discard deletes a mailbox on the way out, even when the pairing was cancelled
// and its context is spent, so a code that is no longer on any screen is no
// longer on the relay. A delete that is lost costs nothing: the mailbox expires.
func discard(box pairbox.Box, plate string, key pairbox.Key) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_ = box.Delete(ctx, plate, key)
}
