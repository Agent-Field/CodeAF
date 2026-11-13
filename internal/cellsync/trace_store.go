package cellsync

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// ObserveStore is opt-in. The nil path preserves the exact underlying store.
// Counts at this seam are encrypted payload bytes, not HTTP wire bytes.
func ObserveStore(inner blobstore.Store, observer Observer) blobstore.Store {
	if observer == nil {
		return inner
	}
	return tracedStore{Store: inner, observer: observer}
}

type tracedStore struct {
	blobstore.Store
	observer Observer
}

func (s tracedStore) PutFrame(ctx context.Context, frame []byte) (blobstore.FrameID, error) {
	start := time.Now()
	id, err := s.Store.PutFrame(ctx, frame)
	header, _, _ := blobstore.Decode(frame)
	observed(s.observer, ctx, TraceEvent{Op: "store.put_frame", Layer: "store", Class: "encrypted_frame", RID: id, Objects: len(header.Objects), PayloadBytes: int64(len(frame))}, start, err)
	return id, err
}
func (s tracedStore) Get(ctx context.Context, rid string) ([]byte, error) {
	start := time.Now()
	out, err := s.Store.Get(ctx, rid)
	observed(s.observer, ctx, TraceEvent{Op: "store.get", Layer: "store", Class: "encrypted_object", RID: rid, Objects: 1, PayloadBytes: int64(len(out))}, start, err)
	return out, err
}
func (s tracedStore) GetFrame(ctx context.Context, id string) ([]byte, error) {
	start := time.Now()
	out, err := s.Store.GetFrame(ctx, id)
	observed(s.observer, ctx, TraceEvent{Op: "store.get_frame", Layer: "store", Class: "encrypted_frame", RID: id, PayloadBytes: int64(len(out))}, start, err)
	return out, err
}
func (s tracedStore) GetMany(ctx context.Context, rids []string) ([]blobstore.Object, error) {
	start := time.Now()
	out, err := s.Store.GetMany(ctx, rids)
	var size int64
	for _, object := range out {
		size += int64(len(object.Bytes))
	}
	observed(s.observer, ctx, TraceEvent{Op: "store.get_many", Layer: "store", Class: "encrypted_objects", Objects: len(out), PayloadBytes: size}, start, err)
	return out, err
}
func (s tracedStore) Has(ctx context.Context, rids []string) ([]bool, error) {
	start := time.Now()
	out, err := s.Store.Has(ctx, rids)
	observed(s.observer, ctx, TraceEvent{Op: "store.has", Layer: "store", Class: "control", Objects: len(rids)}, start, err)
	return out, err
}
func (s tracedStore) Locate(ctx context.Context, rids []string) (map[string]blobstore.Location, error) {
	start := time.Now()
	out, err := s.Store.Locate(ctx, rids)
	observed(s.observer, ctx, TraceEvent{Op: "store.locate", Layer: "store", Class: "control", Objects: len(rids)}, start, err)
	return out, err
}

// ObserveHTTP counts request/response BODY bytes separately from decoded object
// bytes. It logs no URL, header, authentication, body, or error text. Requests
// and streaming bodies are not read ahead or retried by this adapter.
func ObserveHTTP(inner http.RoundTripper, observer Observer) http.RoundTripper {
	if observer == nil {
		return inner
	}
	if inner == nil {
		inner = http.DefaultTransport
	}
	return tracedHTTP{inner: inner, observer: observer}
}

type tracedHTTP struct {
	inner    http.RoundTripper
	observer Observer
}
type countedBody struct {
	io.ReadCloser
	count  *atomic.Int64
	finish func(error, bool)
}

func (b countedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.count.Add(int64(n))
	if err != nil && b.finish != nil {
		b.finish(err, err == io.EOF)
	}
	return n, err
}
func (b countedBody) Close() error {
	err := b.ReadCloser.Close()
	if b.finish != nil {
		b.finish(err, false)
	}
	return err
}
func (t tracedHTTP) RoundTrip(request *http.Request) (*http.Response, error) {
	start := time.Now()
	var sent, received atomic.Int64
	copy := request.Clone(request.Context())
	if copy.Body != nil {
		copy.Body = countedBody{ReadCloser: copy.Body, count: &sent}
	}
	if copy.GetBody != nil {
		reopen := copy.GetBody
		copy.GetBody = func() (io.ReadCloser, error) {
			body, err := reopen()
			if err != nil || body == nil {
				return body, err
			}
			return countedBody{ReadCloser: body, count: &sent}, nil
		}
	}
	response, err := t.inner.RoundTrip(copy)
	class := "unknown"
	switch {
	case strings.HasPrefix(request.URL.Path, "/v1/dir/"), request.URL.Path == "/v1/store/has", request.URL.Path == "/v1/store/locate":
		class = "control"
	case strings.HasPrefix(request.URL.Path, "/v1/store/"):
		class = "encrypted_bulk"
	case strings.HasPrefix(request.URL.Path, "/calibration/"):
		class = "calibration"
	}
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	var once sync.Once
	finish := func(bodyErr error, eof bool) {
		once.Do(func() {
			if bodyErr == io.EOF {
				bodyErr = nil
			}
			if err != nil {
				bodyErr = err
			}
			outcome := "ok"
			if status < 200 || status >= 300 {
				outcome = "http_error"
			}
			complete := eof || response != nil && response.ContentLength >= 0 && received.Load() == response.ContentLength
			if bodyErr == nil && !complete {
				outcome = "incomplete"
			}
			observed(t.observer, request.Context(), TraceEvent{Op: "http.request", Layer: "http_body", Class: class, HTTPStatus: status, Outcome: outcome, SentBytes: sent.Load(), ReceivedBytes: received.Load()}, start, bodyErr)
		})
	}
	if err != nil {
		finish(err, false)
	} else if response == nil || response.Body == nil {
		finish(nil, true)
	} else {
		response.Body = countedBody{ReadCloser: response.Body, count: &received, finish: finish}
	}
	return response, err
}

// CloseIdleConnections preserves the HTTP client's existing cleanup capability.
func (t tracedHTTP) CloseIdleConnections() {
	if closer, ok := t.inner.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// GetFrameBounded preserves optional capabilities through the observer adapter.
func (s tracedStore) GetFrameBounded(ctx context.Context, id string, maxEncoded int64) ([]byte, error) {
	start := time.Now()
	out, err := blobstore.ReadFrameBounded(ctx, s.Store, id, maxEncoded)
	observed(s.observer, ctx, TraceEvent{Op: "store.get_frame", Layer: "store", Class: "encrypted_frame", RID: id, PayloadBytes: int64(len(out))}, start, err)
	return out, err
}
