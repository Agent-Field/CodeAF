package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbeddingsRestoreOrderAndRejectInvalidVectors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		bad        bool
	}{
		{"order", `{"data":[{"index":1,"embedding":[0,2]},{"index":0,"embedding":[2,0]}]}`, false},
		{"count", `{"data":[]}`, true},
		{"dimensions", `{"data":[{"index":0,"embedding":[1]},{"index":1,"embedding":[1,0]}]}`, true},
		{"duplicate", `{"data":[{"index":0,"embedding":[1,0]},{"index":0,"embedding":[1,0]}]}`, true},
		{"zero", `{"data":[{"index":0,"embedding":[0,0]},{"index":1,"embedding":[1,0]}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(tc.body)) }))
			defer server.Close()
			c, err := NewClient(Config{APIKey: "fixture", BaseURL: server.URL, Model: "embedding"})
			if err != nil {
				t.Fatal(err)
			}
			v, _, err := c.Embed(context.Background(), "embedding", []string{"a", "b"}, 2)
			if (err != nil) != tc.bad {
				t.Fatalf("error %v", err)
			}
			if !tc.bad && (v[0][0] != 1 || v[1][1] != 1) {
				encoded, _ := json.Marshal(v)
				t.Fatalf("order %s", encoded)
			}
		})
	}
}
