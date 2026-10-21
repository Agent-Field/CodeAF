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

// hosted stands a hosted relay in for one test, so both states of the constant
// are tested without editing it.
func hosted(t *testing.T, url string) {
	t.Helper()
	was := hostedRelay
	hostedRelay = url
	t.Cleanup(func() { hostedRelay = was })
}

// The resolver is one answer for sync and pairing, in the order variable, saved
// pairing, hosted default, for both states of the hosted constant.
func TestResolveOrdersVariableThenSavedThenHosted(t *testing.T) {
	cases := []struct {
		name, hosted, variable, saved string
		want                          Relay
	}{
		{"nothing anywhere", "", "", "", Relay{}},
		{"hosted is the default", "https://h", "", "", Relay{URL: "https://h", Hosted: true}},
		{"saved beats hosted", "https://h", "", "http://s", Relay{URL: "http://s"}},
		{"variable beats saved", "https://h", "http://v", "http://s", Relay{URL: "http://v"}},
		{"off beats hosted", "https://h", "off", "", Relay{URL: "off", Off: true}},
		{"off in capitals", "", "OFF", "", Relay{URL: "OFF", Off: true}},
		{"off beats saved", "", "off", "http://s", Relay{URL: "off", Off: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hosted(t, c.hosted)
			home := machine(t, c.variable)
			if c.saved != "" {
				if err := SaveRelayURL(home, c.saved); err != nil {
					t.Fatal(err)
				}
			}
			if got := Resolve(home); got != c.want {
				t.Fatalf("Resolve = %+v, want %+v", got, c.want)
			}
		})
	}
}

// Sync is off for "off" and for no relay at all, and Open touches nothing then.
func TestOpenIsOffForTheWordAndForNoRelay(t *testing.T) {
	for name, variable := range map[string]string{"word": "off", "unset": ""} {
		t.Run(name, func(t *testing.T) {
			hosted(t, map[string]string{"word": "https://h", "unset": ""}[name])
			if s, ok, err := Open(machine(t, variable)); s != nil || ok || err != nil {
				t.Fatalf("Open = %v, %v, %v; want nothing, no error", s, ok, err)
			}
		})
	}
}

// With the hosted default set and no word from the person, Open goes there and
// says the relay is the hosted one.
func TestOpenUsesTheHostedDefault(t *testing.T) {
	h := relay(t)
	hosted(t, h.URL)
	got, ok, err := Open(machine(t, ""))
	if err != nil || !ok || got.Relay != h.URL || !got.Hosted {
		t.Fatalf("Open = %+v, %v, %v; want the hosted relay %s", got, ok, err, h.URL)
	}
}

// The first-run line is said once, and only for the hosted default.
func TestFirstRunIsSaidOnceAndOnlyForTheHostedDefault(t *testing.T) {
	h := relay(t)
	hosted(t, h.URL)
	s, _, err := Open(machine(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if line, ok := s.FirstRun(); !ok || line != FirstRunLine {
		t.Fatalf("first FirstRun = %q, %v", line, ok)
	}
	if line, ok := s.FirstRun(); ok {
		t.Fatalf("second FirstRun said %q", line)
	}

	own, _, err := Open(machine(t, h.URL))
	if err != nil {
		t.Fatal(err)
	}
	if line, ok := own.FirstRun(); ok {
		t.Fatalf("a chosen relay was told %q", line)
	}
}

// A pairing names every relay but the hosted default.
func TestNamedLeavesOutOnlyTheHostedDefault(t *testing.T) {
	hosted(t, "https://h")
	if Named("https://h") != "" || Named("http://own") != "http://own" {
		t.Fatal("Named must drop the hosted default and keep any other address")
	}
}
