---
kind: added
title: App wire contracts publish generated TypeScript types
pr: 1800
surface: [engine, build, docs]
invalidates:
  - "The app contract published only JSON Schemas and left TypeScript shapes to the app. go generate ./internal/appapi now publishes wire.ts with registered Go types, method/path request and response maps, and discriminated event payloads."
  - "Schema freshness ran only with the generator package tests. Schema and TypeScript freshness now run in the light contract laws, so make test-quick rejects stale published artifacts."
---

A16 publishes a self-contained file for the app to pin to its bundled binary
commit. Optional updates, nullable required collections, exact money strings,
timestamps, raw JSON and embedded fields retain their wire meaning. Strict app
compiler fixtures cover accepted shapes and rejected routes, enums, money and
event payloads; the compiler is optional on Go-only builders. No resource
handler or shared request/response/event type changes in this lane.
