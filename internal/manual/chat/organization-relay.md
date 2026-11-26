# Your organization's own relay

## self-hosted team relay — what my organization's relay is, what it keeps, and why my computers go through it

An organization that runs its own server can run a relay beside it. A relay is the small server your computers go through to pair, to see which of your other computers are online, to Move a chat and to open a chat another of your computers is running, live. With the organization's relay, none of that touches the public one, and everyone in the organization needs nothing but its address. It is the program `codeaf relay`, started by the organization's stack as its `relay` service.

It keeps each person's list of paired computers and their names, so a restart does not make your computers forget one another. **By default it keeps no chat content.** Your chats stay on your computers and in the organization's own hosted chats, and the relay says so, so your computers stop trying to store them there. The person who runs it can turn storing on (`CODEAF_RELAY_STORAGE=on`); it then keeps encrypted copies of your own computers' chats, as a relay run with `--store` does.

With storing off, a chat another of your computers is running shows on Home as `untitled`, the same as with the public relay's storage off (see *Remote control*). **Settings → Devices** shows relay storage as off, which is the intended state here. The request it offers opens a request on the project's GitHub for the public relay, and does nothing for your organization's.

## use my organization's relay — pair my computers through my company's relay with codeaf pair --org, no variable to change

Pair through the organization's relay with its address, the one you sign in to. You need the sign-in `codeaf chat --org` uses: `CODEAF_ORG_TOKEN`, or `codeaf attach <address> --token <token>`.

```
codeaf pair --org https://org.example.com --code
codeaf pair <code> --via <the address the first computer printed>
```

The first command is on the computer that has your chats. It asks the organization's server which relay it runs, shows a code, and prints the line to type on the other computer. That line carries `--via` with the relay's address, so the other computer is told where to go, and both computers remember it. From then on every start of either computer uses it: nothing is typed into a profile or an environment. `codeaf devices` says which one a computer uses, as `relay: relay.example.com (your own)`.

`--org` does only that lookup and then pairs exactly as `--via <that address>` does. A computer that never says `--org` asks no organization anything. Both computers must be able to reach the relay's address from where they are; if the organization keeps its relay on a private network, that means being on it.

## my organization says it runs no relay — what --org says when it stops, and what that means

`codeaf pair --org` stops, before anything is paired, and says why in a sentence:

- `<host> does not run a relay for your computers. Pair through another with --via <address>, or leave --org off to use the usual one.` The organization has not named one. Ask whoever runs it, or use any other relay with `--via`, or carry on as you did before.
- `Sign in to <host> first: set CODEAF_ORG_TOKEN, or run codeaf attach <address> --token <token>.` You have no sign-in for that server here, or it was not accepted. `codeaf attach` keeps the sign-in, so sign in once and ask again.
- `<host> named a relay address this program cannot use.` The address is not a web address, or has a user name, a query or a fragment in it. Tell whoever runs the organization to correct `CODEAF_ORG_RELAY_URL`; nothing on your computer needs changing.
- `<host> named a relay that is not https, so nothing was paired through it.` The organization was reached over https and named a plain http relay, which this refuses so a lookup can never make a pairing less private. Tell whoever runs the organization to put the relay behind https.
- `--org and --via each name the relay to pair through: use one of them.`
- `this computer is already paired with <names> — codeaf devices lists them` is not about the organization: a computer that already syncs with others refuses any new pairing, with or without `--org`. *Adding a computer to your devices, approving it, and removing it* says how to take one out of the list.

## a relay of my own instead of the organization's — --via and CODEAF_SYNC_URL still win over it

Nothing about an organization's relay overrides a device. `codeaf pair --via https://relay.mine.example` pairs through that relay and asks no organization anything. `CODEAF_SYNC_URL` holding an address always wins over a saved one, and `CODEAF_SYNC_URL=off` turns the relay off, which stops a pairing even with `--org`. Someone who is not pairing through an organization keeps the public relay for their build's channel exactly as before, and picks any other relay with `--via`. See *Hosted relay or your own relay*. Every paired device must use the same relay, or each sees different chats.

## who can reach my organization's relay — does it know who is in my team, can a stranger use it

No. **The relay has no list of people.** It does not know the organization's members: a computer that makes a valid signature may make an identity and use its pairing, its list of devices and its live road, as on any relay. It sees machine names, times and sealed bytes it cannot read, because what passes through is sealed on the devices before it leaves them, and a stranger gets only what their own devices would. Adding a new device always needs a person to approve it on any other that is already paired, so a stranger cannot attach theirs to a list. If that is not acceptable, the person who runs it keeps it on a private network, such as a VPN or behind a firewall that allows only the office, and gives that address to the organization. The stack publishes the relay on the host's own address only until its operator changes `CODEAF_RELAY_BIND`.

## run a relay for my team — the relay service in the organization stack, CODEAF_ORG_RELAY_URL and its settings

This is for whoever runs the organization (`deploy/org`). The stack starts the relay with the rest and answers `http://127.0.0.1:8788/p/health` when it is up. Members' devices need it on a **name of its own with https** (its requests are signed with their path, so it cannot sit under the server's address or any other path), for example `relay.example.com { reverse_proxy 127.0.0.1:8788 }` in Caddy.

In `.env`: `CODEAF_ORG_RELAY_URL=https://relay.example.com` is the address the server tells everyone who is signed in (`relay_url` in its config; left empty it tells nobody and the relay still runs). `CODEAF_RELAY_TRUST_PROXY=on` behind a proxy. `CODEAF_RELAY_STORAGE` is `off` by default and `on` keeps encrypted chats. `CODEAF_RELAY_STATUS=on` publishes the list of connected devices, so leave it off on an open network. `CODEAF_RELAY_BIND` and `CODEAF_RELAY_PORT` choose where the host publishes it (`127.0.0.1:8788`). On Railway the same service comes from `make-template.sh`. A relay lost with its volume makes people pair again.
