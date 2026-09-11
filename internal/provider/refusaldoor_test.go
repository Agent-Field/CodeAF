package provider

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// ── ONE REFUSAL DOOR, TWO TRANSPORTS, ONE TABLE ─────────────────────────────
//
// The measurement these rows are written from is 2026-09-10: a chat turn on
// deepseek/deepseek-v4.1-flash drew three 429s served by Io Net at 13:00:39,
// 13:01:04 and 13:01:32, every one of them delivered INSIDE an already-open
// HTTP 200 stream. The pacing note lived on the status path alone, so none of
// the three reached the ledger and the router handed the request straight back
// to the same saturated pool.
//
// THE ROWS ARE SHARED BETWEEN THE TWO TRANSPORTS ON PURPOSE. A refusal is a
// fact about a machine, and where it was read — before the headers or after
// them — may not change what is done about it. One table, both paths, so the
// day they disagree again this test says so (velocity.go's
// [Client.refuseLane]).

// refusalArrival is one 429 as a wire delivers it.
type refusalArrival struct {
	// name is what a failure message calls this row.
	name string
	// serving is the endpoint a chunk of the stream announced it was being
	// served by, empty when no chunk named one. Only the streamed half has it:
	// a status arrives before any chunk does.
	serving string
	// named is the pool the error object itself named in
	// `error.metadata.provider_name`, empty when the router named nobody.
	named string
}

// paced is the machine this arrival implicates on a given transport, empty when
// it implicates none.
//
// WHOEVER THE WIRE NAMED IS WHO IS PACED, and a stream naming its provider in
// the chunks is the wire naming somebody just as much as `provider_name` is.
// That equivalence is the whole fix, so it is written here rather than left
// implicit in two lists of expectations.
func (a refusalArrival) paced(streamed bool) string {
	if a.named != "" {
		return a.named
	}
	if streamed {
		return a.serving
	}
	return ""
}

// frame is the error object of this arrival, in the shape OpenRouter sends: a
// `code`, a `message`, and the pool's name in the metadata when there is one.
func (a refusalArrival) frame() string {
	if a.named == "" {
		return `{"code":429,"message":"Provider returned error"}`
	}
	return fmt.Sprintf(`{"code":429,"message":"Provider returned error",`+
		`"metadata":{"provider_name":%q,"raw":"rate limited upstream"}}`, a.named)
}

// refusalArrivals is the table. Every row is run twice: once as an HTTP status
// and once inside an open stream.
var refusalArrivals = []refusalArrival{
	{
		// THE PIN BETWEEN THE TWO PATHS: one refusal, both transports, one
		// outcome.
		name:    "the router names the pool that refused",
		serving: "Io Net",
		named:   "Io Net",
	},
	{
		// THE MEASURED ROW. The router omits `provider_name` on a refusal
		// delivered mid-stream, because the stream already named its provider
		// in the chunks — which is exactly the reading the status path had and
		// the stream path did not. Its status twin names nobody at all, and
		// paces nobody, which is the same law read on the other transport.
		name:    "only the stream named who was serving",
		serving: "Io Net",
	},
	{
		// AN ACCOUNT-WIDE 429 IMPLICATES NO MACHINE. Nobody is written off,
		// nothing is routed around, and the call waits it out as it always did:
		// asking a second machine about this account's own ceiling would
		// multiply the traffic that earned it.
		name: "nobody is named at all",
	},
}

// doorClient is a client whose ledger is watched and whose waits are free.
func doorClient(t *testing.T, handler http.Handler) (*Client, *capture) {
	t.Helper()
	client, recorded := pinningClient(t, StaticRouting(RoutingLatency), 0, 0, false, handler)
	client.velocity = newVelocityLedger()
	primed(t, client.config.Model)
	// The waits themselves are retry.go's business and are proved there; this
	// file is about what the ledger knows once they are spent.
	client.wait = func(context.Context, time.Duration) error { return nil }
	return client, recorded
}

func TestAPacedPoolIsRoutedAroundHoweverThe429Arrived(t *testing.T) {
	for _, arrival := range refusalArrivals {
		for _, streamed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", arrival.name, streamed), func(t *testing.T) {
				var refusing atomic.Bool
				refusing.Store(true)
				client, recorded := doorClient(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					if !refusing.Load() {
						writer.Header().Set("Content-Type", "application/json")
						fmt.Fprint(writer, answerFrom("healthy", 100, 0, 0.001))
						return
					}
					if !streamed {
						writer.Header().Set("Content-Type", "application/json")
						writer.WriteHeader(http.StatusTooManyRequests)
						fmt.Fprintf(writer, `{"error":%s}`, arrival.frame())
						return
					}
					// A 200 THAT BECOMES A REFUSAL: the headers landed, a chunk
					// named the machine that was serving, and then the error
					// object arrived in the body of the stream.
					writer.Header().Set("Content-Type", "text/event-stream")
					if arrival.serving != "" {
						fmt.Fprintf(writer, "data: {\"provider\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"th\"}}]}\n\n", arrival.serving)
					}
					fmt.Fprintf(writer, "data: {\"error\":%s}\n\ndata: [DONE]\n\n", arrival.frame())
				}))

				ctx := lineage("refusal-door")
				if streamed {
					ctx = WithProseAnswer(ctx)
					ctx = WithStreamObserver(ctx, func(StreamEvent) {})
				}
				_, err := client.CompleteWithMessages(ctx, userMessages("hello"))
				if err == nil {
					t.Fatal("a 429 on every attempt should have ended the call")
				}
				// THE REFUSAL CARRIES THE SAME TWO FACTS EITHER WAY — the status
				// it wore and the machine it came from — because the door stamps
				// the serving name on before anything reads it. internal/taxonomy
				// builds its Evidence out of exactly these two fields, so a
				// stream's refusal that reached it unnamed was a different fact
				// there too.
				refusal, ok := RefusalFrom(err)
				if !ok {
					t.Fatalf("the call ended with %v, which carries no refusal to read", err)
				}
				if refusal.Status != http.StatusTooManyRequests {
					t.Fatalf("refusal status = %d, want 429", refusal.Status)
				}
				if want := arrival.paced(streamed); refusal.Provider != want {
					t.Fatalf("refusal provider = %q, want %q", refusal.Provider, want)
				}

				refusing.Store(false)
				next := len(recorded.bodies)
				if _, err := client.CompleteWithMessages(lineage("refusal-door"), userMessages("again")); err != nil {
					t.Fatalf("the second call should have landed: %v", err)
				}
				ignored := words(prefsOn(t, recorded, next)["ignore"])
				want := arrival.paced(streamed)
				if want == "" {
					if len(ignored) != 0 {
						t.Fatalf("ignore = %v, want a 429 that named nobody to implicate no machine", ignored)
					}
					return
				}
				if !namesEndpoint(ignored, want) {
					t.Fatalf("ignore = %v, want the next request to route around %q", ignored, want)
				}
				if order := orderOn(t, recorded, next); namesEndpoint(order, want) {
					t.Fatalf("order = %v, want the paced pool off the preference", order)
				}
			})
		}
	}
}
