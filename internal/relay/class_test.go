package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

// dialAll opens n dials of one class against a registered machine and keeps
// them open until the test ends. An empty class is the dial an older build makes.
func dialAll(t *testing.T, address, name, class string, n int) []io.Closer {
	t.Helper()
	var held []io.Closer
	for i := 0; i < n; i++ {
		conn, err := DialDoor(context.Background(), address, name, class)
		if err != nil {
			t.Fatalf("dial %d of class %q: %v", i+1, class, err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		held = append(held, conn)
	}
	return held
}

// refused says whether a dial of this class is turned away as busy, which is
// how a machine at its ceiling answers: a 429, which the client reads as ErrTooMany.
func refused(t *testing.T, address, name, class string) bool {
	t.Helper()
	conn, err := DialDoor(context.Background(), address, name, class)
	if err == nil {
		t.Cleanup(func() { _ = conn.Close() })
		return false
	}
	if !errors.Is(err, ErrTooMany) {
		t.Fatalf("a dial of class %q failed with %v, want it accepted or turned away as busy", class, err)
	}
	return true
}

func aMachine(t *testing.T, address string) string {
	t.Helper()
	registration, err := Register(context.Background(), address, aKey(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registration.Close() })
	// Every surface the relay opens lands on the machine's accept queue, which
	// holds only a ceiling's worth, so a test that dials all of them drains it.
	go func() {
		for {
			stream, err := registration.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(io.Discard, stream) }()
		}
	}()
	return registration.Name()
}

// V21: with fourteen live viewers attached the fifteenth live dial is refused,
// and a move dial still succeeds, twice, because two streams are kept for it.
func TestLiveDialsStopAtFourteenAndMoveKeepsTwo(t *testing.T) {
	_, address := live(t)
	name := aMachine(t, address)

	dialAll(t, address, name, DoorLive, LiveStreams)
	if !refused(t, address, name, DoorLive) {
		t.Fatalf("a live dial past %d streams was accepted", LiveStreams)
	}
	dialAll(t, address, name, DoorMove, MaxStreams-LiveStreams)
	if !refused(t, address, name, DoorMove) {
		t.Fatalf("a move dial past %d streams was accepted", MaxStreams)
	}
	if !refused(t, address, name, "") {
		t.Fatalf("an unclassified dial past %d streams was accepted", MaxStreams)
	}
}

// V21: a dial that claims no class, which is every dial from a build older than
// the class, gets all sixteen streams, exactly as before.
func TestAnUnclassifiedDialStillGetsAllSixteen(t *testing.T) {
	_, address := live(t)
	name := aMachine(t, address)
	dialAll(t, address, name, "", MaxStreams)
	if !refused(t, address, name, "") {
		t.Fatalf("an unclassified dial past %d streams was accepted", MaxStreams)
	}
}

// A class this relay has never heard of is not an error and not a class: the
// dial is held to the full sixteen, so a class a later build adds does not turn
// an older relay's answer into a refusal.
func TestAnUnknownDoorIsTreatedAsNoDoor(t *testing.T) {
	for _, said := range []string{"bogus", "LIVE", "Live", "live,move", ""} {
		t.Run(said, func(t *testing.T) {
			sets := func(door http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					r.Header.Set(HeaderDoor, said)
					door.ServeHTTP(w, r)
				})
			}
			address := mounted(t, "/fabric", sets(&Server{}))
			name := aMachine(t, address)
			dialAll(t, address, name, "", MaxStreams)
			if !refused(t, address, name, "") {
				t.Fatalf("a dial claiming %q past %d streams was accepted", said, MaxStreams)
			}
		})
	}
}

// The reservation is measured against the machine's total, so streams from
// older clients cannot crowd a move out of the two kept for it.
func TestUnclassifiedStreamsCannotCrowdAMoveOutOfItsTwo(t *testing.T) {
	_, address := live(t)
	name := aMachine(t, address)
	dialAll(t, address, name, "", LiveStreams)
	if !refused(t, address, name, DoorLive) {
		t.Fatal("a live dial was accepted at a total of 14")
	}
	dialAll(t, address, name, DoorMove, 1)
	dialAll(t, address, name, "", 1)
	if !refused(t, address, name, DoorMove) {
		t.Fatalf("a move dial past %d streams was accepted", MaxStreams)
	}
}

// A live stream that ends gives its slot back to the next live dial.
func TestAnEndedLiveStreamFreesItsSlot(t *testing.T) {
	_, address := live(t)
	name := aMachine(t, address)
	held := dialAll(t, address, name, DoorLive, LiveStreams)
	if !refused(t, address, name, DoorLive) {
		t.Fatal("the fifteenth live dial was accepted")
	}
	_ = held[0].Close()
	deadline := time.Now().Add(10 * time.Second)
	for refused(t, address, name, DoorLive) {
		if time.Now().After(deadline) {
			t.Fatal("a closed live stream never freed its slot")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The class is read on the raw upgrade as well as the WebSocket, so a relay
// reached either way holds the same ceilings.
func TestTheClassGateHoldsOnTheRawCarrierToo(t *testing.T) {
	address := mounted(t, "/fabric", rawOnly(&Server{}))
	name := aMachine(t, address)
	dialAll(t, address, name, DoorLive, LiveStreams)
	if !refused(t, address, name, DoorLive) {
		t.Fatal("the fifteenth live dial on the raw carrier was accepted")
	}
	dialAll(t, address, name, DoorMove, 1)
}
