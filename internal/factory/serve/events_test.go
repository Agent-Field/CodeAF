package serve_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
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
	if !strings.Contains(got, "event: job.state\n") || !strings.Contains(got, fmt.Sprintf(`"job":%d,"state":"backlog","reason":""`, made.ID)) {
		t.Fatalf("missing typed creation event: %q", got)
	}
	select {
	case got = <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("a store write produced no changed event")
	}
	want := "event: changed\ndata: " + strconv.Itoa(made.ID) + "\n"
	// The job document and its replay record can coalesce into a floor refresh.
	if got != want && got != "event: changed\ndata: *\n" {
		t.Fatalf("event = %q, want %q or a floor refresh", got, want)
	}
	select {
	case extra := <-events:
		t.Fatalf("a store write produced a second event: %q", extra)
	case <-time.After(400 * time.Millisecond):
	}
}

// TestAHeartbeatDoesNotAnnounceAJobChange is the idle watch. Writing the
// runner file is how presence is saved. It must not show up as a job
// change, or every waiting pull re-reads the floor. A real job write still
// does.
func TestAHeartbeatDoesNotAnnounceAJobChange(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config
	cfg.Root = root
	cfg.Store = st
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
	if err := os.WriteFile(filepath.Join(root, "runners.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-events:
		t.Fatalf("a heartbeat announced a job change: %q", got)
	case <-time.After(700 * time.Millisecond):
	}
	if _, err := st.Create(factory.Item{Title: "real"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("a new job produced no changed event")
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

func TestRapidPublishingAndNeedsYouReachOpenStreamWithinOneSecond(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Title: "publish", State: factory.StateRunning})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config
	cfg.Store = st
	seam := factory.Seam{Load: func() (factory.Snapshot, error) { items, err := st.List(); return factory.Snapshot{Items: items}, err }}
	h := serve.New(seam, cfg)
	s := httptest.NewServer(h)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); s.Close(); h.Close() }()
	events := make(chan string, 32)
	ready := make(chan struct{})
	go collectEvents(ctx, t, s.URL+"/v1/events", ready, events)
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("stream did not connect")
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []record.Event{
		{Kind: record.Approved, Job: it.ID, Handle: "Sam"},
		{Kind: record.Took, Job: it.ID, Handle: "Mei"},
	} {
		if _, err := history.Append(context.Background(), action); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []string{"queued", "pushing", "opened", "failed"} {
		err := st.Update(it.ID, func(it *factory.Item) error {
			it.MarkPublishing(state, "", "", now)
			if state == "opened" {
				it.PublishStatus.PR = &factory.PublishedPR{Number: 12, URL: "https://github.com/acme/api/pull/12"}
			}
			if state == "failed" {
				it.State = factory.StateLanded
				it.PublishStatus.Code = "no_push_permission"
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	timeout := time.After(time.Second)
	states := []string{}
	published, failed, need, approved, taken := false, false, false, false, false
	for !published || !failed || !need || !approved || !taken || len(states) != 4 {
		select {
		case frame := <-events:
			lines := strings.Split(frame, "\n")
			kind, data := "", ""
			for _, line := range lines {
				if strings.HasPrefix(line, "event: ") {
					kind = strings.TrimPrefix(line, "event: ")
				}
				if strings.HasPrefix(line, "data: ") {
					data = strings.TrimPrefix(line, "data: ")
				}
			}
			if kind == "changed" {
				continue
			}
			var p api.LiveEvent
			if err := json.Unmarshal([]byte(data), &p); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "approved":
				approved = p.By == "sam"
			case "taken":
				taken = p.By == "mei"
			case "publishing":
				states = append(states, p.State)
			case "published":
				published = p.PR != nil && p.PR.Number == 12
			case "publish_failed":
				failed = p.Reason == "no_push_permission"
			case "needs_you":
				need = p.Kind != nil && *p.Kind == "publish_failed" && p.Since != nil
			}
		case <-timeout:
			t.Fatalf("events missed one-second deadline: states %v published %v failed %v need %v", states, published, failed, need)
		}
	}
	if strings.Join(states, ",") != "queued,pushing,opened,failed" {
		t.Fatalf("transitions %v", states)
	}
}
