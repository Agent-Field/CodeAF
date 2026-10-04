package pairboxtest

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// The collection cannot be listed: there is no way to learn what nameplates
// are live, so a guess is the only way in.
func noListing(t *testing.T, e env) {
	w := e.wireOf(t)
	for _, path := range []string{pairbox.Path, pairbox.Path + "/"} {
		if status, body := w.do(t, http.MethodGet, path, nil, nil); status != http.StatusNotFound {
			t.Errorf("GET %s = %d %q, want 404", path, status, body)
		}
	}
}

// No answer, success or refusal, repeats a side key back to anyone. A key is
// what owns a side, and a relay that echoed one would hand it to a poller.
func noKeyEcho(t *testing.T, e env) {
	w := e.wireOf(t)
	m, body := w.create(t, e)
	stranger := newKey(t)
	answers := [][]byte{body}
	for _, step := range []struct {
		method, path string
		key          *pairbox.Key
		body         []byte
	}{
		{http.MethodPost, side(m, pairbox.SideA), &m.key, []byte("hello")},
		{http.MethodPost, side(m, pairbox.SideA), &stranger, []byte("hello")},
		{http.MethodGet, side(m, pairbox.SideA) + "?after=0", nil, nil},
		{http.MethodDelete, pairbox.Path + "/" + m.plate, &stranger, nil},
	} {
		_, out := w.do(t, step.method, step.path, step.key, step.body)
		answers = append(answers, out)
	}
	for _, key := range []pairbox.Key{m.key, stranger} {
		for _, out := range answers {
			if bytes.Contains(out, []byte(key.String())) {
				t.Errorf("an answer carries a side key: %q", out)
			}
		}
	}
}

func side(m mailbox, s pairbox.Side) string {
	return pairbox.Path + "/" + m.plate + "/" + string(s)
}
