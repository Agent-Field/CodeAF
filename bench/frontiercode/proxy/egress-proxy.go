// Command egress-proxy is an OPEN, LOGGING HTTP/CONNECT proxy: everything the
// task container asks for is permitted and written down, nothing is refused.
//
// FrontierCode runs its agents with internet access ON. A DeepSWE-style
// allowlist cannot match that — the whole point is that some tasks need the
// internet for documentation and lookups — and Cognition's own account of
// their first attempt says why blocking is a dead end: their domain list grew
// past a thousand entries and agents kept finding ways around it. The policy
// this rig runs instead is the opposite: open egress, every connection
// logged, and a SCANNER (grade/scanner.py) that reads the log and the
// harness's own transcript afterwards and flags the run if it visited the
// task's upstream repository, its patches, or its mirrors. A flagged run
// scores 0 and counts toward flag rate.
//
// What the log can and cannot see. A CONNECT request names only the host —
// that is level (a) of the rig's two-level logging, hostname-level, and it is
// what this proxy writes. It cannot see the path inside a TLS tunnel; the
// scanner's second level is the harness's own transcript (web tool
// arguments, every shell command), which names URLs and commands in full.
// A MITM proxy with a CA in the container was considered and rejected for v1:
// it turns the log from evidence into the thing under test, and hostname-plus-
// transcript coverage caught the leak shape the minimal rig actually observed
// (a raw.githubusercontent.com fetch of the task's own tree).
//
// Derived from the DeepSWE minimal rig's allowlisting proxy (the same hijack
// plumbing and dialer), with the allowlist, its backoff and its refusals
// removed: this proxy's job is to be a window, not a gate.
//
// Build (for the container's architecture, by the rig):
//   GOOS=linux GOARCH=<arch> CGO_ENABLED=0 go build -o bin/egress-proxy-arm64 proxy/egress-proxy.go
// Run:
//   egress-proxy -addr :3128 -log /logs/egress-proxy.log
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	permitted atomic.Int64
	// dialer carries the resolver the proxy trusts. The task container sits on
	// an internal Docker network and reaches the proxy by name; hostnames the
	// agent itself resolves are sent to this proxy inside CONNECT, and the
	// proxy — the only process with a route out — resolves them itself.
	dialer = &net.Dialer{Timeout: 30 * time.Second}
)

// useResolver points the proxy's own lookups at addr ("1.1.1.1:53") instead of
// whatever /etc/resolv.conf says, which on an internal network is the
// container runtime's own resolver and cannot see outside.
func useResolver(addr string) {
	if addr == "" {
		return
	}
	dialer.Resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, network, addr)
		},
	}
	log.Printf("[egress] resolver: %s", addr)
}

// logline is one permitted connection, timestamped, the record the scanner
// reads. Hostname level: a CONNECT's target host with its port, or the method
// and URL of a plain request.
func logline(logf *log.Logger, kind, target string) {
	permitted.Add(1)
	logf.Printf("%s %s", kind, target)
	log.Printf("[egress] allow %s %s", kind, target)
}

func main() {
	addr := flag.String("addr", ":3128", "listen address")
	logfile := flag.String("log", "", "append-only connection log (default: stdout only)")
	resolver := flag.String("resolver", "", "upstream DNS for the proxy's own lookups, e.g. 1.1.1.1:53")
	flag.Parse()
	useResolver(*resolver)

	var logf *log.Logger
	if *logfile != "" {
		file, err := os.OpenFile(*logfile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Fatalf("[egress] open log: %v", err)
		}
		defer file.Close()
		logf = log.New(file, "", log.LstdFlags)
	} else {
		logf = log.New(os.Stdout, "", log.LstdFlags)
	}
	log.Printf("[egress] policy: open and logged — every connection is permitted and written down")

	go func() {
		for range time.Tick(60 * time.Second) {
			log.Printf("[egress] permitted=%d", permitted.Load())
		}
	}()

	// The totals on the way out: the periodic line is a heartbeat, so the
	// exact count is written at exit for the run's provenance.
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
		<-stop
		log.Printf("[egress] TOTAL permitted=%d", permitted.Load())
		os.Exit(0)
	}()

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
	}

	server := &http.Server{
		Addr:              *addr,
		ReadHeaderTimeout: 30 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				upstream, err := dialer.DialContext(r.Context(), "tcp", r.Host)
				if err != nil {
					// A permitted connection that cannot be dialed is a
					// network event, not a refusal — recorded as such.
					logf.Printf("FAIL %s %s", r.Host, err)
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					upstream.Close()
					http.Error(w, "hijack unsupported", http.StatusInternalServerError)
					return
				}
				client, _, err := hijacker.Hijack()
				if err != nil {
					upstream.Close()
					return
				}
				logline(logf, "CONNECT", r.Host)
				_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				go func() { defer upstream.Close(); _, _ = io.Copy(upstream, client) }()
				go func() { defer client.Close(); _, _ = io.Copy(client, upstream) }()
				return
			}
			// Plain (non-tunnelled) proxying: the URL is visible, so it is
			// logged with its path, which is stronger evidence than a host.
			r.RequestURI = ""
			resp, err := transport.RoundTrip(r)
			if err != nil {
				logf.Printf("FAIL %s %s", r.URL.Host, err)
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			logline(logf, strings.ToUpper(r.Method), r.URL.String())
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
		}),
	}
	log.Printf("[egress] listening on %s", *addr)
	if err := server.ListenAndServe(); err != nil {
		log.Printf("[egress] %v", err)
		os.Exit(1)
	}
}