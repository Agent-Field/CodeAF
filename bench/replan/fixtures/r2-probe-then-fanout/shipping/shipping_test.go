package shipping

import (
	"strings"
	"testing"

	"bloop/shop/fake"
)

func TestQuoteReadsTheBody(t *testing.T) {
	server := &fake.Server{Status: 200, Body: "4.99\n"}
	defer server.Install()()
	got, err := Quote("10001")
	if err != nil || got != "4.99" {
		t.Fatalf("Quote = %q, %v; want 4.99", got, err)
	}
	if server.LastURL != "https://carrier.local/quote/10001" {
		t.Fatalf("asked %q", server.LastURL)
	}
}

func TestQuoteToAnUnservedZipNamesIt(t *testing.T) {
	server := &fake.Server{Status: 404}
	defer server.Install()()
	_, err := Quote("99999")
	if err == nil || !strings.Contains(err.Error(), "no shipping to 99999") {
		t.Fatalf("Quote answered %v; want the error that names 99999", err)
	}
}

func TestQuoteTriesTwice(t *testing.T) {
	flaky := &fake.Server{Status: 200, Body: "5.00", FailFirst: 1}
	restore := flaky.Install()
	got, err := Quote("10001")
	restore()
	if err != nil || got != "5.00" || flaky.Calls != 2 {
		t.Fatalf("Quote = %q, %v after %d calls; want 5.00 on the second", got, err, flaky.Calls)
	}
	down := &fake.Server{Status: 200, FailFirst: 99}
	defer down.Install()()
	if _, err := Quote("10001"); err == nil || down.Calls != 2 {
		t.Fatalf("a dead service answered %v after %d calls; want an error after 2", err, down.Calls)
	}
}
