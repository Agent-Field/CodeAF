package main

import (
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// ── THE STORAGE-OFF SENTENCE FOLLOWS WHAT IS TRUE, NOT WHAT WAS TRUE WHEN IT WAS SAID ──
//
// The refusal for closed storage is worded when it is said, from whether another
// computer is paired ([chatlist.StorageOffSentence]). That fact can change after
// the words are on screen: a roster read shows a computer that a pairing just
// added, or the last other one was removed. Left alone, a person was told for
// the rest of the run that chats stay on this computer while they were moving,
// or the reverse.
//
// So syncsetup tells this door when the fact changes, and the door says the
// sentence that is true now, once. It says nothing when no storage-off sentence
// was ever said (there is nothing on screen to replace), and nothing on an
// account whose storage is approved (the sentence is gone, not replaced).

// watchStorageOffSentence is called once at startup. The roster read that
// notices the change must not wait for a surface, so the sentence is said from
// a goroutine of its own.
func watchStorageOffSentence() {
	syncsetup.OnOthersChange(func(h string) {
		guard.Go("sync/storage-off-sentence", func() { resayStorageOff(h) })
	})
}

// resayStorageOff says the storage-off sentence again when the words true for
// this computer are no longer the ones it said. A change of another home's is
// not this one's news.
func resayStorageOff(h string) {
	if h != home.Dir() || syncsetup.StorageForHome(h) != "off" {
		return
	}
	if line, ok := chatlist.StorageOffChanged(); ok {
		storageOffAgain(line)
	}
}

// storageOffAgain is the one place the replacement goes: the replay each chat
// keeps for a seat that opens later, and the desk the surface reads its notes
// from. It is a variable so a test can hear what would be said.
var storageOffAgain = func(line string) {
	syncDrives.restateStorageOff(line)
	surfaceNotices.Say(line)
}

// restateStorageOff makes every chat that kept a storage-off sentence for the
// seats that open later keep the replacement instead. A chat that kept some other
// sentence, or none, is left alone.
func (b *driveBook) restateStorageOff(line string) {
	for _, n := range b.keptNotices() {
		n.restateStorageOff(line)
	}
}

// keptNotices are the notices of every drive in the book, read under its lock
// and handed back so each can be locked on its own.
func (b *driveBook) keptNotices() []*driveNotices {
	b.mu.Lock()
	defer b.mu.Unlock()
	var kept []*driveNotices
	for _, d := range b.drive {
		if d != nil && d.notices != nil {
			kept = append(kept, d.notices)
		}
	}
	return kept
}

func (n *driveNotices) restateStorageOff(line string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.last == chatlist.RelayStorageOff || n.last == chatlist.RelayStorageOffLive {
		n.last = line
	}
}
