# UX pairing contract (link pairing, device records, presence)

Status: fixed. Every UX lane builds against this file. All changes are additive. A client that knows
nothing of this file keeps working: the six-digit code path (`/v1/pair`, internal/pairbox,
internal/pair), `{"v":N}` watch frames, `POST /v1/dir/devices/{id}/revoke` and the existing
`directory.Device` JSON all stay as they are.

Words in UI strings follow the vocabulary law: "device", "Move here", "Continue here". The words in
this file (relay, node, lease, manifest) never reach a screen.

Conventions: JSON bodies; errors are `{"err":"<name>"}` with the status given (the relay's existing
shape, `wire.js`). Times are Unix milliseconds. `b64u` is unpadded base64url. Every answer carries
`Codeaf-Now` as today. Signed routes use the existing identity/device cert headers and are called
"signed"; routes marked "open" take no identity (the new device has none yet).

## 1. Pending request record

A new device asks to join by creating a request. The relay keeps it for 10 minutes in one new Durable
Object, `LinkGate` (global decisions and per-IP counters, like `PairGate`), plus one `LinkRequest`
object per code (like `Mailbox`).

```json
{
  "code": "k7m2q9xd",
  "device": "dev_3Fq9...",
  "pubkey": "b64u(32 bytes ed25519 device public key)",
  "x25519": "b64u(32 bytes, key the grant is sealed to)",
  "name_sealed": "b64u(secretbox of the UTF-8 device name under the link key)",
  "platform": "linux",
  "check": "4821",
  "requested_at": 1790000000000,
  "expires_at": 1790000600000,
  "state": "pending",
  "grant": null
}
```

- `code`: 8 characters of Crockford base32 (no I, L, O, U; lowercase accepted, upper-cased on read),
  40 random bits from `crypto.getRandomValues`. It is a lookup key, not a secret that protects keys.
- `device`: `dev_` id derived from `pubkey` exactly as today; the relay recomputes it and refuses a
  mismatch (`bad_request`).
- `platform`: one of `darwin`, `linux`, `windows`, `ios`, `android`, `other`. Unknown values are stored
  as `other`. Plain text; it is the platform icon on the approve screen.
- `name_sealed`: the name is hidden from the relay. It is sealed (NaCl secretbox, 24-byte nonce
  prepended) under the 16-byte link key `k`, which lives only in the URL fragment (section 2) and is
  never sent to any server. At most 96 bytes of name before sealing.
- `check`: four decimal digits, `uint16_be(sha256(pubkey)[0:2]) mod 10000`, zero padded. The new
  device shows it; the approver compares it. It makes a substituted public key visible.
- `expires_at = requested_at + 600000` (10 min, `limits.pairTtlMs`). After expiry every read is `404 gone`.
- `state`: `pending` | `approved` | `denied`. Moves once, only from `pending` (single use). A request is
  never revived; a new attempt is a new code.
- `grant`: `null` until approved. After `approved` it is `b64u(sealed box)` sealed to `x25519`; it holds
  what the new device needs (cert, identity material, vault key). The relay stores it as opaque bytes
  (4 KiB cap, `too_big`) and never reads it. A `denied` request has `grant: null`.
- After a decision the record lives 2 more minutes (so the new device can read it), then it is deleted.

Wire form for the `device` object written into the directory on approval is section 4.

## 2. Short link and mapping to the existing code

```
https://codeaf.agentfield.ai/p/<code>#<k>
```

`<k>` is `b64u(16 bytes)`, the link key of section 1. `codeaf.agentfield.ai/p/*` is a redirect/handoff page: it
sends the visitor to the installed app (`codeaf://pair?code=<code>#<k>`), or shows install and CLI steps.
The fragment never leaves the browser.

Printed or typed form, for headless use: `<code>.<k>` (for example `k7m2q9xd.Qm9v...`). The CLI prints
both the link and this token. Paste into any approve box is accepted in either shape.

