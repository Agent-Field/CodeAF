package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// A KEY WORKS ONLY WHEN OPENROUTER ACCEPTS IT, NOT WHEN ITS CATALOG ANSWERS.
// The stub is the shape a dead key really meets (CodeAF #2022): every listing
// answers 200 with the public catalog whatever the bearer, and only the key
// endpoint tells a live key from a revoked one. A refused, redirected or
// unreadable key row stops the check before the listing, with the provider's
// status and words and never the key; an accepted one gets the models.
func TestAppKeyCheckRefusesAKeyOpenRouterDoesNotAccept(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()

	var mu sync.Mutex
	var paths []string
	var currentKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		wantAuthorization := "Bearer " + currentKey
		mu.Unlock()
		if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/json" {
			t.Error("the check did not ask for JSON with GET")
		}
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.Header.Get("Authorization") != wantAuthorization {
			t.Error("the check did not send the same Bearer authentication")
		}
		if r.URL.Path == "/key" || r.URL.Path == "/auth/key" {
			switch key {
			case "sk-or-dead-key":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"User not found.","code":401}}`))
			case "sk-or-redirect-key":
				http.Redirect(w, r, target.URL, http.StatusFound)
			case "sk-or-fallback-key", "sk-or-missing-key", "sk-or-fallback-redirect-key":
				if r.URL.Path == "/key" || key == "sk-or-missing-key" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if key == "sk-or-fallback-redirect-key" {
					http.Redirect(w, r, target.URL, http.StatusFound)
					return
				}
				_, _ = w.Write([]byte(`{"data":{"label":"test key","usage":0}}`))
			case "sk-or-html-key":
				_, _ = w.Write([]byte(`<html>Sign in</html>`))
			case "sk-or-null-key":
				_, _ = w.Write([]byte(`{"data":null}`))
			case "sk-or-array-key":
				_, _ = w.Write([]byte(`{"data":[]}`))
			case "sk-or-no-details-key":
				_, _ = w.Write([]byte(`{}`))
			case "sk-or-echo-key":
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "Rejected " + key})
			default:
				_, _ = w.Write([]byte(`{"data":{"label":"sk-or-v1-abc...","limit":null,"usage":0}}`))
			}
			return
		}
		// The public catalog answers for any bearer, including the base check
		// at "/", so only the key endpoint can prove acceptance.
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek/deepseek-v4-flash","name":"DeepSeek V4 Flash"},{"id":"qwen/qwen3-coder"}]}`))
	}))
	defer server.Close()
	backend := newFakeBackend(newAppRecorder(), nil)
	defer backend.Close()
	backend.keyAddress = func(string) string { return server.URL }

	for _, tc := range []struct {
		name, key, message string
		status             int
		ok                 bool
		paths              []string
	}{
		{name: "dead key", key: "sk-or-dead-key", status: 401, message: "User not found.", paths: []string{"/key"}},
		{name: "valid key", key: "sk-or-valid-key", ok: true, paths: []string{"/key", "/models/user"}},
		{name: "key redirect", key: "sk-or-redirect-key", status: 302, message: "Found", paths: []string{"/key"}},
		{name: "fallback", key: "sk-or-fallback-key", ok: true, paths: []string{"/key", "/auth/key", "/models/user"}},
		{name: "fallback redirect", key: "sk-or-fallback-redirect-key", status: 302, message: "Found", paths: []string{"/key", "/auth/key"}},
		{name: "missing endpoints", key: "sk-or-missing-key", status: 404, message: "Not Found", paths: []string{"/key", "/auth/key"}},
		{name: "HTML", key: "sk-or-html-key", status: 200, message: "the provider answered, but not with the key's details", paths: []string{"/key"}},
		{name: "null details", key: "sk-or-null-key", status: 200, message: "the provider answered, but not with the key's details", paths: []string{"/key"}},
		{name: "array details", key: "sk-or-array-key", status: 200, message: "the provider answered, but not with the key's details", paths: []string{"/key"}},
		{name: "missing details", key: "sk-or-no-details-key", status: 200, message: "the provider answered, but not with the key's details", paths: []string{"/key"}},
		{name: "redacted rejection", key: "sk-or-echo-key", status: 401, paths: []string{"/key"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			paths = nil
			currentKey = tc.key
			mu.Unlock()
			check, err := backend.CheckKey("openrouter", tc.key)
			if err != nil || check.OK != tc.ok {
				t.Fatalf("key acceptance = %+v, %v; want OK %v", check, err, tc.ok)
			}
			if strings.Contains(check.Message, tc.key) {
				t.Fatal("the returned message contains the key")
			}
			if tc.ok {
				if len(check.Models) != 2 || check.Models[0].ID != "deepseek/deepseek-v4-flash" || check.Models[1].ID != "qwen/qwen3-coder" {
					t.Fatalf("accepted key's models = %+v", check.Models)
				}
				for _, model := range check.Models {
					if model.Provider != "openrouter" {
						t.Fatalf("model provider = %q", model.Provider)
					}
				}
			} else if check.Status != tc.status || len(check.Models) != 0 || check.Message == "" || (tc.message != "" && check.Message != tc.message) {
				t.Fatalf("rejected check = %+v; want status %d, message %q, no models", check, tc.status, tc.message)
			}
			mu.Lock()
			gotPaths := append([]string(nil), paths...)
			mu.Unlock()
			if !reflect.DeepEqual(gotPaths, tc.paths) {
				t.Fatalf("request paths = %v; want %v", gotPaths, tc.paths)
			}
			if n := redirected.Load(); n != 0 {
				t.Fatalf("redirect target received %d requests", n)
			}
		})
	}
}

// AN ACCEPTED KEY STILL NEEDS ITS LISTING. After the key row answered, a
// listing that redirects, refuses or is not a list is said in its own words,
// and the redirect is not followed with the key on it.
func TestAppKeyCheckReportsTheModelListingAfterAcceptance(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()
	for _, tc := range []struct {
		name, body, message string
		status              int
	}{
		{name: "redirect", status: 302, message: "Found"},
		{name: "rejection", status: 403, body: `{"error":{"message":"Listing refused."}}`, message: "Listing refused."},
		{name: "HTML", status: 200, body: `<html>No models</html>`, message: "the provider answered, but not with a list of models"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths = append(paths, r.URL.Path)
				mu.Unlock()
				if r.URL.Path == "/key" {
					_, _ = w.Write([]byte(`{"data":{}}`))
					return
				}
				if tc.status == http.StatusFound {
					http.Redirect(w, r, target.URL, tc.status)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			backend := newFakeBackend(newAppRecorder(), nil)
			defer backend.Close()
			backend.keyAddress = func(string) string { return server.URL }
			const key = "sk-or-listing-key"
			check, err := backend.CheckKey("openrouter", key)
			if err != nil || check.OK || check.Status != tc.status || check.Message != tc.message || len(check.Models) != 0 {
				t.Fatalf("listing rejection = %+v, %v", check, err)
			}
			if strings.Contains(check.Message, key) {
				t.Fatal("the returned message contains the key")
			}
			mu.Lock()
			gotPaths := append([]string(nil), paths...)
			mu.Unlock()
			if !reflect.DeepEqual(gotPaths, []string{"/key", "/models/user"}) {
				t.Fatalf("request paths = %v; want key details then models", gotPaths)
			}
			if n := redirected.Load(); n != 0 {
				t.Fatalf("redirect target received %d requests", n)
			}
		})
	}
}
