# App data policy

## Hide salaries or mask a field

Tell the app Builder which record type and fields to protect. The app that
defines the type declares `policy.fields`, for example
`"policy":{"fields":{"salary":"hide","tax_id":"mask"}}`. Apps that reuse
the type inherit the same policy. A hidden field is omitted. A masked field
shows `••••`. These values do not affect searches, sorts, groups or totals
that use the field. A record's reveal button does not bypass these rules.

A field's `class` can be `public`, `internal`, `confidential`,
`restricted` or `pii`. The Builder can suggest a class or set the one you
ask for. Confidential, restricted and PII fields are masked. A field policy
can hide them entirely.

## Only see my accounts or rows assigned to me

Tell the Builder which field identifies the person who should see a record.
For example, declare `"policy":{"row":{"field":"owner","attribute":"id"}}`
on accounts and give each account an owner. A person field can use `me`
for the local profile owner. A missing owner or principal attribute withholds
the row. App agents use the local person's policy along with their app grants.

Queries, totals and direct record reads use the same row rule. A live view
clears and refetches its records when access changes.

## Don't send customer data to AI or choose an approved model

Classify a field as `restricted` or `pii` to require an approved model.
The run, Builder and app agent stop with
“This app has restricted or PII fields. Choose a model and endpoint approved for its data class.”
when a class in scope lacks approval. App queries still use the same masks
for approved models.

The local profile owner can install approvals with
`codeaf app model-policy FILE.json`. The file lists exact model names and
completion URLs per class, for example
`{"restricted":[{"model":"provider/model","endpoint":"https://example.com/v1/chat/completions"}]}`.
Install an empty object to remove approvals. A fallback model or a different
endpoint needs its own approval. Policy decisions go to the app data log
`policy.jsonl` with decision names and identifiers.
