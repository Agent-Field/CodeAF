package syncsetup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// A message written here to a chat another computer holds travels in this
// device's own directory record, because a device may only ever write its own
// record and every record change already wakes every device that watches.
// The sender keeps it in its outbox; whichever computer holds the chat puts it
// in that chat's queue, so it runs when the turn there ends, and says so by
// listing its id in its own record's took; the sender then drops it. The text
// is sealed under the identity's metadata key, so the relay never reads it.
//
// THE CHAT, NOT THE COMPUTER, IS THE ADDRESS. A message waits for whoever
// holds the chat when it is read: if the chat moves before the holder took it,
// the computer it moved to takes it, and that may be the sender itself.

var (
	// ErrNotesUnsupported is a relay that does not keep a device record's
	// outbox, so a message could not reach the other computer.
	ErrNotesUnsupported = errors.New("this sync service cannot carry a message to another computer yet")
	// ErrOutboxFull is MaxNotes messages already waiting to be taken.
	ErrOutboxFull = errors.New("too many messages are already waiting for the other computer")
	// ErrNoteEmpty and ErrNoteLong refuse a message with nothing in it or
	// longer than directory.MaxNoteBytes.
	ErrNoteEmpty = errors.New("the message is empty")
	ErrNoteLong  = errors.New("the message is too long to send to another computer")
	// ErrNoOwnRecord is a device whose own record the relay does not list yet.
	ErrNoOwnRecord = errors.New("this computer is not listed by the sync service yet")
)

// Mail is what one listing says about messages between computers, from this
// device's side.
type Mail struct {
	// In is the messages waiting for chats this device holds now, oldest
	// first, that it has not taken yet.
	In []InNote
	// Sent is this device's own outbox by chat, opened: the messages still
	// waiting for another computer to take them.
	Sent map[string][]InNote
	// Taken is the messages in this device's outbox that a holder has taken.
	Taken []InNote
}

// InNote is one opened message.
type InNote struct {
	ID, Cell, Text, From string
	At                   int64
}

// MailOf reads the messages out of one listing for device me. A note that does
// not open under key is skipped: it was sealed by another identity's key, or
// damaged, and nothing here could show it.
func MailOf(l directory.Listing, me string, key []byte) Mail {
	m := Mail{Sent: map[string][]InNote{}}
	took := map[string]bool{}
	for _, d := range l.Devices {
		for _, id := range d.Took {
			took[id] = true
		}
	}
	mine := l.Devices[me]
	tookHere := map[string]bool{}
	for _, id := range mine.Took {
		tookHere[id] = true
	}
	for from, d := range l.Devices {
		if d.Revoked {
			continue
		}
		for _, n := range d.Outbox {
			text, err := directory.OpenNote(key, n.Text)
			if err != nil {
				continue
			}
			note := InNote{ID: n.ID, Cell: n.Cell, Text: text, From: from, At: n.At}
			if from == me {
				if took[n.ID] {
					m.Taken = append(m.Taken, note)
				} else {
					m.Sent[n.Cell] = append(m.Sent[n.Cell], note)
				}
			}
			c, ok := l.Cells[n.Cell]
			if ok && c.Lease.Device == me && c.Lease.Expires > l.Now && !tookHere[n.ID] {
				m.In = append(m.In, note)
			}
		}
	}
	sort.SliceStable(m.In, func(i, j int) bool { return m.In[i].At < m.In[j].At })
	return m
}

// MailAt reads the directory and answers what it says about messages.
func (s *Sync) MailAt(ctx context.Context) (Mail, error) {
	current, err := s.currentDisplaySync()
	if err != nil {
		return Mail{}, err
	}
	if current != s {
		return current.MailAt(ctx)
	}
	l, err := s.list(ctx)
	if err != nil {
		return Mail{}, err
	}
	return MailOf(l, s.Device.ID(), directory.MetadataKey(s.Identity.CellKey())), nil
}

// Notes reports whether the relay carries messages between computers.
func (s *Sync) Notes() bool {
	n, ok := s.Dir.(interface{ Notes() bool })
	return ok && n.Notes()
}

// SendNote leaves text for whichever computer holds cell and answers the
// message's id. It never waits for that computer: the message is on its way
// once the relay has this device's record.
func (s *Sync) SendNote(ctx context.Context, cell, text string) (string, error) {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return "", ErrNoteEmpty
	case len(text) > directory.MaxNoteBytes || !utf8.ValidString(text):
		return "", ErrNoteLong
	}
	sealed, err := directory.SealNote(directory.MetadataKey(s.Identity.CellKey()), text)
	if err != nil {
		return "", err
	}
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw)
	err = s.ownRecord(ctx, func(own *directory.Device, _ directory.Listing) (bool, error) {
		if len(own.Outbox) >= directory.MaxNotes {
			return false, ErrOutboxFull
		}
		own.Outbox = append(own.Outbox, directory.Note{ID: id, Cell: cell, Text: sealed, At: time.Now().UnixMilli()})
		return true, nil
	})
	return id, err
}

// TookNotes says this device put the messages ids in a chat's queue here, so
// their senders drop them. It also forgets ids no sender still lists, so the
// list stays as long as what is in flight.
func (s *Sync) TookNotes(ctx context.Context, ids []string) error {
	return s.ownRecord(ctx, func(own *directory.Device, l directory.Listing) (bool, error) {
		listed := map[string]bool{}
		for _, d := range l.Devices {
			for _, n := range d.Outbox {
				listed[n.ID] = true
			}
		}
		next := slices.DeleteFunc(slices.Clone(own.Took), func(id string) bool { return !listed[id] })
		for _, id := range ids {
			if listed[id] && !slices.Contains(next, id) {
				next = append(next, id)
			}
		}
		changed := !slices.Equal(next, own.Took)
		own.Took = next
		return changed, nil
	})
}

// DropNotes takes the messages ids out of this device's outbox once their
// holder has taken them.
func (s *Sync) DropNotes(ctx context.Context, ids []string) error {
	return s.ownRecord(ctx, func(own *directory.Device, _ directory.Listing) (bool, error) {
		before := len(own.Outbox)
		own.Outbox = slices.DeleteFunc(slices.Clone(own.Outbox), func(n directory.Note) bool { return slices.Contains(ids, n.ID) })
		return len(own.Outbox) != before, nil
	})
}

// ownRecord changes this device's own record under the same lock every writer
// of it takes, reading it fresh first, and writes it only when change says it
// changed. A relay that cannot carry messages refuses before anything is read.
func (s *Sync) ownRecord(ctx context.Context, change func(*directory.Device, directory.Listing) (bool, error)) error {
	current, err := s.currentDisplaySync()
	if err != nil {
		return err
	}
	if current != s {
		return current.ownRecord(ctx, change)
	}
	mu := ownDeviceLock(s.Device.ID())
	mu.Lock()
	defer mu.Unlock()
	// Read under the lock, so a write another door made meanwhile stays; the
	// read is also what tells this device whether the relay carries messages.
	l, err := s.list(ctx)
	if err != nil {
		return err
	}
	if !s.Notes() {
		return ErrNotesUnsupported
	}
	own, ok := l.Devices[s.Device.ID()]
	if !ok {
		return ErrNoOwnRecord
	}
	changed, err := change(&own, l)
	if err != nil || !changed {
		return err
	}
	return s.Dir.PutDevice(ctx, s.Device.ID(), own)
}

// ownDeviceLock is the one lock every write of a device's own record takes.
func ownDeviceLock(id string) *sync.Mutex {
	value, _ := ownDeviceWrites.LoadOrStore(id, &sync.Mutex{})
	return value.(*sync.Mutex)
}
