# On-call handover notes — platform rotation

Scratch notes. Not reviewed, not authoritative, kept because they are the only
continuous record of what the rotation actually dealt with.

---

**week of 2026-01-12** — quiet until Wednesday. The auth thing on the 14th was
nasty but short; **impact was about 90 minutes** once we'd rolled back, maybe a
bit less. Backlog drained on its own afterwards so I'm not counting that.
Somebody should write down what the actual token lifetime is supposed to be.

**week of 2026-02-02** — search went stale overnight, took most of a shift to
find. Nothing else.

**week of 2026-02-16** — new ADR landed about the queues. Read it twice and I
still think we're migrating onto Kafka, which is not what it says. Ask
team-platform before you tell anyone anything about this.

**week of 2026-02-23** — cert expiry on auth, overnight, went past midnight.
Handled. The runbook was out of date and I've fixed it.

**week of 2026-03-02** — svc-notify keeps paging the platform rotation. As far
as I'm concerned team-platform owns it now, we're the ones getting up at night
for it. (Catalog still says vacant. Whatever.)

**standing gripe** — nobody can tell me what we pay per message. There are two
pricing PDFs floating around and I have no idea which one finance uses.
