package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A SEEDED LISTING KNOWS NO PRICE; A FETCHED ONE KNOWS WHAT IT PUBLISHED —
// including an explicit zero, which is free, and an absence, which is not.
func TestOwnPricesKeepFreeApartFromUnknown(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"lab/paid","pricing":{"prompt":"0.000001","completion":"0.000002"}},
			{"id":"lab/free","pricing":{"prompt":"0","completion":"0"}},
			{"id":"lab/silent"}]}`))
	}))
	defer server.Close()
	options := Options{Source: "lab", BaseURL: server.URL, Dir: dir}
	if err := Remember(options, []Model{{ID: "lab/paid"}, {ID: "lab/free"}}); err != nil {
		t.Fatal(err)
	}
	for id, fact := range OwnPrices(options) {
		if fact.Known {
			t.Errorf("a seeded listing claims a price for %s: %+v", id, fact)
		}
	}
	if _, err := Refresh(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	facts := OwnPrices(options)
	if f := facts["lab/paid"]; !f.Known || f.Prompt != 1e-6 || f.Completion != 2e-6 {
		t.Errorf("paid: %+v", f)
	}
	if f := facts["lab/free"]; !f.Known || f.Prompt != 0 || f.Completion != 0 {
		t.Errorf("an explicit zero is a known free price: %+v", f)
	}
	if f := facts["lab/silent"]; f.Known {
		t.Errorf("a row that published nothing claims a price: %+v", f)
	}
	if other := OwnPrices(Options{Source: "other", BaseURL: server.URL, Dir: dir}); len(other) != 0 {
		t.Errorf("another compartment answered %v", other)
	}
}
