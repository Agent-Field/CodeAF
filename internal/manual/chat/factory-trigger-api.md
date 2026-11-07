# The factory's Trigger API

## connect my own tool to the factory — register a source, codeaf factory source add, a token for one tool

Any tool can hand work to a factory and hear back, without a connector written for it: a helpdesk, a form, an alerting service, your own script. You register it as a SOURCE on the factory, as its operator:

`codeaf factory source add NAME [--callback URL] [--trust collaborator|stranger] [--factory URL --token T]`

It prints two lines, `token: cft_…` and `signing secret: whsec_…`, and says beside them (on standard error) `registered NAME · trust …` and "Keep both now: neither is shown again." The token is what your tool sends as `Authorization: Bearer <token>`; the secret checks the webhooks the factory sends to `--callback`. `NAME` is lowercase, such as `ops` or `inbox:ops`; `github`, `terminal`, `chat` and `jira` are the factory's own and are refused.

`codeaf factory source list` shows each source's name, trust and callback, never a token or secret. `codeaf factory source revoke NAME` stops its token at once and drops its webhooks still waiting; to change a callback or get a new token, revoke and add again. The desktop does the same with `factory.source_add`, `factory.source_list` and `factory.source_revoke`. Only the operator may do any of this ("only the operator can do this").

## what a source's token can do — only its own work, trust capped, a source token on another source

A source's token files, reads and messages ONLY work whose source is its own name. Anything else is refused with "this token files and reads only work from NAME". It opens no other part of the factory: the queue, jobs and every `/v1` address answer it 401, as an unknown key. The factory's own access token (what `codeaf factory token` prints) keeps doing everything it did, for any source.

Work a source files is trusted at most as the source was registered: `--trust collaborator` or `--trust stranger`, and `stranger` when left out. An entry that asks for `owner` or for more than the source's trust is filed at the source's; one that asks for less keeps the lower one. A stranger's job never runs a step that can change files.

The token is kept only as its SHA-256 in the factory's folder (`trigger-sources.json`, readable by its owner alone). The signing secret is kept in that same private file, because signing a webhook needs it; neither is ever in `config.yaml` or shown again.

## file work from my tool — POST /api/intake with a source token

`POST /api/intake` with the source's token takes the same JSON as from the operator: `{"source":"ops","external_id":"T-1","title":"…","body":"…","url":"…","author":"…","labels":["bug"],"priority":"p1","repo":"owner/name","trust":"collaborator"}`. `source` may be left out and is then the token's own. It answers `{"id":12,"created":true}` (201), or `created:false` (200) when the same source and id were filed before, which updates that job instead of making a second.

It goes through the same inbox as every other door, with the same limits: title 200 characters, body 32000, id 200, at most 30 labels, a request at most 64 KiB. Routing rules decide its repository, pipeline and start the same way.

## send a comment to a job from outside — my tool adds more details to a job it filed, POST /api/intake/{source}/{id}/messages

`POST /api/intake/{source}/{id}/messages` with `{"text":"…","author":"dana"}` says something more about a job your tool filed. `author` is optional and is the source's name when left out. It answers `{"job":12,"accepted":true}`.

The words reach the job the way a comment on a GitHub issue does: they are kept among the job's last three comments, so the next step's brief reads them, and the job's manager handles them once. Under the job's comments one line says what it did:

- from a stranger source, or on a stranger's job: "read as context · no write access to the repo";
- on a job with a run under way: "asked you in the manager chat whether the plan should change", and the manager conversation quotes it ("@dana sent a message about this job: …") and asks you. Reply there;
- on a job that has not started, or has finished: nothing changes, and the line says why.

A message is read at the LOWER of the job's trust and the source's. The manager never changes the plan from a message on its own. Text is 1 to 8000 characters ("a comment is 1 to 8000 characters"); a job never filed from that source and id is 404 "no job was filed from that source with that id". An `Idempotency-Key` header makes a retried send count once.

## check the status of a factory job from my script — GET /api/intake/{source}/{id}, state, steps, pull request, question

`GET /api/intake/{source}/{id}` answers how far the job is:

`{"job":12,"state":"running","steps_done":2,"steps_total":5,"route":"ops to web","pr_url":"https://github.com/…/pull/9","question":"…","updated":"2026-11-06T12:30:00Z"}`

