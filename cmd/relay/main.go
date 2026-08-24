// Command relay is the aforge relay: a blind pipe between a machine that runs
// aforge and a machine somebody is sitting at.
//
// IT IS A SEPARATE BINARY BECAUSE IT IS A SEPARATE THING. Nothing in it imports
// the session, the surface, or the wire they speak; it cannot open a frame and
// has nothing to open one with. Building it apart from `aforge` is how that
// stays true — a relay that linked the session package would be one careless
// import away from being able to read what it forwards.
//
// It is also what internal/pair's own tests drive, which is the other reason it
// exists: the end-to-end story — a machine registering, a device pairing, a
// conversation crossing — is tested against this exact service and not against
// a stub of it.
//
//	relay --listen :8787
//
// In front of it in production goes whatever already terminates TLS. The relay
// reads X-Forwarded-For when it is there, so its rate limits count the caller
// and not the load balancer.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/relay"
)

func main() {
	listen := flag.String("listen", ":8787", "address to listen on")
	quiet := flag.Bool("quiet", false, "do not log arrivals and departures")
	status := flag.Bool("status", true, "answer GET /status with what is connected right now")
	flag.Parse()

	service := &relay.Server{}
	if !*quiet {
		// WHAT A RELAY OPERATOR MAY LOG IS WHAT A RELAY OPERATOR CAN SEE, and
		// that is a machine name and a moment. There is no payload to log here
		// and no argument in this file that could carry one.
		service.Note = func(name, what string) { log.Printf("%s %s", name, what) }
	}

	mux := http.NewServeMux()
	mux.Handle(relay.EnginePath, service)
	mux.Handle(relay.DialPrefix, service)
	if *status {
		mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(service.Ledgers())
		})
	}

	server := &http.Server{
		Addr:    *listen,
		Handler: mux,
		// READ AND WRITE TIMEOUTS ARE DELIBERATELY ABSENT. Every connection
		// this service accepts is meant to be held open for hours, and a write
		// timeout on a hijacked connection is a conversation cut in half.
		// The bounds that matter are in the package: a registration must finish
		// its handshake within relay.HandshakeWithin, and a carrier that stops
		// answering pings is dropped at relay.IdleAfter.
		ReadHeaderTimeout: 20 * time.Second,
	}
	log.Printf("relay on %s, speaking %s", *listen, relay.Protocol)
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "relay:", err)
		os.Exit(1)
	}
}
