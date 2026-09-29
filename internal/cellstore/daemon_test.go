package cellstore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/furrow"
)

type fixedTransport struct {
	out   string
	err   error
	calls int
}

func (f *fixedTransport) Do(context.Context, Target, Op) ([]byte, error) {
	f.calls++
	return []byte(f.out), f.err
}

// Only an absent engine is answered by the next transport; an engine that ran
// and refused is an answer, and running the verb again elsewhere could do it twice.
func TestFallbackAnswersOnlyForAbsence(t *testing.T) {
	refusal := errors.New("engine refused")
	cases := map[string]struct {
		primary error
		calls   int
		want    string
		wantErr error
	}{
		"answered":    {nil, 0, "primary", nil},
		"unavailable": {ErrUnavailable, 1, "secondary", nil},
		"refused":     {refusal, 0, "primary", refusal},
	}
	for name, tc := range cases {
		primary := &fixedTransport{out: "primary", err: tc.primary}
		secondary := &fixedTransport{out: "secondary"}
		out, err := Fallback{primary, secondary}.Do(context.Background(), Target{}, snapOp{})
		if string(out) != tc.want || !errors.Is(err, tc.wantErr) || secondary.calls != tc.calls {
			t.Errorf("%s: got %q, %v with %d fallback calls", name, out, err, secondary.calls)
		}
	}
}

func TestTransportForPicksTheDaemonUnlessToldOtherwise(t *testing.T) {
	if _, ok := transportFor("", nil).(Fallback); !ok {
		t.Error("the default is the daemon with the spawn behind it")
	}
	if _, ok := transportFor("", func(context.Context, string, []string, ...string) ([]byte, error) { return nil, nil }).(Spawn); !ok {
		t.Error("a caller's own Runner must see every spawn")
	}
	t.Setenv(daemonEnv, "0")
	if _, ok := transportFor("", nil).(Spawn); !ok {
		t.Errorf("%s=0 must turn the daemon off", daemonEnv)
	}
}

func TestDaemonStartsOnDemandAndStops(t *testing.T) {
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Skipf("no engine binary: %v", err)
	}
	d := daemonFor(t, bin).(Daemon)
	if _, err := os.Stat(d.Socket); err == nil {
		t.Fatal("the daemon is up before anyone asked")
	}
	c := newCell(t)
	e := Engine{Binary: bin, DataRoot: t.TempDir(), Transport: d}
	for i := 0; i < 2; i++ {
		if _, err := e.Seal(context.Background(), c, TurnInfo{Calls: []Executed{exec1("bash", "")}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(d.Socket); err != nil {
		t.Fatalf("no daemon after two seals: %v", err)
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A program that is not an engine cannot become a daemon: the daemon transport
// says it is unavailable, promptly, and the fallback carries on.
func TestDaemonOfANonEngineIsUnavailable(t *testing.T) {
	false_, err := filepath.Abs("/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	d := Daemon{Socket: filepath.Join(t.TempDir(), "e.sock"), Binary: false_}
	_, err = d.Do(context.Background(), Target{}, snapOp{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

// Two engine builds never share a daemon: the socket is named after the binary.
func TestSocketIsKeyedToTheEngineBinary(t *testing.T) {
	a := socketFor("/x/furrow-0.1.0-aaaaaaaaaaaa")
	b := socketFor("/x/furrow-0.1.0-bbbbbbbbbbbb")
	if a == b || filepath.Base(a) != "engine-0.1.0-aaaaaaaaaaaa.sock" {
		t.Errorf("sockets %q and %q must differ by build", a, b)
	}
}

// serveHealth answers every request on socket as a daemon of the named engine.
func serveHealth(t *testing.T, socket, engine string) {
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			var req wireRequest
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			_ = json.Unmarshal(line, &req)
			ok, _ := json.Marshal(map[string]string{"engine": engine})
			reply, _ := json.Marshal(wireResponse{ID: req.ID, Ok: ok})
			conn.Write(append(reply, '\n'))
			conn.Close()
		}
	}()
}

// A daemon of another engine build is not used: the verb goes to the spawn.
func TestDaemonOfAnotherEngineIsNotUsed(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "e.sock")
	serveHealth(t, socket, "furrow-0.0.9-oldoldoldold")
	d := Daemon{Socket: socket, Binary: "/x/furrow-0.1.0-newnewnewnew"}
	if _, err := d.Do(context.Background(), Target{}, snapOp{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	spawned := &fixedTransport{out: "spawned"}
	out, err := Fallback{d, spawned}.Do(context.Background(), Target{}, snapOp{})
	if err != nil || string(out) != "spawned" || spawned.calls != 1 {
		t.Fatalf("got %q, %v with %d spawns", out, err, spawned.calls)
	}
}
