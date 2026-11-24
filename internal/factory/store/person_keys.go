package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// ── A PERSON'S OWN KEYS, SEALED ────────────────────────────────────────────
//
// A person who signs in to an organization's server may keep a model key or a
// GitHub credential of their own on it, so their chats spend their account and
// act as them. Those secrets are the most dangerous thing this store holds, so
// they are never written in the clear and never come back out except through
// [PersonKeys.Get], which the server's own model and git paths call and no
// route answers.
//
// Two layers. A SERVER KEY (the KEK, from the operator's CODEAF_SERVER_KEY)
// seals one random data key (the DEK) for each person; each person's secrets
// are sealed under their own DEK. A copy of the file alone therefore opens
// nothing, and the loss of one person's data key exposes nobody else. Every
// seal is AES-256-GCM with a fresh random nonce, and its additional data names
// what it belongs to ("dek|<person>", "<person>|<provider>", "canary"), so a
// sealed value moved to another person or provider fails to open instead of
// being used for the wrong one.
//
// A CANARY, sealed under the server key when the file is first written, says
// whether the key the server started with is the key the file was sealed with.
// A wrong key therefore refuses at once, before a single secret is opened
// wrongly or a new one is sealed under a key the old ones cannot be read with.

// PersonKeyProviders are the providers a person may keep a secret for.
var PersonKeyProviders = []string{modelsource.DefaultID, "github"}

// KnownPersonKeyProvider says p is a provider a person may keep a secret for.
func KnownPersonKeyProvider(p string) bool {
	for _, known := range PersonKeyProviders {
		if p == known {
			return true
		}
	}
	return false
}

// The two places a stored secret can come from. A person's own save always
// wins over what their sign-in carried.
const (
	PersonKeyOwn    = "own"
	PersonKeySignIn = "sign-in"
)

var (
	// ErrWrongServerKey is the server key not being the key the stored secrets
	// were sealed with.
	ErrWrongServerKey = errors.New("the server key does not open the stored personal keys")
	// ErrServerKeyLocked is a store opened with no server key: it can say
	// whether a secret exists, and it can open or change none.
	ErrServerKeyLocked = errors.New("the stored personal keys are locked because no server key is set")
	// ErrPersonKeysUnreadable is a file that is not the store's own format. It
	// quotes none of it.
	ErrPersonKeysUnreadable = errors.New("the stored personal keys cannot be read")
	// ErrPersonKeyOpen is one secret that would not open (a damaged file).
	ErrPersonKeyOpen = errors.New("a stored personal key cannot be opened")
)

const (
	personKeysFile   = "person-keys.json"
	personKeysCanary = "codeaf person keys"
)

// PersonKeyRow is one stored secret as a screen may see it: which provider,
// the last four characters and nothing more.
type PersonKeyRow struct {
	Provider string
	Last4    string
	At       time.Time
	Source   string
}

type personKeysDoc struct {
	Version int                      `json:"version"`
	Canary  string                   `json:"canary"`
	People  map[string]*personRecord `json:"people"`
}

type personRecord struct {
	DEK  string                  `json:"dek"`
	Keys map[string]personKeyRow `json:"keys"`
}

type personKeyRow struct {
	Sealed string `json:"sealed"`
	Last4  string `json:"last4"`
	At     string `json:"at"`
	Source string `json:"source"`
}

// PersonKeys is the sealed store of every person's own keys.
type PersonKeys struct {
	st  *Store
	kek []byte // nil when the store is locked

	mu     sync.Mutex
	doc    *personKeysDoc
	stamp  keysFileStamp
	loaded bool
}

// keysFileStamp is what says the file has not changed since it was read, so a read
// of a secret costs one stat and not a parse of the whole file.
type keysFileStamp struct {
	exists bool
	size   int64
	mod    time.Time
}

// OpenPersonKeys opens the store with kek, the server key (32 bytes). A file
// whose canary does not open under kek answers [ErrWrongServerKey] and is left
// untouched. A nil kek opens the store LOCKED: it still answers whether a
// person has a key and what a screen may show of it, and it refuses to open or
// change a secret. Nothing is written until the first secret is stored.
func OpenPersonKeys(st *Store, kek []byte) (*PersonKeys, error) {
	if st == nil {
		return nil, errors.New("personal keys need a store")
	}
	pk := &PersonKeys{st: st}
	if kek != nil {
		if len(kek) != 32 {
			return nil, errors.New("the server key must be 32 bytes")
		}
		pk.kek = append([]byte(nil), kek...)
	}
	doc, err := pk.snapshot()
	if err != nil {
		return nil, err
	}
	if pk.kek != nil && (doc.Canary != "" || len(doc.People) > 0) {
		if err := pk.checkCanary(doc); err != nil {
			return nil, err
		}
	}
	return pk, nil
}

