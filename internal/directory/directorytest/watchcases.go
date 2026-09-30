package directorytest

import (
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// watchCases are the named rows of section 21.9 of the contract.
var watchCases = map[string]func(watchEnv){
	"FirstFrameIsVersion":         firstFrameIsVersion,
	"TwoDevicesBothHear":          twoDevicesBothHear,
	"EveryVisibleChangeBumpsOnce": everyVisibleChangeBumpsOnce,
	"SilentHeartbeat":             silentHeartbeat,
	"BeatWithNewPendingBumps":     beatWithNewPendingBumps,
	"IdenticalDevicePutIsSilent":  identicalDevicePutIsSilent,
	"ListCarriesVersion":          listCarriesVersion,
	"RevokeClosesOnlyThatDevice":  revokeClosesOnlyThatDevice,
	"RevokedDeviceRefused":        revokedDeviceRefused,
	"UnsignedRefused":             unsignedRefused,
	"PlainGetIs426":               plainGetIs426,
	"FreezeClosesAll4410":         freezeClosesAll4410,
	"FrozenRefusesUpgrade":        frozenRefusesUpgrade,
	"OnlyVersionsOnTheWire":       onlyVersionsOnTheWire,
	"PingAnswersPong":             pingAnswersPong,
	"ClientFramesAreIgnored":      clientFramesAreIgnored,
	"CapRefusesBeyond":            capRefusesBeyond,
	"CapIsOneThousand":            capIsOneThousand,
}

// create makes the case's cell as device A, which bumps the version once.
func (w watchEnv) create() {
	w.t.Helper()
	_, err := w.dirAs(devA).Create(ctx, cell, directory.CellInit{Head: head, Class: "chat", Size: 10, Title: "t"})
	must(w.t, err)
}

// twoDevices records both devices, which the revoke cases need.
func (w watchEnv) twoDevices() {
	w.t.Helper()
	twoDevices(w.t, w.env)
}

func firstFrameIsVersion(w watchEnv) {
	before := w.open(devA)
	n := before.version()
	w.create()
	after := w.open(devA)
	after.wantVersion(n + 1)
	before.wantVersion(n + 1)
}

func twoDevicesBothHear(w watchEnv) {
	a, b := w.open(devA), w.open(devB)
	n := a.version()
	b.wantVersion(n)
	w.create()
	a.wantVersion(n + 1)
	b.wantVersion(n + 1)
}

// A change is one step of everyVisibleChangeBumpsOnce.
type change struct {
	name string
	do   func(w watchEnv) error
}

func (w watchEnv) visibleChanges() []change {
	a := func(w watchEnv) directory.Client { return w.dirAs(devA) }
	return []change{
		{"create", func(w watchEnv) error {
			_, err := a(w).Create(ctx, cell, directory.CellInit{Head: head, Class: "chat", Size: 10, Title: "t"})
			return err
		}},
		{"publish", func(w watchEnv) error {
			_, err := a(w).Publish(ctx, cell, directory.Publish{Fence: 1, OldHead: head, Head: next, Size: 11, Class: "chat"})
			return err
		}},
		{"release", func(w watchEnv) error { return a(w).Release(ctx, cell, 1) }},
		{"acquire", func(w watchEnv) error {
			_, err := a(w).Acquire(ctx, cell, directory.AcquireOpts{})
			return err
		}},
		{"archive", func(w watchEnv) error { return a(w).Archive(ctx, cell) }},
		{"put device A", func(w watchEnv) error {
			return a(w).PutDevice(ctx, w.env.id(devA), directory.Device{V: 1, Name: "one"})
		}},
		{"put device B", func(w watchEnv) error {
			return w.dirAs(devB).PutDevice(ctx, w.env.id(devB), directory.Device{V: 1, Name: "two"})
		}},
		{"revoke B", func(w watchEnv) error { return a(w).Revoke(ctx, w.env.id(devB)) }},
		{"set vault", func(w watchEnv) error { return a(w).SetVault(ctx, "", "rid1") }},
	}
}

func everyVisibleChangeBumpsOnce(w watchEnv) {
	s := w.open(devA)
	n := s.version()
	for _, c := range w.visibleChanges() {
		must(w.t, c.do(w))
		n++
		if got := s.version(); got != n {
			w.t.Fatalf("after %s the version is %d, want %d", c.name, got, n)
		}
	}
	s.wantSilent() // a change that bumped twice would leave a frame behind
}

// quietBeat is a heartbeat that renews the lease and nothing a person sees.
func (w watchEnv) quietBeat() {
	w.t.Helper()
	w.env.clock.Wait(time.Second)
	_, err := w.dirAs(devA).Heartbeat(ctx, cell, directory.Beat{Fence: 1})
	must(w.t, err)
}

// visible is a list with the parts that move on their own removed: the read's
// time and the lease's expiry.
func visible(l directory.Listing) directory.Listing {
	l.Now = 0
	cells := map[string]directory.Cell{}
	for id, c := range l.Cells {
		c.Lease.Expires = 0
		cells[id] = c
	}
	l.Cells = cells
	return l
}

func silentHeartbeat(w watchEnv) {
	w.create()
	a, b := w.open(devA), w.open(devB)
	n := a.version()
	b.wantVersion(n)
	listBefore, err := w.dirAs(devA).List(ctx)
	must(w.t, err)

	w.quietBeat()

	a.wantSilent()
	b.wantSilent()
	listAfter, err := w.dirAs(devA).List(ctx)
	must(w.t, err)
	if !reflect.DeepEqual(visible(listBefore), visible(listAfter)) {
		w.t.Fatalf("a quiet heartbeat changed the list:\n%+v\n%+v", listBefore, listAfter)
	}
	if got := w.listVersion(devA); got != n {
		w.t.Fatalf("list version %d after a quiet heartbeat, want %d", got, n)
	}
}

func beatWithNewPendingBumps(w watchEnv) {
	w.create()
	s := w.open(devA)
	n := s.version()
	_, err := w.dirAs(devA).Heartbeat(ctx, cell, directory.Beat{Fence: 1, Pending: 2})
	must(w.t, err)
	s.wantVersion(n + 1)
	s.wantSilent()
}

func identicalDevicePutIsSilent(w watchEnv) {
	put := func() {
		must(w.t, w.dirAs(devA).PutDevice(ctx, w.env.id(devA), directory.Device{V: 1, Name: "one"}))
	}
	s := w.open(devA)
	n := s.version()
	put()
	s.wantVersion(n + 1)
	put()
	s.wantSilent()
}

func listCarriesVersion(w watchEnv) {
	s := w.open(devA)
	s.version()
	for range 2 {
		w.changeSomething()
		n := s.version()
		if got := w.listVersion(devA); got != n {
			w.t.Fatalf("list carries version %d, last frame said %d", got, n)
		}
	}
}

// changeSomething makes one visible change whatever the directory holds: a
// device record that differs from the last one written.
func (w watchEnv) changeSomething() {
	w.t.Helper()
	*w.changes++
	d := directory.Device{V: 1, Name: time.Duration(*w.changes).String()}
	must(w.t, w.dirAs(devA).PutDevice(ctx, w.env.id(devA), d))
}

func revokeClosesOnlyThatDevice(w watchEnv) {
	w.twoDevices()
	a, b := w.open(devA), w.open(devB)
	n := a.version()
	b.wantVersion(n)

	must(w.t, w.dirAs(devA).Revoke(ctx, w.env.id(devB)))

	a.wantVersion(n + 1)
	if heard := b.wantClosed(4401, "revoked"); len(heard) != 1 || heard[0] != n+1 {
		w.t.Fatalf("the revoked socket heard %v before its close, want the bump [%d]", heard, n+1)
	}
	w.create()
	a.wantVersion(n + 2)
}

func revokedDeviceRefused(w watchEnv) {
	w.twoDevices()
	must(w.t, w.dirAs(devA).Revoke(ctx, w.env.id(devB)))
	conn, resp, err := DialWatch(ctx, w.rig.Base, w.rig.Sign(devB))
	wantRefusal(w.t, conn, resp, err, http.StatusUnauthorized, "revoked")
}

func unsignedRefused(w watchEnv) {
	conn, resp, err := DialWatch(ctx, w.rig.Base, unsigned)
	wantRefusal(w.t, conn, resp, err, http.StatusUnauthorized, "unauthorized")
}

func plainGetIs426(w watchEnv) {
	req, err := http.NewRequest(http.MethodGet, w.rig.Base+directory.WatchPath, nil)
	must(w.t, err)
	w.rig.Sign(devA)(req, nil)
	resp, err := http.DefaultClient.Do(req)
	must(w.t, err)
	wantAnswer(w.t, resp, http.StatusUpgradeRequired, "upgrade_required")
}

func freeze(w watchEnv) {
	w.t.Helper()
	_, err := w.dirAs(devA).Rotate(ctx, directory.RotationReq{Op: directory.OpFreeze})
	must(w.t, err)
}

func freezeClosesAll4410(w watchEnv) {
	a, b := w.open(devA), w.open(devB)
	n := a.version()
	b.wantVersion(n)

	freeze(w)

	for name, s := range map[string]*sock{"A": a, "B": b} {
		if heard := s.wantClosed(4410, "rotated"); len(heard) != 1 || heard[0] != n+1 {
			w.t.Fatalf("socket %s heard %v before its close, want the bump [%d]", name, heard, n+1)
		}
	}
}

func frozenRefusesUpgrade(w watchEnv) {
	freeze(w)
	conn, resp, err := DialWatch(ctx, w.rig.Base, w.rig.Sign(devA))
	wantRefusal(w.t, conn, resp, err, http.StatusGone, "rotated")
}

func onlyVersionsOnTheWire(w watchEnv) {
	s := w.open(devA)
	first := s.version()
	keys := map[string]map[string]string{"k1": {w.env.id(devA): "d3JhcHBlZA"}}
	_, err := w.dirAs(devA).Create(ctx, cell, directory.CellInit{Head: head, Class: "chat", Size: 10, Title: "c2VjcmV0LXRpdGxl", Keys: keys})
	must(w.t, err)
	w.changeSomething()
	for s.version() < first+2 { // the create and the device put; version() checks every frame's shape
	}
	s.wantSilent()
}

func pingAnswersPong(w watchEnv) {
	s := w.open(devA)
	n := s.version()
	s.send(websocket.MessageText, "ping")
	if got := s.text(); got != "pong" {
		w.t.Fatalf("answer to ping = %q, want pong", got)
	}
	s.wantSilent()
	w.create()
	s.wantVersion(n + 1)
}

func clientFramesAreIgnored(w watchEnv) {
	s := w.open(devA)
	n := s.version()
	s.send(websocket.MessageText, "hello")
	s.send(websocket.MessageText, `{"v":99}`)
	s.send(websocket.MessageBinary, "\x00\x01")
	s.wantSilent()
	w.create()
	s.wantVersion(n + 1)
}

// dialMany opens count sockets as device A and keeps them open; the sockets
// are never read, which is what a stalled client looks like to the relay.
func (w watchEnv) dialMany(count int) []*websocket.Conn {
	conns := make([]*websocket.Conn, count)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	for i := range conns {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			c, resp, err := DialWatch(ctx, w.rig.Base, w.rig.Sign(devA))
			if err != nil {
				w.t.Errorf("socket %d: %v (%s)", i+1, err, describe(resp))
				return
			}
			conns[i] = c
			w.t.Cleanup(func() { c.CloseNow() })
		}()
	}
	wg.Wait()
	return conns
}

