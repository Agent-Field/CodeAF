// Command hostedvectors writes the shared test vectors that pin any port of the
// relay's pure parts (frame decode, request verification, lease rules) to the
// Go originals. Every expected answer is produced by calling the real Go code,
// so a port that passes the vectors is byte-for-byte compatible with it.
//
//	go run ./test/hostedvectors > /tmp/v.json
//	VECTORS=/tmp/v.json node --test relay/hosted/test/vectors.test.js
//
// Keys and certs are fresh on every run, so CI should regenerate and run the
// port against the new file; relay/hosted/vectors.json is only a snapshot.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// T0 is the fixed time every request vector is judged at.
var T0 = time.UnixMilli(1759049990000)

// Vectors is the whole file.
type Vectors struct {
	Frames   []FrameCase   `json:"frames"`
	Requests []RequestCase `json:"requests"`
	Rules    []RuleCase    `json:"rules"`
}

func main() {
	v := Vectors{Frames: frameCases(), Requests: requestCases(), Rules: ruleCases()}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// ---- frames ----

// FrameCase is one frame and what Decode says about it.
type FrameCase struct {
	Name      string   `json:"name"`
	Frame     string   `json:"frame"` // standard base64
	OK        bool     `json:"ok"`
	CellKeyID string   `json:"cell_key_id,omitempty"`
	RIDs      []string `json:"rids,omitempty"`
	Lens      []int    `json:"lens,omitempty"`
	FrameID   string   `json:"frame_id,omitempty"`
}

const cellKey = "0123456789abcdef0123456789abcdef"

func rid(n byte) string { return strings.Repeat(fmt.Sprintf("%02x", n), 32) }

func object(n byte, body string) blobstore.Object {
	return blobstore.Object{RID: rid(n), Bytes: []byte("AGEO\x01" + body)}
}

// rawFrame builds a frame around a header JSON and a payload, unchecked.
func rawFrame(header string, payload string) []byte {
	n := len(header)
	out := []byte("AGEF\x01")
	out = append(out, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
	return append(append(out, header...), payload...)
}

func refs(entries ...string) string { return "[" + strings.Join(entries, ",") + "]" }

func ref(n byte, off, ln int) string {
	return fmt.Sprintf(`{"rid":%q,"off":%d,"len":%d}`, rid(n), off, ln)
}

func header(v int, key string, objects string) string {
	return fmt.Sprintf(`{"V":%d,"cell_key_id":%q,"objects":%s}`, v, key, objects)
}

func frameCases() []FrameCase {
	good, _ := blobstore.Encode(cellKey, []blobstore.Object{object(1, "one"), object(2, ""), object(3, "three")})
	one, _ := blobstore.Encode(cellKey, []blobstore.Object{{RID: rid(9), Bytes: []byte("AGEV\x01vault")}})
	bytesOf := map[string][]byte{
		"valid three objects": good,
		"valid vault object":  one,
		"wrong magic":         append([]byte("XXXXX"), good[5:]...),
		"too short":           []byte("AGEF"),
		"header past frame":   append([]byte("AGEF\x01\xff\x00\x00\x00"), "{}"...),
		"header over limit":   append([]byte("AGEF\x01\x01\x00\x10\x00"), "{}"...),
		"header not json":     rawFrame("nope", ""),
		"unknown version":     rawFrame(header(2, cellKey, refs(ref(1, 0, 6))), "AGEO\x01x"),
		"cell key uppercase":  rawFrame(header(1, strings.ToUpper(cellKey), refs(ref(1, 0, 6))), "AGEO\x01x"),
		"cell key short":      rawFrame(header(1, "abcd", refs(ref(1, 0, 6))), "AGEO\x01x"),
		"no objects":          rawFrame(header(1, cellKey, "[]"), ""),
		"gap between objects": rawFrame(header(1, cellKey, refs(ref(1, 0, 6), ref(2, 7, 6))), "AGEO\x01aXAGEO\x01b"),
		"object past payload": rawFrame(header(1, cellKey, refs(ref(1, 0, 60))), "AGEO\x01x"),
		"unnamed trailing":    rawFrame(header(1, cellKey, refs(ref(1, 0, 6))), "AGEO\x01xJUNK"),
		"duplicate rid":       rawFrame(header(1, cellKey, refs(ref(1, 0, 6), ref(1, 6, 6))), "AGEO\x01aAGEO\x01b"),
		"rid not hex":         rawFrame(strings.Replace(header(1, cellKey, refs(ref(1, 0, 6))), rid(1), strings.Repeat("g", 64), 1), "AGEO\x01x"),
		"plaintext object":    rawFrame(header(1, cellKey, refs(ref(1, 0, 5))), "hello"),
		"object too short":    rawFrame(header(1, cellKey, refs(ref(1, 0, 3))), "AGE"),
	}
	return casesOf(bytesOf)
}

func casesOf(frames map[string][]byte) []FrameCase {
	var out []FrameCase
	for name, f := range frames {
		out = append(out, frameCase(name, f))
	}
	sortBy(out, func(c FrameCase) string { return c.Name })
	return out
}

func frameCase(name string, f []byte) FrameCase {
	c := FrameCase{Name: name, Frame: b64(f)}
	h, objs, err := blobstore.Decode(f)
	if err != nil {
		return c
	}
	c.OK, c.CellKeyID, c.FrameID = true, h.CellKeyID, blobstore.IDOf(f)
	for _, o := range objs {
		c.RIDs, c.Lens = append(c.RIDs, o.RID), append(c.Lens, len(o.Bytes))
	}
	return c
}

// ---- requests ----

// RequestCase is one signed request and who Verify says sent it.
type RequestCase struct {
	Name     string            `json:"name"`
	Method   string            `json:"method"`
	URI      string            `json:"uri"`
	Headers  map[string]string `json:"headers"`
	Body     string            `json:"body"` // standard base64
	NowMs    int64             `json:"now_ms"`
	Identity string            `json:"identity,omitempty"`
	Device   string            `json:"device,omitempty"`
	Refusal  string            `json:"refusal,omitempty"` // "skew" | "unauthorized"
}

type party struct {
	id  identity.Identity
	dev identity.Dev
}

func (p party) IdentityKey() ed25519.PublicKey { return p.id.PublicKey() }
func (p party) Cert() identity.Cert            { return p.dev.Cert }
func (p party) Sign(msg []byte) []byte         { return p.dev.Sign(msg) }

func newParty() party {
	home, err := os.MkdirTemp("", "hostedvec")
	must(err)
	defer os.RemoveAll(home)
	dev, err := identity.Device(home)
	must(err)
	id, err := identity.Load(home)
	must(err)
	return party{id, dev}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// send signs a request the way a device does and returns it with its body.
func send(p reqsign.Signer, method, uri string, body []byte, at time.Time) map[string]string {
	r := httptest.NewRequest(method, uri, nil)
	reqsign.Sign(r, body, p, at)
	h := map[string]string{}
	for _, k := range []string{reqsign.HeaderIdentity, reqsign.HeaderCert, reqsign.HeaderTime, reqsign.HeaderSig} {
		h[k] = r.Header.Get(k)
	}
	return h
}

func requestCases() []RequestCase {
	a, b := newParty(), newParty()
	body := []byte(`{"rids":["x"]}`)
	mk := func(name string, method, uri string, sent, seen []byte, p reqsign.Signer, at time.Time, edit func(map[string]string)) RequestCase {
		h := send(p, method, uri, sent, at)
		if edit != nil {
			edit(h)
		}
		return judged(RequestCase{Name: name, Method: method, URI: uri, Headers: h, Body: b64(seen), NowMs: T0.UnixMilli()})
	}
	return []RequestCase{
		mk("valid get", "GET", "/v1/store/stats", nil, nil, a, T0, nil),
		mk("valid post with query", "POST", "/v1/store/has?x=1", body, body, a, T0, nil),
		mk("body changed in flight", "POST", "/v1/store/has", body, []byte("{}"), a, T0, nil),
		mk("skew five minutes ok", "GET", "/v1/dir/list", nil, nil, a, T0.Add(-5*time.Minute), nil),
		mk("skew six minutes past", "GET", "/v1/dir/list", nil, nil, a, T0.Add(-6*time.Minute), nil),
		mk("skew six minutes ahead", "GET", "/v1/dir/list", nil, nil, a, T0.Add(6*time.Minute), nil),
		mk("foreign cert", "GET", "/v1/dir/list", nil, nil, a, T0, func(h map[string]string) {
			other := send(b, "GET", "/v1/dir/list", nil, T0)
			h[reqsign.HeaderCert] = other[reqsign.HeaderCert]
		}),
		mk("signature by another device key", "GET", "/v1/dir/list", nil, nil, a, T0, func(h map[string]string) {
			other := send(b, "GET", "/v1/dir/list", nil, T0)
			h[reqsign.HeaderSig] = other[reqsign.HeaderSig]
		}),
		mk("missing signature", "GET", "/v1/dir/list", nil, nil, a, T0, func(h map[string]string) { delete(h, reqsign.HeaderSig) }),
		mk("identity key truncated", "GET", "/v1/dir/list", nil, nil, a, T0, func(h map[string]string) {
			h[reqsign.HeaderIdentity] = h[reqsign.HeaderIdentity][:20]
		}),
		pathChanged(a),
	}
}

// pathChanged is signed for one path and presented for another.
func pathChanged(p reqsign.Signer) RequestCase {
	h := send(p, "GET", "/v1/store/stats", nil, T0)
	return judged(RequestCase{Name: "path changed after signing", Method: "GET", URI: "/v1/dir/list", Headers: h, Body: "", NowMs: T0.UnixMilli()})
}

// judged fills in Verify's answer.
func judged(c RequestCase) RequestCase {
	r := httptest.NewRequest(c.Method, c.URI, nil)
	for k, v := range c.Headers {
		r.Header.Set(k, v)
	}
	body, _ := base64.StdEncoding.DecodeString(c.Body)
	id, dev, err := reqsign.Verify(r, body, time.UnixMilli(c.NowMs))
	switch {
	case err == nil:
		c.Identity, c.Device = id, dev
	case strings.Contains(err.Error(), "clock"):
		c.Refusal = "skew"
	default:
		c.Refusal = "unauthorized"
	}
	return c
}

// ---- lease rules ----

// RuleCase is one rule applied to a cell: the cell in, the answer out.
type RuleCase struct {
	Name   string          `json:"name"`
	Op     string          `json:"op"` // create | acquire | heartbeat | publish | release
	Device string          `json:"device"`
	Now    int64           `json:"now"`
	Cell   directory.Cell  `json:"cell"`
	Arg    json.RawMessage `json:"arg,omitempty"`
	Err    string          `json:"err,omitempty"` // lease_held | fence_stale | head_moved
	Want   *directory.Cell `json:"want,omitempty"`
}

var head1, head2, head3 = rid(0xa1), rid(0xa2), rid(0xa3)

func cellHeldBy(dev string, fence uint64, expires int64) directory.Cell {
	return directory.Cell{V: 1, Head: head1, DurableAt: 1000, Class: "chat", Size: 10, Title: "t1",
		Keys: map[string]map[string]string{}, Lease: directory.Lease{Device: dev, Fence: fence, Expires: expires, Pending: 2}}
}

func ruleCases() []RuleCase {
	live, gone := cellHeldBy("dev_a", 3, 50000), cellHeldBy("dev_a", 3, 500)
	pub := func(fence uint64, old string, title string) json.RawMessage {
		return arg(directory.Publish{Fence: fence, OldHead: old, Head: head2, Size: 20, Class: "chat", Title: title, Pending: 1})
	}
	return runAll([]RuleCase{
		{Name: "acquire free cell", Op: "acquire", Device: "dev_b", Now: 1000, Cell: gone},
		{Name: "acquire live lease of another", Op: "acquire", Device: "dev_b", Now: 1000, Cell: live},
		{Name: "acquire own live lease", Op: "acquire", Device: "dev_a", Now: 1000, Cell: live},
		{Name: "acquire expired lease of another", Op: "acquire", Device: "dev_b", Now: 501, Cell: gone},
		{Name: "acquire at exact expiry", Op: "acquire", Device: "dev_b", Now: 500, Cell: gone},
		{Name: "heartbeat holder", Op: "heartbeat", Device: "dev_a", Now: 1000, Cell: live, Arg: arg(directory.Beat{Fence: 3, Pending: 7})},
		{Name: "heartbeat stale fence", Op: "heartbeat", Device: "dev_a", Now: 1000, Cell: live, Arg: arg(directory.Beat{Fence: 2})},
		{Name: "heartbeat other device", Op: "heartbeat", Device: "dev_b", Now: 1000, Cell: live, Arg: arg(directory.Beat{Fence: 3})},
		{Name: "heartbeat expired same fence", Op: "heartbeat", Device: "dev_a", Now: 9999, Cell: gone, Arg: arg(directory.Beat{Fence: 3})},
		{Name: "publish holder", Op: "publish", Device: "dev_a", Now: 2000, Cell: live, Arg: pub(3, head1, "t2")},
		{Name: "publish empty title keeps", Op: "publish", Device: "dev_a", Now: 2000, Cell: live, Arg: pub(3, head1, "")},
		{Name: "publish stale fence beats moved head", Op: "publish", Device: "dev_a", Now: 2000, Cell: live, Arg: pub(2, head3, "")},
		{Name: "publish head moved", Op: "publish", Device: "dev_a", Now: 2000, Cell: live, Arg: pub(3, head3, "")},
		{Name: "publish after takeover", Op: "publish", Device: "dev_a", Now: 2000, Cell: cellHeldBy("dev_b", 4, 50000), Arg: pub(3, head1, "")},
		{Name: "release holder", Op: "release", Device: "dev_a", Now: 1000, Cell: live, Arg: arg(map[string]uint64{"fence": 3})},
		{Name: "release stale", Op: "release", Device: "dev_a", Now: 1000, Cell: live, Arg: arg(map[string]uint64{"fence": 1})},
		{Name: "create", Op: "create", Device: "dev_a", Now: 1000, Arg: arg(directory.CellInit{Head: head1, Class: "chat", Title: "t", Size: 5, Keys: map[string]map[string]string{"k": {"id_x": "w"}}, OrphanTurns: 1})},
	})
}

func arg(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }

func runAll(cases []RuleCase) []RuleCase {
	for i := range cases {
		cases[i] = run(cases[i])
	}
	return cases
}

func run(c RuleCase) RuleCase {
	cell, err := apply(c)
	if err != nil {
		c.Err = codeOf(err)
		return c
	}
	c.Want = &cell
	return c
}

func apply(c RuleCase) (directory.Cell, error) {
	switch c.Op {
	case "acquire":
		return directory.Acquire(c.Cell, c.Device, c.Now)
	case "heartbeat":
		var b directory.Beat
		must(json.Unmarshal(c.Arg, &b))
		return directory.Heartbeat(c.Cell, c.Device, b, c.Now)
	case "publish":
		var p directory.Publish
		must(json.Unmarshal(c.Arg, &p))
		return directory.PublishTo(c.Cell, c.Device, p, c.Now)
	case "release":
		var r struct{ Fence uint64 }
		must(json.Unmarshal(c.Arg, &r))
		return directory.ReleaseOf(c.Cell, c.Device, r.Fence)
	}
	var in directory.CellInit
	must(json.Unmarshal(c.Arg, &in))
	return directory.Created(in, c.Device, c.Now), nil
}

func codeOf(err error) string {
	for _, e := range []struct {
		err  error
		code string
	}{{directory.ErrLeaseHeld, "lease_held"}, {directory.ErrFenceStale, "fence_stale"}, {directory.ErrHeadMoved, "head_moved"}} {
		if strings.Contains(err.Error(), e.err.Error()) {
			return e.code
		}
	}
	return err.Error()
}

func sortBy[T any](s []T, key func(T) string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && key(s[j]) < key(s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
