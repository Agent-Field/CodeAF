package directory

import (
	"strings"
	"testing"
)

func testKey() []byte { return MetadataKey([]byte("a cell key of any length")) }

func TestSealOpenRoundTrip(t *testing.T) {
	sealed, err := SealName(testKey(), "laptop")
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenName(testKey(), sealed)
	if err != nil || got != "laptop" {
		t.Fatalf("got %q, %v", got, err)
	}
	again, _ := SealName(testKey(), "laptop")
	if again == sealed {
		t.Fatal("two seals of one name matched: the nonce is not random")
	}
}

func TestOpenRefuses(t *testing.T) {
	sealed, _ := SealName(testKey(), "laptop")
	raw := []byte(sealed)
	raw[len(raw)/2] ^= 1
	tests := []struct{ name, key, sealed string }{
		{"wrong key", string(MetadataKey([]byte("another key"))), sealed},
		{"tampered", string(testKey()), string(raw)},
		{"too short", string(testKey()), "AAAA"},
		{"not base64", string(testKey()), "!!!"},
		{"bad key length", "short", sealed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := OpenName([]byte(tc.key), tc.sealed); err == nil {
				t.Fatalf("opened %q", got)
			}
		})
	}
}

func TestSealTitleCutsBeforeSealing(t *testing.T) {
	tests := []struct{ name, title, want string }{
		{"short is kept", "hello", "hello"},
		{"exactly the limit is kept", strings.Repeat("a", 120), strings.Repeat("a", 120)},
		{"long is cut to 120 bytes", strings.Repeat("a", 500), strings.Repeat("a", 120)},
		{"a character is never split", strings.Repeat("a", 119) + "é", strings.Repeat("a", 119)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sealed, err := SealTitle(testKey(), tc.title)
			if err != nil {
				t.Fatal(err)
			}
			got, err := OpenName(testKey(), sealed)
			if err != nil || got != tc.want {
				t.Fatalf("got %d bytes %q, %v", len(got), got, err)
			}
		})
	}
}

func TestMetadataKeyIsStableAndKeyed(t *testing.T) {
	a, b := MetadataKey([]byte("k1")), MetadataKey([]byte("k1"))
	if string(a) != string(b) || len(a) != 32 || string(a) == string(MetadataKey([]byte("k2"))) {
		t.Fatal("metadata key is not a stable 32-byte function of the cell key")
	}
}
