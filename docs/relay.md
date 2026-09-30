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
an identity and store on it. It has no quota of its own, so the disk is the limit.

It logs the first 8 characters of an identity id, the kind of request, the status and two
byte counts. Never a body.

## Back up

Everything is under `--store`:

- `directory/<identity id>.db`: one SQLite file for each identity (chat records, device records, leases);
- `blobs/<identity id>/`: that identity's chunks and the pointers that find an object inside one.

Stop the relay (SIGTERM; requests under way finish) and copy the whole folder, or copy a
filesystem snapshot. Copying the `.db` files while the relay runs can give a torn copy.
Restore by putting the folder back
and starting the relay. A restore to an older copy loses turns sent after it; a computer that
still holds them sends them again at its next sync (not tested here).

The relay cannot decrypt what it keeps, so its backup is ciphertext too. It holds no key that
could recover a lost identity.

## Upgrade

Stop it, replace the `codeaf` binary, start it with the same `--store`. There is no migration command, and this page does not promise that every version reads an older store: back up first. A client newer than the
relay by a pairing change sees `that relay is too old for pairing`.

## The hosted relay

`relay/hosted/` is the same relay as a Cloudflare Worker with Durable Objects and R2, with
per-identity limits (5 GiB, rate limits, 20 new identities per network a day). Its own
README says how to deploy it to an account. Nothing in this repository deploys it.
