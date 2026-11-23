package filesdoor

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// KEYS: the door key is derived from the cell key with the files door's OWN
// salt and the device id, so it is never the task door's key for the device,
// and the name is the relay name of that key.
func TestFilesDoorKeyIsItsOwn(t *testing.T) {
	w := newWorld(t)
	key, err := DoorKey(w.id, w.a.ID())
	if err != nil {
		t.Fatal(err)
	}
	// The derivation written out from its contract, not read from the code.
	want, err := hkdf.Key(sha256.New, w.id.CellKey(), []byte("codeaf files door v1"), w.a.ID(), 32)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key.Bytes(), want) {
		t.Fatal("the door key is not HKDF-SHA256 over the cell key with the files door salt and the device id")
	}
	task, err := taskdoor.DoorKey(w.id, w.a.ID())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(key.Bytes(), task.Bytes()) {
		t.Fatal("the files door key is the task door key")
	}
	other, err := DoorKey(w.id, w.b.ID())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(key.Bytes(), other.Bytes()) {
		t.Fatal("two devices share a door key")
	}
	name, err := DoorName(w.id, w.a.ID())
	if err != nil {
		t.Fatal(err)
	}
	if name != relay.NameFor(key.PublicKey().Bytes()) {
		t.Fatalf("name %q is not the relay name of the door key", name)
	}
}

// KEYS: a tunnel made for one kind of door cannot complete against the other,
// even when the same static key is used for both.
func TestFilesDoorAndTaskDoorTunnelsDoNotInterchange(t *testing.T) {
	t.Run("a task tunnel against a files door", func(t *testing.T) {
		w := newWorld(t)
		store := wrap(t, newDisk(t))
		w.serve(w.config(store))
		name, _ := DoorName(w.id, w.a.ID())
		stream, err := relay.Dial(context.Background(), w.service, name)
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		files, _ := DoorKey(w.id, w.a.ID())
		mine, _ := DoorKey(w.id, w.b.ID())
		if _, err := pair.ConnectWithKey(stream, files.PublicKey().Bytes(), mine); err == nil {
			t.Fatal("a task tunnel completed against a files door")
		}
		if store.reads.Load() != 0 {
			t.Fatal("the store was read")
		}
	})
	t.Run("a files client against a door that speaks the task handshake", func(t *testing.T) {
		w := newWorld(t)
		// A stand-in registered under the files door's own name that answers
		// with the task door's tunnel prologue, which is all a task door adds
		// at this layer.
		key, _ := DoorKey(w.id, w.a.ID())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		registration, err := relay.Register(ctx, w.service, key)
		if err != nil {
			t.Fatal(err)
		}
		defer registration.Close()
		var mu sync.Mutex
		var completed []bool
		go func() {
			for {
				stream, err := registration.Accept()
				if err != nil {
					return
				}
				go func() {
					defer stream.Close()
					_, err := pair.AcceptWithKey(stream, key)
					mu.Lock()
					completed = append(completed, err == nil)
					mu.Unlock()
				}()
			}
		}()
		source := NewSource(SourceConfig{
			Service: w.service, Identity: w.id, Device: w.b, TargetID: w.a.ID(), TargetName: "selfhost-pc",
			RetryWithin: 2 * time.Second,
			Sleep:       func(context.Context, time.Duration) error { return nil },
		})
		defer source.Close()
		_, err = source.Has(context.Background(), []string{rid(1)})
		if !errors.Is(err, ErrSourceOff) {
			t.Fatalf("Has = %v, want a source that is off", err)
		}
		mu.Lock()
		defer mu.Unlock()
		for _, ok := range completed {
			if ok {
				t.Fatal("a files tunnel completed against a task handshake")
			}
		}
	})
	t.Run("a task door's own name is a different name", func(t *testing.T) {
		w := newWorld(t)
		files, _ := DoorName(w.id, w.a.ID())
		task, _ := taskdoor.DoorName(w.id, w.a.ID())
		if files == task {
			t.Fatal("both doors register under one name")
		}
	})
}

