package relayserve

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// nativeProvision is durable before freeze so retrying an interrupted import
// reuses its source UUID and protocol keys. No content or device private key is
// present: these keys authorize only their provisioned service roles.
type nativeProvision struct {
	Cluster string    `json:"cluster"`
	Effects []byte    `json:"effects"`
	Anchors [3][]byte `json:"anchors"`
}
type nativeNamespace struct {
	timer memberTimer
	*raftdirectory.V1Directory
	host     *raftdirectory.Native
	feed     *directory.Feed
	effects  raftdirectory.Effects
	links    *directory.Links
	now      func() time.Time
	grace    directory.GraceBounds
	identity clusterstate.IdentityRec
}

func (d *nativeNamespace) Feed() *directory.Feed { return d.feed }
func (d *nativeNamespace) Close() error          { d.stopMembers(); d.feed.Close(); return d.host.Close() }

// activate runs outside every legacy admission lock. The frozen source drains
// admitted writes itself and can never become a fallback directory afterward.
func (n *namespaces) activate(ctx context.Context, id string, root []byte) (*nativeNamespace, error) {
	if err := checkedID(id); err != nil {
		return nil, err
	}
	if n.gone(id) {
		return nil, wireauth.ErrGone
	}
	n.activation.Lock()
	defer n.activation.Unlock()
	n.mu.Lock()
	d := n.groups[id]
	n.mu.Unlock()
	if d != nil {
		return d, nil
	}
	source, err := n.sqlite(id)
	if err != nil {
		return nil, err
	}
	gate, err := source.LegacyGate(ctx)
	if err != nil {
		return nil, err
	}
	if gate != nil {
		root = gate.Config.SigningKey
	}
	if clusterstate.IdentityID(root) != id {
		return nil, wireauth.ErrUnauthorized
	}
	path := filepath.Join(n.root, "raft", id)
	if err = os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	provisionPath := filepath.Join(path, "provision.json")
	raw, err := os.ReadFile(provisionPath)
	var provision nativeProvision
	if errors.Is(err, os.ErrNotExist) {
		if gate != nil {
			return nil, clusterstate.ErrMigration
		}
		var uuid [16]byte
		if _, err = rand.Read(uuid[:]); err != nil {
			return nil, err
		}
		provision.Cluster = hex.EncodeToString(uuid[:])
		_, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return nil, e
		}
		provision.Effects = key
		for i := range provision.Anchors {
			_, key, e := ed25519.GenerateKey(rand.Reader)
			if e != nil {
				return nil, e
			}
			provision.Anchors[i] = key
		}
		raw, err = json.Marshal(provision)
		if err != nil {
			return nil, err
		}
		// The final filename is published only after its complete contents survive
		// power loss. A killed temporary file is never authoritative on restart.
		f, e := os.CreateTemp(path, ".provision-")
		if e != nil {
			return nil, e
		}
		tmp := f.Name()
		defer os.Remove(tmp)
		_, e = f.Write(raw)
		e = errors.Join(e, f.Sync(), f.Close())
		if e != nil {
			return nil, e
		}
		if e = os.Rename(tmp, provisionPath); e != nil {
			return nil, e
		}
		// Newly created ancestors must survive before the source UUID freezes.
		for _, dir := range []string{path, filepath.Dir(path), n.root, filepath.Dir(n.root)} {
			parent, e := os.Open(dir)
			if e != nil {
				return nil, e
			}
			e = errors.Join(parent.Sync(), parent.Close())
			if e != nil {
				return nil, e
			}
		}
	} else if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &provision) != nil || len(provision.Effects) != ed25519.PrivateKeySize || provision.Cluster == "" {
		return nil, clusterstate.ErrMigration
	}
	for _, key := range append(provision.Anchors[:], provision.Effects) {
		if len(key) != ed25519.PrivateKeySize || !bytes.Equal(ed25519.NewKeyFromSeed(key[:ed25519.SeedSize]), key) {
			return nil, clusterstate.ErrMigration
		}
	}
	cfg := clusterstate.LegacyConfig{Cluster: provision.Cluster, Generation: 1, Identity: id, SigningKey: root, EffectsKey: ed25519.PrivateKey(provision.Effects).Public().(ed25519.PublicKey), Anchors: []uint64{1, 2, 3}, Now: n.now().UnixMilli()}
	for _, private := range provision.Anchors {
		cfg.AnchorKeys = append(cfg.AnchorKeys, ed25519.PrivateKey(private).Public().(ed25519.PublicKey))
	}
	if n.grace != directory.DefaultGraceBounds {
		cfg.Grace = &clusterstate.GracePolicy{Min: n.grace.Min.Milliseconds(), Max: n.grace.Max.Milliseconds(), Default: n.grace.Default.Milliseconds()}
	}
	if gate != nil {
		if gate.Config.Cluster != cfg.Cluster || !bytes.Equal(gate.Config.EffectsKey, cfg.EffectsKey) {
			return nil, clusterstate.ErrMigration
		}
		if len(gate.Config.AnchorKeys) != 0 {
			for i, key := range gate.Config.AnchorKeys {
				if i >= len(cfg.AnchorKeys) || !bytes.Equal(key, cfg.AnchorKeys[i]) {
					return nil, clusterstate.ErrMigration
				}
			}
		}
		cfg = gate.Config
	}
	if err = source.FreezeLegacy(ctx, cfg); err != nil {
		return nil, err
	}
	manifest, err := source.InventoryLegacy(ctx)
	if err != nil {
		return nil, err
	}
	host, err := raftdirectory.OpenNative(ctx, path, source, manifest)
	if err != nil {
		return nil, err
	}
	feed := directory.NewFeed(n.now)
	if n.watchers > 0 {
		feed.SetMaxWatchers(n.watchers)
	}
	if n.after != nil {
		feed.SetAfter(n.after)
	}
	grace := directory.DefaultGraceBounds
	if cfg.Grace != nil {
		grace = directory.GraceBounds{Min: time.Duration(cfg.Grace.Min) * time.Millisecond, Max: time.Duration(cfg.Grace.Max) * time.Millisecond, Default: time.Duration(cfg.Grace.Default) * time.Millisecond}
	}
	d = &nativeNamespace{V1Directory: &raftdirectory.V1Directory{Host: host, Cluster: cfg.Cluster, Now: n.now, Grace: grace}, identity: clusterstate.IdentityRec{V: 1, Identity: id, SigningKey: cfg.SigningKey}, host: host, feed: feed, links: n.links.(*directory.Links), now: n.now, grace: grace}
	d.effects = raftdirectory.Effects{Host: host, Cluster: cfg.Cluster, Actor: clusterstate.DeviceIDOfKey(cfg.EffectsKey), Key: ed25519.PrivateKey(provision.Effects)}
	if err = host.InitializeMembership(ctx, ed25519.PrivateKey(provision.Anchors[0])); err != nil {
		d.Close()
		return nil, err
	}
	d.timer = memberTimer{started: n.now(), anchor: ed25519.PrivateKey(provision.Anchors[0]), after: func(delay time.Duration, fn func()) func() bool { return time.AfterFunc(delay, fn).Stop }}
	if n.after != nil {
		d.timer.after = n.after
	}
	host.Changed = func(store *raftstore.SQLite, app raftstore.Application) error {
		d.armMembers()
		return d.project(store, app)
	}
	feed.OnSeen(func(device string, at int64) { _ = d.seen(device, at) })
	if err = host.Inspect(ctx, func(ctx context.Context, s *raftstore.SQLite, a raftstore.Application) error { return d.project(s, a) }); err != nil {
		d.Close()
		return nil, err
	}
	// Repair committed decisions before returning a public namespace. Restarted
	// in-memory public requests may have expired; CompleteJoin records that fact.
	if _, err = d.effects.Replay(ctx, n.now().UnixMilli(), d.links, ""); err != nil {
		d.Close()
		return nil, err
	}
	n.mu.Lock()
	d.armMembers()
	n.groups[id] = d
	n.mu.Unlock()
	return d, nil
}
func (d *nativeNamespace) project(s *raftstore.SQLite, a raftstore.Application) error {
	ctx := context.Background()
	key := raftstore.Key{Namespace: raftstore.State, Key: clusterstate.IdentityKey().Key}
	rows, _, err := s.Read(ctx, a.Applied, []raftstore.Key{key})
	if err != nil {
		return err
	}
	var identity clusterstate.IdentityRec
	if err = clusterstate.Decode(rows[key], &identity); err != nil {
		return err
	}
	st := directory.Status{Version: a.VisibleVersion, Stopped: map[string]bool{}, Rotation: identity.Rotation}
	// Status needs devices and the identity only. Start at the device prefix so
	// a large retained cell inventory never becomes watch-notification work.
	after := "device/"
	for {
		page, _, err := s.ScanPage(ctx, a.Applied, raftstore.State, after)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			after = r.Key.Key
			if !strings.HasPrefix(after, "device/") {
				d.feed.Publish(st)
				return nil
			}
			var dev clusterstate.Device
			if err = clusterstate.Decode(r.Value, &dev); err != nil {
				return err
			}
			if dev.Revoked {
				id, err := hex.DecodeString(strings.TrimPrefix(after, "device/"))
				if err != nil {
					return err
				}
				st.Stopped[string(id)] = true
			}
		}
	}
	d.feed.Publish(st)
	return nil
}

