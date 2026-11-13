package blobstore_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"io"
	"net/http"
	"testing"
)

type boundedTransport func(*http.Request) (*http.Response, error)

func (f boundedTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type countedFrameBody struct {
	io.Reader
	consumed int
}

func (b *countedFrameBody) Read(p []byte) (int, error) {
	n, e := b.Reader.Read(p)
	b.consumed += n
	return n, e
}
func (*countedFrameBody) Close() error { return nil }
func TestBoundedFrameProductionWrappers(t *testing.T) {
	frame, err := blobstore.Encode("00000000000000000000000000000000", []blobstore.Object{{RID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Bytes: []byte("AGEO\x01mock")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "junk"} {
		t.Run(suffix, func(t *testing.T) {
			body := &countedFrameBody{Reader: bytes.NewReader(append(append([]byte(nil), frame...), suffix...))}
			client := blobstore.NewHTTP("http://fixture.invalid", func(*http.Request, []byte) {}, &http.Client{Transport: boundedTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, ContentLength: -1, Body: body}, nil
			})})
			counter := &blobstore.Counters{}
			wrapped := &blobstore.Resuming{Inner: blobstore.Counting{Inner: client, C: counter}}
			got, err := blobstore.ReadFrameBounded(context.Background(), wrapped, blobstore.IDOf(frame), int64(len(frame)))
			if suffix == "" {
				if err != nil || !bytes.Equal(got, frame) {
					t.Fatalf("exact frame: %v", err)
				}
			} else if err == nil {
				t.Fatal("accepted valid prefix with trailing bytes")
			}
			if body.consumed > len(frame)+1 {
				t.Fatalf("budget exceeded: %d", body.consumed)
			}
			if counter.Gets.Load() != 1 {
				t.Fatal("Counting capability bypassed")
			}
		})
	}
	if _, err := blobstore.ReadFrameBounded(context.Background(), blobstore.NewMemory(), blobstore.IDOf(frame), 32); !errors.Is(err, blobstore.ErrBoundedFrameUnsupported) {
		t.Fatalf("unsupported fell through: %v", err)
	}
}

func TestBoundedFrameRejectsDeclaredOversizeBeforeBodyRead(t *testing.T) {
	body := &countedFrameBody{Reader: bytes.NewReader([]byte("not read"))}
	client := blobstore.NewHTTP("http://fixture.invalid", func(*http.Request, []byte) {}, &http.Client{Transport: boundedTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: 999999, Body: body}, nil
	})})
	if _, err := blobstore.ReadFrameBounded(context.Background(), client, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 12); err == nil {
		t.Fatal("oversize accepted")
	}
	if body.consumed != 0 {
		t.Fatal("declared oversized body read")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := blobstore.ReadFrameBounded(ctx, client, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 12); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %v", err)
	}
}