Existing path, unchanged: a six-digit code with a nameplate (`42-715-302`) goes through `/v1/pair` and
the CPace exchange in internal/pair. The two paths are siblings, chosen by the shape of what is
entered:

| Entered text | Path |
|---|---|
| `^\d{1,4}-\d{3}-\d{3}$` or `\d{3} \d{3}` | existing pairbox + CPace, no change |
| `^[0-9a-hjkmnp-tv-z]{8}([.#].+)?$` (case-insensitive) or a `codeaf.agentfield.ai/p/` URL (the old `codeaf.link` host is not a codeaf link and is answered as such) | link pairing, this file |

There is no secret in the short code, so link pairing has two guards instead of the PAKE: a human
approves on an already-paired device, and the approver sees `check` and the name. The grant is sealed
to the new device key, so the relay and anyone who reads the request learn nothing usable.

## 3. Routes

All paths are under the hosted relay origin. No route removes or changes an existing one.

| # | Method and path | Auth | Purpose |
|---|---|---|---|
| 1 | `POST /v1/link/requests` | open | create a request |
| 2 | `GET /v1/link/requests/{code}` | open | read one (approver and new device) |
| 3 | `POST /v1/dir/requests/{code}/approve` | signed | approve |
| 4 | `POST /v1/dir/requests/{code}/deny` | signed | deny |
| 5 | `GET /v1/dir/presence` | signed | who is online now |
| 6 | `GET /v1/link/limits` | open | numbers below, for clients |

Revoke is unchanged: `POST /v1/dir/devices/{id}/revoke` (204; revoked keys are refused as today).

### 3.1 Create: `POST /v1/link/requests` (open)

Request:

```json
{ "pubkey": "b64u", "x25519": "b64u", "name_sealed": "b64u", "platform": "linux" }
```

Answer `201`:

```json
{ "code": "k7m2q9xd", "device": "dev_3Fq9...", "check": "4821",
  "requested_at": 1790000000000, "expires_at": 1790000600000 }
```

The client builds the link from `code` and its own `k`. Errors:

| Status | err | When |
|---|---|---|
| 400 | `bad_request` | missing field, wrong key length, `device` mismatch, name over 96 bytes |
| 413 | `too_big` | body over 1 KiB |
| 429 | `rate_limited` | over 10 creates per hour per IP, or 3 pending requests at once per IP (`Retry-After` set) |
| 503 | `full` | 2000 live requests on the relay |

### 3.2 Read: `GET /v1/link/requests/{code}` (open)

Answer `200` is the record of section 1 with `state`, `grant`, `check`, `name_sealed`, `platform`,
`requested_at`, `expires_at`, `device`, `pubkey`, `x25519`. Query `?wait=<ms>` (max 25000) long-polls
until `state` leaves `pending`, answering `204` if the wait ends first (same behaviour as the mailbox
read). The new device uses it to wait for approval; the approver uses it once, without `wait`.

| Status | err | When |
|---|---|---|
| 404 | `gone` | unknown, expired or deleted code (these are one answer on purpose) |
| 429 | `rate_limited` | over 60 reads per minute per IP, or over 20 misses per minute per IP, or 4 open polls per IP |

### 3.3 Approve: `POST /v1/dir/requests/{code}/approve` (signed)

Called by an already-paired device of the identity that is approving.

```json
{
  "device": {
    "V": 1, "name": "b64 sealed under the metadata key", "added_by": "id_...",
    "revoked": false, "caps": { "os": "linux", "arch": "arm64", "sandbox": "landlock",
      "container": null, "gpu": null, "cow": "btrfs" },
    "platform": "linux", "created": 1790000123456, "last_seen": 0
  },
  "cert": "b64u device cert for `dev_...` signed by the identity key (existing cert format)",
  "grant": "b64u(sealed box to x25519)"
}
```

