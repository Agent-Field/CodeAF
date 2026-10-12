package appapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransportRouteTable(t *testing.T) {
	want := map[string]string{"/app/v1/health": "GET", "/app/v1/shutdown": "POST"}
	if len(routes) != len(want) {
		t.Fatalf("route table changed: %v", routes)
	}
	for _, route := range routes {
		if want[route.path] != route.method {
			t.Fatalf("unrecognised route: %s %s", route.method, route.path)
		}
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
			w := request(t, New(testToken, Options{}), method, route.path, `{}`, bearer(), "127.0.0.1:4000")
			if method == route.method {
				if w.Code != 200 {
					t.Fatalf("%s %s: %d %s", method, route.path, w.Code, w.Body)
				}
			} else if w.Code != 405 || w.Header().Get("Allow") != route.method {
				t.Fatalf("wrong method %s %s: %d %s", method, route.path, w.Code, w.Body)
			}
		}
	}
}

func TestHealthNamesTheLaunchWithoutDisclosingTheToken(t *testing.T) {
	w := request(t, New(testToken, Options{Version: "dev-test", Epoch: "epoch-test"}), "GET", "/app/v1/health", "", bearer(), "127.0.0.1:4000")
	var health struct {
		Status        string `json:"status"`
		CodeafVersion string `json:"codeaf_version"`
		Contract      int    `json:"contract"`
		Epoch         string `json:"epoch"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.Status != "ready" || health.CodeafVersion != "dev-test" || health.Contract != Contract || health.Epoch != "epoch-test" || strings.Contains(w.Body.String(), testToken) {
		t.Fatalf("wrong health: %s", w.Body)
	}
}

func TestShutdownAcknowledgesBeforeStoppingAndIsIdempotent(t *testing.T) {
	called := 0
	var acknowledgment *httptest.ResponseRecorder
	s := New(testToken, Options{Shutdown: func() {
		called++
		if acknowledgment.Code != 200 || !acknowledgment.Flushed || acknowledgment.Body.String() != "{\"ok\":true}\n" {
			t.Fatal("shutdown began before the acknowledgment was flushed")
		}
	}})
	for range 2 {
		acknowledgment = httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/app/v1/shutdown", strings.NewReader(`{"reason":"quit"}`))
		r.Host, r.Header = "127.0.0.1:4000", bearer()
		s.ServeHTTP(acknowledgment, r)
		w := acknowledgment
		if w.Code != 200 || !w.Flushed || w.Body.String() != "{\"ok\":true}\n" {
			t.Fatalf("shutdown acknowledgment: %d %s", w.Code, w.Body)
		}
	}
	if called != 1 {
		t.Fatalf("shutdown ran %d times", called)
	}
}

func TestInvalidShutdownCannotStopTheEngine(t *testing.T) {
	s := New(testToken, Options{Shutdown: func() { t.Fatal("invalid request stopped the engine") }})
	for _, body := range []string{"", "null", "[]", "garbage", `{"reason":1}`, `{"reason":null}`, `{"unknown":true}`, `{} {}`, `{} trailing`, `{"reason":"` + strings.Repeat("x", 16<<10) + `"}`} {
		w := request(t, s, "POST", "/app/v1/shutdown", body, bearer(), "127.0.0.1:4000")
		if w.Code != 422 {
			t.Fatalf("invalid body accepted: %d %s", w.Code, w.Body)
		}
	}
}

func TestUnknownRoutesAndFutureMountsStillRequireTheToken(t *testing.T) {
	s := New(testToken, Options{})
	s.Handle("/v1/about", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, path := range []string{"/app/v1/missing", "/v1/about"} {
		w := request(t, s, "GET", path, "", nil, "127.0.0.1:4000")
		if w.Code != 401 {
			t.Fatalf("unguarded path %s", path)
		}
		w = request(t, s, "GET", path, "", bearer(), "127.0.0.1:4000")
		want := 404
		if path == "/v1/about" {
			want = 204
		}
		if w.Code != want {
			t.Fatalf("authenticated %s: %d", path, w.Code)
		}
	}
}