// C4: only your own computers. Whoever opens the tunnel, no object byte is
// read or sent before the hello has shown a device of this identity that the
// roster still allows.
func TestFilesDoorAnswersNobodyBeforeTheHelloIsGood(t *testing.T) {
	other, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := identity.NewDevice(other)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		build func(w *world, tunnel *pair.Tunnel) []byte // the first message's body
		typ   byte
		key   func(w *world) (id identity.Identity, device string)
		// before changes the world after the door is serving, as a person
		// removing a computer from their devices would.
		before func(w *world)
		want   string
	}{
		{
			name: "a computer removed from the devices",
			key:  func(w *world) (identity.Identity, string) { return w.id, "" },
			build: func(w *world, tunnel *pair.Tunnel) []byte {
				return jsonOf(t, helloFor(w.b, tunnel))
			},
			before: func(w *world) { w.allow(w.b.ID(), false) },
			typ:    typeHello, want: RemovedLine,
		},
		{
			name: "a computer the roster never heard of",
			key:  func(w *world) (identity.Identity, string) { return w.id, "" },
			build: func(w *world, tunnel *pair.Tunnel) []byte {
				return jsonOf(t, helloFor(w.b, tunnel))
			},
			before: func(w *world) { w.remove(w.b.ID()) },
			typ:    typeHello, want: RemovedLine,
		},
		{
			name: "a device of another identity",
			key:  func(*world) (identity.Identity, string) { return other, stranger.ID() },
			build: func(_ *world, tunnel *pair.Tunnel) []byte {
				return jsonOf(t, hello{V: Version, Cert: stranger.Cert, Sig: stranger.Sign(tunnel.ChannelBinding())})
			},
			typ: typeHello, want: couldNotConfirm,
		},
		{
			name: "a good certificate on another device's key",
			key:  func(w *world) (identity.Identity, string) { return w.id, "dev_somebody_else" },
			build: func(w *world, tunnel *pair.Tunnel) []byte {
				return jsonOf(t, hello{V: Version, Cert: w.b.Cert, Sig: w.b.Sign(tunnel.ChannelBinding())})
			},
			typ: typeHello, want: couldNotConfirm,
		},
		{
			name: "a signature made for another tunnel",
			key:  func(w *world) (identity.Identity, string) { return w.id, "" },
			build: func(w *world, tunnel *pair.Tunnel) []byte {
				return jsonOf(t, hello{V: Version, Cert: w.b.Cert, Sig: w.b.Sign(bytes.Repeat([]byte{1}, 32))})
			},
			typ: typeHello, want: couldNotConfirm,
		},
		{
			name: "a request instead of a hello",
			key:  func(w *world) (identity.Identity, string) { return w.id, "" },
			build: func(w *world, tunnel *pair.Tunnel) []byte {
				body, _ := ridsBody([]string{rid(1)})
				return body
			},
			typ: typeGetMany, want: couldNotConfirm,
		},
		{
			name: "another wire version",
			key:  func(w *world) (identity.Identity, string) { return w.id, "" },
			build: func(w *world, tunnel *pair.Tunnel) []byte {
				return jsonOf(t, hello{V: Version + 1, Cert: w.b.Cert, Sig: w.b.Sign(tunnel.ChannelBinding())})
			},
			typ: typeHello, want: differentVersion,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			disk := newDisk(t)
			frame, _ := w.frameOf(1, 100, 200)
			frameID := putFrame(t, disk, frame)
			store := wrap(t, disk)
			w.serve(w.config(store))
			if c.before != nil {
				c.before(w)
			}

			id, device := c.key(w)
			// The static key a dialer uses: its own device's door key unless the
			// case is about using another device's.
			if device == "" {
				device = w.b.ID()
			}
			key, err := DoorKey(id, device)
			if err != nil {
				t.Fatal(err)
			}
			tunnel := w.rawTunnel(key)
			sendRaw(t, tunnel, c.typ, c.build(w, tunnel))
			got := receive(t, tunnel, 5*time.Second)
			if got.err != nil || got.typ != typeRefused {
				t.Fatalf("the first answer was type %d, %v; want a refusal", got.typ, got.err)
			}
			var no refusal
			if err := json.Unmarshal(got.body, &no); err != nil || no.Reason != c.want {
				t.Fatalf("refusal = %q, want %q", no.Reason, c.want)
			}
			// Ask for objects anyway. The door has closed, so nothing comes back.
			body, _ := ridsBody([]string{rid(1)})
			_, _ = writeMessage(tunnel, typeGetMany, body)
			frameBody, _ := frameRequest(frameID, 0, 0)
			_, _ = writeMessage(tunnel, typeGetFrame, frameBody)
			if next := receive(t, tunnel, 5*time.Second); next.err == nil {
				t.Fatalf("the door answered type %d after refusing", next.typ)
			}
			if n := store.reads.Load(); n != 0 {
				t.Fatalf("the store was read %d times for a device that was not let in", n)
			}
		})
	}
}

