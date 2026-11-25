package store

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testServerKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func openKeys(t *testing.T, kek []byte) (*Store, *PersonKeys) {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pk, err := OpenPersonKeys(st, kek)
	if err != nil {
		t.Fatal(err)
	}
	return st, pk
}

var keysAt = time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)

// P1-5: a stored key never appears in the file, belongs to exactly one person,
// is listed by its last four characters only, and is sealed freshly each time.
func TestPersonKeysAreSealedAndBelongToOnePerson(t *testing.T) {
	st, pk := openKeys(t, testServerKey(t))
	santosh, abir, token := "sk-test-santosh-aaaa-0001", "sk-test-abir-bbbb-0002", "ghp_test_santosh_github_0003"
	for _, put := range []struct{ person, provider, secret, source string }{
		{"santosh", "openrouter", santosh, PersonKeyOwn}, {"abir", "openrouter", abir, PersonKeyOwn}, {"santosh", "github", token, PersonKeySignIn}} {
		if err := pk.Put(put.person, put.provider, put.secret, put.source, keysAt); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(st.Root(), personKeysFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{santosh, abir, token, "aaaa-0001", "santosh_github"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("the file holds %q in the clear: %s", secret, raw)
		}
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the file's mode = %v, %v; want 0600", info, err)
	}
	if got, ok, err := pk.Get("santosh", "openrouter"); err != nil || !ok || got != santosh {
		t.Fatalf("santosh's key = %q, %v, %v", got, ok, err)
	}
	if got, ok, err := pk.Get("@Abir", "openrouter"); err != nil || !ok || got != abir {
		t.Fatalf("abir's key by a display form = %q, %v, %v", got, ok, err)
	}
	// Nobody reads another person's key, and a near name is not the name.
	for _, who := range []string{"mia", "santos", "santosh2", ""} {
		if got, ok, err := pk.Get(who, "openrouter"); got != "" || ok || err != nil {
			t.Fatalf("%q read %q, %v, %v", who, got, ok, err)
		}
	}
	if got, ok, err := pk.Get("abir", "github"); got != "" || ok || err != nil {
		t.Fatalf("abir has no GitHub key, got %q, %v, %v", got, ok, err)
	}
	rows := pk.List("santosh")
	if len(rows) != 2 || rows[0].Provider != "github" || rows[0].Last4 != "0003" || rows[0].Source != PersonKeySignIn ||
		rows[1].Provider != "openrouter" || rows[1].Last4 != "0001" || rows[1].Source != PersonKeyOwn || !rows[1].At.Equal(keysAt) {
		t.Fatalf("santosh's list = %+v", rows)
	}
	if rows := pk.List("mia"); rows == nil || len(rows) != 0 {
		t.Fatalf("a person with no keys lists %+v; want an empty list", rows)
	}
	// A new seal of the same secret is a different byte string.
	var before map[string]any
	_ = json.Unmarshal(raw, &before)
	if err := pk.Put("santosh", "openrouter", santosh, PersonKeyOwn, keysAt); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if bytes.Equal(raw, again) {
		t.Fatal("sealing the same secret twice wrote the same bytes: the nonce is not fresh")
	}
	// Another open of the same store sees the same keys (a restart).
	reopened, err := OpenPersonKeys(st, pk.kek)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok, err := reopened.Get("santosh", "openrouter"); err != nil || !ok || got != santosh {
		t.Fatalf("after a reopen = %q, %v, %v", got, ok, err)
	}
	// A short secret hides nothing behind its last four.
	if err := pk.Put("mia", "github", "short-12", PersonKeyOwn, keysAt); err != nil {
		t.Fatal(err)
	}
	if rows := pk.List("mia"); len(rows) != 1 || rows[0].Last4 != "" {
		t.Fatalf("a short secret's last four = %+v", rows)
	}
}

// P1-5: a different server key refuses to open the store and leaves the file
// alone; a store opened with no key answers that a key exists but never opens or
// changes one.
func TestPersonKeysWrongServerKeyAndNoServerKey(t *testing.T) {
	good := testServerKey(t)
	st, pk := openKeys(t, good)
	if err := pk.Put("santosh", "openrouter", "sk-test-santosh-0001", PersonKeyOwn, keysAt); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(st.Root(), personKeysFile)
	before, _ := os.ReadFile(path)
	if _, err := OpenPersonKeys(st, testServerKey(t)); !errors.Is(err, ErrWrongServerKey) {
		t.Fatalf("a wrong server key opened the store: %v", err)
	}
	locked, err := OpenPersonKeys(st, nil)
	if err != nil || !locked.Locked() || !locked.AnyPeople() {
		t.Fatalf("the locked store = %v, %v", locked, err)
	}
	if has, err := locked.Has("santosh", "openrouter"); err != nil || !has {
		t.Fatalf("a locked store lost track of the key: %v, %v", has, err)
	}
	if got, ok, err := locked.Get("santosh", "openrouter"); got != "" || !ok || !errors.Is(err, ErrServerKeyLocked) {
		t.Fatalf("a locked store opened the key: %q, %v, %v", got, ok, err)
	}
	if err := locked.Put("santosh", "openrouter", "sk-test-other-9999", PersonKeyOwn, keysAt); !errors.Is(err, ErrServerKeyLocked) {
		t.Fatalf("a locked store took a key: %v", err)
	}
	if _, err := locked.Delete("santosh", "openrouter"); !errors.Is(err, ErrServerKeyLocked) {
		t.Fatalf("a locked store forgot a key: %v", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("refusing left the file changed")
	}
	// A writer holding the wrong key built before the file existed cannot
	// overwrite either: it is refused at the write.
	stray := &PersonKeys{st: st, kek: testServerKey(t)}
	if err := stray.Put("mia", "openrouter", "sk-test-mia-000000", PersonKeyOwn, keysAt); !errors.Is(err, ErrWrongServerKey) {
		t.Fatalf("a wrong key wrote into the store: %v", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("a refused write changed the file")
	}
}

// P1-5: a sealed value moved to another person or provider does not open, and a
// damaged file is an error that quotes none of it.
func TestPersonKeysSealedValuesAreBoundToTheirOwner(t *testing.T) {
	st, pk := openKeys(t, testServerKey(t))
	for _, who := range []string{"santosh", "abir"} {
		if err := pk.Put(who, "openrouter", "sk-test-"+who+"-secret-0001", PersonKeyOwn, keysAt); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(st.Root(), personKeysFile)
	raw, _ := os.ReadFile(path)
	var doc personKeysDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	swapped := doc.People["abir"].Keys["openrouter"]
	swapped.Sealed = doc.People["santosh"].Keys["openrouter"].Sealed
	doc.People["abir"].Keys["openrouter"] = swapped
	moved, _ := json.Marshal(doc)
	if err := os.WriteFile(path, moved, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenPersonKeys(st, pk.kek)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := reopened.Get("abir", "openrouter"); got != "" || !errors.Is(err, ErrPersonKeyOpen) {
		t.Fatalf("another person's sealed value opened as abir's: %q, %v", got, err)
	}
	// Garbage is an error that does not repeat it.
	garbage := "sk-test-the-damaged-file-text"
	if err := os.WriteFile(path, []byte(`{"version":1,"people": [`+garbage), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenPersonKeys(st, pk.kek); !errors.Is(err, ErrPersonKeysUnreadable) || strings.Contains(err.Error(), garbage) {
		t.Fatalf("a damaged file opened as %v", err)
	}
}

// P1-5: deleting answers whether a key was there, only touches its own
// person, and a person's last key takes the person with it.
func TestPersonKeysDeleteIsPerPersonAndPerProvider(t *testing.T) {
	_, pk := openKeys(t, testServerKey(t))
	for _, put := range []struct{ person, provider string }{{"santosh", "openrouter"}, {"santosh", "github"}, {"abir", "openrouter"}} {
		if err := pk.Put(put.person, put.provider, "secret-value-for-"+put.person+"-"+put.provider, PersonKeyOwn, keysAt); err != nil {
			t.Fatal(err)
		}
	}
	if had, err := pk.Delete("santosh", "openrouter"); err != nil || !had {
		t.Fatalf("delete = %v, %v", had, err)
	}
	if had, err := pk.Delete("santosh", "openrouter"); err != nil || had {
		t.Fatalf("a second delete = %v, %v; want none", had, err)
	}
	if got, ok, _ := pk.Get("santosh", "github"); !ok || got == "" {
		t.Fatal("deleting one provider took the other")
	}
	if got, ok, _ := pk.Get("abir", "openrouter"); !ok || got == "" {
		t.Fatal("deleting santosh's key took abir's")
	}
	_, _ = pk.Delete("santosh", "github")
	_, _ = pk.Delete("abir", "openrouter")
	if pk.AnyPeople() {
		t.Fatal("the store still holds people after every key was forgotten")
	}
	if err := pk.Put("santosh", "nope", "secret-value-abcdef", PersonKeyOwn, keysAt); err == nil {
		t.Fatal("an unknown provider was stored")
	}
}
