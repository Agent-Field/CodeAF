package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStubCostAndRequiredKey(t *testing.T) {
	const wantKey = "sk-test-org-0001"
	oldCost, oldKey := costUSD, requireKey
	costUSD, requireKey = 0.0123, wantKey
	handler := routes()
	t.Cleanup(func() {
		costUSD, requireKey = oldCost, oldKey
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("DELETE", "/_calls", nil))
	})
	request := func(method, path, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"model":"stub/scripted","messages":[{"role":"user","content":"bash: echo hi"}]}`
	path := "/api/v1/chat/completions"
	refused := request("POST", path, "sk-test-wrong-0001", body)
	if refused.Code != http.StatusUnauthorized || refused.Body.String() != `{"error":{"message":"invalid key","code":401}}` {
		t.Fatalf("refusal = %d %s", refused.Code, refused.Body)
	}
	ok := request("POST", path, wantKey, body)
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), `"prompt_tokens":11,"completion_tokens":7,"total_tokens":18,"cost":0.0123`) {
		t.Fatalf("paid answer = %d %s", ok.Code, ok.Body)
	}
	_, names, _ := toolCallsOf(t, ok.Body.String())
	if len(names) != 1 || names[0] != "bash" {
		t.Fatalf("scripted answer used %v", names)
	}
	var history struct{ Calls []stubCall }
	w := request("GET", "/_calls", "", "")
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil || w.Code != http.StatusOK || len(history.Calls) != 2 {
		t.Fatalf("history = %d %s, %v", w.Code, w.Body, err)
	}
	for i, row := range history.Calls {
		if row.At.IsZero() || row.Model != "stub/scripted" {
			t.Fatalf("call %d = %+v", i, row)
		}
	}
	failed, passed := history.Calls[0], history.Calls[1]
	if failed.Status != 401 || failed.Key != "sk-test-wrong-0001" || failed.USD != 0 ||
		passed.Status != 200 || passed.Key != wantKey || passed.USD != 0.0123 {
		t.Fatalf("history = %+v", history.Calls)
	}
	if w := request("GET", "/api/v1/models", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized listing = %d %s", w.Code, w.Body)
	}
	if w := request("GET", "/api/v1/models", wantKey, ""); w.Code != http.StatusOK {
		t.Fatalf("authorized listing = %d %s", w.Code, w.Body)
	}

	for i := 0; i <= callLimit; i++ {
		request("POST", path, "", fmt.Sprintf(`{"model":"test-%d"}`, i))
	}
	w = request("GET", "/_calls", "", "")
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil || len(history.Calls) != callLimit ||
		history.Calls[0].Model != "test-1" || history.Calls[callLimit-1].Model != fmt.Sprintf("test-%d", callLimit) {
		t.Fatalf("history did not keep the latest %d calls", callLimit)
	}
	if w := request("DELETE", "/_calls", "", ""); w.Code != http.StatusNoContent {
		t.Fatalf("clear = %d %s", w.Code, w.Body)
	}
	if w := request("GET", "/_calls", "", ""); w.Body.String() != "{\"calls\":[]}\n" {
		t.Fatalf("cleared history = %s", w.Body)
	}
}