// C4 at the client: a Source of another identity cannot even find the door
// (the door's name comes from the identity's cell key), and the store sees nothing.
func TestFilesDoorAnotherIdentitysSourceFindsNothing(t *testing.T) {
	w := newWorld(t)
	store := wrap(t, newDisk(t))
	w.serve(w.config(store))
	other, _ := identity.Mint()
	stranger, _ := identity.NewDevice(other)
	source := NewSource(SourceConfig{
		Service: w.service, Identity: other, Device: stranger, TargetID: w.a.ID(), TargetName: "selfhost-pc",
		RetryWithin: time.Second, Sleep: func(context.Context, time.Duration) error { return nil },
	})
	defer source.Close()
	if _, err := source.Has(context.Background(), []string{rid(1)}); !errors.Is(err, ErrSourceOff) {
		t.Fatalf("Has = %v, want a source that is off", err)
	}
	if store.reads.Load() != 0 {
		t.Fatal("the store was read")
	}
}

// C4: a device of this identity that the roster does not allow, or that was
// revoked, is refused at the hello with a sentence, is not retried, and the
// store is never read for it.
func TestFilesDoorRefusesADeviceTheRosterDoesNotAllow(t *testing.T) {
	for _, how := range []string{"removed", "absent"} {
		t.Run(how, func(t *testing.T) {
			w := newWorld(t)
			disk := newDisk(t)
			frame, _ := w.frameOf(1, 100)
			putFrame(t, disk, frame)
			store := wrap(t, disk)
			w.serve(w.config(store))
			if how == "removed" {
				w.allow(w.b.ID(), false)
			} else {
				w.remove(w.b.ID())
			}
			source := w.source()
			_, err := source.Has(context.Background(), []string{rid(1)})
			var no *RefusedError
			if !errors.As(err, &no) {
				t.Fatalf("Has = %v, want a refusal", err)
			}
			if no.Reason != RemovedLine {
				t.Fatalf("reason = %q", no.Reason)
			}
			if got := source.Stats().Dials; got != 1 {
				t.Fatalf("a refusal was retried: %d dials", got)
			}
			if store.reads.Load() != 0 {
				t.Fatal("the store was read for a device the roster does not allow")
			}
		})
	}
}

// A roster that cannot be read refuses rather than guessing.
func TestFilesDoorRefusesWhenTheRosterCannotBeRead(t *testing.T) {
	w := newWorld(t)
	store := wrap(t, newDisk(t))
	cfg := w.config(store)
	cfg.Roster = func(context.Context) (map[string]bool, error) { return nil, errors.New("directory unreachable") }
	w.serve(cfg)
	_, err := w.source().Has(context.Background(), []string{rid(1)})
	var no *RefusedError
	if !errors.As(err, &no) || no.Reason != couldNotCheck {
		t.Fatalf("Has = %v, want %q", err, couldNotCheck)
	}
	if store.reads.Load() != 0 {
		t.Fatal("the store was read")
	}
}

