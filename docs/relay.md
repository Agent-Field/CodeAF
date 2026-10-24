# Running your own relay

The relay is the service your computers sync and pair through. It stores ciphertext and
sees the short list of metadata in the manual page "What the relay stores and what it can
see". This page is for the person who runs one.

## Run

```
codeaf relay --listen :8787 --store /var/lib/codeaf
```

`relay --listen :8787 --store /var/lib/codeaf` (the separate `relay` binary) is the same program.

| Flag | Meaning |
|---|---|
| `--listen` | address to listen on, default `:8787` |
| `--store <dir>` | where the directory and the chunks live. Empty runs the blind pipe and pairing only: no chats are kept |
| `--quiet` | no log lines |
| `--status=false` | do not answer `GET /status` |
| `--trust-proxy` | count the first `X-Forwarded-For` address as the caller's network in the pairing limits. Only behind a proxy that sets it |

Point each computer at it with `CODEAF_SYNC_URL=https://relay.example.com`, or pass
`--relay` to `codeaf pair`.

The relay has no TLS and no allow-list. Put a TLS-terminating proxy in front of it, and keep
it on a private network or behind a proxy that checks who calls: any valid signature may make
an identity and store on it. Coordination rows have an 8 MiB default admission ceiling;
a larger imported identity keeps its measured ceiling. Reaching that ceiling refuses
growth without deleting saved records. Sealed blobs still need operator disk capacity.

It logs the first 8 characters of an identity id, the kind of request, the status and two
byte counts. Never a body.

## Back up

Everything is under `--store`:

- `raft/<identity id>/{1,2,3}.db`: three independent FULL-synchronous WAL stores for coordination;
- `raft/<identity id>/provision.json`: the immutable cluster ID and private protocol keys;
- `directory/<identity id>.db`: the preserved frozen source and migration ownership receipt;
- `blobs/<identity id>/`: that identity's chunks and the pointers that find an object inside one.

Stop the relay (SIGTERM; requests under way finish) and copy the whole folder, or copy a
filesystem snapshot. Copying the `.db` files while the relay runs can give a torn copy.
Restart using the same complete store. Never replace a surviving member with a fresh
import under its old ID. Two actual surviving anchor stores can coordinate; fewer than
two refuse service. Keep damaged or unavailable stores for diagnosis and recovery.

A stale backup is not an ordinary restart. Do not start it beside a live cluster or
reactivate its frozen source. Disaster recovery needs a new epoch and exclusion of the
old cluster; automatic stale-backup recovery is not offered by this relay.

The relay cannot decrypt what it keeps, so its backup is ciphertext too. It holds no key that
could recover a lost identity. Protocol keys cannot decrypt chats, but keep the store
private and back up its provisioning file together with its databases.

## Upgrade

Stop it, replace the `codeaf` binary, start it with the same `--store`. Back up
first. There is no migration command or feature switch. First use freezes an
existing identity, imports the same rows into three local anchors, checks a
fresh majority and publishes one durable owner. Interrupted imports resume with
the same UUID, keys and ceiling; they never reopen the legacy writer. The
original source stays as a backup until the identity is retired.

All directory, rotation and blob authority requests use consensus. Three local voters
still share one host and disk: losing that host loses service. This setup does not
claim independent host failure tolerance. The current v1 adapter bounds encoded
commands at 64 KiB; a legal older request with a very large frame plan can still be
refused before proposal. Larger v1 requests are not supported yet.
A client newer than the relay by a pairing change sees `that relay is too old for pairing`.

## The hosted relay

`relay/hosted/` is the same relay as a Cloudflare Worker with Durable Objects and R2, with
per-identity limits (5 GiB, rate limits, a daily cap on new identities per network, set in `relay/hosted/src/limits.js`). Its own
README says how to deploy it to an account. Nothing in this repository deploys it.

codeaf's own deployment is called the fabric and is served at `https://codeaf.agentfield.ai/fabric`,
the address every computer syncs through when nobody names another. It sees only ciphertext, the
time and size of each request, and the network address of each computer; it keeps no request log.
The address has a path, and every client keeps it. `codeaf relay` (this page) always serves from
the root of its host; behind your own proxy, pass the path through unchanged.