Effect, in this order and in one turn of the identity object: verify `cert` is for the request's
`device`; write the Device record (as `PUT /v1/dir/devices/{id}` would, `created` set by the relay to
directory time); bump the directory version (watchers hear `{"v":N}`); emit `joined` (section 5); set
the request `approved` with `grant`. A retry of the same approve with the same `device` is `204`
(idempotent). Answer `204`.

| Status | err | When |
|---|---|---|
| 400 | `bad_request` | `device`/`cert` do not match the request |
| 401 | `unauthorized` | not signed, or signer revoked (existing causes) |
| 404 | `gone` | no such live request |
| 409 | `already_decided` | request is `approved` for another body, or `denied` |
| 410 | `rotated` | identity replaced (existing) |
| 413 | `too_big` | `grant` over 4 KiB |
| 429 | `rate_limited` | existing per-device limits |

### 3.4 Deny: `POST /v1/dir/requests/{code}/deny` (signed)

Empty body. Sets `denied`; the waiting device reads `state:"denied"` and shows "Request declined".
Answer `204`. Errors: `401 unauthorized`, `404 gone`, `409 already_decided` (a repeat deny is `204`).
Any paired device may approve or deny; the first decision wins.

### 3.5 Presence list: `GET /v1/dir/presence` (signed)

```json
{ "now": 1790000001000,
  "devices": {
    "dev_A": { "online": true,  "last_seen": 1790000001000 },
    "dev_B": { "online": false, "last_seen": 1789999000000 } } }
```

One entry per non-revoked device in the directory. Cost: reads socket state only, no storage write.
Answers with `Codeaf-Dir-Version` like `/v1/dir/list`. Used at app open and as the fallback for a client
whose socket is down.

### 3.6 Limits: `GET /v1/link/limits` (open)

```json
{ "ttl_ms": 600000, "create_per_hour": 10, "pending_per_ip": 3, "read_per_minute": 60,
  "miss_per_minute": 20, "concurrent_polls": 4, "max_grant": 4096, "max_live": 2000 }
```

New keys in `limits.js` `DEFAULTS` (override via `CAF_LIMITS` as today): `linkCreatePerHour` 10,
`linkPendingPerIp` 3, `linkReadPerMinute` 60, `linkMissPerMinute` 20, `linkConcurrentPolls` 4,
`linkMaxGrant` 4096, `linkMaxLive` 2000. `pairTtlMs` is reused for the 10 minutes.

## 4. Device record (additive fields)

`directory.Device` gains three fields. Go (`internal/directory/records.go`):

```go
Platform string `json:"platform,omitempty"`  // darwin|linux|windows|ios|android|other
Created  int64  `json:"created,omitempty"`   // directory ms when the device joined; set by the relay
LastSeen int64  `json:"last_seen,omitempty"` // directory ms of the last watch socket close or hello
```

Full JSON:

```json
{ "V": 1, "name": "b64", "added_by": "id_...", "revoked": false,
  "caps": { "os": "linux", "arch": "arm64", "sandbox": "landlock", "container": null, "gpu": null, "cow": "btrfs" },
  "platform": "linux", "created": 1790000123456, "last_seen": 1790000999000 }
```

Rules:
- `V` stays 1. Old readers ignore unknown fields; old records decode with zero values, shown as
  "unknown" platform and no time. A device written without the fields is valid.
- `created` is set by the relay on first write of a device id and never changes afterward; a client
  value is ignored.
- `last_seen` is set only by the relay: at watch connect (hello) and at watch close. It is not written
  per ping, so an open quiet socket costs no storage write. A client value is ignored.
- `platform` is client-supplied, same value set as section 1, written by self-registration or approve.
- Rename and `caps` changes keep using `PUT /v1/dir/devices/{id}` (self only). `Revoked` is still set
  only by revoke.
- Writing `last_seen` bumps no directory version and sends no `{"v":N}` frame.

## 5. Watch frames

Today a watch socket gets only `{"v":N}` (version). That does not change for a socket opened as today.

