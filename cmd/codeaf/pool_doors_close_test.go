package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/remote"
)

func TestPoolErrandsStopRequestsAndWritesOnBothDoors(t *testing.T) {
	for _, tc := range []struct{ door, errand, hold string }{
		{"process", "index", "document"},
		{"process", "index", "signature"},
		{"window", "index", "document"},
		{"window", "index", "signature"},
		{"window", "push", "response"},
	} {
		t.Run(tc.door+"/"+tc.errand+"/"+tc.hold, func(t *testing.T) {
			poolNetworkFixture(t)
			// Seat the real door without unrelated startup network work. The
			// tested errand below uses the very tracker that door owns.
			t.Setenv("CODEAF_MODEL_POOL", "off")
			var profile string
			var closeDoor func()
			if tc.door == "process" {
				proc := v3TestProcess(t)
				profile, closeDoor = proc.ProfileDir, proc.closeAll
			} else {
				profile = testHome(t)
				t.Setenv("CODEAF_HOME", profile)
				t.Setenv("CODEAF_PROFILE_DIR", profile)
				fleet := onePipeFleet("", hostedClient(t))
				closeDoor = fleet.closeAll
				hostOptions(fleet, remote.Welcome{Version: remote.Version, ProfileDir: profile}, false)
			}
			t.Cleanup(closeDoor)
			held := poolErrandsStart(profile)
			t.Cleanup(func() { stopPoolErrands(profile) })
			t.Setenv("CODEAF_MODEL_POOL", "on")
			t.Setenv("CODEAF_MODEL_POOL_URL", "https://pool.invalid/index.json")
			cfg := config.ModelPoolAt(profile)
			pool := config.ProfilePath(profile, "pool")
			if tc.errand == "push" {
				box, err := outbox.Open(outboxPath(pool))
				if err != nil {
					t.Fatal(err)
				}
				for _, line := range []string{`{"sample":1}`, `{"sample":2}`} {
					if err := box.Append([]byte(line)); err != nil {
						t.Fatal(err)
					}
				}
				box.Close()
			}
			private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
			public := private.Public().(ed25519.PublicKey)
			doc := string(signedPoolDoc(9))
			sig := base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(doc)))
			payload := doc
			if tc.hold == "signature" {
				payload = sig
			}
			if tc.errand == "push" {
				payload = `{"error":"line 1: refused"}`
			}
			body := &poolHeldBody{Reader: strings.NewReader(payload), started: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			free := func() { release.Do(func() { close(body.release) }) }
			var calls atomic.Int32
			previous := http.DefaultTransport
			http.DefaultTransport = moneyTransport(func(request *http.Request) (*http.Response, error) {
				// Catalog warming belongs to a separate owner and must not
				// share the held pool response or its request counter.
				if request.URL.Host != "pool.invalid" {
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
				}
				calls.Add(1)
				response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
				if tc.errand == "push" {
					response.StatusCode, response.Body = http.StatusBadRequest, body
				} else if strings.HasSuffix(request.URL.Path, ".sig") {
					response.Body = io.NopCloser(strings.NewReader(sig))
					if tc.hold == "signature" {
						response.Body = body
					}
				} else {
					response.Body = io.NopCloser(strings.NewReader(doc))
					if tc.hold == "document" {
						response.Body = body
					}
				}
				return response, nil
			})
			finished := make(chan struct{})
			t.Cleanup(func() { free(); <-finished; http.DefaultTransport = previous })
			held.goCtx("pool/"+tc.errand, func(ctx context.Context) {
				defer close(finished)
				if tc.errand == "index" {
					refreshPoolIndex(ctx, profile, cfg, []ed25519.PublicKey{public})
				} else {
					poolPush(ctx, profile, cfg, poolPushBudget)
				}
			})
			select {
			case <-body.started:
			case <-time.After(5 * time.Second):
				t.Fatal("pool errand never reached held response")
			}
			closeDoor()
			beforeCalls, beforeFiles := calls.Load(), poolFiles(t, pool)
			free()
			<-finished
			if got := calls.Load() - beforeCalls; got != 0 {
				t.Errorf("%s started %d requests after %s close returned", tc.errand, got, tc.door)
			}
			if !reflect.DeepEqual(beforeFiles, poolFiles(t, pool)) {
				t.Errorf("%s wrote under %s after %s close returned", tc.errand, filepath.Base(pool), tc.door)
			}
		})
	}
}
