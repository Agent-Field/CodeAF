package billing

import (
	"testing"

	"bloop/shop/fake"
)

func TestInvoiceTotalReadsTheBody(t *testing.T) {
	server := &fake.Server{Status: 200, Body: " 12.50\n"}
	defer server.Install()()
	got, err := InvoiceTotal("42")
	if err != nil || got != "12.50" {
		t.Fatalf("InvoiceTotal = %q, %v; want 12.50", got, err)
	}
	if server.LastURL != "https://billing.local/invoices/42" {
		t.Fatalf("asked %q", server.LastURL)
	}
}

func TestInvoiceTotalOfAnUnknownInvoiceIsZero(t *testing.T) {
	server := &fake.Server{Status: 404}
	defer server.Install()()
	got, err := InvoiceTotal("404")
	if err != nil || got != "0.00" {
		t.Fatalf("InvoiceTotal = %q, %v; want 0.00 and no error", got, err)
	}
}

func TestInvoiceTotalTriesThreeTimes(t *testing.T) {
	flaky := &fake.Server{Status: 200, Body: "1.00", FailFirst: 2}
	restore := flaky.Install()
	got, err := InvoiceTotal("7")
	restore()
	if err != nil || got != "1.00" || flaky.Calls != 3 {
		t.Fatalf("InvoiceTotal = %q, %v after %d calls; want 1.00 on the third", got, err, flaky.Calls)
	}
	down := &fake.Server{Status: 200, FailFirst: 99}
	defer down.Install()()
	if _, err := InvoiceTotal("7"); err == nil || down.Calls != 3 {
		t.Fatalf("a dead service answered %v after %d calls; want an error after 3", err, down.Calls)
	}
}
