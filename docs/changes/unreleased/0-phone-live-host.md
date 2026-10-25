---
kind: added
title: A running app pairs phones with chat and crew snapshots plus relay push
surface: [app, engine]
invalidates:
  - "Phone grants omitted crews and clients had to repeatedly fetch snapshots. New grants include crew reads and queued writes, and the relay supports cancellable long-polls for changed slot names. Existing grants retain their original permissions."
  - "A running codeaf app could not export a phone pairing grant, and it could not open a durable ask without a model call. `codeaf phone pair` exports the grant, and `testing.open_ask` opens the ask when CODEAF_PHONE_FIXTURE=1 is set."
---

`codeaf phone pair` dials the running app and prints the grant. `codeaf app
--phone-fixture` sets `CODEAF_PHONE_FIXTURE=1` and opens one durable ask with
no model call. Without that switch, `testing.open_ask` is an unknown method.


The default phone grant includes `crews.list/get/items/log` and queued
`crews.create/message/decide`. A crew belongs to the executor leasing its manager;
queued mutations check actual chat ownership and irreversible decisions require the
signed Face ID assertion. Team question stakes survive packet escalation and reach
the crew decision ask. Newly started managers are kept and leased before launch.
Crew changes wake the scoped snapshot publisher.

`GET /v1/clients/{id}/slots?since=<version>&wait=25` returns a durable per-client
version and changed slot names after every successful PUT, or 204 at timeout.
Waits release mutation locks, cancel on disconnect, and are bounded to two per
client and 1,024 overall. Versions survive restarts; slots still expire after 24 hours.
