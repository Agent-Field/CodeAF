package remote

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

type measurePipe struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out io.ReadCloser
}

func (p *measurePipe) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *measurePipe) Write(b []byte) (int, error) { return p.in.Write(b) }
func (p *measurePipe) Close() error {
	_ = p.in.Close()
	_ = p.out.Close()
	return p.cmd.Wait()
}

type rateWriter struct {
	w io.Writer
	b int64
}

func (w rateWriter) Write(p []byte) (int, error) {
	if w.b > 0 {
		time.Sleep(time.Duration(int64(time.Second) * int64(len(p)) / w.b))
	}
	return w.w.Write(p)
}

func TestPipeMeasureHelper(t *testing.T) {
	if os.Getenv("AFORGE_PIPE_HELPER") != "1" {
		t.Skip("helper")
	}
	entries := make([]session.DisplayEntry, 24000)
	for i := range entries {
		entries[i] = session.DisplayEntry{Role: "assistant", Text: fmt.Sprintf("%06d the same long transcript sentence repeats enough words to compress cleanly across an ordinary remote link", i)}
	}
	agent := &fakeAgent{model: "measure/model", transcript: entries}
	bytesPerSecond, _ := strconv.ParseInt(os.Getenv("AFORGE_MEASURE_BPS"), 10, 64)
	err := Serve(os.Stdin, rateWriter{w: os.Stdout, b: bytesPerSecond}, Options{Boot: func(Hello) (*Engine, error) {
		return engineOn(agent), nil
	}})
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestPipeMeasure(t *testing.T) {
	if os.Getenv("AFORGE_MEASURE") != "1" {
		t.Skip("measurement")
	}
	testbin, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if helper := os.Getenv("AFORGE_MEASURE_HELPER_BIN"); helper != "" {
		testbin = helper
	}
	remoteCommand := "AFORGE_PIPE_HELPER=1 AFORGE_MEASURE_BPS=" + os.Getenv("AFORGE_MEASURE_BPS") + " " + testbin + " -test.run=TestPipeMeasureHelper"
	cmd := exec.Command("ssh", "-T", "localhost", remoteCommand)
	attachStart := time.Now()
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pipe := &measurePipe{cmd: cmd, in: in, out: out}
	client, err := Dial(pipe, "localhost", Hello{})
	if err != nil {
		t.Fatal(err)
	}
	handshakeElapsed := time.Since(attachStart)
	if os.Getenv("AFORGE_MEASURE_TRANSCRIPT_FIRST") == "1" {
		entries := client.Agent().Transcript()
		t.Logf("long-transcript attach entries=%d handshake=%s total=%s", len(entries), handshakeElapsed, time.Since(attachStart))
		_ = client.Close()
		return
	}

	durations := make([]time.Duration, 200)
	for i := range durations {
		start := time.Now()
		if got := client.Agent().Model(); got != "measure/model" {
			t.Fatalf("model = %q", got)
		}
		durations[i] = time.Since(start)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	at := func(p float64) time.Duration { return durations[int(float64(len(durations)-1)*p)] }
	t.Logf("small-frame n=200 min=%s p50=%s p90=%s p95=%s p99=%s max=%s", durations[0], at(.50), at(.90), at(.95), at(.99), durations[len(durations)-1])

	start := time.Now()
	entries := client.Agent().Transcript()
	t.Logf("transcript entries=%d elapsed=%s", len(entries), time.Since(start))
	_ = client.Close()
}
