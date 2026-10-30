package pair

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"io"
	"net"
	"testing"
	"time"
)

// Contract 14 depends on an encrypted task channel whose binding both devices
// can sign; the separate prologue prevents a machine door accepting a task.
func TestTaskTunnelBindsBothEndsAndSeparatesMachineDoors(t *testing.T) {
	key := func() *ecdh.PrivateKey {
		k, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	a, b := key(), key()
	t.Run("binding and plaintext", func(t *testing.T) {
		near, far := net.Pipe()
		defer near.Close()
		defer far.Close()
		_ = near.SetDeadline(time.Now().Add(3 * time.Second))
		_ = far.SetDeadline(time.Now().Add(3 * time.Second))
		result := make(chan *Tunnel, 1)
		failed := make(chan error, 1)
		go func() {
			tunnel, err := AcceptWithKey(far, b)
			if err != nil {
				failed <- err
				return
			}
			result <- tunnel
			_, err = tunnel.Write([]byte("only the endpoints can read this"))
			failed <- err
		}()
		client, err := ConnectWithKey(near, b.PublicKey().Bytes(), a)
		if err != nil {
			t.Fatal(err)
		}
		var server *Tunnel
		select {
		case server = <-result:
		case err := <-failed:
			t.Fatal(err)
		}
		binding := client.ChannelBinding()
		if len(binding) == 0 || !bytes.Equal(binding, server.ChannelBinding()) {
			t.Fatal("ends do not share the completed handshake binding")
		}
		binding[0] ^= 1
		if bytes.Equal(binding, client.ChannelBinding()) {
			t.Fatal("a caller could mutate the binding")
		}
		if !bytes.Equal(server.Peer(), a.PublicKey().Bytes()) {
			t.Fatal("server did not authenticate the initiator static")
		}
		data := make([]byte, len("only the endpoints can read this"))
		if _, err := io.ReadFull(client, data); err != nil {
			t.Fatal(err)
		}
		if string(data) != "only the endpoints can read this" {
			t.Fatalf("plaintext = %q", data)
		}
		if err := <-failed; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("task cannot enter a machine door", func(t *testing.T) {
		near, far := net.Pipe()
		defer near.Close()
		defer far.Close()
		_ = near.SetDeadline(time.Now().Add(3 * time.Second))
		_ = far.SetDeadline(time.Now().Add(3 * time.Second))
		machine := Device{private: b}
		finished := make(chan error, 1)
		go func() {
			defer far.Close()
			var intent [1]byte
			_, _ = io.ReadFull(far, intent[:])
			_, _, err := acceptAsMachine(far, machine, func([]byte, string) error { t.Error("task reached machine authorization"); return nil })
			finished <- err
		}()
		if _, err := ConnectWithKey(near, b.PublicKey().Bytes(), a); err == nil {
			t.Fatal("task handshake completed against a machine door")
		}
		if err := <-finished; err == nil {
			t.Fatal("machine accepted task handshake")
		}
	})
	t.Run("machine cannot enter a task door", func(t *testing.T) {
		near, far := net.Pipe()
		defer near.Close()
		defer far.Close()
		_ = near.SetDeadline(time.Now().Add(3 * time.Second))
		_ = far.SetDeadline(time.Now().Add(3 * time.Second))
		finished := make(chan error, 1)
		go func() { defer far.Close(); _, err := AcceptWithKey(far, b); finished <- err }()
		if _, err := connectAsSurface(near, b.PublicKey().Bytes(), Device{private: a}, "laptop"); err == nil {
			t.Fatal("machine handshake completed against a task door")
		}
		if err := <-finished; err == nil {
			t.Fatal("task door accepted machine handshake")
		}
	})
}
