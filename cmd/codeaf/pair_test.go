package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// pairScreenText is a terminal's output that a test can read while the door that
// writes it is still running.
type pairScreenText struct {
	mu   sync.Mutex
	text bytes.Buffer
}

func (b *pairScreenText) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.Write(p)
}

func (b *pairScreenText) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.String()
}

// waitFor is the first match of pattern in what a terminal has printed, or a
// failure after a while.
func (b *pairScreenText) waitFor(t *testing.T, pattern string) []string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if found := re.FindStringSubmatch(b.String()); found != nil {
			return found
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the terminal never printed %q; it printed:\n%s", pattern, b.String())
	return nil
}

// A typo is refused before any relay is looked up, so it needs no network and
// no relay setting at all.
func TestPairBadCode(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_SYNC_URL", "")
	for _, typed := range []string{"12", "abc-def-ghi", "4-715-30x"} {
		out := &pairScreenText{}
		err := newPairDoor(strings.NewReader(""), out).run(context.Background(), []string{typed})
		if !errors.Is(err, pair.ErrCodeShape) {
			t.Fatalf("typing %q ended with %v, want the code-shape sentence", typed, err)
		}
		if out.String() != "" {
			t.Fatalf("typing %q printed %q before it was refused", typed, out.String())
		}
	}
}

// A relay that is switched off is said at once, by both doors, and nothing is
// sent to any relay: not the saved one and not the one named on the command line.
func TestPairSyncOff(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request reached the relay: %s %s", r.Method, r.URL.Path)
	}))
	defer stub.Close()
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	if err := syncsetup.SaveRelayURL(dir, stub.URL); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_SYNC_URL", "OFF")
	for _, args := range [][]string{nil, {"42-715-302"}} {
		err := newPairDoor(strings.NewReader(""), &pairScreenText{}).run(context.Background(), args)
		if !errors.Is(err, pair.ErrSyncOff) {
			t.Fatalf("`codeaf pair %v` with sync off ended with %v", args, err)
		}
	}
}

// pairRig is two computers and a relay between them, in this process.
type pairRig struct {
	url   string
	homeA string
	homeB string
}

func newPairRig(t *testing.T) *pairRig {
	t.Helper()
	srv := httptest.NewServer(relayserve.New(relayserve.Config{}).Handler)
	t.Cleanup(srv.Close)
	rig := &pairRig{url: srv.URL, homeA: t.TempDir(), homeB: t.TempDir()}
	if _, err := identity.Ensure(rig.homeA); err != nil {
		t.Fatal(err)
	}
	return rig
}

// door is the real command with its three seams pointed at one of the homes.
func (r *pairRig) door(home string, in io.Reader, out io.Writer) pairDoor {
	d := newPairDoor(in, out)
	d.mailbox = func(string) (pair.Mailbox, error) {
		return pair.Mailbox{Box: pairbox.NewHTTP(r.url, &http.Client{Timeout: time.Minute}), URL: r.url, Host: "relay.test"}, nil
	}
	d.grant = func(route pair.Mailbox) (pair.Grant, error) {
		id, err := identity.Ensure(home)
		return pair.Grant{Identity: id, SyncURL: route.URL}, err
	}
	d.joining = func(replace bool) pair.Joining {
		return pair.Joining{Home: home, Label: "laptop", Replace: replace,
			SaveSyncURL: func(url string) error { return syncsetup.SaveRelayURL(home, url) }}
	}
	return d
}

// showing is the device that has the chats, running in the background with a
// pipe a test types its answers into.
type showing struct {
	out   *pairScreenText
	typed *io.PipeWriter
	done  chan error
	stop  context.CancelFunc
}

func (r *pairRig) show(t *testing.T) *showing {
	t.Helper()
	in, typed := io.Pipe()
	s := &showing{out: &pairScreenText{}, typed: typed, done: make(chan error, 1)}
	ctx, stop := context.WithCancel(context.Background())
	s.stop = stop
	t.Cleanup(func() { stop(); _ = typed.Close() })
	go func() { s.done <- r.door(r.homeA, in, s.out).run(ctx, nil) }()
	return s
}

func (s *showing) code(t *testing.T) string {
	t.Helper()
	return s.out.waitFor(t, `codeaf pair (\S+)`)[1]
}

func (s *showing) end(t *testing.T) error {
	t.Helper()
	select {
	case err := <-s.done:
		return err
	case <-time.After(20 * time.Second):
		t.Fatal("`codeaf pair` did not finish")
		return nil
	}
}

// joinWith types a code on the other computer and answers what it ends with.
func (r *pairRig) joinWith(t *testing.T, code string, replace bool) (*pairScreenText, error) {
	t.Helper()
	out := &pairScreenText{}
	args := []string{code}
	if replace {
		args = append(args, "--replace")
	}
	return out, r.door(r.homeB, strings.NewReader(""), out).run(context.Background(), args)
}

