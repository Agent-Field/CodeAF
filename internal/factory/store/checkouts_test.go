package store

import "testing"

func TestCheckoutsRoundTripAndFullNameLookup(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if m, err := st.Checkouts(); err != nil || len(m) != 0 {
		t.Fatalf("empty store: %v %v", m, err)
	}
	if err := st.SetCheckout("acme/ledger", "/src/ledger"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCheckout("ledger", "/src/ledger"); err != nil {
		t.Fatal(err)
	}
	m, _ := st.Checkouts()
	if len(m) != 1 || m["ledger"] != "/src/ledger" {
		t.Fatalf("got %v", m)
	}
	for _, name := range []string{"ledger", "acme/ledger"} {
		if got := st.CheckoutDir(name); got != "/src/ledger" {
			t.Fatalf("%s -> %q", name, got)
		}
	}
	if st.CheckoutDir("other") != "" {
		t.Fatal("unknown repo answered a folder")
	}
	if st.SetCheckout("", "/x") == nil {
		t.Fatal("empty repo accepted")
	}
}
