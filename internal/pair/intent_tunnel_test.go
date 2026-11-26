package pair

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// V21 / D2: a single registration can serve multiple doors when the first byte
// on the stream selects the Noise prologue for that intent, and a stream whose
// intent and prologue do not match fails the handshake.
func TestAnIntentAndItsPrologueSelectTheTunnel(t *testing.T) {
	newKey := func() *ecdh.PrivateKey {
		k, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	a, b := newKey(), newKey()
	prologues := map[byte]string{
		IntentFiles: "codeaf files door v1 connect",
		IntentLive:  "codeaf live door v1 connect",
	}

	for _, tc := range []struct {
		name     string
		intent   byte
		prologue string
	}{
		{name: "files intent", intent: IntentFiles, prologue: prologues[IntentFiles]},
		{name: "live intent", intent: IntentLive, prologue: prologues[IntentLive]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			near, far := net.Pipe()
			defer near.Close()
			defer far.Close()
			_ = near.SetDeadline(time.Now().Add(3 * time.Second))
			_ = far.SetDeadline(time.Now().Add(3 * time.Second))

			type accepted struct {
				tunnel *Tunnel
				intent byte
				err    error
			}
			done := make(chan accepted, 1)
			go func() {
				tun, got, err := AcceptWithKeyPrologues(far, b, nil, prologues)
				done <- accepted{tunnel: tun, intent: got, err: err}
			}()

			client, err := ConnectWithKeyIntent(near, b.PublicKey().Bytes(), a, tc.intent, tc.prologue)
			if err != nil {
				t.Fatal(err)
			}
			server := <-done
			if server.err != nil {
				t.Fatal(server.err)
			}
			if server.intent != tc.intent {
				t.Fatalf("accepted intent = %q, want %q", server.intent, tc.intent)
			}
			if !bytes.Equal(client.ChannelBinding(), server.tunnel.ChannelBinding()) {
				t.Fatal("endpoints do not share the completed channel binding")
			}
			if !bytes.Equal(server.tunnel.Peer(), a.PublicKey().Bytes()) {
				t.Fatal("responder did not authenticate the initiator key")
			}

			msg := []byte("encrypted payload for selected intent")
			errCh := make(chan error, 1)
			go func() {
				_, err := client.Write(msg)
				errCh <- err
			}()
			got := make([]byte, len(msg))
			if _, err := io.ReadFull(server.tunnel, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, msg) {
				t.Fatalf("read %q, want %q", got, msg)
			}
			if err := <-errCh; err != nil {
				t.Fatal(err)
			}
		})
	}

	t.Run("mismatched intent and prologue fail the handshake", func(t *testing.T) {
		near, far := net.Pipe()
		defer near.Close()
		defer far.Close()
		_ = near.SetDeadline(time.Now().Add(3 * time.Second))
		_ = far.SetDeadline(time.Now().Add(3 * time.Second))

		done := make(chan error, 1)
		go func() {
			defer far.Close()
			_, _, err := AcceptWithKeyPrologues(far, b, nil, prologues)
			done <- err
		}()

		if _, err := ConnectWithKeyIntent(near, b.PublicKey().Bytes(), a, IntentLive, prologues[IntentFiles]); err == nil {
			t.Fatal("handshake completed when the intent selected a different prologue")
		}
		if err := <-done; err == nil {
			t.Fatal("responder accepted a mismatched prologue")
		}
	})
}

// V21 / D2: an intent refused by the admission check or absent from the
// responder's prologue map closes the connection before any byte is written back.
func TestAnUnadmittedIntentIsClosedBeforeAnyByteIsWrittenBack(t *testing.T) {
	b, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	prologues := map[byte]string{
		IntentFiles: "codeaf files door v1 connect",
		IntentLive:  "codeaf live door v1 connect",
	}

	for _, tc := range []struct {
		name      string
		intent    byte
		admit     func(byte) bool
		prologues map[byte]string
	}{
		{
			name:      "refused by admit callback",
			intent:    IntentLive,
			admit:     func(intent byte) bool { return intent != IntentLive },
			prologues: prologues,
		},
		{
			name:      "intent absent from prologues",
			intent:    IntentLive,
			admit:     nil,
			prologues: map[byte]string{IntentFiles: prologues[IntentFiles]},
		},
		{
			name:      "unknown intent byte",
			intent:    'Z',
			admit:     func(byte) bool { return true },
			prologues: prologues,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			near, far := net.Pipe()
			defer near.Close()
			defer far.Close()
			_ = near.SetDeadline(time.Now().Add(3 * time.Second))
			_ = far.SetDeadline(time.Now().Add(3 * time.Second))

			done := make(chan error, 1)
			go func() {
				_, _, err := AcceptWithKeyPrologues(far, b, tc.admit, tc.prologues)
				done <- err
			}()

			if _, err := near.Write([]byte{tc.intent}); err != nil {
				t.Fatal(err)
			}
			var buf [16]byte
			n, readErr := near.Read(buf[:])
			if n != 0 || !errors.Is(readErr, io.EOF) {
				t.Fatalf("read %d bytes (%v) after unadmitted intent, want 0 bytes and io.EOF", n, readErr)
			}
			if err := <-done; err == nil {
				t.Fatal("responder returned nil error for an unadmitted intent")
			}
		})
	}
}
