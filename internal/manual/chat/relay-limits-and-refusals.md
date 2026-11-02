# Relay limits and refusals

## Taking an open chat from another computer waits for a sync service update

An older service cannot promise that taking a chat displaces only the computer you saw holding it, so this build never sends that take to it. The sentence is `taking an open chat from another computer waits for a sync service update; all else works, so close it there and continue here`. Writing, saving, pairing, listing, viewing and continuing a chat the other computer closed keep working. Close the chat on that computer and continue here; update your own `codeaf relay` to this build.

## Is there a storage limit on the relay — how much can I sync

It depends on the relay.

**A hosted relay has limits.** The hosted relay in this repository sets, for each identity:

- 5 GiB stored, and 5,000,000 stored objects;
- 5,000 new chunks in a day;
- 1,200 requests a minute for the identity, and 600 for one computer;
- 2 chunks being sent at one moment;
- 16 MiB for one chunk, and 1 MiB for any other message.

A chunk the relay already holds costs nothing again, so a computer that sends it twice is never refused for that.

**Coordination storage has a separate limit on both relays.** Each identity starts with an 8 MiB allowance for directory and coordination records. A larger imported identity keeps its measured allowance. New growth beyond it is refused; existing records and saved chats are retained. A relay you run yourself also needs enough disk for encrypted chunks and all three coordination copies. Older directory requests keep their 1 MiB HTTP limit; staging a large request does not bypass retained storage capacity.

## Too many new identities from one network

The hosted relay lets one network make **200 new identities in a day**. A network is one IPv4 address, or one IPv6 block of /64 (changing the address inside your own block does not make a new network). Computers of an identity that exists are never counted. After 200 the relay answers `429` with the code `too_many_identities` and a `Retry-After` that says how many seconds are left until that network's day ends. A relay you run yourself has no such limit.

Pairing has limits on both kinds of relay: 10 new codes in an hour for one network, and a relay holds at most 2,000 waiting pairings. The messages are on the pairing page, for example `too many pairings from this network; wait 7 min` and `sync is full right now; try again in a few minutes`.

## What do I see when the relay says no

**The chat says one plain sentence for each refusal.** The turns you save always stay on this computer, and they go up when the relay takes them again. What differs is what the chat says and how it tries again:

- **The relay is full** (`507`, code `full`). The chat says `the relay has no room left (5 GiB), so new turns stay on this computer; free space there and reopen this chat`. The number is the stored-bytes ceiling the relay names in its answer (5 GiB on the hosted relay); when the relay names none, as a relay on its own disk does not, the chat leaves the brackets out: `the relay has no room left, so new turns stay on this computer; free space there and reopen this chat`. It says this at once, and then it stops trying: nothing you do in the chat can make room, so it does not ask the relay again until you reopen the chat. On the hosted relay the daily count of new chunks ends the same way, and it clears by itself the next day.
- **The relay asks this computer to slow down** (`429`, code `rate_limited`). The chat says nothing for the first 3 minutes, because a short burst clears by itself. If the refusals last longer, it says `the relay is asking this computer to slow down; new turns stay here and go up as soon as it allows`. It waits as long as the relay's `Retry-After` says, and never less than its own wait, which doubles after each failure up to 60 seconds.
## Too many identities, a removed device or a clock that is off — other relay refusals

A refused upload leaves the saved turn on this computer. These are the other sentences:

- **Too many new identities from this network** (`429`, code `too_many_identities`). The chat says `too many new identities from this network today — try again in about 3 hours` at once. The time is the relay's `Retry-After`, rounded up to minutes or hours; when the relay names none, the chat says `this network has started too many new identities today; sync begins when it allows more`. It waits as long as the `Retry-After` says, up to an hour at a time.
- **Another of your computers stopped this one** (`401`, code `revoked`). The chat says `this device was removed by another of your devices, so this chat stays here only — /pair brings it back`, and does not try again: only a new pairing brings the computer back.
- **The clock is off** (`401`, code `skew`). The chat says `this computer's clock is off by more than 5 minutes`, once, and tries again at each normal turn of sync.

Each sentence is said once for as long as the trouble lasts, and again if the relay answers in between and then refuses again. An unreachable relay is retried with the doubling wait. If this computer holds the chat and renewals fail for longer than one renewal period, it says `the sync service at 127.0.0.1:8901 is not answering; this chat keeps working here and catches up when it is back` once. A successful renewal or publish then says `this chat is syncing again` once. A displaced or removed computer does not claim that it is syncing again.

On your own relay the full case is its disk: free space on the machine that runs it, then reopen the chat.

## Is the hosted relay full for everyone — global admission and storage limits

The hosted relay also has operator-controlled limits across all identities: by default,
10,000 new identities per day and 1 TiB of stored frame bytes. These are separate from your
identity's 5 GiB quota and the limit for one network. The operator may change the ceilings.

When global admission is closed, a new identity gets `429 relay_admission_full` with
`Retry-After`; existing identities can still use the service. When the shared storage ceiling
is reached, new identities and new chunks get `507 relay_storage_full`. Existing chunks stay
readable, retries of a chunk already held are allowed, and retiring an identity still erases
its data and returns capacity. A local turn stays on this computer if upload is refused.
These operator brakes do not delete your existing data. There is no automatic expiry for
an identity whose computers have all gone silent.
