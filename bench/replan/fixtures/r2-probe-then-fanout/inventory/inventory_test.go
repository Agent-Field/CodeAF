package inventory

import (
	"testing"

	"bloop/shop/fake"
)

func TestInStockReadsYes(t *testing.T) {
	server := &fake.Server{Status: 200, Body: "yes\n"}
	defer server.Install()()
	got, err := InStock("mug-1")
	if err != nil || !got {
		t.Fatalf("InStock = %v, %v; want true", got, err)
	}
	if server.LastURL != "https://warehouse.local/stock?sku=mug-1" {
		t.Fatalf("asked %q", server.LastURL)
	}
}

func TestInStockOfAnUnknownItemIsNo(t *testing.T) {
	server := &fake.Server{Status: 404}
	defer server.Install()()
	got, err := InStock("nope")
	if err != nil || got {
		t.Fatalf("InStock = %v, %v; want false and no error", got, err)
	}
}

func TestInStockTriesFourTimes(t *testing.T) {
	flaky := &fake.Server{Status: 200, Body: "no", FailFirst: 3}
	restore := flaky.Install()
	got, err := InStock("cup")
	restore()
	if err != nil || got || flaky.Calls != 4 {
		t.Fatalf("InStock = %v, %v after %d calls; want false on the fourth", got, err, flaky.Calls)
	}
	down := &fake.Server{Status: 200, FailFirst: 99}
	defer down.Install()()
	if _, err := InStock("cup"); err == nil || down.Calls != 4 {
		t.Fatalf("a dead service answered %v after %d calls; want an error after 4", err, down.Calls)
	}
}
