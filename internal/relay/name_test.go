package relay

import (
	"crypto/ecdh"
	"crypto/rand"
	"strings"
	"testing"
)

// The word list is the name space. A duplicate in it would silently halve a
// slot's worth of names, and a short list would make the arithmetic in the
// header of name.go a lie.
func TestTheWordListIsTwoHundredAndFiftySixDistinctWords(t *testing.T) {
	seen := map[string]bool{}
	for i, word := range nameWords {
		if word == "" {
			t.Fatalf("word %d is empty", i)
		}
		if seen[word] {
			t.Fatalf("word %d, %q, is in the list twice", i, word)
		}
		seen[word] = true
		for _, r := range word {
			if r < 'a' || r > 'z' {
				t.Fatalf("word %d, %q, is not plain lower-case letters", i, word)
			}
		}
	}
	if len(seen) != 256 {
		t.Fatalf("the list holds %d words, not 256", len(seen))
	}
}

// A NAME IS A FACT ABOUT A KEY. The same key always answers to the same name,
// on any machine, in any process, for ever — which is what lets a person write
// one down.
func TestANameIsTheSameEveryTimeForTheSameKey(t *testing.T) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	first := NameFor(key.PublicKey().Bytes())
	for i := 0; i < 10; i++ {
		if again := NameFor(key.PublicKey().Bytes()); again != first {
			t.Fatalf("the same key answered to %q and then to %q", first, again)
		}
	}
	if !ValidName(first) {
		t.Fatalf("a minted name %q does not pass the shape check", first)
	}
	parts := strings.Split(first, "-")
	if len(parts) != 3 || len(parts[2]) != 2 {
		t.Fatalf("a name should read like otter-lamp-42, and this one is %q", first)
	}
}

func TestDifferentKeysGetDifferentNames(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		seen[NameFor(key.PublicKey().Bytes())] = true
	}
	// Two hundred draws out of nearly six million: a collision here would mean
	// the derivation is not spreading, not that we were unlucky.
	if len(seen) < 199 {
		t.Fatalf("200 keys produced only %d names", len(seen))
	}
}

func TestTheShapeCheckRefusesWhatIsNotAName(t *testing.T) {
	for _, bad := range []string{
		"", "otter", "otter-lamp", "otter-lamp-4", "otter-lamp-420",
		"otter-lamp-4a", "Otter-lamp-42", "otter lamp 42", "../etc/passwd",
		"otter-lamp-42-extra", strings.Repeat("x", 80) + "-lamp-42",
	} {
		if ValidName(bad) {
			t.Fatalf("%q was accepted as a machine name", bad)
		}
	}
	if !ValidName("otter-lamp-42") {
		t.Fatal("otter-lamp-42 was refused as a machine name")
	}
}
