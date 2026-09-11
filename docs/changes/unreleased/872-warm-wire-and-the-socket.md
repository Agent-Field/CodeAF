---
kind: changed
title: A warm wire and a socket that never waits — the reader reads, and a keystroke crosses
pr: 872
surface: [engine, remote]
invalidates:
  - "\"Typing pre-warms the lane.\" It did not, on the road every person is on.
    internal/session's Typing() buys a probe whose second purpose is a warm
    connection, and the surface reaches for it through an optional interface
    that *remote.Agent did not satisfy — so on every launch that is not
    --no-host, --debug, first-run setup or a hostless --once, the assertion
    failed silently and no probe was ever bought from a keystroke. It crosses
    now, as MethodTyping."
  - "\"An ordered call runs on the reader goroutine, and that is what makes it
    ordered.\" Two separate things wearing one name. Ordered calls now run on
    the connection's orderedLane — one goroutine, arrival order — and NOTHING
    runs on the reader. staysOnReader is deleted; callClass.road() has two
    values and neither is the reader. Anything that said Compact or Submit
    \"holds the reader\" is stale: they hold the lane, which is the thing that
    owes the order."
  - "\"A pause between turns is free.\" It cost a handshake. The shared
    transport inherited http.DefaultTransport's 90-second IdleConnTimeout,
    which is shorter than a person reads for, so the next turn paid DNS, TCP
    and TLS inside its own httpClient.Do. It was invisible because ttft_ms
    folds the handshake into the model's wait. The four transport values are
    now stated with a reason each, and the idle window is the router's OWN,
    measured against it from the Spark on 2026-09-11: a connection held idle
    for six minutes still rode the same socket and one held for seven did not.
    An HTTP/2 PING every two minutes keeps it open through fifteen and twenty
    minutes of silence, so the pool also carries a keep-alive rather than
    racing the far end at the edge of its window."
  - "\"A slow ttft_ms means a slow model.\" Not on its own. The finish row now
    carries conn_reused, and dns_ms / connect_ms / tls_ms where the connection
    was opened fresh, so a cold pool and a slow machine are separable. `make
    census` grows a `warm %` and an `opening ms` column on the lane-health
    table. Rows written before this carry none of the four, and the census
    draws nothing rather than a zero."
---

The three findings are one sentence each and they are all about waiting for
something that was not the model. A send held the whole socket shut while the
engine assembled a prompt; a keystroke that was supposed to open the connection
early never reached the engine at all; and the pool threw the connection away
while the person was still reading. `internal/remote/orderedlane.go` is the one
mechanism the first needed, and
`TestEverySurfaceDoorTheEngineHasCrossesTheWire` is the ledger that stops the
second class coming back — twenty-three other doors the local engine answers
and the wire does not are written down in it, and the count may only fall.
