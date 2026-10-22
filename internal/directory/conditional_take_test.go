package directory_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// TestC7OldServiceRefusesTakesAcrossClients proves C7 at the HTTP door:
// capability absence is cached per service, while ordinary reads still work.
func TestC7OldServiceRefusesTakesAcrossClients(t *testing.T) {
	reads, takes := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/acquire") {
			takes++
			t.Error("old service received a take")
		}
		reads++
		_ = json.NewEncoder(w).Encode(directory.CellView{})
	}))
	defer srv.Close()
	for _, opts := range []directory.AcquireOpts{{Force: true}, {RequireConditional: true}} {
		client := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
		_, err := client.Acquire(context.Background(), cellX, opts)
		if !errors.Is(err, directory.ErrConditionalTake) {
			t.Fatalf("take = %v", err)
		}
	}
	if reads != 1 || takes != 0 {
		t.Fatalf("requests: reads=%d takes=%d", reads, takes)
	}
}

// TestC7OldServiceStillAllowsAnOrdinaryResume proves C7's compatibility
// boundary: a local non-forced resume cannot displace a live other holder.
func TestC7OldServiceStillAllowsAnOrdinaryResume(t *testing.T) {
	reads, resumes := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/acquire") {
			resumes++
		} else {
			reads++
		}
		_ = json.NewEncoder(w).Encode(directory.CellView{})
	}))
	defer srv.Close()
	client := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
	_, err := client.Acquire(context.Background(), cellX, directory.AcquireOpts{Expect: &directory.LeaseExpectation{Device: devA, Fence: 1}})
	if err != nil || resumes != 1 || reads != 0 {
		t.Fatalf("ordinary resume: err=%v resumes=%d reads=%d", err, resumes, reads)
	}
}
