package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnobservedCallLeavesToollessNoticeForVisibleTurn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: "review/no-tools", Direct: true,
		SupportsParameter: func(_, p string) (bool, bool) { return p != "tools", true }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("helper")); err != nil {
		t.Fatal(err)
	}
	var news []string
	ctx := WithStreamObserver(context.Background(), func(e StreamEvent) {
		if e.Kind == StreamRowNews {
			news = append(news, e.Delta)
		}
	})
	for i := 0; i < 2; i++ {
		if _, err := client.CompleteWithMessages(ctx, userMessages("visible chat")); err != nil {
			t.Fatal(err)
		}
	}
	if len(news) != 1 || news[0] != toollessNotice("review/no-tools") {
		t.Fatalf("visible notices = %q, want one", news)
	}
}