func (d *nativeNamespace) authority(ctx context.Context, actor string, write bool) error {
	err := d.host.Inspect(ctx, func(ctx context.Context, s *raftstore.SQLite, a raftstore.Application) error {
		keys := []raftstore.Key{{Namespace: raftstore.State, Key: clusterstate.IdentityKey().Key}, {Namespace: raftstore.State, Key: clusterstate.DeviceKey(actor).Key}}
		rows, _, err := s.Read(ctx, a.Applied, keys)
		if err != nil {
			return err
		}
		var identity clusterstate.IdentityRec
		if err = clusterstate.Decode(rows[keys[0]], &identity); err != nil {
			return err
		}
		if raw := rows[keys[1]]; raw != nil {
			var dev clusterstate.Device
			if err = clusterstate.Decode(raw, &dev); err != nil {
				return err
			}
			if dev.Revoked {
				return wireauth.ErrRevoked
			}
		}
		if write && identity.Rotation != nil {
			return wireauth.ErrRotated
		}
		return nil
	})
	if err != nil && !errors.Is(err, wireauth.ErrRevoked) && !errors.Is(err, wireauth.ErrRotated) {
		return errors.Join(wireauth.ErrUnavailable, err)
	}
	return err
}

// SubscribeWatch retains the certificate from this exact authenticated socket,
// rather than enrolling a row that would consume grandfathered capacity.
func (d *nativeNamespace) SubscribeWatch(device string, r *http.Request, holds []directory.Hold, beat time.Duration) (*directory.Sub, error) {
	certificate := r.Header.Get(reqsign.HeaderCert)
	if clusterstate.PersonalCertificate(d.identity, device, certificate) != nil {
		return nil, directory.ErrBadRequest
	}
	return d.feed.SubscribeCommitted(device, holds, beat, func(device string, holds []directory.Hold, at int64) ([]dirwatch.HoldReceipt, error) {
		return d.renew(device, holds, at, certificate)
	})
}
func (d *nativeNamespace) renew(device string, holds []directory.Hold, at int64, certificate string) ([]dirwatch.HoldReceipt, error) {
	// Idle presence never wakes consensus just because a hibernating socket pings.
	if len(holds) == 0 {
		return nil, nil
	}
	ctx := context.Background()
	if err := d.authority(ctx, device, false); err != nil {
		return nil, err
	}
	// Old sockets may name zero, missing or several historical fences. Only
	// their one current held tuple is evidence; none of the others can renew it.
	items := []clusterstate.HoldFence{}
	seen := map[string]bool{}
	for _, h := range holds {
		if h.Fence == 0 || seen[h.Cell] {
			continue
		}
		raw, err := d.host.ReadEffect(ctx, clusterstate.CellKey(h.Cell))
		if err != nil {
			return nil, err
		}
		if raw == nil {
			continue
		}
		var cell clusterstate.Cell
		if err = clusterstate.Decode(raw, &cell); err != nil {
			return nil, err
		}
		if cell.Lease.Device != device || cell.Lease.Fence != h.Fence || cell.Lease.Expires == 0 {
			continue
		}
		seen[h.Cell] = true
		items = append(items, clusterstate.HoldFence{Cell: h.Cell, Fence: h.Fence})
	}
	if len(items) == 0 {
		return nil, nil
	}
	return d.effects.Renew(ctx, at, clusterstate.RenewHolds{Certificate: certificate, Device: device, SeenAt: at, Items: items})
}

