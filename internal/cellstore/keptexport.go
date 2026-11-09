package cellstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// keptExportName is the note an export leaves in its outbox. It sits beside
// the frames, never inside the store, and only this file reads it.
const keptExportName = "kept-export.json"

// frameExt is the extension the engine gives every frame file it writes.
const frameExt = ".agef"

// AN EXPORT THAT HAS NOT BEEN PUBLISHED IS KEPT, NOT REPEATED.
//
// The engine's export seals every object under a head that the ledger does
// not list and writes the frames afresh, whatever the outbox already holds. A
// frame leaves the outbox only once it is published, so while uploads keep
// failing — a relay that refuses every put, a connection that never finishes
// — every retry sealed the whole chat again: on 2026-11-09 a chat whose folder
// was a repository re-encrypted, hashed and rewrote 470 MB of identical frames
// every minute, for several chats at once, and held a laptop at 400% CPU.
//
// The answer depends only on the head, the keys, the ledger and the frame
// size (furrow's own contract: "a repeat after a crash writes the same
// files"), and a head's objects never change. So an export whose stamp still
// matches, and whose frames are all still on disk at their sizes, is answered
// from the note without asking the engine. Anything that could make it wrong
// breaks the stamp: a publish shrinks the outbox, any record grows the ledger,
// a new turn moves the head, a rotation changes the keys. The worst a stale
// note can cost is one export the engine would have run anyway.
type keptExport struct {
	Head     string `json:"head"`
	Ledger   string `json:"ledger"`
	MaxFrame int    `json:"max_frame"`
	// Keys is a digest of the two secrets, never the secrets: frames sealed
	// under keys this identity no longer holds are not offered again.
	Keys string `json:"keys"`
	// LedgerBytes is the ledger's size when the frames were written. The ledger
	// only grows by appends, so the same size is the same set of records.
	LedgerBytes int64  `json:"ledger_bytes"`
	Export      Export `json:"export"`
}

// keysDigest names a pair of secrets without revealing either.
func keysDigest(k SyncKeys) string {
	h := sha256.New()
	h.Write([]byte("codeaf kept export\x00"))
	h.Write(k.CellKey)
	h.Write([]byte{0})
	h.Write(k.Dedup)
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// ledgerBytes is the size of the named ledger in dataDir; a ledger never
// written is empty. ok is false when the size cannot be known, and then no
// note is trusted.
func ledgerBytes(dataDir, ledger string) (int64, bool) {
	if dataDir == "" {
		return 0, false
	}
	info, err := os.Stat(filepath.Join(dataDir, "published."+ledger))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, true
	}
	if err != nil {
		return 0, false
	}
	return info.Size(), true
}

// stampFor is the note an export of head would leave now, without its answer.
func (e SyncEngine) stampFor(dataDir, head string) (keptExport, bool) {
	size, ok := ledgerBytes(dataDir, e.Ledger)
	return keptExport{Head: head, Ledger: e.Ledger, MaxFrame: e.MaxFrame, Keys: keysDigest(e.Keys), LedgerBytes: size}, ok
}

// sameStamp says two notes describe the same export, answers aside.
func sameStamp(a, b keptExport) bool {
	return a.Head == b.Head && a.Ledger == b.Ledger && a.MaxFrame == b.MaxFrame &&
		a.Keys == b.Keys && a.LedgerBytes == b.LedgerBytes
}

// readKept is the outbox's note, and false when there is none worth reading.
func readKept(outbox string) (keptExport, bool) {
	raw, err := os.ReadFile(filepath.Join(outbox, keptExportName))
	if err != nil {
		return keptExport{}, false
	}
	var k keptExport
	if json.Unmarshal(raw, &k) != nil {
		return keptExport{}, false
	}
	return k, true
}

// reuse answers the kept export when it is still exactly what the engine
// would write: the stamp matches and every frame it names is on disk at its
// size. An empty export is never kept, so a chat with nothing to send still
// asks the engine, which is cheap when nothing is unsent.
func reuse(outbox string, want keptExport) (Export, bool) {
	k, ok := readKept(outbox)
	if !ok || !sameStamp(k, want) || len(k.Export.Frames) == 0 {
		return Export{}, false
	}
	for _, f := range k.Export.Frames {
		info, err := os.Stat(f.Path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != f.Bytes {
			return Export{}, false
		}
	}
	return k.Export, true
}

// keep writes the note for a fresh export, and deletes the frames of the note
// it replaces that the new export does not name: those were sealed for an
// earlier head or ledger, nothing will ever publish them, and leaving them is
// how an outbox grows by a whole chat at every turn while uploads fail. The
// note is best-effort: one that cannot be written only costs a repeat export.
func keep(outbox string, k keptExport) {
	if old, ok := readKept(outbox); ok {
		named := make(map[string]bool, len(k.Export.Frames))
		for _, f := range k.Export.Frames {
			named[filepath.Base(f.Path)] = true
		}
		// Only a name inside this outbox is removed, whatever path the old
		// note spelled, so a damaged note cannot reach outside it.
		for _, f := range old.Export.Frames {
			if name := filepath.Base(f.Path); !named[name] && filepath.Ext(name) == frameExt {
				_ = os.Remove(filepath.Join(outbox, name))
			}
		}
	}
	note := filepath.Join(outbox, keptExportName)
	if len(k.Export.Frames) == 0 {
		_ = os.Remove(note)
		return
	}
	raw, err := json.Marshal(k)
	if err != nil {
		return
	}
	tmp := note + ".part"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, note); err != nil {
		_ = os.Remove(tmp)
	}
}
