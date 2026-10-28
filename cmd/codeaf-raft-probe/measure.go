package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/raftabi"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// measureRows runs the memory gate without retaining the Node consensus
// test coordinator. Six production handles and real disk-backed DOs remain in
// workerd; the bulk driver streams input. Inspector sampling never forces GC.
func measureRows(out, binaryPath, schema string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "codeaf-p03-rows-")
	if err != nil {
		return err
	}
	freePort := func() (string, error) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
		addr := l.Addr().String()
		err = l.Close()
		return addr, err
	}
	httpAddr, err := freePort()
	if err != nil {
		return err
	}
	inspectAddr, err := freePort()
	if err != nil {
		return err
	}
	config := fmt.Sprintf(`using Workerd = import %q;
const config :Workerd.Config = (
 services = [
  (name = "raft-proof", worker = (
   compatibilityDate = "2026-09-01",
   modules = [(name = "worker.js", esModule = embed %q), (name = "core.wasm", wasm = embed %q)],
   bindings = [(name = "PROOF_STORE", durableObjectNamespace = (className = "ProofStore", serviceName = "proof-storage"))], globalOutbound = "blocked"
  )),
  (name = "proof-storage", worker = (
   compatibilityDate = "2026-09-01", modules = [(name = "storage.js", esModule = embed %q)],
   durableObjectNamespaces = [(className = "ProofStore", uniqueKey = "p03-local-proof", enableSql = true)],
   durableObjectStorage = (localDisk = "storage"), globalOutbound = "blocked"
  )),
  (name = "storage", disk = (path = %q, writable = true)),
  (name = "blocked", worker = (compatibilityDate = "2026-09-01", modules = [(name = "blocked.js", esModule = "export default {fetch() {throw new Error('proof forbids outbound network')}}")], globalOutbound = "blocked"))
 ], sockets = [(name = "http", address = %q, http = (), service = "raft-proof")]
);
`, schema, filepath.Join(out, "worker.js"), filepath.Join(out, "core.wasm"), filepath.Join(out, "storage-worker.js"), dir, httpAddr)
	configPath := filepath.Join(dir, "proof.capnp")
	if err = os.WriteFile(configPath, []byte(config), 0600); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, binaryPath, "serve", "--experimental", "-I/", "--inspector-addr", inspectAddr, configPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return err
	}
	stopped := false
	defer func() {
		if !stopped {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	client := &http.Client{Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 2}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	origin := "http://" + httpAddr
	request := func(method, path string, b []byte) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 && res.StatusCode != 204 {
			return nil, errors.New("memory proof HTTP refusal")
		}
		return io.ReadAll(io.LimitReader(res.Body, raftabi.MaxBuffer+1))
	}
	start := time.Now()
	for {
		if _, err = request("GET", "/ready", nil); err == nil {
			break
		}
		if time.Since(start) > 10*time.Second {
			return errors.New("memory proof workerd startup timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	readyMs := float64(time.Since(start).Microseconds()) / 1000
	res, err := client.Get("http://" + inspectAddr + "/json/list")
	if err != nil {
		return err
	}
	var targets []struct{ Title, WebSocketDebuggerURL string }
	err = json.NewDecoder(res.Body).Decode(&targets)
	res.Body.Close()
	if err != nil {
		return err
	}
	var target string
	for _, t := range targets {
		if strings.Contains(t.Title, "raft-proof") {
			target = t.WebSocketDebuggerURL
			break
		}
	}
	if target == "" {
		return errors.New("memory proof inspector target absent")
	}
	conn, _, err := websocket.Dial(ctx, target, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	serial := 0
	type heap struct{ UsedSize, TotalSize, EmbedderHeapUsedSize, BackingStorageSize uint64 }
	inspect := func() (heap, error) {
		serial++
		id := serial
		if err := wsjson.Write(ctx, conn, map[string]any{"id": id, "method": "Runtime.getHeapUsage"}); err != nil {
			return heap{}, err
		}
		for {
			var reply struct {
				ID     int
				Result heap
				Error  json.RawMessage
			}
			if err := wsjson.Read(ctx, conn, &reply); err != nil {
				return heap{}, err
			}
			if reply.ID == id {
				if len(reply.Error) > 0 {
					return heap{}, errors.New("memory proof inspector refusal")
				}
				return reply.Result, nil
			}
		}
	}
	ticksText, err := exec.Command("getconf", "CLK_TCK").Output()
	if err != nil {
		return err
	}
	ticks, err := strconv.ParseFloat(strings.TrimSpace(string(ticksText)), 64)
	if err != nil {
		return err
	}
	type proc struct {
		PID        int
		RSSBytes   uint64
		CPUSeconds float64
	}
	readProc := func(pid int) (proc, error) {
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			return proc{}, err
		}
		p := proc{PID: pid}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				f := strings.Fields(line)
				n, _ := strconv.ParseUint(f[1], 10, 64)
				p.RSSBytes = n * 1024
			}
		}
		b, err = os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return proc{}, err
		}
		f := strings.Fields(string(b)[strings.LastIndex(string(b), ") ")+2:])
		u, _ := strconv.ParseFloat(f[11], 64)
		s, _ := strconv.ParseFloat(f[12], 64)
		p.CPUSeconds = (u + s) / ticks
		return p, nil
	}
	var peakRSS, peakComponents, peakWasm uint64
	var samples int
	var peakProcesses []proc
	var sampleErr error
	var mu sync.Mutex
	sample := func() error {
		h, err := inspect()
		if err != nil {
			return err
		}
		b, err := request("GET", "/metrics", nil)
		if err != nil {
			return err
		}
		var metrics struct{ PeakMemory uint64 }
		if err = json.Unmarshal(b, &metrics); err != nil {
			return err
		}
		var sum uint64
		var ps []proc
		for _, pid := range []int{os.Getpid(), cmd.Process.Pid} {
			p, err := readProc(pid)
			if err != nil {
				return err
			}
			sum += p.RSSBytes
			ps = append(ps, p)
		}
		mu.Lock()
		defer mu.Unlock()
		samples++
		if sum > peakRSS {
			peakRSS = sum
			peakProcesses = ps
		}
		peakWasm = max(peakWasm, metrics.PeakMemory)
		peakComponents = max(peakComponents, h.TotalSize+h.EmbedderHeapUsedSize+h.BackingStorageSize)
		return nil
	}
	// Complete each bootstrap Ready against real storage before adding load.
	// These are dormant followers; the preceding mixed test proves elections,
	// leadership, native peers and recovery. No authority read is served here.
	header := func(op uint32) *bytes.Buffer {
		b := new(bytes.Buffer)
		binary.Write(b, binary.LittleEndian, raftabi.Version)
		binary.Write(b, binary.LittleEndian, op)
		return b
	}
	put32 := func(b *bytes.Buffer, n uint32) { binary.Write(b, binary.LittleEndian, n) }
	put64 := func(b *bytes.Buffer, n uint64) { binary.Write(b, binary.LittleEndian, n) }
	putBlob := func(b *bytes.Buffer, v []byte) { binary.Write(b, binary.LittleEndian, uint32(len(v))); b.Write(v) }
	payload := func(b []byte) (*bytes.Reader, error) {
		r := bytes.NewReader(b)
		var version, n uint32
		if binary.Read(r, binary.LittleEndian, &version) != nil || version != raftabi.Version || binary.Read(r, binary.LittleEndian, &n) != nil || n != 0 {
			return nil, errors.New("memory proof ABI refusal")
		}
		return r, nil
	}
	step := func(handle, tag uint32, batch uint64) ([]byte, error) {
		b := header(3)
		put32(b, handle)
		put32(b, tag)
		if tag == 5 || tag == 6 {
			put64(b, batch)
		}
		return request("POST", "/call", b.Bytes())
	}
	coldMs := float64(0)
	for cluster := 1; cluster <= 2; cluster++ {
		for id := uint64(1); id <= 3; id++ {
			b := header(1)
			putBlob(b, []byte(fmt.Sprintf("bulk-%d", cluster)))
			put64(b, id)
			put64(b, 100+id)
			put32(b, 1)
			put32(b, 3)
			for _, v := range []uint64{1, 2, 3} {
				put64(b, v)
			}
			for i := 0; i < 3; i++ {
				putBlob(b, nil)
			}
			put32(b, 0)
			put64(b, 0)
			begin := time.Now()
			raw, err := request("POST", "/call", b.Bytes())
			if err != nil {
				return err
			}
			if coldMs == 0 {
				coldMs = float64(time.Since(begin).Microseconds()) / 1000
			}
			r, err := payload(raw)
			if err != nil {
				return err
			}
			var handle uint32
			if binary.Read(r, binary.LittleEndian, &handle) != nil {
				return errors.New("memory proof handle missing")
			}
			raw, err = step(handle, 0, 0)
			if err != nil {
				return err
			}
			r, err = payload(raw)
			if err != nil {
				return err
			}
			var flag uint32
			var batch uint64
			if binary.Read(r, binary.LittleEndian, &flag) != nil || flag != 1 || binary.Read(r, binary.LittleEndian, &batch) != nil {
				return errors.New("memory proof bootstrap persist missing")
			}
			path := fmt.Sprintf("/store/bulk-%d-%d/", cluster, id)
			if _, err = request("PUT", path+"persist", raw); err != nil {
				return err
			}
			raw, err = step(handle, 5, batch)
			if err != nil {
				return err
			}
			r, err = payload(raw)
			if err != nil {
				return err
			}
			var sends uint32
			if binary.Read(r, binary.LittleEndian, &flag) != nil || flag != 0 || binary.Read(r, binary.LittleEndian, &sends) != nil || sends != 0 || binary.Read(r, binary.LittleEndian, &flag) != nil || flag != 1 || binary.Read(r, binary.LittleEndian, &batch) != nil {
				return errors.New("memory proof bootstrap apply missing")
			}
			if _, err = request("PUT", path+"apply", raw); err != nil {
				return err
			}
			raw, err = step(handle, 6, batch)
			if err != nil {
				return err
			}
			if _, err = payload(raw); err != nil {
				return err
			}
		}
	}
	if err = sample(); err != nil {
		return err
	}
	done := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := sample(); err != nil {
					sampleErr = err
					return
				}
			}
		}
	}()
	var legacy bytes.Buffer
	rowErr := rowProof(origin, &legacy)
	close(done)
	<-joined
	if sampleErr != nil {
		return sampleErr
	}
	if err = sample(); err != nil {
		return err
	}
	if rowErr != nil {
		legacy.Reset()
		json.NewEncoder(&legacy).Encode(map[string]string{"error": rowErr.Error()})
	}
	report := map[string]any{"readyMs": readyMs, "coldFirstCallMs": coldMs, "residentHandles": 6, "identities": 2, "heapSamples": samples, "observedIsolateBytes": peakComponents + peakWasm, "peakWasmBytes": peakWasm, "observedCombinedRSSBytes": peakRSS, "peakProcesses": peakProcesses, "legacy": json.RawMessage(legacy.Bytes()), "elapsedMs": float64(time.Since(start).Microseconds()) / 1000, "allocatorArenas": os.Getenv("MALLOC_ARENA_MAX")}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(out, "rows-runtime.json"), append(encoded, '\n'), 0600); err != nil {
		return err
	}
	cmd.Process.Kill()
	cmd.Wait()
	stopped = true
	if rowErr != nil {
		return rowErr
	}
	if peakRSS > 256<<20 {
		return errors.New("R27 combined local process RSS exceeds 256 MiB")
	}
	if peakComponents+peakWasm > 96<<20 || peakWasm > 64<<20 {
		return errors.New("R27 isolate memory exceeds budget")
	}
	if coldMs >= 1000 {
		return errors.New("R27 startup exceeds 1 s envelope")
	}
	_, err = os.Stdout.Write(append(encoded, '\n'))
	return err
}