// The whole of both doors through the real command: one shows a code, the other
// types it, both screens show the same three words, a person types y, and the
// second computer holds the first one's chats and the relay it came through.
func TestPairBothDoorsOnATerminal(t *testing.T) {
	rig := newPairRig(t)
	shown := rig.show(t)
	code := shown.code(t)

	joined := make(chan error, 1)
	var joiner *pairScreenText
	go func() {
		var err error
		joiner, err = rig.joinWith(t, code, false)
		joined <- err
	}()

	asked := shown.out.waitFor(t, `"laptop" wants your chats\. Same three words on that screen: ([a-z ]+)\?  y / n `)
	if _, err := io.WriteString(shown.typed, "y\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-joined; err != nil {
		t.Fatal(err)
	}
	if err := shown.end(t); err != nil {
		t.Fatal(err)
	}

	wanted := pair.WaitingChatsLine(asked[1])
	if got := joiner.String(); got != wanted+"\n"+pair.JoinedLine+"\n" {
		t.Fatalf("the joining terminal printed:\n%s\nwant:\n%s\n%s", got, wanted, pair.JoinedLine)
	}
	if !strings.HasSuffix(shown.out.String(), pair.PairedChatsLine("laptop")+"\n") {
		t.Fatalf("the sharing terminal did not end on the paired line:\n%s", shown.out.String())
	}
	first, _ := identity.Load(rig.homeA)
	second, err := identity.Load(rig.homeB)
	if err != nil || second.ID() != first.ID() {
		t.Fatalf("the second computer holds %v (%v), want the first one's chats", second.ID(), err)
	}
	if got := syncsetup.Resolve(rig.homeB).URL; got != rig.url {
		t.Fatalf("the second computer saved the relay %q, want %q", got, rig.url)
	}
}

// A person who does not say y lets nobody in: Enter alone is a no, and the
// joining computer is told so in the pair package's own sentence.
func TestPairEnterIsNo(t *testing.T) {
	rig := newPairRig(t)
	shown := rig.show(t)
	code := shown.code(t)

	joined := make(chan error, 1)
	go func() { _, err := rig.joinWith(t, code, false); joined <- err }()
	shown.out.waitFor(t, `y / n `)
	if _, err := io.WriteString(shown.typed, "\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-joined; !errors.Is(err, pair.ErrRefused) {
		t.Fatalf("the joining computer ended with %v, want a refusal", err)
	}
	if err := shown.end(t); !errors.Is(err, pair.ErrRefused) {
		t.Fatalf("the sharing computer ended with %v, want a refusal", err)
	}
	if _, err := identity.Load(rig.homeB); err == nil {
		t.Fatal("a refused computer was given the chats")
	}
}

// A wrong code costs the person nothing but a new code, which is on the screen
// before anyone presses a key; ctrl+c then ends the door cleanly.
func TestPairWrongCodeBurnsAndShowsANewOne(t *testing.T) {
	rig := newPairRig(t)
	shown := rig.show(t)
	code := shown.code(t)

	wrong := code[:len(code)-1] + string(rune('0'+(code[len(code)-1]-'0'+1)%10))
	if _, err := rig.joinWith(t, wrong, false); !errors.Is(err, pair.ErrCodeDidNotWork) {
		t.Fatalf("a wrong code ended with %v, want the code-did-not-work sentence", err)
	}
	shown.out.waitFor(t, regexp.QuoteMeta(pair.BurnLine)+`\n(?s:.*)codeaf pair \S+`)
	shown.stop()
	if err := shown.end(t); err != nil {
		t.Fatalf("ctrl+c ended the sharing door with %v, want a clean end", err)
	}
}

// Every answer but y and yes is a no, whatever its case or spacing, and the end
// of the input is one too.
func TestTerminalConfirmReadsOnlyYesAsYes(t *testing.T) {
	for typed, want := range map[string]bool{
		"y\n": true, " YES \n": true, "Yes\n": true,
		"\n": false, "n\n": false, "no\n": false, "yep\n": false, "ok\n": false, "": false,
	} {
		out := &pairScreenText{}
		got := newTerminal(strings.NewReader(typed), out).confirm(context.Background(), "ok?")
		if got != want {
			t.Errorf("answering %q gave %v, want %v", typed, got, want)
		}
		if !strings.HasPrefix(out.String(), "ok?  "+pair.AskChoice+" ") {
			t.Errorf("the question was printed as %q", out.String())
		}
	}
}

// Silence is a no: a question that runs out of time answers false, and the next
// question still gets the line a person types afterwards.
func TestTerminalConfirmSilenceIsNo(t *testing.T) {
	in, typed := io.Pipe()
	defer typed.Close()
	person := newTerminal(in, &pairScreenText{})

	quiet, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if person.confirm(quiet, "first?") {
		t.Fatal("silence was read as a yes")
	}
	go func() { _, _ = io.WriteString(typed, "y\n") }()
	if !person.confirm(context.Background(), "second?") {
		t.Fatal("the answer typed after the silence was lost")
	}
}

// `codeaf serve` asks nobody when nobody is at a terminal, which the host reads
// as a no.
func TestServeApproverIsAbsentWithoutATerminal(t *testing.T) {
	if serveApprover(context.Background(), nil, io.Discard) != nil {
		t.Fatal("a machine with no terminal was given someone to ask")
	}
}
