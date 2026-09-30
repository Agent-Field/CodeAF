# Relay limits and refusals

## Is there a storage limit on the relay — how much can I sync

It depends on the relay.

**A hosted relay has limits.** The hosted relay in this repository sets, for each identity:

- 5 GiB stored, and 5,000,000 stored objects;
- 5,000 new chunks in a day;
- 1,200 requests a minute for the identity, and 600 for one computer;
- 2 chunks being sent at one moment;
- 16 MiB for one chunk, and 1 MiB for any other message.

A chunk the relay already holds costs nothing again, so a computer that sends it twice is never refused for that.

**A relay you run yourself has no limit of its own on an identity.** It stops only when its disk is full.

## Too many new identities from one network

The hosted relay lets one network address make **20 new identities in a day**. Computers of an identity that exists are never counted. After 20 the relay answers `429` with the code `too_many_identities` and a `Retry-After`. A relay you run yourself has no such limit.

Pairing has limits on both kinds of relay: 10 new codes in an hour for one network, and a relay holds at most 2,000 waiting pairings. The messages are on the pairing page, for example `too many pairings from this network; wait 7 min` and `the relay is full right now; try again in a few minutes`.

## What do I see when the relay says no

**The chat has no message of its own for a full store or for a rate limit.** This is a gap, and these words are not in the chat:

- a full store is `507` with the code `full`. The client names it `blobstore: full (507)`;
- a rate limit or the new-identity limit is `429` (`rate_limited`, `too_many_identities`). The client names it `blobstore: server refused with 429 "rate_limited"`.

The chat does not stop. The turns you save stay on this computer, and the next try is later: the wait doubles after each failure, up to 60 seconds. When the relay takes the turns again, they go. The only sync message the chat shows is the clock one: `this computer's clock is off by more than 5 minutes`.

So a computer that seems not to sync, with nothing said, may be met by a limit. Check that the relay is up and that it is not full. On your own relay look at its log and its disk.
