package pair

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// countingWriter remembers every call to Write, so a test can say how many
// messages a record became on a stream that sends one message per write.
type countingWriter struct {
	writes [][]byte
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.writes = append(c.writes, append([]byte(nil), p...))
	return len(p), nil
}

// D21 / V35: a record is ONE write, and the bytes it puts on the wire are the
// same bytes the old prefix-then-body pair of writes produced, so a build with
// either behaviour reads the other's records.
func TestARecordIsOneWrite(t *testing.T) {
	for _, size := range []int{0, 1, 31, 4096, maxRecord} {
		body := bytes.Repeat([]byte{0xA5}, size)
		var counted countingWriter
		if err := writeRecord(&counted, body); err != nil {
			t.Fatalf("a %d-byte record: %v", size, err)
		}
		if len(counted.writes) != 1 {
			t.Fatalf("a %d-byte record took %d writes, want exactly 1", size, len(counted.writes))
		}
		var prefix [2]byte
		binary.BigEndian.PutUint16(prefix[:], uint16(size))
		want := append(prefix[:], body...)
		if !bytes.Equal(counted.writes[0], want) {
			t.Fatalf("a %d-byte record's bytes are not the two-byte big-endian length then the body", size)
		}
		got, err := readRecord(bytes.NewReader(counted.writes[0]))
		if err != nil {
			t.Fatalf("a %d-byte record does not read back: %v", size, err)
		}
		if size == 0 {
			if len(got) != 0 {
				t.Fatalf("an empty record read back %d bytes", len(got))
			}
		} else if !bytes.Equal(got, body) {
			t.Fatalf("a %d-byte record read back different bytes", size)
		}
	}
	if err := writeRecord(&countingWriter{}, make([]byte, maxRecord+1)); err == nil {
		t.Fatal("a record over the Noise ceiling was written")
	}
}

// D8 / V38: the direct road has its own intent byte and its own prologue, so a
// stream made for the direct road completes only against the direct prologue
// and never against the relay's live door or the files door.
func TestTheDirectIntentSelectsItsOwnPrologueAndNoOtherCompletesIt(t *testing.T) {
	a, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	prologues := map[byte]string{
		IntentFiles:  "codeaf files door v1 connect",
		IntentLive:   "codeaf live door v1 connect",
		IntentDirect: "codeaf live door v1 direct",
	}
	if IntentDirect == IntentLive || IntentDirect == IntentFiles {
		t.Fatal("the direct intent shares a byte with another door")
	}

	attempt := func(intent byte, prologue string) (client *Tunnel, serverIntent byte, clientErr, serverErr error) {
		near, far := net.Pipe()
		defer near.Close()
		defer far.Close()
		_ = near.SetDeadline(time.Now().Add(3 * time.Second))
		_ = far.SetDeadline(time.Now().Add(3 * time.Second))
		type accepted struct {
			intent byte
			err    error
		}
		done := make(chan accepted, 1)
		go func() {
			defer far.Close()
			tun, got, err := AcceptWithKeyPrologues(far, b, nil, prologues)
			if tun != nil {
				// The stream must carry bytes both ways once it completes.
				buf := make([]byte, 2)
				_, err = io.ReadFull(tun, buf)
			}
			done <- accepted{intent: got, err: err}
		}()
		client, clientErr = ConnectWithKeyIntent(near, b.PublicKey().Bytes(), a, intent, prologue)
		if client != nil {
			_, _ = client.Write([]byte("ok"))
		}
		res := <-done
		return client, res.intent, clientErr, res.err
	}

	client, got, clientErr, serverErr := attempt(IntentDirect, prologues[IntentDirect])
	if clientErr != nil || serverErr != nil || client == nil {
		t.Fatalf("the direct intent with the direct prologue did not complete: client %v, server %v", clientErr, serverErr)
	}
	if got != IntentDirect {
		t.Fatalf("the responder saw intent %q, want %q", got, IntentDirect)
	}
	for _, wrong := range []string{prologues[IntentLive], prologues[IntentFiles]} {
		if _, _, clientErr, serverErr := attempt(IntentDirect, wrong); clientErr == nil || serverErr == nil {
			t.Fatalf("the direct intent completed with the prologue %q (client %v, server %v)", wrong, clientErr, serverErr)
		}
	}
	if _, _, clientErr, serverErr := attempt(IntentLive, prologues[IntentDirect]); clientErr == nil || serverErr == nil {
		t.Fatalf("the live intent completed with the direct prologue (client %v, server %v)", clientErr, serverErr)
	}
}
