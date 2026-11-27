//go:build linux

package sessionhost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/coder/websocket"
)

// helloLine is a hello as a window writes it.
func helloLine(payload string) string {
	return `{"kind":"hello","payload":` + payload + `}` + "\n"
}

// surfaceOf reads the name a hello gives its window, and whether the hello has a remote stamp.
func surfaceOf(t *testing.T, line string) (surface string, remoteStamped bool) {
	t.Helper()
	var frame struct {
		Kind    string
		Payload map[string]json.RawMessage
	}
	if err := json.Unmarshal([]byte(line), &frame); err != nil || frame.Kind != "hello" {
		t.Fatalf("not a hello: %q: %v", line, err)
	}
	for name, raw := range frame.Payload {
		switch strings.ToLower(name) {
		case "surface":
			if surface != "" {
				t.Fatalf("the hello names its window twice: %q", line)
			}
			_ = json.Unmarshal(raw, &surface)
		case "remote":
			remoteStamped = true
		}
	}
	return surface, remoteStamped
}

// Contract S5, at the renamer: a window the server asked to name is named by the person, with its own word kept only
// after the name, and a word of its own can neither stand alone, nor stand in for the name, nor bring a remote stamp.
func TestS5_TheWindowsFirstLineIsNamedByThePersonWhateverItSaid(t *testing.T) {
	own := windowNamed{name: "kai", device: true}
	bot := windowNamed{name: "kai's bot"}
	for _, tc := range []struct {
		name   string
		window windowNamed
		hello  string
		want   string
	}{
		{"a phone", own, `{"version":3,"surface":"phone"}`, "kai's phone"},
		{"a computer's own name", own, `{"version":3,"surface":"tower"}`, "kai's tower"},
		{"no word at all", own, `{"version":3}`, "kai"},
		{"a word that is the name", own, `{"version":3,"surface":"Kai"}`, "kai"},
		{"a word that says it is someone else", own, `{"version":3,"surface":"mia"}`, "kai's mia"},
		{"a word spelled in another case", own, `{"version":3,"SURFACE":"phone"}`, "kai's phone"},
		{"a word that is not text", own, `{"version":3,"surface":7}`, "kai"},
		{"a word with control characters", own, `{"version":3,"surface":"ph\u001b[0mone\u0000"}`, "kai's ph[0mone"},
		{"a bot with a word of its own", bot, `{"version":3,"surface":"phone"}`, "kai's bot"},
		{"a stamp as another door's viewer", own, `{"version":3,"surface":"phone","remote":{"label":"mia","drive":true},"Remote":{"label":"mia"}}`, "kai's phone"},
		{"a word too long for the engine to draw", own, `{"version":3,"surface":"` + strings.Repeat("w", 80) + `"}`, "kai's " + strings.Repeat("w", 32-len("kai's "))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			later := "{\"kind\":\"call\",\"id\":1,\"method\":\"Submit\"}\n\x00\xff binary\n"
			r := &windowRenamer{src: iotest.OneByteReader(strings.NewReader(helloLine(tc.hello) + later)), window: tc.window}
			got, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			first, rest, _ := strings.Cut(string(got), "\n")
			if rest != later {
				t.Fatalf("what came after the hello was changed: %q", rest)
			}
			surface, stamped := surfaceOf(t, first)
			if surface != tc.want || stamped {
				t.Fatalf("the window was named %q (remote stamp %v), want %q with none", surface, stamped, tc.want)
			}
			if label := []rune(surface); len(label) > windowNameMost {
				t.Fatalf("the name is %d characters and the engine draws %d", len(label), windowNameMost)
			}
		})
	}
}

