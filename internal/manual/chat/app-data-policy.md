# App data policy

## Hide salaries or mask a field

Tell the app Builder which record type and fields to protect. The app that
defines the type declares `policy.fields`, for example
`"policy":{"fields":{"salary":"hide","tax_id":"mask"}}`. Apps that reuse
the type inherit the same policy. A hidden field is omitted. A masked field
shows `••••`. Searches, sorts and groups cannot use masked or hidden
fields. Sum and average can use a masked numeric field over rows the viewer
can access. Hidden fields are unavailable to totals. Choose an accessible
field if the app refuses a total. A record's reveal button does not bypass
an explicit hide or mask.

A field's `class` can be `public`, `internal`, `confidential`,
`restricted` or `pii`. The Builder can suggest a class or set the one you
ask for. Confidential, restricted and PII fields are masked for other
viewers and app agents. The owner can see their own local confidential
values, such as transaction amounts, unless the type declares an explicit
mask or hide. Restricted and PII record values stay masked. Values from
connected sources use the source's rules.

## Only see my accounts or rows assigned to me

Tell the Builder which field identifies the person who should see a record.
For example, declare `"policy":{"row":{"field":"owner","attribute":"id"}}`
on accounts and give each account an owner. A person field can use `me`
for the local profile owner. A missing owner or principal attribute withholds
the row. App agents use the local person's policy along with their app grants.

Queries, totals and direct record reads use the same row rule. A live view
clears and refetches its records when access changes.

## Don't send customer data to AI or choose an approved model

The Builder can plan and edit the schema for any data class. It receives
field definitions and worked examples. The contents of an existing sample
file are withheld. If a change needs new example rows, ask the Builder to
write synthetic records.

For restricted or PII data in a Personal app, codeaf can send the request
to the exact model and endpoint you chose in your profile. A model named
only by an app, a fallback destination, or a connected app using restricted
data needs explicit approval. An external AI harness cannot receive
restricted or PII app input. Use an approved model within codeaf instead.

The profile owner can install an explicit allowlist with
`codeaf app model-policy FILE.json`. The file lists exact model names and
completion URLs per class, for example
`{"restricted":[{"model":"provider/model","endpoint":"https://example.com/v1/chat/completions"}]}`.
An empty object denies all restricted and PII sends. Refusals and masks
go to the app data log `policy.jsonl` with decision names and identifiers.
