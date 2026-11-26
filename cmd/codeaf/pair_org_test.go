package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pair"
)

// orgAnswering is an organization's server that answers GET /v1/org to the one sign-in it knows, with relay as the address it
// runs for its members (empty: it runs none).
func orgAnswering(t *testing.T, token, relay string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/org" || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, `{"error":"sign in first"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := `{"name":"Acme","set_up":true`
		if relay != "" {
			body += `,"relay":"` + relay + `"`
		}
		_, _ = io.WriteString(w, body+"}")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A MEMBER PAIRS THROUGH THE ORGANIZATION'S RELAY WITH THE ONE ADDRESS THEY HAVE: `--org` looks up the relay the organization runs
// and the pairing goes through it, as if they had typed it after --via. The other computer is told to use it the way a --via
// pairing always tells it.
func TestPairOrgGoesThroughTheRelayTheOrganizationNames(t *testing.T) {
	rig := newPairRig(t)
	org := orgAnswering(t, "member-token", rig.url)
	t.Setenv("CODEAF_ORG_TOKEN", "member-token")
	t.Setenv("CODEAF_HOME", rig.homeA)
	t.Setenv("CODEAF_SYNC_URL", "")

	asked := make(chan string, 4)
	in, typed := io.Pipe()
	defer typed.Close()
	out := &pairScreenText{}
	door := rig.door(rig.homeA, in, out)
	inner := door.mailbox
	door.mailbox = func(relay string) (pair.Mailbox, error) {
		asked <- relay
		return inner(relay)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan error, 1)
	go func() { done <- door.run(ctx, []string{"--org", org.URL, "--code"}) }()
	out.waitFor(t, `codeaf pair (\S+)`)
	if got := <-asked; got != rig.url {
		t.Fatalf("the pairing went through %q; the organization named %q", got, rig.url)
	}
	stop()
	<-done
}

// What a member is told when it cannot work is a sentence that says what to do, and a pairing that was refused went nowhere.
func TestPairOrgRefusesInPlainWords(t *testing.T) {
	rig := newPairRig(t)
	t.Setenv("CODEAF_HOME", rig.homeA)
	t.Setenv("CODEAF_SYNC_URL", "")
	hosted := func(t *testing.T) pairDoor {
		d := rig.door(rig.homeA, strings.NewReader(""), &pairScreenText{})
		d.mailbox = func(relay string) (pair.Mailbox, error) {
			t.Errorf("a pairing went to a relay (%q) after the lookup was refused", relay)
			return pair.Mailbox{}, nil
		}
		return d
	}
	for _, tc := range []struct {
		name  string
		token string
		org   func(t *testing.T) string
		args  []string
		want  string
	}{
		{"an organization that runs no relay", "tok", func(t *testing.T) string { return orgAnswering(t, "tok", "").URL },
			[]string{"--code"}, "does not run a relay for your computers. Pair through another with --via <address>, or leave --org off"},
		{"no sign-in", "", func(t *testing.T) string { return orgAnswering(t, "tok", rig.url).URL },
			[]string{"--code"}, "Sign in to"},
		{"a sign-in the organization does not know", "stale", func(t *testing.T) string { return orgAnswering(t, "tok", rig.url).URL },
			[]string{"--code"}, "sign in first"},
		{"a server that is not there", "tok", func(t *testing.T) string {
			srv := httptest.NewServer(http.NotFoundHandler())
			srv.Close()
			return srv.URL
		}, []string{"--code"}, "is unreachable"},
		{"both --org and --via", "tok", func(t *testing.T) string { return orgAnswering(t, "tok", rig.url).URL },
			[]string{"--via", "http://127.0.0.1:1", "--code"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEAF_ORG_TOKEN", tc.token)
			args := append([]string{"--org", tc.org(t)}, tc.args...)
			var said strings.Builder
			old := usageErr
			usageErr = &said
			defer func() { usageErr = old }()
			err := hosted(t).run(context.Background(), args)
			if err == nil {
				t.Fatal("the pairing went on")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("it said %q; want a sentence with %q", err.Error(), tc.want)
			}
			if tc.want == "" && !strings.Contains(said.String(), "--org and --via each name the relay") {
				t.Fatalf("both flags together were not refused in words: %q %v", said.String(), err)
			}
		})
	}
}

// The organization names the address, and the address is checked before a pairing is sent through it: a web address, no sign-in of
// its own, and never plain http when the organization was reached over https.
func TestAdvertisedRelayIsCheckedBeforeAPairingUsesIt(t *testing.T) {
	for _, tc := range []struct {
		org, raw, want, err string
	}{
		{"https://org.example.com", "https://relay.example.com/", "https://relay.example.com", ""},
		{"https://org.example.com", "https://example.com/fabric", "https://example.com/fabric", ""},
		{"http://127.0.0.1:8787", "http://127.0.0.1:8788", "http://127.0.0.1:8788", ""},
		{"https://org.example.com", "", "", "does not run a relay"},
		{"https://org.example.com", "relay.example.com", "", "cannot use"},
		{"https://org.example.com", "https://u:p@relay.example.com", "", "cannot use"},
		{"https://org.example.com", "https://relay.example.com/?x=1", "", "cannot use"},
		{"https://org.example.com", "ftp://relay.example.com", "", "cannot use"},
		{"https://org.example.com", "http://relay.example.com", "", "not https"},
	} {
		got, err := advertisedRelay(tc.org, "org.example.com", tc.raw)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%q: want an error with %q, got %q, %v", tc.raw, tc.err, got, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%q: want %q, got %q, %v", tc.raw, tc.want, got, err)
		}
	}
}
