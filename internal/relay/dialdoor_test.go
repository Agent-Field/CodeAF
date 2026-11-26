package relay

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// dialLog records the class header of every dial that reaches a relay, so a test
// can say what a client SENT and not merely what the relay did with it.
type dialLog struct {
	mu      sync.Mutex
	dials   int
	classes []string
	present []bool
}

func (l *dialLog) wrap(door http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, DialPrefix) {
			l.mu.Lock()
			l.dials++
			_, sent := r.Header[http.CanonicalHeaderKey(HeaderDoor)]
			l.classes = append(l.classes, r.Header.Get(HeaderDoor))
			l.present = append(l.present, sent)
			l.mu.Unlock()
		}
		door.ServeHTTP(w, r)
	})
}

// The classes and the number they are measured against are the wire's spelling,
// and the Worker spells them the same way.
func TestTheClassesAreWhatTheWireSpells(t *testing.T) {
	if DoorLive != "live" || DoorMove != "move" || HeaderDoor != "Codeaf-Door" {
		t.Errorf("classes %q and %q in header %q; want live, move, Codeaf-Door", DoorLive, DoorMove, HeaderDoor)
	}
	if LiveStreams != 14 || MaxStreams != 16 {
		t.Errorf("LiveStreams %d, MaxStreams %d; want 14 and 16", LiveStreams, MaxStreams)
	}
	if MaxStreams-LiveStreams != 2 {
		t.Errorf("%d streams are kept for moving a chat; want 2", MaxStreams-LiveStreams)
	}
}

// A classified dial says its class on both carriers, and an unclassified one,
// which is what every older build sends, says nothing at all.
func TestADialSaysItsClassOnBothCarriersOrNothing(t *testing.T) {
	carriers := map[string]func(http.Handler) http.Handler{
		"websocket": func(h http.Handler) http.Handler { return wsOnly(h) },
		"raw":       func(h http.Handler) http.Handler { return rawOnly(h) },
	}
	for carrier, front := range carriers {
		for _, class := range []string{DoorLive, DoorMove, ""} {
			t.Run(carrier+"/"+class, func(t *testing.T) {
				var log dialLog
				address := mounted(t, "/fabric", front(log.wrap(&Server{})))
				registration, err := Register(context.Background(), address, aKey(t))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = registration.Close() }()

				conn, err := DialDoor(context.Background(), address, registration.Name(), class)
				if err != nil {
					t.Fatalf("DialDoor(%q): %v", class, err)
				}
				defer func() { _ = conn.Close() }()
				engine, err := registration.Accept()
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = engine.Close() }()
				if _, err := conn.Write([]byte("hi")); err != nil {
					t.Fatal(err)
				}
				if got := readN(t, engine, 2); got != "hi" {
					t.Fatalf("a classified dial carried %q, want it to work like any other", got)
				}

				log.mu.Lock()
				defer log.mu.Unlock()
				if log.dials != 1 {
					t.Fatalf("%d dials reached the relay, want 1", log.dials)
				}
				if log.classes[0] != class || log.present[0] != (class != "") {
					t.Fatalf("sent class %q (header present: %v), want %q (present: %v)", log.classes[0], log.present[0], class, class != "")
				}
			})
		}
	}
}

// Dial is the unclassified dial and always was: a build that has not been
// taught a class must reach a machine exactly as it did.
func TestDialSendsNoClass(t *testing.T) {
	var log dialLog
	address := mounted(t, "/fabric", log.wrap(&Server{}))
	registration, err := Register(context.Background(), address, aKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registration.Close() }()
	conn, err := Dial(context.Background(), address, registration.Name())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.dials != 1 || log.present[0] {
		t.Fatalf("Dial sent %d dials, class header present: %v; want one dial and no header", log.dials, log.present)
	}
}

// A class this build does not know is an error before anything is sent, so a
// misspelling cannot quietly become an unclassified dial that fills the machine.
func TestAClassNobodyKnowsIsRefusedBeforeItIsSent(t *testing.T) {
	var log dialLog
	address := mounted(t, "/fabric", log.wrap(&Server{}))
	for _, class := range []string{"Live", "watch", " move", "live,move"} {
		conn, err := DialDoor(context.Background(), address, "otter-lamp-42", class)
		if err == nil {
			_ = conn.Close()
			t.Fatalf("DialDoor(%q) was accepted", class)
		}
		if !strings.Contains(err.Error(), "not a dial class") {
			t.Errorf("DialDoor(%q) said %q, want it to say the class is not one", class, err)
		}
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.dials != 0 {
		t.Fatalf("%d dials reached the relay for classes that should never be sent", log.dials)
	}
}

// A bad name is still refused first, with the sentence it always had.
func TestDialDoorKeepsTheNameCheck(t *testing.T) {
	if _, err := DialDoor(context.Background(), "http://127.0.0.1:1", "nope", DoorLive); err == nil || !strings.Contains(err.Error(), "shape of a machine name") {
		t.Fatalf("a bad name gave %v", err)
	}
}
