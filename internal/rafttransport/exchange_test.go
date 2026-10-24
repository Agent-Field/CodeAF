package rafttransport

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	pb "go.etcd.io/raft/v3/raftpb"
)

// R10/R16 use the same signed exchange on reconnectable outbound sockets and
// HTTP fallback. Every frame rechecks enrollment, including an already open
// socket when its member is revoked or its router generation changes.
func TestR10R16OutboundSocketPushReconnectAndRevoke(t *testing.T) {
	r := router(t)
	server := httptest.NewServer(Handler(r, func() time.Time { return time.UnixMilli(1000) }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dial := func(req Request) *websocket.Conn {
		t.Helper()
		b, err := Encode(req)
		must(t, err)
		conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+SocketPath, &websocket.DialOptions{HTTPHeader: http.Header{SocketAuthHeader: []string{base64.RawURLEncoding.EncodeToString(b)}}})
		if err != nil {
			if resp != nil {
				t.Log(resp.StatusCode)
			}
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.CloseNow() })
		return conn
	}
	read := func(conn *websocket.Conn) Response {
		t.Helper()
		kind, data, err := conn.Read(ctx)
		must(t, err)
		if kind != websocket.MessageBinary {
			t.Fatal("R10: socket changed framing")
		}
		var got Response
		must(t, Decode(data, &got))
		return got
	}
	conn := dial(request(t, 2))
	if got := read(conn); len(got.Messages) != 0 {
		t.Fatal("R10: initial poll was not empty")
	}
	client := Client{URL: server.URL, HTTP: server.Client()}
	packet := message(t, 1, 2, 1, pb.MsgApp, 0)
	_, err := client.Exchange(ctx, request(t, 1, packet))
	must(t, err)
	got := read(conn)
	if len(got.Messages) != 1 || !sameEnvelope(got.Messages[0].Envelope, packet) {
		t.Fatal("R10: live socket did not receive queued packet")
	}
	conn.CloseNow()
	conn = dial(request(t, 2))
	again := read(conn)
	if len(again.Messages) != 1 || again.Messages[0].Cursor != got.Messages[0].Cursor {
		t.Fatal("R10: reconnect lost unacknowledged delivery")
	}
	ack := request(t, 2)
	ack.Ack[Voter] = got.Messages[0].Cursor
	must(t, ack.Sign(key(2)))
	data, err := Encode(ack)
	must(t, err)
	must(t, conn.Write(ctx, websocket.MessageBinary, data))
	if got = read(conn); len(got.Messages) != 0 {
		t.Fatal("R11: socket ack did not release inbox")
	}
	v := view()
	v.Revision++
	v.Members[1].Revoked = true
	must(t, r.Install(v))
	if _, _, err = conn.Read(ctx); err == nil {
		t.Fatal("R16: revoked socket remained usable")
	}
	if _, err = client.Exchange(ctx, request(t, 2)); err != ErrAuth {
		t.Fatal("R16: revoked socket identity used HTTP fallback", err)
	}
}

