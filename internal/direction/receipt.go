package direction

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// PersonReceipt IS THE ONLY KEY TO ACCEPTED. Its fields are unexported, so it
// cannot be spelled as a literal outside this package, and it has exactly four
// constructors — one per door a person's act reaches the runtime through. A
// law (receipt_law_test.go) holds every call to them to an allow-list of
// runtime sites, and keeps them out of every tool a model can call. A model
// therefore cannot hold one: it can propose, and the person accepts.
//
// The zero value is not a receipt, and every person-only change refuses it.
type PersonReceipt struct {
	door  Door
	ref   string
	quote string // only for DoorStatement: the verified words
}

func (r PersonReceipt) valid() bool { return r.door != "" && r.ref != "" }

func newReceipt(door Door, ref string) (PersonReceipt, error) {
	if !workspace.ValidLine(ref, maxRef) {
		return PersonReceipt{}, invalid("a %s receipt names what the person answered", door)
	}
	return PersonReceipt{door: door, ref: ref}, nil
}

// FromCardAnswer is the receipt for a person's answer to a card. proposalID is
// the card the person answered.
func FromCardAnswer(proposalID string) (PersonReceipt, error) {
	return newReceipt(DoorCard, proposalID)
}

// FromTerminal is the receipt for a command the person ran. command is the
// invocation as the person typed it.
func FromTerminal(command string) (PersonReceipt, error) {
	return newReceipt(DoorTerminal, command)
}

// FromPage is the receipt for a person's act on a page. event names it.
func FromPage(event string) (PersonReceipt, error) {
	return newReceipt(DoorPage, event)
}

// JournalLine is the part of a conversation's journal line a statement
// receipt is verified against.
type JournalLine struct {
	Role  string // "user" for a person's line
	Note  bool   // the harness's wake and delivery notes carry note: true
	Input bool   // written through an input door: typed, or steer
	Text  string
}

// FromVerifiedStatement is the receipt for words the person said themselves
// (C09, design §1.4). A line qualifies only if it is a user line, not a
// harness note, and written through an input door; the quote must be a
// verbatim part of it. The receipt's reference is the hash of the whole line,
// and a revision accepted with it must quote those words and cite that hash as
// its source, so the accepted wording is anchored to what the person typed.
// A wake note that says "the user decided …" fails here.
func FromVerifiedStatement(line JournalLine, quote string) (PersonReceipt, error) {
	if line.Role != "user" || line.Note || !line.Input {
		return PersonReceipt{}, invalid("only a line the person typed is a statement")
	}
	if quote == "" || !strings.Contains(line.Text, quote) {
		return PersonReceipt{}, invalid("a statement quotes the person's own words")
	}
	sum := sha256.Sum256([]byte(line.Text))
	return PersonReceipt{door: DoorStatement, ref: hex.EncodeToString(sum[:]), quote: quote}, nil
}

// Actor is who asks for a change: a non-person writer named by class, or a
// person holding a receipt.
type Actor struct {
	author  Author
	receipt PersonReceipt
}

// As names a writer that is not a person: a model, the memory extractor, an
// unattended run, a voice or a delegated principal. It writes what its row of
// §4.1 allows (section41 in authority.go), which is never authority.
func As(class AuthorClass, ref string) Actor {
	return Actor{author: Author{Class: class, Ref: ref}}
}

// AsPerson names the person, by the receipt of the act that asked.
func AsPerson(r PersonReceipt) Actor {
	return Actor{author: Author{Class: AuthorPerson, Ref: r.ref}, receipt: r}
}

func (a Actor) person() bool { return a.author.Class == AuthorPerson }

func (a Actor) check() error {
	if a.person() {
		if !a.receipt.valid() {
			return ErrNoReceipt
		}
		return nil
	}
	if !a.author.Class.writer() {
		return invalid("%q does not write through this door", a.author.Class)
	}
	if a.author.Ref != "" && !workspace.ValidLine(a.author.Ref, maxRef) {
		return invalid("an author reference is at most %d bytes", maxRef)
	}
	return nil
}
