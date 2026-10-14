# Builder model bench

Run PLAN, WRITE and up to three FIX rounds for a preset:

```sh
go run ./cmd/apps-buildbench -model deepseek/deepseek-v4.1-flash -preset crm
```

Calls go through `builder.NewModel(svc.Config{}, nil).Complete`, the same
`builder.Model` door the shipped Builder uses. It resolves the key from the
codeaf profile or `OPENROUTER_API_KEY`, applies the provider stack's retries,
and returns its billed USD cost. A missing key fails with setup guidance.
Builder forces reasoning off; `-think` is gone.

`-model` accepts comma-separated model ids. `-preset` accepts `crm`, `tracker`,
`inventory`, `booking`, or `all` (the default). `-paragraph` supplies your own
ask instead. `-rounds` bounds FIX calls; `-v` prints issues and quality notes.
Jobs run concurrently. Each result prints elapsed PLAN, WRITE and FIX seconds,
total cost, and PASS or FAIL from the bench's schema, binding and coverage
checks. Timing includes the complete Builder call and its provider retries.

Files and `result.json` land under
`.tmp/buildbench/<model-with-slash-replaced-by-underscore>/<preset>/`;
`-out` changes the root. The command exits nonzero if any run fails. This
bench is a separate command and is absent from the shipped binary.

Measured comparisons and reproduction details live in
`internal/apps/builder/prompt/BENCH.md`.
