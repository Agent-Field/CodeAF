package relayserve

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/Agent-Field/codeaf/internal/relay"
)

// Main is the relay program, shared by cmd/relay and `codeaf relay`:
//
//	relay --listen :8787 [--store /var/lib/codeaf] [--quiet] [--status=false]
//
// It runs until SIGINT or SIGTERM and then lets in-flight requests finish.
// In front of it in production goes whatever already terminates TLS; the pipe
// reads X-Forwarded-For when it is there, so its rate limits count the caller.
func Main(args []string) error {
	flags := flag.NewFlagSet("relay", flag.ContinueOnError)
	listen := flags.String("listen", ":8787", "address to listen on")
	store := flags.String("store", "", "directory that keeps the directory and blob stores; empty runs the blind pipe only")
	quiet := flags.Bool("quiet", false, "do not log arrivals, departures and requests")
	status := flags.Bool("status", true, "answer GET /status with what is connected right now")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg := Config{Store: *store, Status: *status}
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
	svc := New(cfg)
	defer svc.Close()
	log.Printf("relay on %s, speaking %s", ln.Addr(), relay.Protocol)
	if err := Serve(ctx, ln, svc.Handler); err != nil {
		return fmt.Errorf("relay: %w", err)
	}
	return nil
}
