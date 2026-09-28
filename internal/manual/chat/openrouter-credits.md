# OpenRouter credits and free models: it worked and then stopped

## Out of credits — why codeaf picks only free models

For the default OpenRouter service, codeaf reads the account's credit balance and
the key's own spending cap, whichever is smaller. A known remaining balance of $0.50
or less is low. While it is low, a new conversation on a profile where you never
chose a model opens on `qwen/qwen3.8-27b:free`, and the two helper rows nobody set
use `nvidia/nemotron-3.5-lightning:free` for reflex and
`thinkingmachines/inkling-small:free` for small work. The crew's three seats — worker,
planner and checker — are picked per task, and a low balance reaches them as an
OpenRouter account out of credit: a seat nobody pinned is routed to a free pool, and the
task's crew line says `free routes in use (may log prompts) · credit unavailable on
openrouter` (*Route health* in *Models and cost*).

Free ids are defaults only. A model you chose with `/model` or the setup screen,
`--model`, `CODEAF_MODEL`, a seat you pinned with `/crew pin` or a helper row you set stays
chosen, and the free defaults are never written into your settings. Nothing is spent
reading the balance: it is two account lookups and no model call.

OpenRouter limits free models to 20 requests per minute, and to 50 requests per
day until the account has ever bought $10 of credit (1,000 per day after that),
so a long task can use up the free allowance quickly. The balance, the free
defaults and the warning apply only to OpenRouter — never to Codex, a local model
or another connected service.

## It worked and then stopped

If codeaf worked and then stopped on a paid OpenRouter model, the account has
probably run out of credit: OpenRouter reserves the most a request could cost
before it runs, so a few small turns work and then a larger one is refused and
the chat stops. The refusal row keeps the vendor's whole sentence and its top-up
link, and codeaf reads the balance again, unless it read it in the last 30
seconds.

## A payment refusal — the affordable output cap and the top-up link

When OpenRouter's payment refusal says the account can only afford a number of
output tokens, codeaf sends the same request once more with `max_tokens` set to
that number, and the reply usually lands. If that is refused too, the turn ends
on the vendor's whole sentence, top-up link included, for example
`openrouter accepted the key but the account cannot pay — … can only afford 641. To increase, visit https://openrouter.ai/settings/credits and add more credits`.

Any payment refusal from OpenRouter — on your turn, or on a background call such
as the reflex, a title or a task — also starts a fresh balance read, unless one
finished in the last 30 seconds. If the account reads low, the free defaults and
the warning below take effect without a relaunch.

## Why is my model a free one — when the balance is read

codeaf reads the balance at launch when it has never read this key, when the key
changed, or when the last reading was low; right after a key is saved; after a
payment refusal; and, while low, when you switch the conversation or Home to a
paid model. It never polls it every turn, and reads caused by refusals or model
switches wait 30 seconds after the last one. A read that fails — no network, an
error from OpenRouter — changes nothing and never warns. A key with no spending
cap whose account balance OpenRouter will not report is simply unknown: no free
defaults, no warning.

A conversation that has sent nothing and whose model nobody chose follows the
default both ways: onto the free model when the account reads low, and back to
the usual default when a later read finds more than $0.50. A conversation that has
sent a message keeps its model either way. After a top-up, the next new
conversation opens on the usual default, the helper rows return to their usual models,
and the crew is routed over paid routes again.

## Low on credits warning under the message box

`Your OpenRouter account is low on credits — some models may not be available`
appears at the right of the keys row under a conversation's or Home's message box
when the account is known low and that box names a paid OpenRouter model — any
model whose id does not end in `:free` and that the catalog does not list at a
price of zero, even one that still answers. It is drawn in the warning colour. It
goes away when you choose a free model, or when a read finds more than $0.50. It
never shows for a model served by Codex, a local model or another connected
service, and it never shows a dollar figure. On a window too narrow for the whole
sentence it is left out rather than cut.
