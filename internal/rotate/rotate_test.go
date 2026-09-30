package rotate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
)

func mustRotate(t *testing.T, e Env) Result {
	t.Helper()
	res, err := e.Rotate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func listing(t *testing.T, n *namespace, dev string) directory.Listing {
	t.Helper()
	l, err := n.dir.For(dev).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// A finished rotation leaves this machine as the new identity, every chat and
// the vault on the new relay under the new keys, and the old identity retired.
func TestRotateMovesEverything(t *testing.T) {
	r := newRig(t)
	r.putVault()
	res := mustRotate(t, r.env)

	now, err := identity.Load(r.home)
	if err != nil {
		t.Fatal(err)
	}
	if now.ID() == r.old.ID() || now.ID() != res.NewID {
		t.Fatalf("identity on disk %s, old %s, result %+v", now.ID(), r.old.ID(), res)
	}
	dev, err := identity.Device(r.home)
	if err != nil || dev.Cert.Verify(now.PublicKey()) != nil {
		t.Fatalf("device does not verify under the new identity: %v", err)
	}
	if Pending(r.home) {
		t.Fatal("the journal outlived a finished rotation")
	}
	next := r.relay.of(now.PublicKey())
	l := listing(t, next, dev.ID())
	if len(l.Cells) != 2 || l.Identity.Vault == "" {
		t.Fatalf("new relay lists %d chats, vault %q", len(l.Cells), l.Identity.Vault)
	}
	if old := r.relay.of(r.old.PublicKey()); old.state != "retired" || old.grace != DefaultGrace {
		t.Fatalf("old namespace is %s with grace %v", old.state, old.grace)
	}
	v, err := keys.Open(r.home)
	if err != nil {
		t.Fatalf("the vault does not open under the new identity: %v", err)
	}
	if e, err := v.Get("p/KEY"); err != nil || e.Value != "secret" {
		t.Fatalf("secret after rotation = %+v, %v", e, err)
	}
}

// Titles, the vault and the device name move under the new metadata key, and the
// old key opens none of it.
func TestNothingNewOpensUnderOldKeys(t *testing.T) {
	r := newRig(t)
	r.putVault()
	mustRotate(t, r.env)
	now, _ := identity.Load(r.home)
	dev, _ := identity.Device(r.home)
	l := listing(t, r.relay.of(now.PublicKey()), dev.ID())

	title := l.Cells[chatA].Title
	if got, err := directory.OpenName(directory.MetadataKey(now.CellKey()), title); err != nil || got != "hello" {
		t.Fatalf("title under the new key = %q, %v", got, err)
	}
	if _, err := directory.OpenName(directory.MetadataKey(r.old.CellKey()), title); err == nil {
		t.Fatal("the old key opens a title on the new identity")
	}
	if _, err := directory.OpenName(directory.MetadataKey(r.old.CellKey()), l.Devices[dev.ID()].Name); err == nil {
		t.Fatal("the old key opens the device name on the new identity")
	}
}

// Branch links, archived chats and sizes are carried with their chat.
func TestBranchesSurvive(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	old := r.relay.of(r.old.PublicKey())
	c := r.dev.ID()
	if err := old.dir.For(c).Archive(ctx, chatB); err != nil {
		t.Fatal(err)
	}
	mustRotate(t, r.env)
	now, _ := identity.Load(r.home)
	dev, _ := identity.Device(r.home)
	l := listing(t, r.relay.of(now.PublicKey()), dev.ID())
	if !l.Cells[chatB].Archived || l.Cells[chatA].Archived {
		t.Fatalf("archived flags: A %v, B %v", l.Cells[chatA].Archived, l.Cells[chatB].Archived)
	}
	if l.Cells[chatA].Size != 1<<20 || l.Cells[chatA].Head == "" {
		t.Fatalf("chat A came over as %+v", l.Cells[chatA])
	}
	if l.Cells[chatA].Lease.Expires > l.Now {
		t.Fatal("the keeper still holds a chat it carried over")
	}
}

// The new relay is empty and the old one still takes writes until the freeze, and
// read-only from the freeze on: no moment has two writable roots.
func TestOldIsReadOnlyFromTheFreeze(t *testing.T) {
	r := newRig(t)
	old, ctx := r.relay.of(r.old.PublicKey()), context.Background()
	steps := map[string]bool{}
	r.env.After = func(step string) error {
		steps[step] = true
		j, _, _ := load(r.home)
		next := r.relay.of(mustIdentity(t, j).PublicKey())
		switch {
		case step == string(Minted):
			if !old.writable() || len(listing(t, next, r.dev.ID()).Cells) != 0 {
				t.Errorf("minted: old writable %v, new chats %d", old.writable(), len(listing(t, next, r.dev.ID()).Cells))
			}
		case steps[string(Frozen)]:
			if old.writable() {
				t.Errorf("after %s the old identity still takes writes", step)
			}
			err := guardedDir{old.dir.For(r.dev.ID()), old}.Archive(ctx, chatA)
			if !errors.Is(err, ErrRotated) {
				t.Errorf("after %s a device writing to the old identity got %v", step, err)
			}
		}
		return nil
	}
	mustRotate(t, r.env)
	if !steps[string(Frozen)] || !steps[string(Verified)] {
		t.Fatalf("hook saw %v", steps)
	}
}

func mustIdentity(t *testing.T, j Journal) identity.Identity {
	t.Helper()
	id, err := identity.Unmarshal(j.New)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Every crash point is finished by running the command again, with the same new
// root every time.
func TestResumeFromEveryCrashPoint(t *testing.T) {
	for _, step := range []string{"minted", "frozen", "item:" + chatA, "item:" + chatB, "sealed", "verified", "staged", "installed", "switched"} {
		t.Run(step, func(t *testing.T) {
			r := newRig(t)
			r.putVault()
			r.env.After = crashAt(step)
			if _, err := r.env.Rotate(context.Background()); !errors.Is(err, errCrash) {
				t.Fatalf("first run = %v, want a crash at %s", err, step)
			}
			j, found, err := load(r.home)
			if err != nil || !found {
				t.Fatalf("no journal after a crash at %s: %v", step, err)
			}
			assertVaultOpens(t, r) // whichever identity is on disk, the vault on disk opens under it
			res := mustRotate(t, r.reload())
			if res.NewID != j.NewID {
				t.Fatalf("resume minted a second root: %s then %s", j.NewID, res.NewID)
			}
			assertRotated(t, r, res)
		})
	}
}

func assertVaultOpens(t *testing.T, r *rig) {
	t.Helper()
	v, err := keys.Open(r.home)
	if err != nil {
		t.Fatal(err)
	}
	if e, err := v.Get("p/KEY"); err != nil || e.Value != "secret" {
		t.Fatalf("the vault does not open under the identity on disk: %+v, %v", e, err)
	}
}

func assertRotated(t *testing.T, r *rig, res Result) {
	t.Helper()
	now, err := identity.Load(r.home)
	if err != nil || now.ID() != res.NewID {
		t.Fatalf("identity on disk = %v, %v; want %s", now.ID(), err, res.NewID)
	}
	if Pending(r.home) {
		t.Fatal("journal left behind")
	}
	if _, err := keys.Open(r.home); err != nil {
		t.Fatalf("vault: %v", err)
	}
	v, _ := keys.Open(r.home)
	if e, err := v.Get("p/KEY"); err != nil || e.Value != "secret" {
		t.Fatalf("secret = %+v, %v", e, err)
	}
	dev, _ := identity.Device(r.home)
	if l := listing(t, r.relay.of(now.PublicKey()), dev.ID()); len(l.Cells) != 2 {
		t.Fatalf("new relay has %d chats", len(l.Cells))
	}
	if r.relay.of(r.old.PublicKey()).state != "retired" {
		t.Fatal("old identity not retired")
	}
	if got := identity.Predecessors(r.home); len(got) != 1 || got[0] != r.old.ID() {
		t.Fatalf("predecessors = %v, want [%s]", got, r.old.ID())
	}
}

// A chat the keeper does not hold is fetched first and carried like the rest.
func TestMissingChatsAreFetchedFirst(t *testing.T) {
	r := newRig(t)
	// Chat C exists on the relay only: another device's engine sealed it.
	other := cellsyncEngine(t)
	c := r.cell("01J0000000000000000000000C")
	head := other.Seal(c, map[string]string{"c.txt": "from elsewhere"})
	old := r.relay.of(r.old.PublicKey())
	publishFrom(t, other, old, c, head)
	if _, err := old.dir.For("dev_other").Create(context.Background(), c.ID, directory.CellInit{Head: head, Class: "host-bound"}); err != nil {
		t.Fatal(err)
	}
	plan, err := r.env.Plan(context.Background())
	if err != nil || plan.Chats != 3 || plan.Missing != 1 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	mustRotate(t, r.env)
	now, _ := identity.Load(r.home)
	dev, _ := identity.Device(r.home)
	if l := listing(t, r.relay.of(now.PublicKey()), dev.ID()); l.Cells[c.ID].Head != head {
		t.Fatalf("chat C on the new relay = %+v", l.Cells[c.ID])
	}
}

// A fetch that fails stops the rotation before anything changes: the old
// identity is still writable and there is no journal.
func TestFetchFailureBeforeFreeze(t *testing.T) {
	r := newRig(t)
	other := cellsyncEngine(t)
	c := r.cell("01J0000000000000000000000C")
	head := other.Seal(c, map[string]string{"c.txt": "x"})
	old := r.relay.of(r.old.PublicKey())
	publishFrom(t, other, old, c, head)
	if _, err := old.dir.For("dev_other").Create(context.Background(), c.ID, directory.CellInit{Head: head, Class: "host-bound"}); err != nil {
		t.Fatal(err)
	}
	old.getFail = errors.New("store down")
	if _, err := r.env.Rotate(context.Background()); err == nil {
		t.Fatal("rotation went on with a chat it could not fetch")
	}
	if !old.writable() || Pending(r.home) {
		t.Fatalf("writable %v, journal %v", old.writable(), Pending(r.home))
	}
}

// Abandoning before the switch thaws the old identity and deletes the journal.
func TestAbandonBeforeSwitch(t *testing.T) {
	r := newRig(t)
	r.env.After = crashAt("sealed")
	_, _ = r.env.Rotate(context.Background())
	old := r.relay.of(r.old.PublicKey())
	if old.writable() {
		t.Fatal("not frozen at the crash point")
	}
	if err := r.reload().Abandon(context.Background()); err != nil {
		t.Fatal(err)
	}
	cur, _ := identity.Load(r.home)
	if !old.writable() || Pending(r.home) || cur.ID() != r.old.ID() {
		t.Fatalf("writable %v, journal %v, identity %s", old.writable(), Pending(r.home), cur.ID())
	}
}

// After the switch the only way on is forward.
func TestAbandonAfterSwitchRefused(t *testing.T) {
	r := newRig(t)
	r.env.After = crashAt("switched")
	_, _ = r.env.Rotate(context.Background())
	if err := r.reload().Abandon(context.Background()); !errors.Is(err, ErrSwitched) {
		t.Fatalf("abandon after the switch = %v", err)
	}
	if !Pending(r.home) {
		t.Fatal("the journal was deleted")
	}
}

// With the keeper's disk gone there is no journal; another computer of the old
// identity thaws it.
func TestThawFromOtherDevice(t *testing.T) {
	r := newRig(t)
	r.env.After = crashAt("sealed")
	_, _ = r.env.Rotate(context.Background())
	old := r.relay.of(r.old.PublicKey())
	otherHome := t.TempDir()
	dev2, err := identity.NewDevice(r.old)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.Install(otherHome, r.old, dev2); err != nil {
		t.Fatal(err)
	}
	e := r.newEnv()
	e.Home, e.Device = otherHome, dev2
	if err := e.Abandon(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !old.writable() {
		t.Fatal("still frozen")
	}
}

// Whoever rotates first wins: a device that finds the old identity already
// retired is told so and keeps nothing half-made.
func TestSomeoneRotatedFirst(t *testing.T) {
	r := newRig(t)
	if err := r.relay.of(r.old.PublicKey()).gate().Retire(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	_, err := r.env.Rotate(context.Background())
	if !errors.Is(err, ErrSomeoneFirst) {
		t.Fatalf("rotation = %v, want ErrSomeoneFirst", err)
	}
	if Pending(r.home) {
		t.Fatal("a journal of unused keys was left behind")
	}
}

// The new relay missing the newest turn of a chat stops the switch.
func TestVerifyRefusesMissingObject(t *testing.T) {
	r := newRig(t)
	r.env.After = crashAt("sealed")
	_, _ = r.env.Rotate(context.Background())
	j, _, _ := load(r.home)
	next := r.relay.of(mustIdentity(t, j).PublicKey())
	next.store = blobstore.NewMemory() // the relay lost its objects
	if _, err := r.reload().Rotate(context.Background()); err == nil {
		t.Fatal("verify passed with no objects on the new relay")
	}
	if cur, _ := identity.Load(r.home); cur.ID() != r.old.ID() {
		t.Fatal("switched without the objects")
	}
}

// What a rotation would do is told before it does anything.
func TestPlanCounts(t *testing.T) {
	r := newRig(t)
	plan, err := r.env.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Chats != 2 || plan.Missing != 0 || plan.UpBytes != 2<<20 || plan.Wait == 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if !r.relay.of(r.old.PublicKey()).writable() || Pending(r.home) {
		t.Fatal("planning changed something")
	}
}
