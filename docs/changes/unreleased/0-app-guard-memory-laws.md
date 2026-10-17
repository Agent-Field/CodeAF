---
kind: fixed
title: App crew updates defer registry unlocks and stop-all work runs under the panic guard
pr: 0
surface: [engine]
invalidates: []
---

Crew registry updates now release their locks through deferred helpers.
Stop-all job and service work uses the existing goroutine guard. Memory scope
counting is split into validation, snapshot reads and revision calculation,
preserving the queries, scope order and revision hash.