// The sentences a person can be shown say what happened in plain words: they
// are not empty, and they do not name the plumbing between the computers.
func TestFilesDoorSentencesDoNotNameThePlumbing(t *testing.T) {
	for _, line := range []string{RemovedLine, couldNotConfirm, couldNotCheck, differentVersion, LostLine, BackLine, (&OffError{Name: "selfhost-pc"}).Error()} {
		if line == "" || strings.Contains(strings.ToLower(line), "relay") {
			t.Errorf("%q", line)
		}
	}
}

// WIRE: a length prefix over the limit is refused after reading the four bytes
// of the prefix, without allocating anything near what it claims.
func TestFilesDoorRefusesALengthOverTheLimitWithoutAllocatingIt(t *testing.T) {
	huge := []byte{0xff, 0xff, 0xff, 0xff, 'h', 'i'}
	reader := &countingReader{data: huge}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, _, _, err := readMessage(reader, MaxRequestBytes)
	runtime.ReadMemStats(&after)
	if !errors.Is(err, errProtocol) {
		t.Fatalf("err = %v, want a protocol error", err)
	}
	if reader.read != 4 {
		t.Fatalf("read %d bytes before refusing, want the 4 of the prefix", reader.read)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Fatalf("allocated %d bytes to refuse a length", grew)
	}
	// The limit is a limit on the declared size: one over is refused, exactly
	// at it is read.
	at := append([]byte{0, 0, 0, 3}, 'a', 'b', 'c')
	if _, _, _, err := readMessage(bytes.NewReader(at), 3); err != nil {
		t.Fatalf("a message at the limit: %v", err)
	}
	if _, _, _, err := readMessage(bytes.NewReader(at), 2); !errors.Is(err, errProtocol) {
		t.Fatalf("a message over the limit: %v", err)
	}
	if _, _, _, err := readMessage(bytes.NewReader([]byte{0, 0, 0, 0}), 10); !errors.Is(err, errProtocol) {
		t.Fatalf("an empty message: %v", err)
	}
}

type countingReader struct {
	data []byte
	read int
}

func (c *countingReader) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, errors.New("read past the end")
	}
	n := copy(p, c.data)
	c.data = c.data[n:]
	c.read += n
	return n, nil
}

// WIRE: at the door, a request that claims more than the request limit ends
// the connection; the door keeps serving others.
func TestFilesDoorClosesOnAnOversizedRequest(t *testing.T) {
	w := newWorld(t)
	store := wrap(t, newDisk(t))
	w.serve(w.config(store))
	key, _ := DoorKey(w.id, w.b.ID())
	tunnel := w.rawTunnel(key)
	sendRaw(t, tunnel, typeHello, jsonOf(t, helloFor(w.b, tunnel)))
	if got := receive(t, tunnel, 5*time.Second); got.err != nil || got.typ != typeHello {
		t.Fatalf("hello answer = type %d, %v", got.typ, got.err)
	}
	// A prefix that claims 100 MiB, then nothing.
	if _, err := tunnel.Write([]byte{0x06, 0x40, 0x00, 0x00, typeGetMany}); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, tunnel, 5*time.Second); got.err == nil {
		t.Fatalf("the door answered type %d to an oversized request", got.typ)
	}
	// A fresh client is still served.
	if _, err := w.source().Has(context.Background(), []string{rid(1)}); err != nil {
		t.Fatalf("the door stopped answering after one bad client: %v", err)
	}
}

// WIRE: a request type the door does not know is answered and ends the
// connection; the store is not touched.
func TestFilesDoorEndsAConnectionThatSendsAnUnknownRequest(t *testing.T) {
	w := newWorld(t)
	store := wrap(t, newDisk(t))
	w.serve(w.config(store))
	key, _ := DoorKey(w.id, w.b.ID())
	tunnel := w.rawTunnel(key)
	sendRaw(t, tunnel, typeHello, jsonOf(t, helloFor(w.b, tunnel)))
	receive(t, tunnel, 5*time.Second)
	sendRaw(t, tunnel, 77, []byte{1, 2, 3})
	if got := receive(t, tunnel, 5*time.Second); got.err != nil || got.typ != typeError {
		t.Fatalf("answer = type %d, %v", got.typ, got.err)
	}
	if got := receive(t, tunnel, 5*time.Second); got.err == nil {
		t.Fatal("the connection stayed open")
	}
	if store.reads.Load() != 0 {
		t.Fatal("the store was read")
	}
}

