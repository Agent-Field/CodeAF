# Evidence rules for the platform audit

These rules decide which document wins when two of them disagree. They are not
guidance; they are the answer key for every conflict in this corpus, and an
audit that resolves a conflict some other way is wrong even if the other way
seems more sensible.

1. **Service metadata is owned by the service catalog.** Owner, tier, queue
   technology, and message volume come from `01-service-catalog.md` and nothing
   else. Any other document that states one of these facts is reporting it
   second-hand and loses.

2. **Incident facts are owned by that incident's report.** The date, the
   affected service, and the duration of customer impact come from the incident
   report filed for it. No other document may adjust them.

3. **Architecture decisions are owned by the newest ADR that has not been
   superseded.** An ADR whose status line says it was superseded is not
   authoritative for anything, including the parts the superseding ADR did not
   mention. Follow the chain to its end before concluding anything.

4. **Vendor pricing is owned by the schedule with the latest effective date.**
   An earlier schedule is superseded on its whole contents, not line by line.

5. **On-call notes are never authoritative.** They are written at three in the
   morning by someone who is guessing. Where they disagree with a document that
   is authoritative under rules 1–4, they are wrong, and the disagreement is a
   finding in its own right.

6. **A document is "superseded"** if another document in this corpus explicitly
   replaces it — an ADR that says so in its status line, or a pricing schedule
   with a later effective date covering the same services.
