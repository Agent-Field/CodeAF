package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// shelfRouter is a router with no network behind it: it lists rows, or fails
// the way a dead connection fails.
func shelfRouter(rows string, fail error) *http.Client {
	return &http.Client{Transport: mediaRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if fail != nil {
			return nil, fail
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"data":[` + rows + `]}`)), Request: request}, nil
	})}
}

const shelfRow = `{"id":"vendor/old","architecture":{"input_modalities":["text"],"output_modalities":["text"]}}`
const shelfNewRow = `{"id":"vendor/shipped-this-morning","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}}`

// A LANDED REFRESH IS ON THE SHELF, IN THE PICKER'S CACHE AND DATED; A FAILED
// ONE CHANGES NOTHING AND SAYS WHY IN THE TRANSPORT'S OWN WORDS. The shelf is
// what /model's list and the vision gate read, so a model the router shipped
// this morning is both listed and able to see the moment the refresh lands.
func TestTheShelfTakesTodaysListAndKeepsYesterdaysOnFailure(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	dir := t.TempDir()
	launch := catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir, HTTPClient: shelfRouter(shelfRow, nil),
	})

	dead := newV3ModelShelf(launch, catalog.Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir,
		HTTPClient: shelfRouter("", errors.New("no such host")),
	})
	if rows, _, err := dead.refresh(context.Background()); err == nil || err.Error() != "no such host" || rows != nil {
		t.Fatalf("a dead router answered %v rows, error %v — want the transport's own reason", len(rows), err)
	}
	if got := v3Models(dead); len(got) != 1 || got[0].ID != "vendor/old" {
		t.Fatalf("a failed refresh changed the shelf: %+v", got)
	}

	live := newV3ModelShelf(launch, catalog.Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir,
		HTTPClient: shelfRouter(shelfRow+","+shelfNewRow, nil),
	})
	rows, at, err := live.refresh(context.Background())
	if err != nil || len(rows) != 2 || at.IsZero() {
		t.Fatalf("a landed refresh answered %d rows at %v, error %v", len(rows), at, err)
	}
	if !v3SeesImages(live)("vendor/shipped-this-morning") {
		t.Fatal("the vision gate does not know the model the refresh brought")
	}
	cached := tui3.CachedModels()
	if len(cached) != 2 || cached[1].ID != "vendor/shipped-this-morning" {
		t.Fatalf("~/.aforge/v3/models.json is not today's list: %+v", cached)
	}
}
