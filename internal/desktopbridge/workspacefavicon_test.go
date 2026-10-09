package desktopbridge

import (
	"context"
	"strings"
	"testing"
)

func TestWorkspaceFaviconOnlyReadsCanonicalWebPanes(t *testing.T) {
	b, _ := newWorkspaceBridge(t)
	calls := []string{}
	b.icons.fetch = func(_ context.Context, domain string) string {
		calls = append(calls, domain)
		return "data:image/png;base64,aGVsbG8="
	}
	doc := sharedDoc("web", "chat", "split")
	tabs := doc["tabs"].([]map[string]any)
	tabs[0]["kind"] = "web"
	tabs[0]["target"] = map[string]any{"url": "https://example.com/page"}
	tabs[1]["target"] = map[string]any{"url": "https://unrelated.example/page"}
	tabs[2]["split"] = map[string]any{"layout": "1x2", "panes": []map[string]any{
		{"id": "left", "kind": "web", "title": "Page", "draft": "", "target": map[string]any{"url": "https://example.com/other"}},
		{"id": "right", "kind": "conversation", "title": "Chat", "draft": ""},
	}}
	if w := request(b, "PUT", "/api/engine/workspaces/now", putBody(0, "win", doc)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, id := range []string{"web", "left"} {
		w := request(b, "GET", "/api/engine/workspaces/now/favicon?pane="+id+"&url=https://evil.example", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"domain":"example.com"`) || !strings.Contains(w.Body.String(), "data:image/png") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, id := range []string{"chat", "missing", "right", ""} {
		w := request(b, "GET", "/api/engine/workspaces/now/favicon?pane="+id, "")
		if strings.TrimSpace(w.Body.String()) != "{}" {
			t.Fatal(w.Body.String())
		}
	}
	if len(calls) != 1 || calls[0] != "example.com" {
		t.Fatal(calls)
	}
	if w := request(b, "POST", "/api/engine/workspaces/now/favicon?pane=web", ""); w.Code != 405 {
		t.Fatal(w.Code)
	}
}
