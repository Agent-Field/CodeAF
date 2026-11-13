package rafttransport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/coder/websocket"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// R6/R11/R27 require a full sleeping learner's inbox to reject delivery while
// the SAME outward connection continues carrying normal voter traffic.
func TestR6R11R27SocketBackpressurePreservesVoterConnection(t *testing.T) {
	r := router(t)
	server := httptest.NewServer(Handler(r, nil))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), socketTimeout)
	defer cancel()
	auth, err := Encode(request(t, 1))
	must(t, err)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+SocketPath, &websocket.DialOptions{HTTPHeader: http.Header{SocketAuthHeader: []string{base64.RawURLEncoding.EncodeToString(auth)}}})
	must(t, err)
	defer conn.CloseNow()
	read := func() Response {
		t.Helper()
		kind, raw, err := conn.Read(ctx)
		must(t, err)
		if kind != websocket.MessageBinary {
			t.Fatal("R6 changed framing")
		}
		var response Response
		must(t, Decode(raw, &response))
		return response
	}
	read()
	send := func(packet Envelope) Response {
		t.Helper()
		raw, err := Encode(request(t, 1, packet))
		must(t, err)
		must(t, conn.Write(ctx, websocket.MessageBinary, raw))
		return read()
	}
	for sequence := uint64(1); sequence <= 3; sequence++ {
		response := send(message(t, 1, 4, sequence, pb.MsgHeartbeat, 0))
		if sequence < 3 {
			if !slices.Equal(response.Accepted, []string{decimal(sequence)}) || len(response.Rejected) != 0 {
				t.Fatal("R11 legal learner reservation refused", response)
			}
		} else {
			if !slices.Equal(response.Rejected, []string{"3"}) || len(response.Accepted) != 0 {
				t.Fatal("R11 overflow was acknowledged", response)
			}
		}
	}
	packet := message(t, 1, 2, 4, pb.MsgHeartbeat, 0)
	response := send(packet)
	if !slices.Equal(response.Accepted, []string{"4"}) || len(response.Rejected) != 0 {
		t.Fatal("R6 blocked voter behind sleeping learner", response)
	}
	delivered := exchange(t, r, request(t, 2))
	if len(delivered.Messages) != 1 || !sameEnvelope(delivered.Messages[0].Envelope, packet) {
		t.Fatal("R6 voter traffic was lost", delivered)
	}
	sum := 0
	for _, n := range r.Stats(testScope, 1000).Bytes {
		sum += n
	}
	if sum > MaxQueuedBytes {
		t.Fatal("R27 queue cap changed")
	}
}

// R6/R13/R27 allow a multi-chunk snapshot to advance only when the target
// releases its bounded staging reservation, without polling the source socket.
func TestR6R13R27SnapshotChunkCreditPushesOnTheSameSocket(t *testing.T) {
	r := router(t)
	server := httptest.NewServer(Handler(r, nil))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), socketTimeout)
	defer cancel()
	auth, err := Encode(request(t, 1))
	must(t, err)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+SocketPath, &websocket.DialOptions{HTTPHeader: http.Header{SocketAuthHeader: []string{base64.RawURLEncoding.EncodeToString(auth)}}})
	must(t, err)
	defer conn.CloseNow()
	read := func() Response {
		t.Helper()
		_, raw, err := conn.Read(ctx)
		must(t, err)
		var response Response
		must(t, Decode(raw, &response))
		return response
	}
	read()
	for sequence := uint64(1); sequence <= 2; sequence++ {
		payload := bytes.Repeat([]byte{byte(sequence)}, MaxChunkBytes)
		packet := Envelope{Scope: testScope, From: 1, To: 4, Incarnation: 1, Destination: 1, Sequence: sequence, Payload: payload, Chunk: &Chunk{Generation: digest([]byte("snapshot")), Offset: sequence - 1, Hash: digest(payload)}}
		must(t, packet.Sign(key(1)))
		raw, err := Encode(request(t, 1, packet))
		must(t, err)
		must(t, conn.Write(ctx, websocket.MessageBinary, raw))
		admitted := read()
		if !slices.Equal(admitted.Accepted, []string{decimal(sequence)}) || len(admitted.Rejected) != 0 {
			t.Fatal("R13 chunk refused after target credit", admitted)
		}
		delivered := exchange(t, r, request(t, 4))
		if len(delivered.Messages) != 1 || !sameEnvelope(delivered.Messages[0].Envelope, packet) {
			t.Fatal("R13 lost snapshot chunk", delivered)
		}
		ack := request(t, 4)
		ack.Ack[Snapshot] = delivered.Messages[0].Cursor
		must(t, ack.Sign(key(4)))
		exchange(t, r, ack)
		credited := read()
		if !slices.Equal(credited.Chunks, []ChunkAck{{Node: 4, Sequence: sequence}}) {
			t.Fatal("R13 no target credit without source poll", credited)
		}
	}
	if r.Stats(testScope, 1000).Queued != 0 {
		t.Fatal("R27 snapshot reservation retained")
	}
}

// R6/R13/R16/R27 bind an oversized framed append to its actual sender and type
// while retaining BOTH the normal proposal budget and existing chunk ceiling.
func TestR6R13R16R27FramedAppendUsesExistingChunkCaps(t *testing.T) {
	wrap := func(kind pb.MessageType, from uint64) Envelope {
		raw, err := proto.Marshal(&pb.Message{Type: kind.Enum(), From: proto.Uint64(from), To: proto.Uint64(2), Term: proto.Uint64(1), Context: bytes.Repeat([]byte("x"), MaxBatchBytes)})
		must(t, err)
		hash := digest(raw)
		return Envelope{Scope: testScope, From: 1, To: 2, Incarnation: 1, Destination: 1, Sequence: 1, Payload: raw, Chunk: &Chunk{Generation: hash, Hash: hash}}
	}
	e := wrap(pb.MsgApp, 1)
	must(t, e.Sign(key(1)))
	if !e.AppendChunk() || len(e.Payload) <= MaxBatchBytes || len(e.Payload) > MaxChunkBytes {
		t.Fatal("R27 framed append missed the two existing bounds")
	}
	must(t, e.Verify(key(1).Public().(ed25519.PublicKey)))
	raw, err := Encode(e)
	must(t, err)
	var round Envelope
	must(t, Decode(raw, &round))
	must(t, round.Verify(key(1).Public().(ed25519.PublicKey)))
	for _, bad := range []Envelope{wrap(pb.MsgProp, 1), wrap(pb.MsgReadIndex, 1), wrap(pb.MsgApp, 3)} {
		if bad.Sign(key(1)) == nil {
			t.Fatal("R16 framed append bypassed peer sender/type checks")
		}
	}
	e.Chunk = nil
	if e.Sign(key(1)) == nil {
		t.Fatal("R27 normal peer cap was raised")
	}
}
