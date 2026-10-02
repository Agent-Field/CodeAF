# Hosted relay or your own relay

## Hosted relay or my own — which one do I use

A relay is the small service your computers sync through. There are two kinds. Both speak the same wire: the chat cannot tell them apart.

- **A hosted relay** is run for you on a cloud account. You do nothing to run it. codeaf's own is the fabric, built in; any other you give the chat the address of.
- **Your own relay** is the program `codeaf relay`. You run it on a computer you control.

Either way the relay sees only ciphertext and the short list on "What can the relay see".

**In this build the address `https://codeaf.agentfield.ai/fabric` is built in: it is codeaf's fabric, the hosted sync service, and a computer with no other word syncs through it.** The address has a path, `/fabric`, and you keep it if you type it yourself. You can still choose another: set `CODEAF_SYNC_URL`, or pass `--via <address>` to a pairing command. A computer that has paired with a relay of its own uses that one. A build with no address built in does not pick one for you, and says `no sync address is set, so there is nowhere to pair through: set CODEAF_SYNC_URL to a sync address, or pass --via <address>`. Give it one of the two below.

## Point codeaf at a relay — CODEAF_SYNC_URL, url or off

Set `CODEAF_SYNC_URL` in the environment of every computer that shares the chats.

```
CODEAF_SYNC_URL=https://relay.example.com codeaf
```

- **A web address** like `https://relay.example.com` or `http://host:8787`. Plain `http` is for trying it out inside an ssh tunnel. An address that is not a web address stops with `CODEAF_SYNC_URL is not a web address like http://host:8787`.
- **`off`** turns the relay off. Pairing then says `sync is off (CODEAF_SYNC_URL=off); pairing needs a sync address`.
- **Unset:** a computer that was paired uses the relay the pairing saved. A computer that never paired syncs through the built-in `https://codeaf.agentfield.ai/fabric`.

Your own computers must all name the same relay and hold the same identity, or they see different chats.

## Run my own relay — codeaf relay --listen and --store

```
codeaf relay --listen :8787 --store /var/lib/codeaf
```

- `--listen` is the address to listen on. The default is `:8787`.
- `--store <directory>` is where the relay keeps the directory and the chunks. **Without `--store` the relay only passes pairing and remote-access traffic. It keeps no chats, and sync does not work through it.** The directory is made if it is not there.
- `--quiet` turns the log off. `--status=false` turns off `GET /status`.
- `--trust-proxy` makes the pairing limits count the address in `X-Forwarded-For`. Use it only behind a proxy that sets that header, and never on a relay that faces the internet.

Stop it with SIGTERM or ctrl+c: requests under way finish first. **The relay has no TLS of its own.** Put a proxy that ends TLS in front of it if any computer reaches it over a network you do not own.

There is no Docker image for the relay. The page `docs/relay.md` in the repository says how to run, back up and upgrade it.

## Who can use my own relay

**Any caller with a valid signature may make an identity and store on your relay.** The relay has no list of allowed people: an identity is made from a key, and the relay accepts any key. Keep your relay on a private network, a tailnet or behind a proxy that checks who calls. One identity cannot read another's data.