// Contract S6: everything that is not the hello is the window's, byte for byte. A first line that is not a hello, a
// stream that ends before its first newline, one that never has a newline, and an error in the middle all reach the
// engine as they came.
func TestS6_NothingButTheHelloIsChanged(t *testing.T) {
	window := windowNamed{name: "kai", device: true}
	for _, tc := range []struct{ name, in string }{
		{"a first line that is a call", "{\"kind\":\"call\",\"method\":\"Submit\"}\n{\"kind\":\"hello\",\"payload\":{\"surface\":\"phone\"}}\n"},
		{"a hello with no payload", "{\"kind\":\"hello\"}\n"},
		{"a hello whose payload is not an object", "{\"kind\":\"hello\",\"payload\":[1]}\n"},
		{"binary that is not a line", "\x00\x01\xff\xfe\n\x80"},
		{"a line cut short by the end", "{\"kind\":\"hello\",\"payl"},
		{"nothing", ""},
		{"only a newline", "\n"},
	} {
		got, err := io.ReadAll(&windowRenamer{src: strings.NewReader(tc.in), window: window})
		if err != nil || string(got) != tc.in {
			t.Errorf("%s: the engine was sent %q (%v), want %q", tc.name, got, err, tc.in)
		}
	}
	long := strings.Repeat("x", windowLineMost+10)
	if got, err := io.ReadAll(&windowRenamer{src: strings.NewReader(long), window: window}); err != nil || string(got) != long {
		t.Errorf("a first line with no end was changed or cut: %d bytes, %v", len(got), err)
	}
	// An error from the window's side is delivered after the bytes read before it.
	broken := errors.New("the window's connection broke")
	got, err := io.ReadAll(&windowRenamer{src: io.MultiReader(strings.NewReader("{\"kind\":\"hel"), iotest.ErrReader(broken)), window: window})
	if string(got) != "{\"kind\":\"hel" || !errors.Is(err, broken) {
		t.Errorf("a broken stream gave %q and %v", got, err)
	}
}

// Contract S5 and S6 at the runner's door: a wire the server asked to name is renamed in its hello and nothing else
// changes in either direction; a wire with no such request (a guest's, whose hello the server renames itself, or an
// older server's) reaches the engine exactly as the window wrote it.
func TestS5_TheRunnerNamesAnOwnersWindowFromTheServersHeaders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
		hello   string
		want    string
	}{
		{"an owner's phone", map[string]string{windowHeader: "alice", windowDeviceHeader: windowDeviceOwn}, `{"version":3,"surface":"phone"}`, "alice's phone"},
		{"a bot", map[string]string{windowHeader: "alice's bot"}, `{"version":3,"surface":"phone"}`, "alice's bot"},
		{"no request to name it", nil, `{"version":3,"surface":"phone"}`, "phone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			launcher := newWireTestLauncher()
			procs := &fakeProcesses{live: processTable{}}
			s := newTestSupervisor(t, cfg, healthyReport(), launcher, procs)
			createChat(t, s, "alice", chatA)
			procs.live[cfg.UIDBase] = []int{4321}
			header := http.Header{"Authorization": {"Bearer " + s.cfg.Secret}, "X-Codeaf-Person": {"alice"}}
			for name, value := range tc.headers {
				header.Set(name, value)
			}
			server := httptest.NewServer(s)
			t.Cleanup(server.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/sessions/"+chatA+"/wire", &websocket.DialOptions{HTTPHeader: header})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { conn.CloseNow() })
			child := launcher.child(t)

			// The hello and the next call go in one message and in two: the engine must see the same stream either way.
			later := `{"kind":"call","id":1,"method":"Submit"}` + "\n"
			if err := conn.Write(ctx, websocket.MessageBinary, []byte(helloLine(tc.hello)+later)); err != nil {
				t.Fatal(err)
			}
			engine := bufio.NewReader(child.in)
			first, err := engine.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if surface, _ := surfaceOf(t, first); surface != tc.want {
				t.Fatalf("the engine's hello named the window %q, want %q", surface, tc.want)
			}
			if got, err := engine.ReadString('\n'); err != nil || got != later {
				t.Fatalf("the call after the hello reached the engine as %q (%v)", got, err)
			}
			if err := conn.Write(ctx, websocket.MessageBinary, []byte("\x00raw\n")); err != nil {
				t.Fatal(err)
			}
			if got, err := engine.ReadString('\n'); err != nil || got != "\x00raw\n" {
				t.Fatalf("a later message reached the engine as %q (%v)", got, err)
			}
			// What the engine says goes back to the window unchanged.
			say := bytes.Repeat([]byte{0, 0xff, '\n', '{'}, 100)
			go func() { _, _ = child.out.Write(say) }()
			if _, got, err := conn.Read(ctx); err != nil || !bytes.Equal(got, say) {
				t.Fatalf("what the engine said came back as %d bytes (%v)", len(got), err)
			}
		})
	}
}
