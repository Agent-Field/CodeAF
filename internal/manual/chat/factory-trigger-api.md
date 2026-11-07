# The factory's Trigger API

## connect my own tool to the factory — the HTTP API or AgentField, no SDK

Any tool can hand work to a factory and hear back without a connector written for it: a helpdesk, a form, an alerting service, your own script. There are two ways, and codeaf ships no SDK of its own:

- **The factory's HTTP API**, with the factory's access token (what `codeaf factory token` prints) as `Authorization: Bearer <token>`: `POST /api/intake` files work, `POST /api/intake/{source}/{id}/messages` says more about it, `GET /api/intake/{source}/{id}` reads how far it is, and a webhook set with `codeaf factory webhook add SOURCE URL` hears back. Plain HTTP from any language.
- **AgentField**: register the factory as a node on an AgentField control plane, and AgentField's own SDKs (Python, Go, TypeScript) and triggers call it. That is the way for a caller that should have its own credential rather than the factory's key.

Every one of these calls is the operator's: a teammate's sign-in is refused with "only the operator can do this". There is no token that files only one source's work; a tool given the factory's access token can do what the operator can.

## send a comment to a job from outside — my tool adds more details to a job it filed, POST /api/intake/{source}/{id}/messages

`POST /api/intake/{source}/{id}/messages` with `{"text":"…","author":"dana"}` says something more about a job filed from outside. `author` is optional and is the source's name when left out. It answers `{"job":12,"accepted":true,"outcome":"…","line":"…"}`.

The words reach the job the way a comment on a GitHub issue does: they are kept among the job's last three comments, so the next step's brief reads them, and the job's manager handles them once. Under the job's comments one line says what it did:

- on a stranger's job: "read as context · no write access to the repo";
- on a job with a run under way: "asked you in the manager chat whether the plan should change", and the manager conversation quotes it ("@dana sent a message about this job: …") and asks you. Reply there;
- on a job that has not started, or has finished: nothing changes, and the line says why.

A message is held to the job's own trust. The manager never changes the plan from a message on its own. Text is 1 to 8000 characters ("a comment is 1 to 8000 characters"); a source and id never filed is 404 "no job was filed from that source with that id". An `Idempotency-Key` header makes a retried send count once.

## check the status of a factory job from my script — GET /api/intake/{source}/{id}, state, steps, pull request, question

`GET /api/intake/{source}/{id}` answers how far the job is:

`{"job":12,"state":"running","steps_done":2,"steps_total":5,"route":"ops to web","pr_url":"https://github.com/…/pull/9","question":"…","updated":"2026-11-06T12:30:00Z"}`

`state` is the job's state as the queue shows it (`new`, `queued`, `running`, `needs_you`, `ready`, `done`, …); `route` is the routing rule that decided it, `pr_url` its pull request, `question` what it is waiting to be told. Each of those three is left out when there is none. These are the job page's own facts, so your tool and the queue never disagree. AgentField's `status` reasoner answers the same facts.

## set a webhook for a source — codeaf factory webhook add SOURCE URL, the signing secret

`codeaf factory webhook add SOURCE URL [--factory URL --token T]` makes the factory tell `URL` about every job whose source is `SOURCE` (such as `inbox:ops`). It prints `secret: whsec_…` and says beside it (on standard error) that the secret is not shown again. Adding again for the same source replaces the address and gives a NEW secret; the old one stops signing at once.

`codeaf factory webhook list` prints `SOURCE · URL` for each, never a secret. `codeaf factory webhook remove SOURCE` stops it and drops its deliveries still waiting. The address must be http or https with no password in it; `github`, `terminal` and `chat` are the factory's own and are refused. The same doors are `GET`, `POST {source, url}` and `DELETE /{source}` on `/v1/webhooks`, the operator's only. The secret is kept in the factory's folder (`trigger-webhooks.json`, readable by its owner alone), never in `config.yaml`.

## webhook events — job.filed, job.started, job.question, job.pr_opened, job.merged, job.stuck, job.done, job.message

A source with a webhook is sent a POST for each moment of a job it filed:

- `job.filed` — the work became a job (not on a repeat delivery);
- `job.started` — its first step began;
- `job.question` — it stopped on a question for a person, with the question in `text`;
- `job.pr_opened` — it opened its pull request, the link in `url`;
- `job.merged` — the pull request merged; a `job.done` follows;
- `job.stuck` — it stopped and needs a person to look, with no question;
- `job.done` — it is finished;
- `job.message` — the manager read a message, and `text` says what it made of it ("codeaf read your message · …").

The body is `{"id":"dlv_…","event":"job.filed","at":"…","source":"ops","external_id":"T-1","job":12,"text":"…","url":"…"}`. These are the same moments Jira is told of (as comments) and AgentField is told of; the job never knows which listen.

## check a webhook's signature — X-Codeaf-Signature, the signing secret, timestamp, delivery id

Every delivery carries three headers: `X-Codeaf-Signature: t=<unix seconds>,sha256=<hex>`, `X-Codeaf-Delivery: dlv_…` (the body's `id`) and `X-Codeaf-Event: job.…`. To check it in any language: take `t` and `sha256` from the header, compute the HMAC-SHA256, keyed by the secret, of `t`, a full stop and the raw body exactly as received (`<t>.<body>`), compare the hex in constant time, and refuse a `t` more than five minutes from your clock. Do it before parsing the body.

A retry of one delivery carries the same id, so a receiver that keeps the ids it handled can drop a repeat.

## webhook retries — a webhook endpoint that is down, how long codeaf keeps trying, a restart

Deliveries wait in the factory's folder (`trigger-outbox`), one file each, until the address answers with a 2xx. A refusal, an error or no answer within 15 seconds is tried again after 2 seconds, then twice as long each time, up to once an hour, for three days; after that it is given up and the factory's log says so. A restart of the factory loses nothing: what was waiting is sent when it starts. Removing the webhook drops what is waiting for it. A slow or failing receiver never holds up or fails the job.

## what the Trigger API cannot do — no SDK, no token that only files its own tickets, limits and refusals

- No SDK from codeaf: plain HTTP, or AgentField's SDKs.
- No token scoped to one source: every call takes the factory's access token, the operator's. Use AgentField for a caller with its own credential.
- A webhook's secret is shown once, at add; it cannot be read back. Lost, add the webhook again for a new one.
- One webhook per source.
- A message never changes a job's plan by itself, and it does not start, stop or answer a job; a person does that in the manager chat.
- Webhooks are sent only for jobs filed from outside. GitHub issues keep their own way of answering back.
