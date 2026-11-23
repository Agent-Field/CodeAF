package relay

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ONE SOURCE OF TRUTH FOR EVERY NUMBER THE TWO RELAYS ENFORCE. The hosted Worker
// states them again in relay/hosted/src/engine/carrier.js, under the Go names in a
// trailing comment, and a number stated in two places drifts. A relay whose Worker
// still allowed sixteen live dials after this package said fourteen would pass
// every test of both and fail a person, so this reads the Worker's file and holds
// each constant to this package's. The Worker's own tests read relay.go for the
// same table from the other side.
func TestTheWorkerStatesTheSameNumbersAsThisPackage(t *testing.T) {
	raw, err := os.ReadFile("../../relay/hosted/src/engine/carrier.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	expression := func(name string) string {
		t.Helper()
		found := regexp.MustCompile(`(?m)^export const ` + name + ` = ([^;]+);`).FindStringSubmatch(source)
		if found == nil {
			t.Fatalf("carrier.js has no `export const %s`", name)
		}
		return strings.TrimSpace(found[1])
	}
	number := func(name string) int64 {
		t.Helper()
		text := expression(name)
		if left, right, shifted := strings.Cut(text, "<<"); shifted {
			base, errBase := strconv.ParseInt(strings.TrimSpace(left), 10, 64)
			by, errBy := strconv.ParseInt(strings.TrimSpace(right), 10, 64)
			if errBase != nil || errBy != nil {
				t.Fatalf("%s = %q is not a number or a shift", name, text)
			}
			return base << by
		}
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			t.Fatalf("%s = %q is not a number", name, text)
		}
		return n
	}
	text := func(name string) string {
		t.Helper()
		return strings.Trim(expression(name), `'"`)
	}

	for _, row := range []struct {
		js   string
		want int64
	}{
		{"MAX_PAYLOAD", MaxPayload},
		{"MAX_STREAMS", MaxStreams},
		{"LIVE_STREAMS", LiveStreams},
		{"HANDSHAKE_MS", HandshakeWithin.Milliseconds()},
		{"PROBE_MS", ProbeWithin.Milliseconds()},
		{"DIALS_PER_MINUTE", DialsPerMinute},
		{"REGISTRATIONS_PER_MINUTE", RegistrationsPerMinute},
		{"BREAKER_BYTES_PER_SECOND", BreakerRate},
		{"BREAKER_WINDOW_MS", BreakerWindow.Milliseconds()},
	} {
		if got := number(row.js); got != row.want {
			t.Errorf("%s is %d in the Worker and %d in this package", row.js, got, row.want)
		}
	}
	for _, row := range []struct{ js, want string }{
		{"DOOR_LIVE", DoorLive},
		{"DOOR_MOVE", DoorMove},
		{"HEADER_DOOR", strings.ToLower(HeaderDoor)},
		{"BREAKER_REASON", breakerReason},
	} {
		if got := text(row.js); got != row.want {
			t.Errorf("%s is %q in the Worker and %q in this package", row.js, got, row.want)
		}
	}
	if MaxStreams-LiveStreams != 2 {
		t.Errorf("%d streams are kept for a move; the design keeps 2", MaxStreams-LiveStreams)
	}
}