// R11/R16/R27 bound both wire ingress and sockets, keep signing material out
// of diagnostics, and stop cross-origin redirects of signed member batches.
func TestR11R16R27WireLimitsAndRedirectRefusal(t *testing.T) {
	r := router(t)
	h := Handler(r, func() time.Time { return time.UnixMilli(1000) })
	req := request(t, 1)
	wire, err := Encode(req)
	must(t, err)
	for _, test := range []struct {
		method, path string
		body         []byte
		status       int
	}{
		{"POST", ExchangePath, wire, 200}, {"GET", ExchangePath, nil, 405}, {"POST", ExchangePath, append(wire, ' '), 400},
		{"POST", ExchangePath, bytes.Repeat([]byte("x"), MaxExchangeBytes+1), 400}, {"POST", ExchangePath + "?identity=other", wire, 400},
		{"POST", ExchangePath, []byte(`{"secret":"private-sentinel"}`), 400},
	} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(test.method, test.path, bytes.NewReader(test.body)))
		if response.Code != test.status || strings.Contains(response.Body.String(), "private-sentinel") {
			t.Fatal("R16/R27: incorrect bounded refusal", response.Code, response.Body.String())
		}
	}
	releases := []func(){}
	for range MaxInflight {
		release, err := r.openSocket(req)
		must(t, err)
		releases = append(releases, release)
	}
	if _, err = r.openSocket(req); err != ErrCapacity {
		t.Fatal("R27: socket admission unbounded", err)
	}
	for _, release := range releases {
		release()
	}
	if r.Stats(testScope, 1000).Sockets != 0 {
		t.Fatal("R27: closed socket retained admission")
	}
	redirected := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected++; w.WriteHeader(200) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	_, err = (Client{URL: origin.URL, HTTP: origin.Client()}).Exchange(context.Background(), req)
	if err == nil || redirected != 0 {
		t.Fatal("R16: signed peer traffic followed a cross-origin redirect")
	}
}

// R14 keeps the native envelope compatible with independently signed Node
// vectors, including all byte values and integers above JavaScript's floor.
func TestR14IndependentJavaScriptWireVectors(t *testing.T) {
	data, err := readVector()
	must(t, err)
	var fixture struct {
		PublicKey []byte  `json:"public_key"`
		Request   Request `json:"request"`
	}
	must(t, Decode(data, &fixture))
	must(t, fixture.Request.Verify(ed25519.PublicKey(fixture.PublicKey)))
	for _, e := range fixture.Request.Messages {
		must(t, e.Verify(ed25519.PublicKey(fixture.PublicKey)))
	}
	if fixture.Request.Ack[Voter] != 1<<53 || fixture.Request.Messages[0].Sequence != ^uint64(0) {
		t.Fatal("R14: golden vector lost uint64 bits")
	}
}

// readVector intentionally reads a committed cross-runtime fixture, not output
// generated by the production codec during the test.
func readVector() ([]byte, error) {
	f, err := http.Dir("testdata").Open("exchange-v1.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// R15 carries sealed bytes without adding readable names, titles, paths or
// task text to transport metadata, diagnostics or malformed-request refusals.
func TestR15SealedPayloadCaptureAndSafeDiagnostics(t *testing.T) {
	r := router(t)
	plaintext := []byte("private-title-SENTINEL device-name-SENTINEL /private/path-SENTINEL task-body-SENTINEL secret-SENTINEL")
	block, err := aes.NewCipher(bytes.Repeat([]byte{7}, 32))
	must(t, err)
	aead, err := cipher.NewGCM(block)
	must(t, err)
	sealed := aead.Seal(nil, make([]byte, aead.NonceSize()), plaintext, nil)
	e := Envelope{Scope: testScope, From: 1, To: 2, Incarnation: 1, Destination: 1, Sequence: 1, Payload: sealed, Chunk: &Chunk{Generation: digest([]byte("snapshot")), Hash: digest(sealed)}}
	must(t, e.Sign(key(1)))
	req := request(t, 1, e)
	wire, err := Encode(req)
	must(t, err)
	exchange(t, r, req)
	got := exchange(t, r, request(t, 2))
	capture, err := Encode(got)
	must(t, err)
	stats, err := json.Marshal(r.Stats(testScope, 1000))
	must(t, err)
	for _, text := range [][]byte{wire, capture, stats} {
		if bytes.Contains(text, []byte("SENTINEL")) {
			t.Fatal("R15: plaintext appeared in transport capture/diagnostics")
		}
	}
	opened, err := aead.Open(nil, make([]byte, aead.NonceSize()), got.Messages[0].Envelope.Payload, nil)
	must(t, err)
	if !bytes.Equal(opened, plaintext) {
		t.Fatal("R15: transport changed sealed payload")
	}
}
