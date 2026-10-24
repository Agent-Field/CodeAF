//go:build linux

package executor

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

func membersOf(pgid int) []int { return processgroup.Members(pgid) }

const tcpFixture = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12345 1 0000 100 0 0 10 0
   1: 0100007F:0050 0100007F:9C40 01 00000000:00000000 00:00000000 00000000  1000        0 22222 1 0000 100 0 0 10 0
   2: 00000000000000000000000001000000:1538 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 33333 1 0000 100 0 0 10 0
`

func TestParseListeningKeepsOnlyListenSockets(t *testing.T) {
	want := map[uint64]int{12345: 8080, 33333: 5432}
	if got := parseListening(tcpFixture); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseSocketLink(t *testing.T) {
	if n, ok := parseSocketLink("socket:[987]"); !ok || n != 987 {
		t.Fatal("socket link")
	}
	if _, ok := parseSocketLink("/dev/null"); ok {
		t.Fatal("not a socket")
	}
}
