# A general factory — intake, router, pipeline, output, follow

Status: wave 1 in progress on `factory/general-intake` (off `app/v3`), 2026-11-06.

## Why

The factory reads GitHub issues and ends in a pull request. Its core is already
general (`factory.Source` reads and writes any system; a pipeline's four step
kinds — chat, check, gate, post — describe any work), but nothing outside
GitHub can enter it, and what an item does on arrival is decided by labels
alone, with the queue run oldest first. Wave 1 makes intake general and adds the
router; the output stays a pull request.

```
Intake ──► Router ──► Pipeline ──► Output ──► Follow
any source  repo, pipeline,  steps in a     PR (wave 1),   CI, reviews,
(Source)    priority, start  workspace      reply/doc later  replies
```

## Wave 1 — what lands

1. **The router** (`internal/factory/route`). An ordered rule table, kept by the
   factory (house level, operator-only to change), first match wins:
   `when {sources, repos, labels, authors, text, priority}` →
   `then {repo, pipeline, priority, start: auto | ask | backlog}`.
   It is read in intake for every item from every source. A matched rule's
   `pipeline` names a pipeline in the target repository's `factory.yaml`
   (the same names `factory.job_start {pipeline}` takes). Priority now orders
   execution: `p0` before `p1` … before none, then oldest first; Start now
   (Marked) still wins over all. With no rules, behaviour is today's.
1b. **The AI filter** (`when.ask` on a rule, `internal/factory/route/judge`).
   A rule may carry one yes/no question in a person's words. It is asked only
   after the rule's other fields hold, by a cheap model (the factory's default
   model unless the rule names one), once per version of the item's words
   (`route.VerdictKey`), and the answer and its one-sentence reason are kept on
   the item (`Item.Judged`) and shown wherever the job says why it started or
   didn't. `route.Decide` stays pure: it returns `Pending` for an unjudged
   question; the intake asks the `Judge`, stores the verdict, decides again.
   Guards: a judge failure is "no", never "yes"; spend is charged to the
   factory's day like triage; a stranger's ticket can at most be held to ask
   first by an AI yes (prompt injection); the judge's answer can only select
   the rule's own action, never invent one. Lane A owns the judge
   (`internal/factory/route/judge/**`); lane R owns calling it from intake.
2. **The inbox** (`internal/factory/inbox`). One `Add(Entry)` that files outside
   work as a job — identity is `(source, external_id)` kept in
   `Item.SourceRef`, so a second delivery updates rather than duplicates.
   Items behave like today's "job with no GitHub issue" (no issue comment, no
   close-on-merge on GitHub). Three trigger doors, all through `Add`:
   - wire `factory.intake_add` (the desktop and scripts);
   - CLI `codeaf factory add --source NAME --id ID --title … [--repo owner/name] [--label L]… [--priority p1] [--body -]`;
   - HTTP `POST /api/intake` on the factory service, with the factory's access token
     (what a Jira automation, Zapier or a webhook calls today, before a connector exists).
3. **The Jira connector** (`internal/factory/jira`). A `factory.Source` that
   polls Jira Cloud REST v3 (`/rest/api/3/search/jql`, a JQL the person writes),
   maps each issue to an `inbox.Entry` (`source: jira`, `external_id: ENG-142`,
   Jira priority Highest/High/Medium/Low/Lowest → p0/p1/p2/p3/none, labels,
   reporter), and writes back: a comment with the PR link when the job opens
   its pull request, and one when it merges. Connected with
   `factory.jira_connect {site, email, token, jql}`; the token is kept like
   the GitHub one, never in a config file in the clear. `jirastub` is an
   in-process fake of exactly the endpoints used, for tests and e2e.

## The abstractions (the shape every later source follows)

- `route` is a PURE function of (rules, item) → decision. No I/O, no store; the
  intake applies the decision. A person can read the rule list and say which
  line moved a ticket.
- `inbox.Add` is the ONLY door that turns outside work into a job. The wire,
  the CLI, `POST /api/intake` and every connector call it; dedup lives there once.
- `inbox.Feed` is a connector: it TRANSLATES only (fetch → `[]Entry`). It never
  touches the store and never decides policy. `inbox.Poll` is the one loop that
  drives every feed. A new connector (Linear, Sentry, mail) is one type.
- `inbox.Replier` is the optional write-back half of a connector. `inbox.Notify`
  is called by the job lifecycle (PR opened, merged, stuck) and dispatches by
  source; the lifecycle never learns a vendor.
- The GitHub poller is left as is in wave 1. Moving it onto `Feed` is a later
  refactor, not a reason to touch it now.

## Ownership (so lanes do not collide)

| Lane | Branch | Owns | Must not edit |
|---|---|---|---|
| R router | `factory/gi-router` | `internal/factory/route/**`, `internal/factory/store/routes*.go`, `intake/intake.go` routing hook, `lease/lease.go` ordering, `internal/appwire/factory_routes*.go`, wire `factory.routes`, `factory.routes_set` | inbox, jira |
| I inbox | `factory/gi-inbox` | `internal/factory/inbox/**` (Add, Poll, Notify, the replier registry, and calling Notify from the publish/merge path), `internal/appwire/factory_intake*.go`, `factory.intake_add`, CLI `codeaf factory add`, `POST /api/intake` in `internal/factory/serve` | route, lease, jira |
| J jira | `factory/gi-jira` | `internal/factory/jira/**` (a `Feed` + `Replier`, incl. `jirastub`), `internal/appwire/factory_jira*.go`, `factory.jira_connect`/`factory.jira_status`/`factory.jira_disconnect`, the Jira poll loop next to the GitHub one in `cmd/codeaf/app_factory.go`, PR-link write-back | route, lease; calls `inbox.Add` only |
| A judge | `factory/gi-judge` | `internal/factory/route/judge/**`: the model-backed `route.Judge` (prompt, strict JSON answer, timeout, spend, model choice, fake for tests) | intake, route.go |
| E e2e | integration branch | `internal/e2e/factory_general_e2e_test.go`, the live mac-studio run | — |

Shared, already on the integration branch: `route` types, `inbox.Entry/Add`
signatures, `Item.SourceRef`, `Item.Route`. Change a shared signature only by
saying so in the lane's report.

Each lane also: updates `internal/manual/chat/` (THE MANUAL LAW) for its own
verbs, keeps `internal/session/prompts/system.md` honest, and adds no
resident vocabulary.

## Acceptance (end to end, deepseek/deepseek-v4.1-flash on every tier and as the factory default model)

1. With a rule `jira project FL → santoshkumarradha/factory-demo, pipeline quick, start auto`,
   a ticket created in the Jira stub becomes a job within one poll, runs the
   `quick` steps, opens a PR on factory-demo, and the stub receives a comment
   containing that PR's URL.
2. `codeaf factory add --source inbox:ops --id A1 --title … --priority p0` with jobs-at-once 1 and two older p2 jobs queued: the p0 job runs next.
3. The same `(source, id)` added twice makes one job.
4. With no rules, a GitHub issue labelled `agent-ready` behaves exactly as before.

## Later waves (not now)

Workspace and deliverable interfaces (a non-git workspace, a non-PR output),
splitting `Item` into a core and a code facet, a shared pipeline/step library
kept by the factory, the desktop's routing page, a real Jira site.
