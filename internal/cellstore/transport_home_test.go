package cellstore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
)

// C28 keeps a deferred durability operation on the home that captured its files,
// including fallback after an engine health check refuses the first connection.
func TestC28DeferredDurabilityKeepsItsHome(t *testing.T) {
	for _, mode := range []string{"1", "0"} {
		t.Run(mode, func(t *testing.T) {
			// Short profiles avoid requiring filesystem aliases for native Unix sockets.
			root, err := os.MkdirTemp("", "ch")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(root); err != nil {
					t.Error(err)
				}
			})
			first, second := filepath.Join(root, "a"), filepath.Join(root, "b")
			for _, dir := range []string{first, second} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv(home.EnvVar, first)
			t.Setenv("LOCALAPPDATA", first)
			t.Setenv(daemonEnv, mode)
			binary := filepath.Join(first, "furrow-fixture")
			socket := socketFor(binary)
			if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(socket)) })
			done := make(chan error, 1)
			go func() {
				for attempt := 0; ; attempt++ {
					conn, err := listener.Accept()
					if err != nil {
						done <- err
						return
					}
					err = func() error {
						defer conn.Close()
						_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
						decoder, encoder := json.NewDecoder(bufio.NewReader(conn)), json.NewEncoder(conn)
						var request wireRequest
						if err := decoder.Decode(&request); err != nil {
							return err
						}
						if request.Verb != "health" {
							return fmt.Errorf("first request %s", request.Verb)
						}
						name := filepath.Base(binary)
						reject := mode == "1" && attempt == 0
						if reject {
							name = "other-engine"
						}
						raw, _ := json.Marshal(struct{ Engine string }{name})
						if err := encoder.Encode(wireResponse{ID: request.ID, Ok: raw}); err != nil {
							return err
						}
						if reject {
							return nil
						}
						if err := decoder.Decode(&request); err != nil {
							return err
						}
						if request.Verb != "sync" || request.Target.DataDir != first {
							return fmt.Errorf("durability route %+v", request)
						}
						return encoder.Encode(wireResponse{ID: request.ID, Ok: json.RawMessage(`{"durable":true}`)})
					}()
					if err != nil || mode == "0" || attempt > 0 {
						done <- err
						return
					}
				}
			}()
			transport := transportFor(binary, nil)
			t.Setenv(home.EnvVar, second)
			t.Setenv("LOCALAPPDATA", second)
			if err := syncEngine(t.Context(), transport, Target{Tree: first, DataDir: first}); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(second)
			if err != nil || len(entries) != 0 {
				t.Fatalf("durability wrote into another home: %v, %v", entries, err)
			}
		})
	}
}
