package syncsetup

import "testing"

// A computer that was paired syncs through the relay it was told, with no
// variable set; a variable that is set is the person's own word and wins.
func TestOpenUsesTheRelayAPairingSavedUnlessTheVariableIsSet(t *testing.T) {
	saved, named := relay(t), relay(t)
	home := machine(t, "")
	if err := SaveRelayURL(home, saved.URL); err != nil {
		t.Fatal(err)
	}

	got, ok, err := Open(home)
	if err != nil || !ok || (got == nil || got.Relay != saved.URL) {
		t.Fatalf("with no variable Open = relay %v, ok %v, err %v; want the saved %s", got, ok, err, saved.URL)
	}

	t.Setenv(URLVar, named.URL)
	got, ok, err = Open(home)
	if err != nil || !ok || (got == nil || got.Relay != named.URL) {
		t.Fatalf("with the variable set Open = relay %v, ok %v, err %v; want %s", got, ok, err, named.URL)
	}
}