// WIRE: a connection that goes quiet is closed, both before the hello and
// after it, so a vanished peer cannot hold a stream for ever.
func TestFilesDoorClosesConnectionsThatGoQuiet(t *testing.T) {
	t.Run("before the hello", func(t *testing.T) {
		old := handshakeWithin
		handshakeWithin = 300 * time.Millisecond
		defer func() { handshakeWithin = old }()
		w := newWorld(t)
		w.serve(w.config(wrap(t, newDisk(t))))
		key, _ := DoorKey(w.id, w.b.ID())
		tunnel := w.rawTunnel(key)
		start := time.Now()
		if got := receive(t, tunnel, 5*time.Second); got.err == nil {
			t.Fatalf("an answer arrived: type %d", got.typ)
		}
		if time.Since(start) > 3*time.Second {
			t.Fatal("the quiet connection was held far past its bound")
		}
	})
	t.Run("between requests", func(t *testing.T) {
		old := idleAfter
		idleAfter = 300 * time.Millisecond
		defer func() { idleAfter = old }()
		w := newWorld(t)
		w.serve(w.config(wrap(t, newDisk(t))))
		key, _ := DoorKey(w.id, w.b.ID())
		tunnel := w.rawTunnel(key)
		sendRaw(t, tunnel, typeHello, jsonOf(t, helloFor(w.b, tunnel)))
		receive(t, tunnel, 5*time.Second)
		start := time.Now()
		if got := receive(t, tunnel, 5*time.Second); got.err == nil {
			t.Fatalf("an answer arrived: type %d", got.typ)
		}
		if time.Since(start) > 3*time.Second {
			t.Fatal("the idle connection was held far past its bound")
		}
	})
}

// Serve refuses a configuration that can never work, and says so at once.
func TestFilesDoorServeRejectsABadConfig(t *testing.T) {
	w := newWorld(t)
	good := w.config(wrap(t, newDisk(t)))
	other, _ := identity.Mint()
	otherDevice, _ := identity.NewDevice(other)
	cases := map[string]func(*ServeConfig){
		"no service":     func(c *ServeConfig) { c.Service = "" },
		"not a url":      func(c *ServeConfig) { c.Service = "ftp://relay.example.com" },
		"no roster":      func(c *ServeConfig) { c.Roster = nil },
		"no store":       func(c *ServeConfig) { c.Store = nil },
		"no lock":        func(c *ServeConfig) { c.LockPath = "" },
		"a foreign cert": func(c *ServeConfig) { c.Device = otherDevice },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := good
			change(&cfg)
			done := make(chan error, 1)
			go func() { done <- Serve(context.Background(), cfg) }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("Serve accepted the config")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Serve did not return for a bad config")
			}
		})
	}
	// The lock file lives in a directory only its owner can enter.
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	cfg := good
	cfg.Ready = func() { cancel() }
	go func() { served <- Serve(ctx, cfg) }()
	if err := <-served; err != nil {
		t.Fatalf("Serve = %v", err)
	}
	info, err := os.Stat(filepath.Dir(good.LockPath))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("the lock's directory is %v", mode)
	}
}

// The store is only ever read: the counting wrapper fails the test on a write,
// and this makes sure it would notice by proving the door has no way to write.
func TestFilesDoorNeverWrites(t *testing.T) {
	w := newWorld(t)
	disk := newDisk(t)
	frame, objects := w.frameOf(1, 1000, 2000)
	id := putFrame(t, disk, frame)
	store := wrap(t, disk)
	w.serve(w.config(store))
	source := w.source()
	ctx := context.Background()
	if _, err := source.GetFrame(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := source.GetMany(ctx, []string{objects[0].RID, objects[1].RID}); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Has(ctx, []string{objects[0].RID}); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Locate(ctx, []string{objects[0].RID}); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Get(ctx, objects[1].RID); err != nil {
		t.Fatal(err)
	}
	var _ blobstore.Store = store
}
