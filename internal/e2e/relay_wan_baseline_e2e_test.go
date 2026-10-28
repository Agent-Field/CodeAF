//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// This is a bounded, seeded native Publish/Fetch baseline, not terminal pairing
// or UI readiness acceptance. Authentication uses the existing wire fixture
// seam. Packet shaping is confined to a new user+network namespace per case.
func TestRelayWANBaseline(t *testing.T) {
	if os.Getenv("CODEAF_WAN_BASELINE") != "1" {
		t.Skip("opt-in bounded WAN baseline")
	}
	if config := os.Getenv("CODEAF_WAN_CHILD"); config != "" {
		wanBaselineChild(t, config)
		return
	}
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skipf("isolated netem unavailable: %v", err)
	}
	parentNS, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{"loopback"}
	for _, rtt := range []int{50, 100, 200} {
		for _, mbps := range []int{5, 10, 50} {
			cases = append(cases, fmt.Sprintf("%d-%d", rtt, mbps))
		}
	}
	if selected := os.Getenv("CODEAF_WAN_CASES"); selected != "" {
		cases = strings.Split(selected, ",")
	}
	evidence := os.Getenv("CODEAF_WAN_EVIDENCE_DIR")
	if evidence == "" {
		evidence = t.TempDir()
	}
	if !filepath.IsAbs(evidence) {
		t.Fatal("evidence directory must be absolute")
	}
	visits := map[string]int{}
	for _, config := range cases {
		visits[config]++
		label := config
		if visits[config] > 1 {
			label = fmt.Sprintf("%s-sample-%d", config, visits[config])
		}
		t.Run(config, func(t *testing.T) {
			command := exec.Command("unshare", "-Urn", os.Args[0], "-test.run=^TestRelayWANBaseline$", "-test.v", "-test.timeout=3m")
			command.Env = append(os.Environ(), "CODEAF_WAN_CHILD="+config, "CODEAF_WAN_PARENT_NETNS="+parentNS, "CODEAF_WAN_EVIDENCE_DIR="+filepath.Join(evidence, label))
			output, err := command.CombinedOutput()
			t.Log(string(output))
			if err != nil {
				t.Fatalf("isolated WAN child: %v", err)
			}
		})
	}
}

type wanPhase struct{ HTTPRequests, SentBody, ReceivedBody, Has, GetMany, GetFrames, PutFrames, Objects, Payload int64 }
type wanTrace struct {
	mu     sync.Mutex
	sink   *cellsync.JSONLTrace
	phases map[string]*wanPhase
}

func (w *wanTrace) Observe(event cellsync.TraceEvent) {
	w.sink.Observe(event)
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.phases[event.Phase]
	if p == nil {
		p = &wanPhase{}
		w.phases[event.Phase] = p
	}
	switch event.Op {
	case "http.request":
		p.HTTPRequests++
		p.SentBody += event.SentBytes
		p.ReceivedBody += event.ReceivedBytes
	case "store.has":
		p.Has++
	case "store.get_many":
		p.GetMany++
		p.Objects += int64(event.Objects)
		p.Payload += event.PayloadBytes
	case "store.get_frame":
		p.GetFrames++
		p.Payload += event.PayloadBytes
	case "store.put_frame":
		p.PutFrames++
		p.Objects += int64(event.Objects)
		p.Payload += event.PayloadBytes
	}
}

type wanNativeMode struct {
	mu                      sync.Mutex
	DaemonCalls, SpawnCalls int
}
type wanModeTransport struct {
	inner  cellstore.Transport
	mode   string
	counts *wanNativeMode
}

func (w wanModeTransport) Do(ctx context.Context, target cellstore.Target, op cellstore.Op) ([]byte, error) {
	w.counts.mu.Lock()
	if w.mode == "daemon" {
		w.counts.DaemonCalls++
	} else {
		w.counts.SpawnCalls++
	}
	w.counts.mu.Unlock()
	return w.inner.Do(ctx, target, op)
}