A client opts in to events with a query flag: `GET /v1/dir/watch?events=1` (plus existing `hold`
parameters). A socket without the flag never receives an event frame, so old clients cannot be
confused. With the flag the socket receives `{"v":N}` frames exactly as before, and in addition event
frames. An event frame has a string key `t` and no key `v`:

```json
{"t":"joined","device":"dev_B","name":"b64 sealed","platform":"linux","at":1790000123456}
{"t":"presence","device":"dev_B","online":true,"at":1790000123999}
{"t":"presence","device":"dev_B","online":false,"at":1790000555000}
{"t":"revoked","device":"dev_B","at":1790000700000}
```

- `joined`: sent to every open event socket of the identity (the new device itself excluded) in the same
  turn as the approve. The directory version also moves, so a client that only lists still sees it.
  The UI string is "<name> is now paired", decoded from `name` with the metadata key.
- `presence`: sent to the other event sockets when the first socket of a device opens (`online:true`)
  and when its last socket closes (`online:false`). A device is online iff it holds at least one open
  watch socket. A flapping device is debounced: an `online:false` is sent 15 s after the last socket
  closes unless a socket opens first, and no `online:true` is sent for a reconnect inside that gap.
  The close 15 s timer is a Durable Object alarm, set only when the last socket closes.
- On connect an event socket first gets `{"v":N}` then one `presence` frame per other online device, so
  it needs no `/v1/dir/presence` call at start. `presence` frames for offline devices are not sent on
  connect; use the list for `last_seen`.
- `revoked`: informational; the close `4401` for the revoked device itself is unchanged.
- Event frames are at most 512 bytes and are never queued for a socket that is gone. Clients must
  ignore frames with an unknown `t`.

## 6. Security properties (must hold in every implementation)

1. The relay never holds an unsealed device name, a grant's contents, or the link key.
2. Approval needs a signed call by a non-revoked device of the identity. Open routes can only create,
   read and wait.
3. A request decides once. Two approvers racing: one gets `204`, the other `409 already_decided`.
4. Code guessing is bounded: 40 bits, 10 min, 20 misses per minute per IP, and a guessed code reveals a
   sealed name, a platform and a public key only.
5. Revoked device ids are refused at the relay as today; a revoked device re-joins only as a new
   request and a new device id.

## 7. Errors added

`already_decided` (409). Reused with the existing meaning: `bad_request` 400, `unauthorized` 401,
`gone` 404, `too_big` 413 (here, `too_large` for directory bodies stays), `rate_limited` 429, `full` 503,
`rotated` 410.

## 7a. Nameplates and generations (`/v1/pair`)

A nameplate is a short number, so it is drawn again some day. Two rules keep a device that still holds an
old code from being answered by a newer mailbox that landed on the same nameplate.

1. **Quarantine.** A relay does not draw a nameplate again until `plateQuarantineMs` after its mailbox ended
   (released or expired). The hosted relay's default is one pairing life (`pairTtlMs`, 10 minutes): a device
   may come back to a code until the code's life ends, and a poll that starts during the quarantine finds the
   old mailbox gone. Quarantined plates count as occupied when the nameplate length (2 to 4 digits) is chosen.
2. **Generation.** Each opening of a mailbox has a random generation, 32 hex characters, sent in the header
   `Codeaf-Pair-Gen` on every answer about that mailbox (create, post, poll). A device sends back the one it
   was told, on post, poll and delete. A mailbox that is a different opening answers `404 gone`, the same
   answer as for a mailbox that is not there.

The header is optional both ways. A request with none is not fenced, and a mailbox opened before generations
existed, or a relay that names none (the self-hosted one), answers without it; a device then sends none. No
body, status or word changes, so old and new builds keep working together. A joining device learns the
generation from its first answer, so it is fenced from its second call on.

## 8. Test vectors

`docs/testdata/ux-pairing-vectors.json` holds the record shapes above as fixtures for the lanes
(request, approve body, presence answer, the four event frames, an old-style `{"v":N}` frame, and a
legacy Device record without the new fields).
