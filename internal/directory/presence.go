package directory

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
)

// DevicePresence is one device's line in a presence answer.
type DevicePresence struct {
	Online   bool  `json:"online"`
	LastSeen int64 `json:"last_seen"` // directory ms; the answer's now while Online
}

// PresenceView is the answer to GET /v1/dir/presence (docs/ux-pairing-contract.md, section 3.5):
// one entry per device that is not revoked. It is the fallback for a client
// whose watch socket is down, and what a surface reads at open.
type PresenceView struct {
	Now     int64                     `json:"now"`
	Devices map[string]DevicePresence `json:"devices"`
	// Version is the directory version the answer was read at, sent as the VersionHeader.
	Version uint64 `json:"-"`
}

func (v PresenceView) header(h http.Header) {
	h.Set(VersionHeader, strconv.FormatUint(v.Version, 10))
}

// presenceOf is the view of listing l when the devices in online hold a watch socket.
func presenceOf(l Listing, online map[string]bool) PresenceView {
	v := PresenceView{Now: l.Now, Version: l.Version, Devices: map[string]DevicePresence{}}
	for id, d := range l.Devices {
		if d.Revoked {
			continue
		}
		p := DevicePresence{Online: online[id], LastSeen: d.LastSeen}
		if p.Online {
			p.LastSeen = l.Now
		}
		v.Devices[id] = p
	}
	return v
}

func (c *memoryClient) Presence(ctx context.Context) (PresenceView, error) {
	l, err := c.List(ctx)
	return presenceOf(l, c.m.feed.OnlineSet()), err
}

func (c *sqliteClient) Presence(ctx context.Context) (PresenceView, error) {
	l, err := c.List(ctx)
	return presenceOf(l, c.s.feed.OnlineSet()), err
}

// Presence asks the relay who is online. ErrTooOld-style fallbacks are the caller's: a relay without the route answers not found.
func (h *HTTP) Presence(ctx context.Context) (v PresenceView, err error) {
	hdr, err := h.doHeader(ctx, http.MethodGet, dirBase+"/presence", nil, &v)
	if err == nil {
		v.Version, _ = strconv.ParseUint(hdr.Get(VersionHeader), 10, 64)
	}
	return v, err
}

// seen stamps when device last held a watch socket. It moves no version,
// because LastSeen is not a thing a person sees in the list.
func (m *Memory) seen(device string, at int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.devices[device]; ok {
		d.LastSeen = at
		set(m, m.devices, device, d)
	}
}

func (s *SQLite) seen(device string, at int64) {
	_ = s.inTx(context.Background(), func(tx *sql.Tx) error {
		var d Device
		if get(tx, "devices", device, &d) != nil {
			return nil // not a device of this identity: nothing to stamp
		}
		d.LastSeen = at
		return put(tx, s.feed, "devices", device, d, at)
	})
}

// wireFeed connects a backend to its watch feed: the devices it approves are
// announced to the event sockets, and the sockets' comings and goings stamp last_seen.
func wireFeed(f *Feed, p *pairing, seen func(device string, at int64)) {
	p.SetOnJoin(f.announceJoin)
	f.OnSeen(seen)
}
