---
kind: fixed
title: a background reading over a connection lets go of its latch when it dies, and says so once
pr: 396
surface: [chat, remote]
invalidates:
  - "The host door's four background readings (places, spending, memory, standing) each kept their own `fetching` and `read` fields and released the latch inside the trip, so a panic froze the page for the life of the process. They now share one `hostDuty` (cmd/aforge/chatv3_host_duty.go) whose release is deferred under the trip; the next beat asks again."
  - "`hostOptions` built with a nil client used to fire three goroutines into it and log three recovered nil dereferences per test run. A duty armed through `hostFar` with no client never starts; the door tests dial `remote.Loopback` against a real in-process engine instead of passing nil."
  - "A recovered panic in a background reading was silent on the surface. The first one now puts one sentence on the connection's one-off notice line — `reading what has been spent over this connection fell over once and will be tried again` — and the manual page *When the connection drops* documents it."
---

The constructors `newHostWorld`, `newHostLedger`, `newHostMemory` and `newHostStanding`
take a `hostFar` rather than a `*remote.Client`; the client is still the one door, but
the fact "there is a connection behind this" is established once, there, rather than
discovered on the wire by a goroutine.
