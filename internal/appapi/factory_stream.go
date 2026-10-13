package appapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// The Inbox adapter keeps ownership of decisions while the factory lane relays
// all typed floor events, including runner and Activity changes.
type factoryEventSource struct{ *FactoryInboxSource }

func (f *factoryEventSource) WatchInbox(ctx context.Context, publish func(FactoryEvent)) {
	cursor := ""
	for ctx.Err() == nil {
		f.followFactory(ctx, &cursor, publish)
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (f *factoryEventSource) followFactory(ctx context.Context, cursor *string, publish func(FactoryEvent)) {
	request, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(f.URL, "/")+"/v1/events", nil)
	if err != nil {
		return
	}
	request.Header.Set("Authorization", "Bearer "+f.Token)
	request.Header.Set("Accept", "text/event-stream")
	if *cursor != "" {
		request.Header.Set("Last-Event-ID", *cursor)
	}
	client := f.Client
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		defer transport.CloseIdleConnections()
		client = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	kind, id := "", ""
	var data strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if id != "" {
				*cursor = id
			}
			if kind != "" && json.Valid([]byte(data.String())) {
				publish(FactoryEvent{Factory: f.ID, Type: kind, Data: json.RawMessage(data.String())})
			}
			kind, id = "", ""
			data.Reset()
			continue
		}
		if value, ok := strings.CutPrefix(line, "event:"); ok {
			kind = strings.TrimSpace(value)
		}
		if value, ok := strings.CutPrefix(line, "id:"); ok {
			id = strings.TrimSpace(value)
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			if data.Len()+len(value) > 64<<10 {
				return
			}
			data.WriteString(strings.TrimPrefix(value, " "))
		}
	}
}
func (f *FactoryRegistry) inboxSource(row factoryConnection) InboxSource {
	return &factoryEventSource{f.source(row)}
}
