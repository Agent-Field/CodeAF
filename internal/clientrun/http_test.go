package clientrun_test

import (
	"bytes"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"io"
	"net/http"
	"testing"
)

func (g *rig) request(t *testing.T, s signer, method, path string, body, out any, status int) []byte {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, g.url+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(req, raw, s, g.now)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != status {
		t.Fatalf("%s %s: %d %s; want %d", method, path, resp.StatusCode, data, status)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
	}
	return data
}