func wanBaselineChild(t *testing.T, config string) {
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil || current == os.Getenv("CODEAF_WAN_PARENT_NETNS") || os.Getenv("CODEAF_WAN_PARENT_NETNS") == "" {
		t.Fatal("refusing network changes outside explicitly isolated namespace")
	}
	run := func(args ...string) string {
		t.Helper()
		var command *exec.Cmd
		switch args[0] {
		case "ip":
			command = exec.Command("ip", args[1:]...)
		case "tc":
			command = exec.Command("tc", args[1:]...)
		default:
			t.Fatalf("unsupported namespace utility %q", args[0])
		}
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("namespace command %s failed: %v %s", args[0], err, out)
		}
		return string(out)
	}
	run("ip", "link", "set", "lo", "up")
	rtt, mbps := 0, 0
	if config != "loopback" {
		parts := strings.Split(config, "-")
		if len(parts) != 2 {
			t.Fatal("invalid matrix case")
		}
		rtt, _ = strconv.Atoi(parts[0])
		mbps, _ = strconv.Atoi(parts[1])
		if (rtt != 50 && rtt != 100 && rtt != 200) || (mbps != 5 && mbps != 10 && mbps != 50) {
			t.Fatal("case outside approved matrix")
		}
		run("tc", "qdisc", "add", "dev", "lo", "root", "netem", "limit", "1000", "delay", fmt.Sprintf("%dms", rtt/2), "rate", fmt.Sprintf("%dmbit", mbps))
		defer exec.Command("tc", "qdisc", "del", "dev", "lo", "root").Run()
	}
	t.Setenv(home.EnvVar, t.TempDir())
	evidence := os.Getenv("CODEAF_WAN_EVIDENCE_DIR")
	if err := os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(evidence, "operations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	trace := &wanTrace{sink: cellsync.NewJSONLTrace(file), phases: map[string]*wanPhase{}}
	native, err := furrow.ResolveOwned()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cellstore.StopSpawned)
	cleanup := map[string]bool{"namespace_isolated": current != os.Getenv("CODEAF_WAN_PARENT_NETNS")}
	newDevice := func(role string) (cellstore.Engine, *wanNativeMode) {
		deviceHome := t.TempDir()
		socket := filepath.Join(deviceHome, "engine.sock")
		daemon := cellstore.Daemon{Binary: native, Socket: socket}
		counts := &wanNativeMode{}
		selected := cellstore.Fallback{
			Primary:   wanModeTransport{inner: daemon, mode: "daemon", counts: counts},
			Secondary: wanModeTransport{inner: cellstore.Spawn{Binary: native, Socket: socket}, mode: "spawn", counts: counts},
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stopErr := daemon.Stop(ctx)
			cleanup[role+"_engine_stopped"] = stopErr == nil
			if stopErr == nil {
				if _, err := os.Lstat(socket); err == nil {
					if err := os.Remove(socket); err != nil {
						t.Errorf("remove owned IPC socket: %v", err)
					}
				}
			}
			_, socketErr := os.Lstat(socket)
			cleanup[role+"_ipc_socket_absent"] = os.IsNotExist(socketErr)
			receipt, _ := json.Marshal(cleanup)
			if err := os.WriteFile(filepath.Join(evidence, "cleanup.json"), receipt, 0600); err != nil {
				t.Errorf("cleanup receipt: %v", err)
			}
			if stopErr != nil {
				t.Errorf("owned engine shutdown: %v", stopErr)
			}
		})
		return cellstore.Engine{Binary: native, DataRoot: filepath.Join(deviceHome, "stores"), Transport: selected}, counts
	}
	relayRoot := t.TempDir()
	relayDisk, err := blobstore.NewDisk(relayRoot)
	if err != nil {
		t.Fatal(err)
	}
	directoryMemory := directory.NewMemory(time.Now)
	auth := func(r *http.Request, _ []byte) (string, string, error) {
		if r.Header.Get("X-Fixture-Identity") != "seeded-baseline" {
			return "", "", wireauth.ErrUnauthorized
		}
		return "seeded-baseline", "fixture-device", nil
	}
	storeHandler := blobstore.Handler(auth, func(string) (blobstore.Store, error) { return relayDisk, nil })
	dirHandler := directory.Handler(auth, func(string) (directory.Directory, error) { return directoryMemory, nil })
	calibration := bytes.Repeat([]byte{0x61}, 1<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/calibration/ping":
			w.WriteHeader(204)
		case r.URL.Path == "/calibration/stream":
			_, _ = w.Write(calibration)
		case strings.HasPrefix(r.URL.Path, "/v1/dir/"):
			dirHandler.ServeHTTP(w, r)
		default:
			storeHandler.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(server.Close)
	newClient := func() *http.Client {
		client := &http.Client{Transport: cellsync.ObserveHTTP(&http.Transport{Proxy: nil}, trace.Observe), Timeout: 30 * time.Second}
		t.Cleanup(client.CloseIdleConnections)
		return client
	}
	client := newClient()
	calibrationCtx := cellsync.TracePhase(context.Background(), "calibration", 1)
	var ping []float64
	for i := 0; i < 4; i++ {
		started := time.Now()
		req, _ := http.NewRequestWithContext(calibrationCtx, "GET", server.URL+"/calibration/ping", nil)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if i > 0 {
			ping = append(ping, float64(time.Since(started).Microseconds())/1000)
		}
	}
	started := time.Now()
	req, _ := http.NewRequestWithContext(calibrationCtx, "GET", server.URL+"/calibration/stream", nil)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if err != nil || n != int64(len(calibration)) {
		t.Fatal("calibration stream incomplete")
	}
	streamSeconds := time.Since(started).Seconds()
	sign := func(r *http.Request, _ []byte) { r.Header.Set("X-Fixture-Identity", "seeded-baseline") }
	remote := cellsync.ObserveStore(blobstore.NewHTTP(server.URL, sign, newClient()), trace.Observe)
	source, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	sourceRevision, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	nativeHash := wanFileSHA256(t, native)
	harnessHash := wanFileSHA256(t, os.Args[0])
	const seed int64 = 20261025
	const count = 64
	const size = 8192
	random := rand.New(rand.NewSource(seed))
	files := map[string][]byte{}
	digest := sha256.New()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("mock/module-%03d.bin", i)
		body := make([]byte, size)
		_, _ = random.Read(body)
		files[name] = body
		digest.Write([]byte(name))
		digest.Write(body)
		path := filepath.Join(source.Root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	trace.Observe(cellsync.TraceEvent{Op: "workload", Layer: "fixture", Class: "mixed_fixture", Seed: seed, Files: count, LogicalBytes: count * size, Outcome: "ok"})
	keys := cellstore.SyncKeys{CellKey: make([]byte, 32), Dedup: make([]byte, 32)}
	engine, sourceMode := newDevice("source")
	sourceSync := cellsync.ObserveEngine(engine.Sync(keys, cellstore.LedgerName(server.URL, "seeded-fixture")), trace.Observe)
	first, err := engine.Seal(context.Background(), source, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	hintsEnabled := os.Getenv("CODEAF_TEST_WARM_FRAME_HINTS") == "1"
	publisher := cellsync.Publisher{Engine: sourceSync, Store: remote, Dir: directory.NewHTTP(server.URL, sign, client), WarmFrameHints: hintsEnabled}
	driving := cellsync.Driving{Cell: source}
	info := cellsync.PublishInfo{Class: string(cell.Sandboxed), Keys: map[string]map[string]string{}}
	if err := publisher.Publish(cellsync.TracePhase(context.Background(), "publish", 1), &driving, first.Turn.ID, info); err != nil {
		t.Fatal(err)
	}

	receiver := cell.Cell{ID: source.ID, Root: t.TempDir()}
	receiverEngine, receiverMode := newDevice("receiver")
	receiverSync := cellsync.ObserveEngine(receiverEngine.Sync(keys, cellstore.LedgerName(server.URL, "seeded-fixture")), trace.Observe)
	receiverClient := newClient()
	receiverStore := cellsync.ObserveStore(&blobstore.Resuming{Inner: blobstore.Counting{Inner: blobstore.NewHTTP(server.URL, sign, receiverClient), C: &blobstore.Counters{}}}, trace.Observe)
	inbox := filepath.Join(evidence, "inbox")
	defer os.RemoveAll(inbox)
	fetcher := cellsync.Fetcher{WarmFrameHints: hintsEnabled, Engine: receiverSync, Store: receiverStore, Dir: directory.NewHTTP(server.URL, sign, receiverClient), Inbox: func(cell.Cell) string { return inbox }}
	started = time.Now()
	if err := fetcher.Fetch(cellsync.TracePhase(context.Background(), "cold", 1), receiver, first.Turn.ID); err != nil {
		t.Fatal(err)
	}
	cold := time.Since(started)
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(receiver.Root, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("cold receiver bytes differ")
		}
	}
	files["mock/module-000.bin"] = []byte("seeded one-file replacement\n")
	if err := os.WriteFile(filepath.Join(source.Root, "mock/module-000.bin"), files["mock/module-000.bin"], 0600); err != nil {
		t.Fatal(err)
	}
	second, err := engine.Seal(context.Background(), source, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	err = publisher.Publish(cellsync.TracePhase(context.Background(), "publish", 2), &driving, second.Turn.ID, info)
	if err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	if err := fetcher.Fetch(cellsync.TracePhase(context.Background(), "warm", 1), receiver, second.Turn.ID); err != nil {
		t.Fatal(err)
	}
	warm := time.Since(started)
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(receiver.Root, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("warm receiver bytes differ")
		}
	}
	if sourceMode.SpawnCalls != 0 || receiverMode.SpawnCalls != 0 || sourceMode.DaemonCalls == 0 || receiverMode.DaemonCalls == 0 {
		t.Fatal("baseline did not stay on the production daemon transport")
	}
	if err := trace.sink.Err(); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(filepath.Join(evidence, "operations.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := cellsync.ReadTraceSummary(input)
	input.Close()
	if err != nil {
		t.Fatal(err)
	}
	analysisJSON, _ := json.MarshalIndent(analysis, "", "  ")
	if err := os.WriteFile(filepath.Join(evidence, "analysis.json"), analysisJSON, 0600); err != nil {
		t.Fatal(err)
	}
	storeFootprint := wanStoreFootprint(t, relayRoot)
	summary := map[string]any{"warm_frame_hints": hintsEnabled, "relay_backend": "legacy", "relay_footprint": storeFootprint, "native_transport": "production daemon with standalone fallback, isolated per simulated device", "native_modes": map[string]*wanNativeMode{"source": sourceMode, "receiver": receiverMode}, "source_revision": strings.TrimSpace(string(sourceRevision)), "native_sha256": nativeHash, "harness_sha256": harnessHash, "config": config, "requested_rtt_ms": rtt, "shared_rate_mbps": mbps, "one_way_delay_ms": rtt / 2, "ping_ms": ping, "stream_bytes": n, "stream_seconds": streamSeconds, "stream_observed_mbps": float64(n) * 8 / streamSeconds / 1e6, "seed": seed, "files": count, "logical_bytes": count * size, "workload_sha256": hex.EncodeToString(digest.Sum(nil)), "cold_native_g3_ms": float64(cold.Microseconds()) / 1000, "warm_native_g3_ms": float64(warm.Microseconds()) / 1000, "G1": "unobserved", "G2": "unobserved", "phases": trace.phases, "scope": "native protocol fixture; HTTP body bytes exclude headers/TLS; no UI or WAN acceptance gate claim", "transport_setup": "separate calibration/source/receiver connection pools; cold includes receiver TCP setup; no TLS fixture", "namespace_cleanup": "isolated namespace exits; owned engine shutdown checked on test cleanup"}
	raw, _ := json.MarshalIndent(summary, "", "  ")
	if err := os.WriteFile(filepath.Join(evidence, "summary.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log(string(raw))
	if config != "loopback" {
		t.Log(run("tc", "-s", "qdisc", "show", "dev", "lo"))
	}
}

func wanFileSHA256(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// Fixture paths are private. Receipt contains category totals only. Allocated
// blocks are not exclusive CoW accounting. Includes live SQLite WAL and SHM.
func wanStoreFootprint(t *testing.T, root string) map[string]any {
	t.Helper()
	var logical, allocated, files, wal, shm int64
	known := true
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		blocks, ok := wanAllocated(info)
		known = known && ok
		allocated += blocks
		if info.Mode().IsRegular() {
			files++
			logical += info.Size()
			if strings.HasSuffix(path, "-wal") {
				wal += info.Size()
			}
			if strings.HasSuffix(path, "-shm") {
				shm += info.Size()
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"logical_bytes": logical, "allocated_block_bytes": allocated, "allocation_known": known, "regular_files": files, "wal_logical_bytes": wal, "shm_logical_bytes": shm, "scope": "live relay store including directory blocks and SQLite sidecars; not exclusive CoW bytes"}
}