// Locked says the store was opened with no server key.
func (pk *PersonKeys) Locked() bool { return pk.kek == nil }

func (pk *PersonKeys) checkCanary(doc *personKeysDoc) error {
	plain, err := openGCM(pk.kek, doc.Canary, []byte("canary"))
	if err != nil || string(plain) != personKeysCanary {
		return ErrWrongServerKey
	}
	return nil
}

func (pk *PersonKeys) path() string { return filepath.Join(pk.st.root, personKeysFile) }

func (f keysFileStamp) same(o keysFileStamp) bool {
	return f.exists == o.exists && f.size == o.size && f.mod.Equal(o.mod)
}

func statKeysFile(path string) keysFileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return keysFileStamp{}
	}
	return keysFileStamp{exists: true, size: info.Size(), mod: info.ModTime()}
}

// snapshot is the file as it is now. It reparses only when the file changed
// since the last read, so the same process answering a model call per request
// does not read the whole store each time. The returned document is never
// modified by a caller.
func (pk *PersonKeys) snapshot() (*personKeysDoc, error) {
	pk.mu.Lock()
	defer pk.mu.Unlock()
	stamp := statKeysFile(pk.path())
	if pk.loaded && pk.stamp.same(stamp) {
		return pk.doc, nil
	}
	doc, err := pk.readFile()
	if err != nil {
		return nil, err
	}
	pk.doc, pk.stamp, pk.loaded = doc, stamp, true
	return doc, nil
}

