package remote

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// finalDial is a dial that the far end answered with a no it will keep saying: the sign-in was taken away, the chat is
// gone.
type finalDial struct{ words string }

func (e finalDial) Error() string { return e.words }

// roamAfterACut dials one real link, then answers every later dial with err, and cuts the first link.
func roamAfterACut(t *testing.T, ends func(error) bool, err error) (*Client, *atomic.Int32) {
	t.Helper()
	var dials atomic.Int32
	dialer := func() (io.ReadWriteCloser, error) {
		if dials.Add(1) > 1 {
			return nil, err
		}
		ours, theirs := net.Pipe()
		engine := &scripted{t: t, conn: theirs, said: Welcome{Version: Version, SessionFile: "/j.jsonl", Persistent: true}}
		go engine.serve(nil)
		time.AfterFunc(50*time.Millisecond, func() { _ = ours.Close() })
		return ours, nil
	}
	client, derr := Roam("the server", Hello{Workspace: "app"}, Roaming{Dial: dialer, Window: 1500 * time.Millisecond, EndsOn: ends})
	if derr != nil {
		t.Fatalf("roam: %v", derr)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, &dials
}

// Contract RT6: a window whose link drops redials by itself, EXCEPT when the dial is answered with a no the caller
// knows to be final. Then it stops at once and says the far end's own words, so a person whose access was taken away
// reads that, and not "reconnecting for up to 30 minutes".
func TestRT6_AFinalDialRefusalEndsTheRedialAtOnceWithItsOwnWords(t *testing.T) {
	refusal := finalDial{"There is no such chat."}
	client, dials := roamAfterACut(t, func(err error) bool { var f finalDial; return errors.As(err, &f) }, refusal)
	waitFor(t, "the client to be over", func() bool { return client.Err() != nil })
	if got := client.Err().Error(); !strings.Contains(got, refusal.words) {
		t.Fatalf("a final refusal reads %q, want it to carry %q", got, refusal.words)
	}
	if n := dials.Load(); n != 2 {
		t.Fatalf("it dialled %d times; a final refusal is asked once", n)
	}
}

// Contract RT7: left as it has always been (no EndsOn), the same refusal is a blip and the redial keeps trying until its
// window ends, because a fix keeps defaults.
func TestRT7_WithoutEndsOnTheSameRefusalIsStillRetried(t *testing.T) {
	refusal := finalDial{"There is no such chat."}
	client, dials := roamAfterACut(t, nil, refusal)
	waitFor(t, "the client to give up", func() bool { return client.Err() != nil })
	if n := dials.Load(); n < 2 {
		t.Fatalf("it dialled %d times; with no EndsOn a refused dial is tried again within the window", n)
	}
	if got := client.Err().Error(); strings.Contains(got, refusal.words) {
		t.Fatalf("with no EndsOn the client was ended with the refusal's words %q: the old schedule says the link died", got)
	}
}
