package raftdirectory_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func TestR5_6NativeExplicitEmptyFramePlans(t *testing.T) {
	service := relayserve.New(relayserve.Config{Store: t.TempDir()})
	defer service.Close()
	server := httptest.NewServer(service.Handler)
	defer server.Close()
	signer := fixtureSigner{private: key("p17-a")}
	for _, one := range frozenV1(t).Cases {
		if one.Name == "put-a" {
			raw, err := base64.RawURLEncoding.DecodeString(one.Headers["Codeaf-Cert"])
			must(t, err)
			must(t, json.Unmarshal(raw, &signer.certificate))
		}
	}
	send := func(t *testing.T, method, path, body string, want int) []byte {
		t.Helper()
		r, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		must(t, err)
		reqsign.Sign(r, []byte(body), signer, time.Now())
		response, err := server.Client().Do(r)
		must(t, err)
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		must(t, err)
		if response.StatusCode != want {
			t.Errorf("%s %s: HTTP %d %s, want %d", method, path, response.StatusCode, raw, want)
		}
		return raw
	}
	for _, spelling := range []struct{ name, field string }{{"absent", ""}, {"null", `,"frames":null`}, {"empty", `,"frames":[]`}} {
		t.Run(spelling.name, func(t *testing.T) {
			path := "/v1/dir/cells/" + spelling.name
			send(t, "POST", path, `{"head":"saved","class":"work","keys":{},"size":0`+spelling.field+`}`, 200)
			publishPath := path + "-publish"
			send(t, "POST", publishPath, `{"head":"saved","class":"work","keys":{},"size":0}`, 200)
			send(t, "POST", publishPath+"/publish", `{"old_head":"saved","head":"next","fence":1,"size":0,"class":"work"`+spelling.field+`}`, 200)
			raw := send(t, "GET", publishPath, "", 200)
			if !strings.Contains(string(raw), `"head":"next"`) {
				t.Errorf("published head missing: %s", raw)
			}
		})
	}
}
