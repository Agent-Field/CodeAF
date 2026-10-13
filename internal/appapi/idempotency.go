package appapi

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
	"sync"
)

type mutationReply struct {
	fingerprint [32]byte
	code        int
	header      http.Header
	body        []byte
}
type mutationState struct {
	sync.Mutex
	replies map[string]mutationReply
	order   []string
}
type replyWriter struct {
	header http.Header
	code   int
	bytes.Buffer
}

func (w *replyWriter) Header() http.Header { return w.header }
func (w *replyWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *replyWriter) Write(raw []byte) (int, error) {
	if w.code == 0 {
		w.code = 200
	}
	return w.Buffer.Write(raw)
}

// Idempotency covers the new POST doors for one launch, with bounded storage.
// A replay carries the original response; changed bytes with the same key refuse.
func (s *Server) idempotent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if r.Method != "POST" || key == "" {
			next(w, r)
			return
		}
		if len(key) > 240 {
			fail(w, 422, "bad_input", "idempotency key is too long")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<10+1))
		if err != nil || len(raw) > 32<<10 {
			fail(w, 422, "bad_input", "needs one JSON object of at most 32 KiB")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		fingerprint := sha256.Sum256(raw)
		identity := r.URL.Path + "\x00" + key
		s.mutations.Lock()
		defer s.mutations.Unlock()
		if reply, ok := s.mutations.replies[identity]; ok {
			if reply.fingerprint != fingerprint {
				fail(w, 409, "conflict", "idempotency key was used with different input")
				return
			}
			sendReply(w, reply)
			return
		}
		captured := &replyWriter{header: make(http.Header)}
		next(captured, r)
		reply := mutationReply{fingerprint: fingerprint, code: captured.code, header: captured.header.Clone(), body: append([]byte{}, captured.Bytes()...)}
		if s.mutations.replies == nil {
			s.mutations.replies = make(map[string]mutationReply)
		}
		if len(s.mutations.order) == 128 {
			delete(s.mutations.replies, s.mutations.order[0])
			s.mutations.order = s.mutations.order[1:]
		}
		s.mutations.order = append(s.mutations.order, identity)
		s.mutations.replies[identity] = reply
		sendReply(w, reply)
	}
}
func sendReply(w http.ResponseWriter, reply mutationReply) {
	for key, values := range reply.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	code := reply.code
	if code == 0 {
		code = 200
	}
	w.WriteHeader(code)
	_, _ = w.Write(reply.body)
}
