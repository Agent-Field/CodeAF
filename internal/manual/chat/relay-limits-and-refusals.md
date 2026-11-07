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

## How much sync space have I used

Open `/status` in a chat to see a dim `sync space` line, for example
`sync space  2.8 GiB of 5 GiB`. It shows how big your relay storage is and how
close you are to the relay limit: the amount stored, then the byte limit the
hosted relay reports. The same reading appears in `/status --json`. It is read when the chat opens and
again after each `/status`, so it can trail what the last few turns uploaded.

This counts everything stored for your identity on the hosted relay across all
your computers and chats, not this chat alone. GiB and MiB are binary byte units,
the same units shown when your sync space is full.

The stored amount never goes down while your identity exists today. Nothing is
deleted from the relay for a living identity, even if you delete or archive a
chat on a computer. Cleanup is planned, but is not built.

A self-hosted relay shows no line. There is also no line when sync is off, no
relay is selected, an older relay does not report the amount and limit, there
is nothing stored yet, or the relay does not answer the short request.

## Too many new identities from one network

The hosted relay lets one network make **200 new identities in a day**. A network is one IPv4 address, or one IPv6 block of /64 (changing the address inside your own block does not make a new network). Computers of an identity that exists are never counted. After 200 the relay answers `429` with the code `too_many_identities` and a `Retry-After` that says how many seconds are left until that network's day ends. A relay you run yourself has no such limit.

Pairing has limits on both kinds of relay: 10 new codes in an hour for one network, and a relay holds at most 2,000 waiting pairings. The messages are on the pairing page, for example `too many pairings from this network; wait 7 min` and `sync is full right now; try again in a few minutes`.

## What do I see when the relay says no

**The chat says one plain sentence for each refusal.** The turns you save stay on this computer. The storage-full, too-many-identities and slow-down refusals below retry automatically while the chat stays open, including when sync could not start; saved turns upload when a retry succeeds. What differs is what the chat says and how it tries again:

- **Your sync space is full** (`507`, code `full`). The chat says `your sync space is full (5 GiB), so new turns stay on this computer`. The number is the stored-bytes ceiling the relay names (5 GiB on the hosted relay). Without a number, it says `your sync space is full, so new turns stay on this computer`, with no brackets. The hosted daily count of new chunks and a self-hosted relay's full disk give this answer without a number. The chat says it at once and retries after an hour while it stays open. New turns remain on this computer; reopening is not required for that retry. The daily count clears the next day.
- **Sync is full for everyone right now** (`507`, code `relay_storage_full`). The chat says `sync is full for everyone right now, so new turns stay on this computer and go up when it has room`. This is shared capacity, separate from your identity's ceiling. It says this at once and retries after an hour while the chat stays open. Saved turns upload when a retry succeeds.
- **The relay asks this computer to slow down** (`429`, code `rate_limited`). The chat says nothing for the first 3 minutes, because a short burst clears by itself. If the refusals last longer, it says `sync is asking this computer to slow down; new turns stay here and go up as soon as it allows`. Its own wait doubles after each failure up to 60 seconds. The relay's `Retry-After` can extend that wait, up to an hour.

## My sync space is full — how do I free it, does deleting a chat free sync space

Deleting or archiving a chat, removing a computer, or cleaning this computer frees nothing on the hosted relay: it keeps every chunk an identity stored, and erases them only when that whole identity is retired, after its grace period. Your identity's stored-byte ceiling is 5 GiB; new turns keep working and stay on this computer when it is full. The hosted daily count of new chunks is a separate ceiling that clears the next day. While the chat stays open, it retries a full refusal after an hour; reopening is not required.

On a self-hosted relay, the full case is its disk. Free space on the machine that runs the relay. An open chat retries after an hour, or reopening it starts a fresh attempt.

## Too many identities, a removed device or a clock that is off — other relay refusals

A refused upload leaves the saved turn on this computer. These are the other sentences:

- **Too many new identities from this network** (`429`, code `too_many_identities`). The chat says `too many new identities from this network today — try again in about 3 hours` at once. The time is the relay's `Retry-After`, rounded up to minutes or hours; when the relay names none, the chat says `this network has started too many new identities today; sync begins when it allows more`. It waits as long as the `Retry-After` says, up to an hour at a time.
- **Another of your computers stopped this one** (`401`, code `revoked`). The chat says `this device was removed by another of your devices, so this chat stays here only — run codeaf pair to bring it back`. Once removal is known, this computer stops contacting the relay; only a new pairing brings it back.
- **The clock is off** (`401`, code `skew`). The chat says `this computer's clock is off by more than 5 minutes`, once. Running sync tries again at each normal turn of sync. If the clock refusal prevented sync from starting, correct the clock and reopen the chat.

Each startup refusal is said once while the chat waits. Running sync says each sentence once for as long as the trouble lasts, and again if the relay answers in between and then refuses again. Running sync retries an unreachable relay with the doubling wait; an unreachable startup is tried again on the chat's next activity. If this computer holds the chat and renewals fail for longer than one renewal period, it says `the sync service at 127.0.0.1:8901 is not answering; this chat keeps working here and catches up when it is back` once. A successful renewal or publish then says `this chat is syncing again` once. A displaced or removed computer does not claim that it is syncing again.

## Is the hosted relay full for everyone — global admission and storage limits

The hosted relay also has operator-controlled limits across all identities: the production
configuration sets 10,000 new identities per day and a shared ceiling of 1 TiB of stored
frame bytes. These are separate from your
identity's 5 GiB quota and the limit for one network. The operator may change the ceilings.

When global admission is closed, a new identity gets `429 relay_admission_full` with
`Retry-After`; existing identities can still use the service. When the shared storage ceiling
is reached, newcomers get `507 relay_storage_full`; people already syncing keep storing up to
their own 5 GiB and their own object and daily chunk limits. Their new chunks still count in
the shared total, keeping admission closed. The operator also has an emergency stop, off by
default, that refuses new chunks for everyone with `507 relay_storage_full` if they would
exceed its ceiling; setting it to zero refuses all new chunks. Under either brake, existing
chunks stay readable, retries of a chunk already held are allowed, and retiring an identity
still erases its data after the grace period and returns capacity. A local turn stays on this
computer if upload is refused. An open chat retries storage refusals after an hour,
including when sync could not start. Admission refusals follow `Retry-After`, up to
an hour at a time; new turns do not shorten either wait.
These operator brakes do not delete your existing data. There is no automatic expiry for
an identity whose computers have all gone silent.
