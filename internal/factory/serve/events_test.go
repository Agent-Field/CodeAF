package serve_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestStoreWriteProducesOneChangedEvent(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config
	cfg.Root = root
	h := serve.New(factory.FixtureSeam(now), cfg)
	s := httptest.NewServer(h)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		s.Close()
		h.Close()
	}()

	events := make(chan string, 4)
	ready := make(chan struct{})
	go collectEvents(ctx, t, s.URL+"/v1/events", ready, events)
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("event stream did not answer")
	}

	made, err := st.Create(factory.Item{Title: "one change"})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	select {
	case got = <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("a store write produced no changed event")
	}
	want := "event: changed\ndata: " + strconv.Itoa(made.ID) + "\n"
	if got != want {
		t.Fatalf("event = %q, want %q", got, want)
	}
	select {
	case extra := <-events:
		t.Fatalf("a store write produced a second event: %q", extra)
	case <-time.After(400 * time.Millisecond):
	}
}

func collectEvents(ctx context.Context, t *testing.T, url string, ready chan struct{}, out chan<- string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Error(err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			t.Error(err)
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("stream headers: %s %s", resp.Status, resp.Header.Get("Content-Type"))
	}
	close(ready)
	sc := bufio.NewScanner(resp.Body)
	var b strings.Builder
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if b.Len() > 0 {
				out <- b.String()
				b.Reset()
			}
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
}
