Audit the platform-group documents in `corpus/` and produce two files in the
workspace root: `findings.json` and `REPORT.md`.

There are ten documents. **Read all of them.** They disagree with each other in
places, and `corpus/RULES.md` decides every disagreement — it is not advice, it
is the answer key for which document wins. Some documents are superseded by
others and are wrong about things the superseding document changed. At least
one document states a fact confidently that a document with authority over that
fact contradicts.

Throughout, a **document id** is the two-digit prefix of its filename:
`03-incident-2026-02-03.md` has id `"03"`. `RULES.md` has no id and is never
an answer.

## `findings.json`

Strict JSON, exactly these seven keys, no others:

```json
{
  "services_on_deprecated_queue": ["svc-…", "…"],
  "total_incident_minutes": 0,
  "most_impacted_service": "svc-…",
  "monthly_cost_usd_cents": 0,
  "unowned_services": ["svc-…"],
  "superseded_docs": ["05"],
  "contradictions": [
    {"claim_doc": "09", "authoritative_doc": "01", "field": "owner"}
  ]
}
```

- **`services_on_deprecated_queue`** — every service id whose queue technology
  is deprecated, according to the architecture decision that is currently in
  force. Sorted ascending. Follow the ADR chain to its end before answering;
  an ADR that has been superseded is not authoritative about anything.

- **`total_incident_minutes`** — the sum, over every incident report in the
  corpus, of that incident's customer-impact duration in whole minutes. Each
  report defines its own impact window explicitly; use the window the report
  says is customer impact, not the whole timeline. One report states its
  duration in words, one gives clock times, and one crosses midnight UTC.

- **`most_impacted_service`** — the single service id with the largest total
  customer-impact minutes across all incidents.

- **`monthly_cost_usd_cents`** — the total monthly queue bill for the whole
  catalog, in whole cents, under the pricing schedule currently in force.
  Volume comes from the service catalog's messages-per-day figure; a billing
  month is 30 days. The answer is an exact integer — no rounding is required
  and none should be applied.

- **`unowned_services`** — every service id with no owner. Sorted ascending.

- **`superseded_docs`** — the ids of every document in the corpus that another
  document supersedes. Sorted ascending.

- **`contradictions`** — one object per disagreement between two documents
  about the same fact, where `claim_doc` is the id of the document that is
  wrong under `RULES.md` and `authoritative_doc` is the id of the document that
  wins. `field` must be **exactly one of**:
  `"incident_duration_minutes"`, `"owner"`, `"queue_client"`, `"unit_price"`.
  Order does not matter and duplicates are ignored, but the set must be exactly
  right: a contradiction you missed and one you invented both cost the same.

  Two edges are worth stating so this is decidable rather than a matter of
  taste:

  - **Being superseded and contradicting are not exclusive.** A superseded
    document that still states a *value* for a field some other document owns
    contradicts it, and belongs in this list as well as in `superseded_docs`.
  - **A difference between two architecture decisions is not a contradiction.**
    An ADR records a decision, not a fact about a subject, and no entry in the
    field vocabulary above covers "which technology is deprecated". A later ADR
    replacing an earlier one is supersession and belongs only in
    `superseded_docs`.

## `REPORT.md`

A short prose write-up for a reader who has not read the corpus. It must:

- state each of the seven findings above in prose;
- cite the document id it came from, written as `doc NN` (for example
  `doc 06`), for every finding;
- name every contradiction it found and say which document won and why,
  citing the rule from `RULES.md` that decided it;
- be at least 400 words.

Do not include an executive summary that restates the JSON verbatim, and do not
speculate beyond the corpus.

## How this is graded

`findings.json` is compared field by field against an answer key computed from
the corpus: lists as sets, numbers exactly, `contradictions` as a set of
triples. `REPORT.md` is checked mechanically for the citations and coverage
listed above. There is no judge and no partial credit inside a field — a number
is right or it is not.

Both files must be at the workspace root. Work in this directory.