func (d *nativeNamespace) seen(device string, at int64) error {
	c, e := clusterstate.Sign(clusterstate.Command{Schema: clusterstate.SchemaVersion, Cluster: d.Cluster, Actor: d.effects.Actor, Session: "presence", Sequence: 1, Now: at, Change: clusterstate.Mutation{DeviceSeen: &clusterstate.DeviceSeen{Device: device}}}, d.effects.Key)
	if e != nil {
		return e
	}
	r, e := d.host.CommitEffect(context.Background(), c)
	if e != nil {
		return e
	}
	return directory.ResultError(r.Code)
}

// Every signature is checked before allocating storage or looking up authority.
// The actor then comes from that proof for both the initial and post-I/O check.
func (n *namespaces) authenticate(r *http.Request, body []byte) (string, string, error) {
	id, actor, err := reqsign.Verify(r, body, n.now())
	if err != nil {
		return "", "", err
	}
	// Signature verification already consumed the body. This lookup needs only
	// public headers; do not copy an encrypted frame into a coordination proof.
	p, err := reqsign.Capture(r, nil)
	if err != nil {
		return "", "", err
	}
	d, err := n.activate(r.Context(), id, p.IdentityKey)
	if err != nil {
		if errors.Is(err, wireauth.ErrGone) {
			return "", "", err
		}
		return "", "", errors.Join(wireauth.ErrUnavailable, err)
	}
	if err = d.authority(r.Context(), actor, false); err != nil {
		if errors.Is(err, wireauth.ErrRevoked) {
			return "", "", err
		}
		return "", "", errors.Join(wireauth.ErrUnavailable, err)
	}
	*r = *r.WithContext(context.WithValue(r.Context(), blobActorKey{}, actor))
	return id, actor, nil
}

