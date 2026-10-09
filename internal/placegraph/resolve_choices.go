package placegraph

// THE ASK-ONCE ANSWERS (design 6e: "With no common ancestor, the chat asks once
// and remembers the answer").
//
// A pick is per chat and per field, and it names the PLACE whose value won, not
// the value: if that place later changes its model, the chat follows the place
// the person chose rather than a string they never typed. A pick naming a place
// that no longer has an opinion in this chat's context is simply not used, and
// the chat asks again — the question it answered is not the one standing now.
//
// IT IS A SEPARATE FILE FROM THE GRAPH because the graph's document and its
// validator belong to the store (store.go), and a pick is not structure: it does
// not move the revision, it is not undone by ⌘Z, and deleting a place does not
// need to rewrite it. It is written the same way — one flock, read fresh, whole
// file renamed into place — so two processes never lose a pick.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// MaxChoices bounds the choices file; the oldest picks are dropped past it.
const MaxChoices = MaxMemberships

// Choice is one remembered pick.
type Choice struct {
	ChatID  string      `json:"chatId"`
	Field   PolicyField `json:"field"`
	PlaceID string      `json:"placeId"`
	At      time.Time   `json:"at"`
}

type choiceDoc struct {
	Version int      `json:"version"`
	Choices []Choice `json:"choices"`
}

// ChoiceBook is the handle on the choices file. Safe for concurrent use and for
// several processes on one path.
type ChoiceBook struct {
	path string
	mu   sync.Mutex
	now  func() time.Time
}

// OpenChoices prepares a choices file at path (created on the first Set). The
// conventional place is beside the graph: <dir of places.json>/place-choices.json.
func OpenChoices(path string) (*ChoiceBook, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: empty path", ErrInvalid)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &ChoiceBook{path: path, now: time.Now}, nil
}

// For lists chatID's picks.
func (c *ChoiceBook) For(chatID string) ([]Choice, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ReadChoices(c.path, chatID)
}

// Set remembers that, for chatID, field follows placeID. It replaces an earlier
// pick for the same chat and field.
func (c *ChoiceBook) Set(chatID string, field PolicyField, placeID string) (Choice, error) {
	if err := validChatID(chatID); err != nil {
		return Choice{}, err
	}
	if !field.Valid() {
		return Choice{}, fmt.Errorf("%w: policy field %q", ErrInvalid, field)
	}
	if err := validID(placeID); err != nil {
		return Choice{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	lock, err := os.OpenFile(c.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Choice{}, err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return Choice{}, err
	}
	defer filelock.Unlock(lock)
	doc, err := readChoiceDoc(c.path)
	if err != nil {
		return Choice{}, err
	}
	pick := Choice{ChatID: chatID, Field: field, PlaceID: placeID, At: c.now().UTC()}
	kept := doc.Choices[:0]
	for _, have := range doc.Choices {
		if have.ChatID != chatID || have.Field != field {
			kept = append(kept, have)
		}
	}
	kept = append(kept, pick)
	if over := len(kept) - MaxChoices; over > 0 {
		kept = kept[over:]
	}
	doc.Version, doc.Choices = 1, kept
	if err := writeChoiceDoc(c.path, doc); err != nil {
		return Choice{}, err
	}
	return pick, nil
}

// ReadChoices reads chatID's picks WITHOUT the lock (the file is only ever
// replaced whole), for the engine reading them with its turn lock held. A
// missing file is no picks.
func ReadChoices(path, chatID string) ([]Choice, error) {
	doc, err := readChoiceDoc(path)
	if err != nil {
		return nil, err
	}
	var out []Choice
	for _, c := range doc.Choices {
		if c.ChatID == chatID {
			out = append(out, c)
		}
	}
	return out, nil
}

func readChoiceDoc(path string) (choiceDoc, error) {
	data, err := readCapped(path)
	if errors.Is(err, os.ErrNotExist) {
		return choiceDoc{Version: 1}, nil
	}
	if errors.Is(err, errOversize) {
		return choiceDoc{}, fmt.Errorf("%w: choices file over %d bytes", ErrTooLarge, MaxFileBytes)
	}
	if err != nil {
		return choiceDoc{}, err
	}
	var doc choiceDoc
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return choiceDoc{}, fmt.Errorf("%w: choices file is not valid JSON: %v", ErrInvalid, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return choiceDoc{}, fmt.Errorf("%w: unexpected data after the choices document", ErrInvalid)
	}
	if doc.Version > 1 {
		return choiceDoc{}, fmt.Errorf("%w (choices version %d)", ErrUnsupportedVersion, doc.Version)
	}
	return doc, nil
}

func writeChoiceDoc(path string, doc choiceDoc) error {
	data, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, ".place-choices-*.tmp", data)
}