`state` is the job's state as the queue shows it (`new`, `queued`, `running`, `needs_you`, `ready`, `done`, …); `route` is the routing rule that decided it, `pr_url` its pull request, `question` what it is waiting to be told. Each of those three is left out when there is none. These are the job page's own facts, so your tool and the queue never disagree. The operator's token reads any source's job; a source's token only its own.

## webhook events — job.filed, job.started, job.question, job.pr_opened, job.merged, job.stuck, job.done, job.message

A source registered with `--callback URL` is sent a POST to that address for each moment of a job it filed:

- `job.filed` — the work became a job (not on a repeat delivery);
- `job.started` — its first step began;
- `job.question` — it stopped on a question for a person, with the question in `text`;
- `job.pr_opened` — it opened its pull request, the link in `url`;
- `job.merged` — the pull request merged; a `job.done` follows;
- `job.stuck` — it stopped and needs a person to look, with no question;
- `job.done` — it is finished;
- `job.message` — the manager read a message you sent, and `text` says what it made of it ("codeaf read your message · …").

The body is `{"id":"dlv_…","event":"job.filed","at":"…","source":"ops","external_id":"T-1","job":12,"text":"…","url":"…"}`. A source with no callback is sent nothing.

## check a webhook's signature — X-Codeaf-Signature, the signing secret, timestamp, delivery id

Every delivery carries three headers: `X-Codeaf-Signature: t=<unix seconds>,sha256=<hex>`, `X-Codeaf-Delivery: dlv_…` (the body's `id`) and `X-Codeaf-Event: job.…`. The signature is the HMAC-SHA256, keyed by the source's signing secret, of the timestamp, a full stop and the raw body: `<t>.<body>`. Check it against the bytes exactly as received, before parsing them, and refuse a timestamp more than five minutes from your clock. Both SDKs' `verifyWebhook` do exactly this.

A retry of one delivery carries the same id, so a receiver that keeps the ids it handled can drop a repeat.

## webhook retries — a callback that is down, how long codeaf keeps trying, a restart

Deliveries wait in the factory's folder (`trigger-outbox`), one file each, until your callback answers with a 2xx. A refusal, an error or no answer within 15 seconds is tried again after 2 seconds, then twice as long each time, up to once an hour, for three days; after that it is given up and the factory's log says so. A restart of the factory loses nothing: what was waiting is sent when it starts. Revoking the source drops what is waiting for it. A slow or failing callback never holds up or fails the job.

## SDK — the Go client pkg/codeaftrigger, the TypeScript codeaf-trigger.ts, an example program

Two thin clients wrap exactly these calls and nothing else:

- Go: `github.com/Agent-Field/codeaf/pkg/codeaftrigger`. `codeaftrigger.New(url, token)` gives `File(ctx, entry)`, `Message(ctx, source, id, text)` (and `MessageAs` with an author), `Status(ctx, source, id)`; `codeaftrigger.VerifyWebhook(secret, body, header)` and `ParseWebhook(body)` handle a delivery.
- TypeScript: `sdk/typescript/codeaf-trigger.ts`, one file with no dependencies, using fetch and Web Crypto (Node 18+, Deno, Bun, workers, browsers). `new Client(url, token)` has `file(entry)`, `message(source, id, text, author?)` and `status(source, id)`; `await verifyWebhook(secret, body, header)` is true or false.

`pkg/codeaftrigger/examples/filer` is a runnable Go program that files a ticket read from standard input and prints the webhooks that come back: `go run ./pkg/codeaftrigger/examples/filer -factory URL -token cft_… -secret whsec_… -source ops -id T-1 -listen :8099 < ticket.txt`; register the source with `--callback` pointing at that listen address.

## what the Trigger API cannot do — limits and refusals

- A source's token cannot reach another source's work, the queue, or any `/v1` address, and cannot register or revoke sources.
- A source's work is never trusted as the operator: `--trust owner` is refused ("trust must be collaborator or stranger").
- A token or signing secret is shown once, at add; neither can be read back. Lost, revoke and add again.
- One callback per source. A callback with a password in it, or that is not http or https, is refused.
- A message never changes a job's plan by itself, and it does not start, stop or answer a job; a person does that in the manager chat.
- Webhooks are sent only for jobs a source filed. GitHub issues and Jira tickets keep their own way of answering back.
