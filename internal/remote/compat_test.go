package remote

import (
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

// sessionInstanceJSON matches the random per-Session instance identifier so a
// byte-level golden comparison normalises that per-run token.
var sessionInstanceJSON = regexp.MustCompile(`"sessionInstance":"[A-Z2-7]+"`)

// machineJSON matches the name of the computer the test happens to run on. It
// is the one other value in a welcome that depends on where the test runs, so it
// is normalised too and nothing else is: every other byte is pinned.
var machineJSON = regexp.MustCompile(`"machine":"[^"]*"`)

// TestAnOldViewerGetsTheSameBytesAsBefore verifies V14: a local hello without
// a remote viewer stamp receives the exact welcome payload bytes captured on
// the untouched tree, with no remote key present.
func TestAnOldViewerGetsTheSameBytesAsBefore(t *testing.T) {
	sess := heldSession(&fakeAgent{})
	viewer := dialSession(t, sess)
	frame := viewer.hello(Hello{Version: Version, Surface: "macbook"})
	if frame.Kind != "welcome" {
		t.Fatalf("handshake answered %q: %s", frame.Kind, frame.Error)
	}
	if strings.Contains(string(frame.Payload), `"remote"`) {
		t.Fatalf("local welcome carried a remote key: %s", frame.Payload)
	}
	got := sessionInstanceJSON.ReplaceAllString(string(frame.Payload), `"sessionInstance":"<INSTANCE>"`)
	got = machineJSON.ReplaceAllString(got, `"machine":"<MACHINE>"`)
	const golden = `{"machine":"<MACHINE>","sessionInstance":"<INSTANCE>","version":22,"workspace":"/home/somebody/api","sessionFile":"/home/somebody/.codeaf/v3/sessions/-home-somebody-api/one.jsonl","resumed":true,"model":"","build":"dev","note":"session open elsewhere — started a new one","bashBackgroundAfterSeconds":47,"profileDir":"/srv/profile","facts":{"rev":1,"facts":{}},"persistent":true,"driver":{"yours":true},"steerOwner":true,"teams":true,"team_membership_version":1,"delegation":true,"team_settings":true,"wrap_up":true}`
	if got != golden {
		t.Fatalf("welcome payload changed:\ngot:  %s\nwant: %s", got, golden)
	}
}

// TestARemoteWelcomeEchoesTheStampedViewer verifies W1: a remote hello has its
// stamp copied onto the connection and echoed back in Welcome.Remote.
func TestARemoteWelcomeEchoesTheStampedViewer(t *testing.T) {
	sess := heldSession(&fakeAgent{})
	viewer := dialSession(t, sess)
	stamped := RemoteViewer{
		Device: "dev-laptop",
		Label:  "laptop",
		Watch:  true,
		Drive:  true,
		Road:   "relay",
	}
	frame := viewer.hello(Hello{
		Version: Version,
		Surface: "spoofed",
		Session: sess.engine.SessionFile,
		Join:    true,
		Remote:  &stamped,
	})
	if frame.Kind != "welcome" {
		t.Fatalf("handshake answered %q: %s", frame.Kind, frame.Error)
	}
	welcome := decode[Welcome](t, frame.Payload)
	if welcome.Remote == nil {
		t.Fatal("remote welcome omitted Remote")
	}
	if *welcome.Remote != stamped {
		t.Fatalf("welcome echoed %+v, want %+v", *welcome.Remote, stamped)
	}
}

// TestARemoteHelloMustJoinAnOpenChatAndNeverStartOne verifies W1: a remote
// connection whose hello has Join false or New true is refused at the
// handshake before the session opener is ever reached.
func TestARemoteHelloMustJoinAnOpenChatAndNeverStartOne(t *testing.T) {
	sess := heldSession(&fakeAgent{})
	stamp := &RemoteViewer{Device: "dev-phone", Label: "phone", Watch: true, Drive: true, Road: "relay"}

	for _, tc := range []struct {
		name  string
		hello Hello
	}{
		{
			name: "join false is refused",
			hello: Hello{
				Version: Version,
				Session: sess.engine.SessionFile,
				Join:    false,
				Remote:  stamp,
			},
		},
		{
			name: "new true is refused even with join true",
			hello: Hello{
				Version: Version,
				Session: sess.engine.SessionFile,
				Join:    true,
				New:     true,
				Remote:  stamp,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opened atomic.Int64
			conn := dialOpening(t, func(Hello) (*Session, error) {
				opened.Add(1)
				return sess, nil
			})
			frame := conn.hello(tc.hello)
			if frame.Kind != "fatal" {
				t.Fatalf("handshake answered %q, want fatal refusal", frame.Kind)
			}
			const want = "remote control opens a chat that is already running; it never starts one"
			if frame.Error != want {
				t.Fatalf("refusal = %q, want %q", frame.Error, want)
			}
			if got := opened.Load(); got != 0 {
				t.Fatalf("refused remote hello called open %d times", got)
			}
		})
	}
}