// readFile parses the file; a file never written is an empty store.
func (pk *PersonKeys) readFile() (*personKeysDoc, error) {
	raw, err := os.ReadFile(pk.path())
	if errors.Is(err, os.ErrNotExist) {
		return &personKeysDoc{Version: 1, People: map[string]*personRecord{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc personKeysDoc
	if json.Unmarshal(raw, &doc) != nil || doc.Version != 1 {
		return nil, ErrPersonKeysUnreadable
	}
	if doc.People == nil {
		doc.People = map[string]*personRecord{}
	}
	return &doc, nil
}

// AnyPeople says somebody has a secret stored here.
func (pk *PersonKeys) AnyPeople() bool {
	doc, err := pk.snapshot()
	return err == nil && len(doc.People) > 0
}

// Has says person has a secret stored for provider. It needs no server key, so
// a locked store can still tell a caller that a key EXISTS and must not be
// replaced by the organization's.
func (pk *PersonKeys) Has(person, provider string) (bool, error) {
	doc, err := pk.snapshot()
	if err != nil {
		return false, err
	}
	rec := doc.People[factory.NormalizeHandle(person)]
	if rec == nil {
		return false, nil
	}
	_, ok := rec.Keys[provider]
	return ok, nil
}

// List is what a screen may show of person's secrets, by provider, and nothing
// else: never a secret, never another person's row.
func (pk *PersonKeys) List(person string) []PersonKeyRow {
	out := []PersonKeyRow{}
	doc, err := pk.snapshot()
	if err != nil {
		return out
	}
	rec := doc.People[factory.NormalizeHandle(person)]
	if rec == nil {
		return out
	}
	for provider, row := range rec.Keys {
		at, _ := time.Parse(time.RFC3339, row.At)
		out = append(out, PersonKeyRow{Provider: provider, Last4: row.Last4, At: at, Source: row.Source})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// Get opens person's secret for provider. It is the only call that answers a
// secret. A missing one is ("", false, nil); one that exists and will not open
// is an error, never a quiet miss, because the caller must refuse and not fall
// back to somebody else's key.
func (pk *PersonKeys) Get(person, provider string) (string, bool, error) {
	person = factory.NormalizeHandle(person)
	doc, err := pk.snapshot()
	if err != nil {
		return "", false, err
	}
	rec := doc.People[person]
	if rec == nil {
		return "", false, nil
	}
	row, ok := rec.Keys[provider]
	if !ok {
		return "", false, nil
	}
	if pk.kek == nil {
		return "", true, ErrServerKeyLocked
	}
	dek, err := openGCM(pk.kek, rec.DEK, []byte("dek|"+person))
	if err != nil {
		return "", true, ErrPersonKeyOpen
	}
	defer zero(dek)
	plain, err := openGCM(dek, row.Sealed, []byte(person+"|"+provider))
	if err != nil {
		return "", true, ErrPersonKeyOpen
	}
	secret := string(plain)
	zero(plain)
	return secret, true, nil
}

// Put seals secret as person's key for provider, replacing the one before.
// source is [PersonKeyOwn] or [PersonKeySignIn].
func (pk *PersonKeys) Put(person, provider, secret, source string, at time.Time) error {
	person = factory.NormalizeHandle(person)
	if pk.kek == nil {
		return ErrServerKeyLocked
	}
	if person == "" || !KnownPersonKeyProvider(provider) || secret == "" || (source != PersonKeyOwn && source != PersonKeySignIn) {
		return errors.New("that personal key cannot be stored")
	}
	return pk.change(func(doc *personKeysDoc) error {
		if doc.Canary == "" {
			canary, err := sealGCM(pk.kek, []byte(personKeysCanary), []byte("canary"))
			if err != nil {
				return err
			}
			doc.Canary = canary
		}
		rec := doc.People[person]
		var dek []byte
		if rec == nil {
			dek = make([]byte, 32)
			if _, err := rand.Read(dek); err != nil {
				return err
			}
			wrapped, err := sealGCM(pk.kek, dek, []byte("dek|"+person))
			if err != nil {
				zero(dek)
				return err
			}
			rec = &personRecord{DEK: wrapped, Keys: map[string]personKeyRow{}}
			doc.People[person] = rec
		} else {
			var err error
			if dek, err = openGCM(pk.kek, rec.DEK, []byte("dek|"+person)); err != nil {
				return ErrPersonKeyOpen
			}
		}
		defer zero(dek)
		sealed, err := sealGCM(dek, []byte(secret), []byte(person+"|"+provider))
		if err != nil {
			return err
		}
		rec.Keys[provider] = personKeyRow{Sealed: sealed, Last4: lastFour(secret), At: at.UTC().Format(time.RFC3339), Source: source}
		return nil
	})
}

// Delete forgets person's secret for provider and says whether there was one.
// A person's last secret takes the person's data key with it.
func (pk *PersonKeys) Delete(person, provider string) (bool, error) {
	person = factory.NormalizeHandle(person)
	if pk.kek == nil {
		return false, ErrServerKeyLocked
	}
	var had bool
	err := pk.change(func(doc *personKeysDoc) error {
		rec := doc.People[person]
		if rec == nil {
			return nil
		}
		if _, had = rec.Keys[provider]; !had {
			return nil
		}
		delete(rec.Keys, provider)
		if len(rec.Keys) == 0 {
			delete(doc.People, person)
		}
		return nil
	})
	return had, err
}

// change reads the file afresh under the store's lock, applies edit, and writes
// it back atomically, so two writers cannot lose one another's edit. A file
// whose canary does not open under this store's key is never written to.
func (pk *PersonKeys) change(edit func(*personKeysDoc) error) error {
	return pk.st.underLock(filepath.Join(pk.st.root, "person-keys.lock"), func() error {
		doc, err := pk.readFile()
		if err != nil {
			return err
		}
		if doc.Canary != "" || len(doc.People) > 0 {
			if err := pk.checkCanary(doc); err != nil {
				return err
			}
		}
		if err := edit(doc); err != nil {
			return err
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		if err := writeAtomic(pk.path(), append(raw, '\n')); err != nil {
			return err
		}
		pk.mu.Lock()
		pk.doc, pk.stamp, pk.loaded = doc, statKeysFile(pk.path()), true
		pk.mu.Unlock()
		return nil
	})
}

// sealGCM seals plain under key, with aad bound to it, as base64 of nonce then
// ciphertext. The nonce is fresh and random for every seal.
func sealGCM(key, plain, aad []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plain, aad)), nil
}

// openGCM is the inverse of sealGCM; any failure, a wrong key, wrong aad or a
// changed byte, is the one error.
func openGCM(key []byte, encoded string, aad []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrPersonKeyOpen
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrPersonKeyOpen
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize()+gcm.Overhead() {
		return nil, ErrPersonKeyOpen
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], aad)
	if err != nil {
		return nil, ErrPersonKeyOpen
	}
	return plain, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
