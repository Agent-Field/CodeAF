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

Pairing has limits on both kinds of relay: 10 new codes in an hour for one network, and a relay holds at most 2,000 waiting pairings. The messages are on the pairing page, for example `too many pairings from this network; wait 7 min` and `sync is full right now; try again in a few minutes`.

## What do I see when the relay says no

**The chat says one plain sentence for each refusal.** The turns you save always stay on this computer, and they go up when the relay takes them again. What differs is what the chat says and how it tries again:

- **The relay is full** (`507`, code `full`). The chat says `the relay has no room left (5 GiB), so new turns stay on this computer; free space there and reopen this chat`. The number is the stored-bytes ceiling the relay names in its answer (5 GiB on the hosted relay); when the relay names none, as a relay on its own disk does not, the chat leaves the brackets out: `the relay has no room left, so new turns stay on this computer; free space there and reopen this chat`. It says this at once, and then it stops trying: nothing you do in the chat can make room, so it does not ask the relay again until you reopen the chat. On the hosted relay the daily count of new chunks ends the same way, and it clears by itself the next day.
- **The relay asks this computer to slow down** (`429`, code `rate_limited`). The chat says nothing for the first 3 minutes, because a short burst clears by itself. If the refusals last longer, it says `the relay is asking this computer to slow down; new turns stay here and go up as soon as it allows`. It waits as long as the relay's `Retry-After` says, and never less than its own wait, which doubles after each failure up to 60 seconds.
- **Too many new identities from this network** (`429`, code `too_many_identities`). The chat says `this network has started too many new identities today; sync begins when the relay allows more` at once, and waits as long as the relay's `Retry-After` says, up to an hour at a time.
- **Another of your computers stopped this one** (`401`, code `revoked`). The chat says `this computer was stopped by another of your computers, so this chat stays here only; run `codeaf pair` to bring it back`, and does not try again: only a new pairing brings the computer back.
- **The clock is off** (`401`, code `skew`). The chat says `this computer's clock is off by more than 5 minutes`, once, and tries again at each normal turn of sync.

Each sentence is said once for as long as the trouble lasts, and again if the relay answers in between and then refuses again. An unreachable relay is not a refusal: the chat says nothing about it and tries again with the doubling wait.

On your own relay the full case is its disk: free space on the machine that runs it, then reopen the chat.
