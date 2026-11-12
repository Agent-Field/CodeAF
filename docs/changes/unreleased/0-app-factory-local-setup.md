---
kind: added
title: The app can set up a local factory and explicitly enable its runner
pr: 0
surface: [engine]
invalidates: []
---

The typed factory app protocol can start or adopt this computer's factory,
connect GitHub, select repositories, read issues into Backlog, and clone or
skip checkouts. Setup starts no runner or job. A separate explicit runner
request uses the existing worker lifecycle and app credential custody.
Remote attachments survive local factory start and shutdown.

The local factory keeps one id, `factory_local`, across launches, and a service
started by an explicit `factory.local_start` is served again, quietly, when the
app next lists it: same queue, jobs and checkouts; new port and bearer; no
runner, job, model or GitHub request. Detaching the row, or never having
started one, resumes nothing; another codeaf's service is never joined by a
resume. Before this, quitting forgot the row and its address-derived id, so a
finished profile opened to "No factory yet".
Quitting keeps the local row (no credential) whether or not it resumes; only
detaching removes it. Attaching, joining by access link or adopting the address
the local factory already has is that same row: no second connection, and its
Resume choice is untouched. Address-only attach keeps the held bearer; a
completed explicit sign-in replaces it with the verified caller grant.
When saved rows share an address, manual connection metadata wins over a
conflicting local marker. Starting a local service never overwrites a manual
connection at the same address; it reports the conflict instead.
