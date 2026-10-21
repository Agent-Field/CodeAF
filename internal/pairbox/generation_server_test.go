package pairbox

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSelfHostedGenerationFence proves C19: a client of an earlier opening
// cannot read, append to or delete a later mailbox that reuses its nameplate.
func TestSelfHostedGenerationFence(t *testing.T) {
	m := NewMemory(DefaultLimits, nil)
	server := httptest.NewServer(Handler(m, SocketPeer))
	defer server.Close()
	client := NewHTTP(server.URL, server.Client())
	key, _ := NewKey()
	made, err := client.Create(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	old := client.gens[made.Nameplate]
	if len(old) != 32 {
		t.Fatalf("generation = %q", old)
	}
	if err := client.Delete(context.Background(), made.Nameplate, key); err != nil {
		t.Fatal(err)
	}
	// A forced reuse makes the race deterministic without waiting for a random draw.
	m.mu.Lock()
	replacement := newMailbox(m.now().Add(DefaultLimits.TTL))
	replacement.generation = "0123456789abcdef0123456789abcdef"
	replacement.claim(SideA.index(), key)
	m.boxes[made.Nameplate] = replacement
	m.mu.Unlock()
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		path := Path + "/" + made.Nameplate
		if method != http.MethodDelete {
			path += "/a"
		}
		for _, generation := range []string{old, "malformed", ""} {
			req, _ := http.NewRequest(method, server.URL+path, bytes.NewBufferString("ciphertext"))
			req.Header.Set(KeyHeader, key.String())
			req.Header.Set(GenHeader, generation)
			res, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("%s with %q = %d", method, generation, res.StatusCode)
			}
		}
	}
	// A pre-generation client remains compatible and learns the current opening.
	legacy := NewHTTP(server.URL, server.Client())
	if _, err := legacy.Poll(context.Background(), made.Nameplate, SideA, 0, 0); err != nil {
		t.Fatal(err)
	}
	if legacy.gens[made.Nameplate] != replacement.generation {
		t.Fatal("client did not learn the current generation")
	}
}
