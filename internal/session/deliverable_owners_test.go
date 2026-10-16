package session

import "testing"

func TestLineNamesSessionMatchesPerOwnerSearch(t *testing.T) {
	line := []byte(`{"path":"/x/\"session\":\"inside\"","session":"real-1","title":"t"}`)
	if !lineNamesSession(line, map[string]bool{"real-1": true}) {
		t.Fatal("missed the session value")
	}
	if lineNamesSession(line, map[string]bool{"real": true, "inside": true, "real-12": true}) {
		t.Fatal("matched a prefix or text inside a string")
	}
}
