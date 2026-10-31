# Hosted relay or your own relay

## Hosted relay or my own — which one do I use, where does sync go by default, and which relay each channel uses

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Older chats move through an independent copy. Once paired, codeaf prepares chats active in the last 30 days in the background, one at a time; opening another older chat prepares its copy then. This build shows and writes to the copy. The original files stay untouched and remain usable in older codeaf builds.

A relay is the small service your computers sync through. There are two kinds. Both speak the same wire: the chat cannot tell them apart.

- **A hosted relay** is run for you on a cloud account. You do nothing to run it. You give the chat its address.
- **Your own relay** is the program `codeaf relay`. You run it on a computer you control.

Either way the relay sees only ciphertext and the short list on "What can the relay see".

**Where sync goes by default.** Unset, each release channel syncs through its own hosted relay: stable releases and release candidates use `https://codeaf.agentfield.ai/fabric`, staging builds use `https://caf-fabric-staging.instrument-santosh.workers.dev`, and dev builds and local builds from source use `https://caf-fabric-dev.instrument-santosh.workers.dev`. Channels do not sync with each other: computers must run the same channel (or set the same `CODEAF_SYNC_URL`) to pair and share chats. A build whose `HostedRelayURL` is empty has no default, and then nothing is copied and pairing says `no sync service is set, so there is nowhere to pair through: set CODEAF_SYNC_URL to its address, or pass --via <address>`. A new home sends no relay requests before completing a pairing or offering a code or link. Reading the add-device instructions does not start sync. Older identities without the quiet marker retain their earlier behavior: see "Is my code sent anywhere before I pair" on the page *Pairing your chats with a second computer*. To use your own relay instead, set `CODEAF_SYNC_URL` as below. Turn sync off with `CODEAF_SYNC_URL=off`; pairing cannot override that with `--via`.

## Point codeaf at a relay — CODEAF_SYNC_URL, url or off

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Set `CODEAF_SYNC_URL` in the environment of every computer that shares the chats.

```
CODEAF_SYNC_URL=https://relay.example.com codeaf
```

- **A web address** like `https://relay.example.com` or `http://host:8787`. Plain `http` is for trying it out inside an ssh tunnel. An address that is not a web address stops with `CODEAF_SYNC_URL is not a web address like http://host:8787`.
- **`off`** turns the relay off. Pairing then says `sync is off (CODEAF_SYNC_URL=off); pairing needs the sync service`. Home still lists every chat on this computer — chats this version saved and chats from older versions alike — each once, and each opens and can be written.
- **Unset:** a computer that was paired with a named relay uses the relay the pairing saved. Otherwise it uses the hosted default above, if this build has one, and has none if it does not.

Your own computers must all name the same relay and hold the same identity, or they see different chats.

## Run my own relay — codeaf relay --listen and --store

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

```
codeaf relay --listen :8787 --store /var/lib/codeaf
```

- `--listen` is the address to listen on. The default is `:8787`.
- `--store <directory>` is where the relay keeps the directory and the chunks. **Without `--store` the relay only passes pairing and remote-access traffic. It keeps no chats, and sync does not work through it.** The directory is made if it is not there.
- `--quiet` turns the log off. `--status=false` turns off `GET /status`.
- `--trust-proxy` makes registration, dial and pairing limits count the first address in `X-Forwarded-For`, falling back to the socket address when absent or blank. without it, forwarding headers are ignored. a relay behind Railway, a load balancer or nginx must pass this flag or every caller shares the proxy's allowance. use it only behind a proxy that sets that header.

Stop it with SIGTERM or ctrl+c: requests under way finish first. **The relay has no TLS of its own.** Put a proxy that ends TLS in front of it if any computer reaches it over a network you do not own.

There is no Docker image for the relay. The page `docs/relay.md` in the repository says how to run, back up and upgrade it.

## Who can use my own relay

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

**Any caller with a valid signature may make an identity and store on your relay.** The relay has no list of allowed people: an identity is made from a key, and the relay accepts any key. Keep your relay on a private network, a tailnet or behind a proxy that checks who calls. One identity cannot read another's data.

## My sync service is too old to move chats — update before continuing here

An older sync service still pairs computers, lists and shows chats, saves every chat you write, and continues a chat another computer has closed. Only taking a chat that is open on another computer waits for it, because only an updated service can promise that the take displaces the holder you saw. This build says `taking an open chat from another computer waits for a sync service update; all else works, so close it there and continue here`. Close the chat on that computer and continue here. For your own service, replace the running `codeaf relay` with this build; the next attempt uses the update without reopening codeaf.

A chat you write on this computer is never held back by an older service. Releases of codeaf check that the hosted service answers what they need before they ship.

## Starting your own sync service — codeaf relay help and the pairing command

Run `codeaf relay --listen 127.0.0.1:8901 --store <folder>`. The first line is `codeaf relay is listening on 127.0.0.1:8901 — on each computer: codeaf pair --via http://127.0.0.1:8901`. Use an address the other computers can reach; 127.0.0.1 reaches only the computer itself. `codeaf relay --help` lists the ordinary options, and `codeaf help env` lists both service variables.

`CODEAF_SYNC_URL` selects the sync service for chats; `off` disables syncing. `CODEAF_RELAY` is the fallback service override for pairing when `CODEAF_SYNC_URL` is unset, and the remote connection override for `serve` and `chat --at`. An explicit `--via` at pairing selects and remembers the service for later starts. Every instruction for the new computer includes that `--via` argument when the service is your own.

## The sync service did not answer — unreachable, slow or unable to agree

A refused connection, failed name lookup or missing route to your own service says `nothing is answering at http://127.0.0.1:8901 — if that is your own sync service, start it there with codeaf relay`. The hosted equivalent is `cannot reach codeaf.agentfield.ai — check this computer's internet connection`.

A timeout says `the sync service at http://127.0.0.1:8901 did not answer in time — try again`. A server error says `the sync service at 127.0.0.1:8901 is up but cannot agree on changes right now; it usually recovers within a minute`. Each sentence uses the address you selected.

`codeaf pair`, `codeaf devices` and `codeaf cluster status` say `asking the sync service at 127.0.0.1:8901…` to the error stream after two seconds without an answer, only when that stream is a terminal.

## A chat keeps working when the sync service is down

A sync outage says `the sync service at 127.0.0.1:8901 is not answering; this chat keeps working here and catches up when it is back`, once per outage. Local saves and rewind keep working. A local save by itself does not prove that syncing recovered. After an actual successful sync call, the chat can say `syncing again`; a sustained renewal outage keeps its existing `this chat is syncing again` recovery line.
