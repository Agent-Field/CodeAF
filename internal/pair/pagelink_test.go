package pair

import (
	"encoding/json"
	"os"
	"testing"
)

// The link the Worker's browser page builds (relay/hosted/test/page.test.js reads the same file)
// must give the app the same code and key.
func TestPageDeepLinkReadsBack(t *testing.T) {
	raw, err := os.ReadFile("testdata/pagelink.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Code, Key, URL string }
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	ref, err := ReadLink(v.URL)
	if err != nil {
		t.Fatalf("ReadLink(%q): %v", v.URL, err)
	}
	if ref.Code != v.Code || b64u.EncodeToString(ref.Key) != v.Key {
		t.Fatalf("got code %q key %q, want %q %q", ref.Code, b64u.EncodeToString(ref.Key), v.Code, v.Key)
	}
}