type blobActorKey struct{}

func (d *nativeNamespace) ForRequest(actor string, r *http.Request, raw []byte) directory.Client {
	return &nativeClient{Client: d.V1Directory.ForRequest(actor, r, raw), owner: d}
}

type nativeClient struct {
	directory.Client
	owner *nativeNamespace
}

func (c *nativeClient) Presence(ctx context.Context) (directory.PresenceView, error) {
	p, e := c.Client.Presence(ctx)
	if e != nil {
		return p, e
	}
	online := c.owner.feed.OnlineSet()
	for id, v := range p.Devices {
		v.Online = online[id]
		if v.Online {
			v.LastSeen = p.Now
		}
		p.Devices[id] = v
	}
	return p, nil
}
func (c *nativeClient) ApproveRequest(ctx context.Context, code string, a directory.Approval) error {
	if err := c.owner.effects.Admit(ctx, c.owner.now().UnixMilli(), c.owner.links, code); err != nil {
		return err
	}
	if err := c.Client.ApproveRequest(ctx, code, a); err != nil {
		return err
	}
	raw, err := c.owner.host.ReadEffect(ctx, clusterstate.JoinKey(directory.NormalizeCode(code)))
	if err != nil {
		return err
	}
	var request clusterstate.JoinRequest
	if err = clusterstate.Decode(raw, &request); err != nil {
		return err
	}
	if err = c.owner.effects.Deliver(ctx, c.owner.now().UnixMilli(), c.owner.links, code); err != nil {
		return err
	}
	if request.Outcome == "" && request.Decision != nil && !request.Decision.Deny {
		raw, err = c.owner.host.ReadEffect(ctx, clusterstate.DeviceKey(request.Decision.Device))
		if err != nil {
			return err
		}
		var device clusterstate.Device
		if err = clusterstate.Decode(raw, &device); err != nil {
			return err
		}
		c.owner.feed.AnnounceJoin(directory.Joined{Device: request.Decision.Device, Name: device.Name, Platform: device.Platform, At: device.Created})
	}
	return nil
}

func (c *nativeClient) DenyRequest(ctx context.Context, code string) error {
	if err := c.owner.effects.Admit(ctx, c.owner.now().UnixMilli(), c.owner.links, code); err != nil {
		return err
	}
	if err := c.Client.DenyRequest(ctx, code); err != nil {
		return err
	}
	return c.owner.effects.Deliver(ctx, c.owner.now().UnixMilli(), c.owner.links, code)
}
