package session_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestUsageLineWithoutSharedIDPreservesSavedBytes(t *testing.T) {
	row := session.UsageLine{
		GenerationID: "receipt-17", ID: "row-17",
		Billing: "metered", BillingBasis: "door:test/local",
		At: time.Date(2026, 9, 29, 10, 0, 1, 0, time.UTC), Day: "2026-09-29",
		Model: "test/model", Calls: 1, Input: 100, Output: 10, USD: .125,
	}
	// These bytes were marshaled by origin/app/v3 at 012b87c3318e.
	// ID and Billing bracket the optional shared_id field in the saved format.
	const want = `{"generation_id":"receipt-17","id":"row-17","billing":"metered","billing_basis":"door:test/local","at":"2026-09-29T10:00:01Z","day":"2026-09-29","model":"test/model","calls":1,"in":100,"out":10,"usd":0.125}`
	got, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("saved usage bytes changed:\n got: %s\nwant: %s", got, want)
	}
}
