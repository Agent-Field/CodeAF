package relayserve

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// Main is the relay program, shared by cmd/relay and `codeaf relay`:
//
//	relay --listen :8787 [--store /var/lib/codeaf] [--quiet] [--status=false] [--trust-proxy]
//
// It runs until SIGINT or SIGTERM and then lets in-flight requests finish.
// In front of it in production goes whatever already terminates TLS; pass --trust-proxy
// so registration, dial and pairing limits count callers behind that proxy.
func Main(args []string) error {
	flags := flag.NewFlagSet("relay", flag.ContinueOnError)
	flags.Usage = func() { RelayHelp(flags.Output()) }
	listen := flags.String("listen", ":8787", "address to listen on")
	store := flags.String("store", "", "directory that keeps the directory and blob stores; empty runs the blind pipe only")
	quiet := flags.Bool("quiet", false, "do not log arrivals, departures and requests")
	status := flags.Bool("status", true, "answer GET /status with what is connected right now")
	trust := flags.Bool("trust-proxy", false, "count the first X-Forwarded-For address for registration, dial and pairing limits; required behind a proxy that sets it")
	minGrace := flags.Duration("min-grace", 0, "shortest grace period a rotation may ask for (default 1h); a test hook")
	sweepEvery := flags.Duration("sweep-every", 0, "how often replaced identities whose grace has ended are deleted (default 1h); a test hook")
	watchers := flags.Int("max-watchers", 0, "directory watch sockets one identity may hold (default 1000); a test hook")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cfg := Config{Store: *store, Status: *status, TrustProxy: *trust, SweepEvery: *sweepEvery, MaxWatchers: *watchers}
	if *minGrace > 0 {
		cfg.Grace = directory.DefaultGraceBounds
		cfg.Grace.Min = *minGrace
	}
	if !*quiet {
		// A relay operator may log what a relay operator can see: a machine
		// name and a moment, and per request the line note.go describes.
		cfg.Note = func(name, what string) { log.Printf("%s %s", name, what) }
		cfg.Logf = log.Printf
	}
	if *store != "" {
		if err := os.MkdirAll(*store, 0o755); err != nil {
			return err
		}
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintln(os.Stdout, relayStartLine(ln.Addr().String()))
	svc := New(cfg)
	defer svc.Close()
	if err := Serve(ctx, ln, svc.Handler); err != nil {
		return fmt.Errorf("relay: %w", err)
	}
	return nil
}

// RelayHelp hides test-only switches while keeping the ordinary subcommand shape.
func RelayHelp(out io.Writer) {
	fmt.Fprintln(out, `usage: codeaf relay [--listen host:port] [--store path]

Run your own sync service.

  --listen host:port  address to listen on (default :8787)
  --store path        keep your devices and saved chats here; empty connects machines only
  --quiet             do not log arrivals, departures or requests
  --status=false      turn off the service status endpoint
  --trust-proxy       use the caller address supplied by a trusted proxy`)
}

func relayStartLine(address string) string {
	return "codeaf relay is listening on " + address + " — on each computer: codeaf pair --via http://" + address
}
