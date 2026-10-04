# Hosted relay or your own relay

## Hosted relay or my own — which one do I use, and where does sync go by default

A relay is the small service your computers sync through. There are two kinds. Both speak the same wire: the chat cannot tell them apart.

- **A hosted relay** is run for you on a cloud account. You do nothing to run it. You give the chat its address.
- **Your own relay** is the program `codeaf relay`. You run it on a computer you control.

Either way the relay sees only ciphertext and the short list on "What can the relay see".

**Where sync goes by default.** Unset, a computer syncs through codeaf's hosted relay at `https://codeaf.agentfield.ai/fabric`, once this build carries that address. The address lives in one place in the program, `HostedRelayURL`; a build whose value is empty has no default, and then nothing is copied and pairing says `no sync address is set, so there is nowhere to pair through: set CODEAF_SYNC_URL to a sync address, or pass --via <address>`. Even with a default, a computer that has never been paired sends nothing: see "Is my code sent anywhere before I pair" on the page *Pairing your chats with a second computer*. To use your own relay instead, set `CODEAF_SYNC_URL` as below. Turn sync off with `CODEAF_SYNC_URL=off`.

## Point codeaf at a relay — CODEAF_SYNC_URL, url or off

Set `CODEAF_SYNC_URL` in the environment of every computer that shares the chats.

```
CODEAF_SYNC_URL=https://relay.example.com codeaf
```

- **A web address** like `https://relay.example.com` or `http://host:8787`. Plain `http` is for trying it out inside an ssh tunnel. An address that is not a web address stops with `CODEAF_SYNC_URL is not a web address like http://host:8787`.
- **`off`** turns the relay off. Pairing then says `sync is off (CODEAF_SYNC_URL=off); pairing needs a sync address`.
- **Unset:** a computer that was paired with a named relay uses the relay the pairing saved. Otherwise it uses the hosted default above, if this build has one, and has none if it does not.

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
