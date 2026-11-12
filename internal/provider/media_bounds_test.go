package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func bodyResponse(status int, length int64, body io.Reader) *http.Response {
	return &http.Response{StatusCode: status, ContentLength: length, Body: io.NopCloser(body)}
}

// A body of exactly the ceiling is a complete file; one byte more is a file
// that would have been cut, and it must never come back as a success.
func TestReadMediaBodyRefusesOneByteOverAndKeepsExactlyAtTheLimit(t *testing.T) {
	const limit = 1024
	got, err := readMediaBody(bodyResponse(200, -1, bytes.NewReader(make([]byte, limit))), limit, "video response")
	if err != nil || len(got) != limit {
		t.Fatalf("exact-limit body = %d bytes, %v; want it whole", len(got), err)
	}
	got, err = readMediaBody(bodyResponse(200, -1, bytes.NewReader(make([]byte, limit+1))), limit, "video response")
	if !errors.Is(err, ErrMediaTooLarge) || got != nil {
		t.Fatalf("limit+1 body = %d bytes, %v; want ErrMediaTooLarge and no bytes", len(got), err)
	}
}

// A declared length over the ceiling is refused before the body is touched.
func TestReadMediaBodyRefusesADeclaredOversizeWithoutReading(t *testing.T) {
	reader := &tripReader{}
	_, err := readMediaBody(bodyResponse(200, 4096, reader), 1024, "speech response")
	if !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("err = %v, want ErrMediaTooLarge", err)
	}
	if reader.reads != 0 {
		t.Fatalf("body was read %d times before the refusal", reader.reads)
	}
}

type tripReader struct{ reads int }

func (r *tripReader) Read([]byte) (int, error) { r.reads++; return 0, io.EOF }

// An error status is diagnosis text: it is read through the small peek, may be
// truncated, and is never reported as oversize.
func TestReadMediaBodyTruncatesAnErrorBodyWithoutRefusing(t *testing.T) {
	got, err := readMediaBody(bodyResponse(502, -1, strings.NewReader(strings.Repeat("x", maxErrorPeek*3))), 1<<30, "media response")
	if err != nil || len(got) != maxErrorPeek {
		t.Fatalf("error body = %d bytes, %v; want %d, nil", len(got), err, maxErrorPeek)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The three downloads that return raw bytes refuse an oversize reply as an
// error, and the response body is closed on the way out.
func TestSpeechAndVideoDownloadRefuseOversizeAndCloseTheBody(t *testing.T) {
	var closed int
	oversize := func(limit int64) *http.Client {
		return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			body := &closeCounter{Reader: io.LimitReader(zeros{}, limit+1), closed: &closed}
			return &http.Response{StatusCode: 200, ContentLength: -1, Body: body, Header: http.Header{}, Request: r}, nil
		})}
	}
	// Streams limit+1 zero bytes: the only cost is the test's own read of them.
	speak, _ := NewMediaClient(Config{APIKey: "k", BaseURL: "https://x.example/api/v1", HTTPClient: oversize(maxMediaResponseBytes)})
	if _, err := speak.Speak(context.Background(), SpeechRequest{Model: "m", Input: "hi"}); !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("Speak err = %v, want ErrMediaTooLarge", err)
	}
	video, _ := NewMediaClient(Config{APIKey: "k", BaseURL: "https://x.example/api/v1", HTTPClient: oversize(maxVideoResponseBytes)})
	_, err := video.downloadVideo(context.Background(), videoJob{ID: "j", UnsignedURLs: []string{"https://x.example/api/v1/videos/j/content"}}, "m")
	if !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("downloadVideo err = %v, want ErrMediaTooLarge", err)
	}
	if closed != 2 {
		t.Fatalf("closed %d bodies, want 2", closed)
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type closeCounter struct {
	io.Reader
	closed *int
}

func (c *closeCounter) Close() error { *c.closed++; return nil }

func musicEvent(data string) string {
	return fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"audio\":{\"data\":%q}}}]}\n\n", data)
}

// The ceiling is on the CUMULATIVE decoded clip across events, not per event.
func TestMusicStreamRefusesACumulativeOversizeClipInsteadOfTruncatingIt(t *testing.T) {
	chunk := strings.Repeat("AAAA", 8) // 24 decoded bytes per event
	stream := musicEvent(chunk) + musicEvent(chunk) + musicEvent(chunk) + "data: [DONE]\n\n"
	if _, err := readMusicStream(strings.NewReader(stream), 72); err != nil {
		t.Fatalf("clip of exactly the ceiling: %v", err)
	}
	_, err := readMusicStream(strings.NewReader(stream), 71)
	if !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("clip over the ceiling err = %v, want ErrMediaTooLarge", err)
	}
}

// One event with no separator cannot grow the stitch buffer past the limit.
func TestMusicStreamBoundsASingleEndlessMessage(t *testing.T) {
	endless := io.MultiReader(strings.NewReader("data: {\"x\":\""), io.LimitReader(zerosOf('a'), 1<<20))
	_, err := readMusicStream(endless, 1024)
	if !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("endless message err = %v, want ErrMediaTooLarge", err)
	}
}

type zerosOf byte

func (b zerosOf) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}