// fillAndRefuse fills an identity's places and checks the next one is refused,
// that a closed socket frees its place, and that another identity is unaffected.
func fillAndRefuse(w watchEnv, limit int) {
	conns := w.dialMany(limit)
	if w.t.Failed() {
		w.t.FailNow()
	}
	refused := func() {
		conn, resp, err := DialWatch(ctx, w.rig.Base, w.rig.Sign(devA))
		wantRefusal(w.t, conn, resp, err, http.StatusTooManyRequests, "too_many_watchers")
	}
	refused()

	other, resp, err := DialWatch(ctx, w.rig.Base, w.rig.Stranger())
	if err != nil {
		w.t.Fatalf("another identity is refused while this one is full: %v (%s)", err, describe(resp))
	}
	defer other.CloseNow()

	conns[0].CloseNow()
	w.awaitPlace()
}

// awaitPlace redials until the relay has noticed a closed socket, which a
// relay does when its reader sees the close, a moment after the client's.
func (w watchEnv) awaitPlace() {
	w.t.Helper()
	deadline := time.Now().Add(frameWait)
	for {
		c, resp, err := DialWatch(ctx, w.rig.Base, w.rig.Sign(devA))
		if err == nil {
			w.t.Cleanup(func() { c.CloseNow() })
			return
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("a closed socket never freed its place: %v (%s)", err, describe(resp))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func capRefusesBeyond(w watchEnv) {
	if w.rig.Cap == 0 {
		w.t.Skip("the relay keeps the default cap; CapIsOneThousand covers it")
	}
	fillAndRefuse(w, w.rig.Cap)
}

func capIsOneThousand(w watchEnv) {
	if w.rig.Cap != 0 {
		w.t.Skip("the relay was built with a lowered cap; CapRefusesBeyond covers it")
	}
	fillAndRefuse(w, directory.MaxWatchers)
}
