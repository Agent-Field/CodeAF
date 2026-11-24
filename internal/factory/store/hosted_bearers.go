package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ── THE SAVED MODEL ACCESS OF HOSTED CHATS ─────────────────────────────────
//
// A hosted chat's engine is given a model bearer once and keeps it for as long
// as the chat runs, so the organization's server must still recognise that
// bearer after the server itself restarts. The table of live bearers is saved
// here for that reason and for no other.
//
// THE FILE HOLDS NO BEARER. Each row is keyed by the sha256 of the text a chat
// presents, so a copy of this file, a backup or a screenshot of it holds
// nothing a chat can say; the row carries only whose chat it is, whose
// person, and when it ends. It is one file only this server's user may read
// (0600, written atomically under a lock).

// HostedBearer is one saved row: the chat the bearer belongs to, the person who
// owns that chat, and the moment the bearer stops working.
type HostedBearer struct {
	Session string    `json:"session"`
	Person  string    `json:"person"`
	Expires time.Time `json:"expires"`
}

type hostedBearersDoc struct {
	Rows map[string]HostedBearer `json:"rows"`
}

const hostedBearersFile = "hosted-bearers.json"

// ErrHostedBearersUnreadable is every way the saved table can fail to read. It
// names no byte of the file: a damaged file is an operator's to look at, and an
// error that quoted it could repeat whatever was in it.
var ErrHostedBearersUnreadable = errors.New("the saved model access of hosted chats cannot be read")

// HostedBearers is the saved table by the sha256 hex of each bearer. A file
// that was never written is an empty table, and a file that is not the table
// is an error.
func (st *Store) HostedBearers() (map[string]HostedBearer, error) {
	raw, err := os.ReadFile(filepath.Join(st.root, hostedBearersFile))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]HostedBearer{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc hostedBearersDoc
	if json.Unmarshal(raw, &doc) != nil {
		return nil, ErrHostedBearersUnreadable
	}
	if doc.Rows == nil {
		doc.Rows = map[string]HostedBearer{}
	}
	return doc.Rows, nil
}

// SaveHostedBearers replaces the whole saved table. The table is small (one row
// for each chat that is running), so a replace is cheaper than any scheme that
// merges, and it is what makes an ended bearer disappear from the file.
func (st *Store) SaveHostedBearers(rows map[string]HostedBearer) error {
	if rows == nil {
		rows = map[string]HostedBearer{}
	}
	raw, err := json.Marshal(hostedBearersDoc{Rows: rows})
	if err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, "hosted-bearers.lock"), func() error {
		return writeAtomic(filepath.Join(st.root, hostedBearersFile), append(raw, '\n'))
	})
}
