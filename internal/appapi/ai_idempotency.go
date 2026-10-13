package appapi

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
)

// aiReplyWriter preserves live flushing while retaining one response for replay.
// The cached response contains events, never a second paid model call.
type aiReplyWriter struct {
	http.ResponseWriter
	reply *replyWriter
}

func (w *aiReplyWriter) WriteHeader(code int) {
	w.reply.WriteHeader(code)
	w.ResponseWriter.WriteHeader(code)
}
func (w *aiReplyWriter) Write(raw []byte) (int, error) {
	_, _ = w.reply.Write(raw)
	return w.ResponseWriter.Write(raw)
}
func (w *aiReplyWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (s *Server) aiIdempotent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			next(w, r)
			return
		}
		if len(key) > 240 {
			fail(w, 422, "bad_input", "idempotency key is too long")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, (32<<10)+1))
		if err != nil || len(raw) > 32<<10 {
			fail(w, 422, "bad_input", "needs one JSON object of at most 32 KiB")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		fingerprint := sha256.Sum256(raw)
		identity := r.URL.Path + "\x00" + key
		// The shared launch cache serializes keyed mutations until this call ends.
		// Holding it also makes simultaneous retries of the same key pay only once.
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
		captured := &replyWriter{header: w.Header()}
		next(&aiReplyWriter{ResponseWriter: w, reply: captured}, r)
		reply := mutationReply{fingerprint: fingerprint, code: captured.code, header: captured.header.Clone(), body: append([]byte{}, captured.Bytes()...)}
		if s.mutations.replies == nil {
			s.mutations.replies = map[string]mutationReply{}
		}
		if len(s.mutations.order) == 128 {
			delete(s.mutations.replies, s.mutations.order[0])
			s.mutations.order = s.mutations.order[1:]
		}
		s.mutations.order = append(s.mutations.order, identity)
		s.mutations.replies[identity] = reply
	}
}
