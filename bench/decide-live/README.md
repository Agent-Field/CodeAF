# Live place decisions

Run from the repository root:

```sh
make test-focus PKGS=./internal/e2e RUN='^TestDecideLive$' \
  TEST_FLAGS='-tags e2e -count=1 -v' TEST_TIMEOUT=12m
```

The test uses OpenRouter and fixes every registered role, tier and session
floor to `deepseek/deepseek-v4.1-flash`. Credentials come through the same
`liveKey` helper as the other end-to-end lanes: the product's environment
and profile resolution. It writes only to throwaway directories. No key,
transport token or request body is printed.

The observed run took 47 seconds; allow up to 10 minutes for provider delays.
The whole run must cost less than $0.25,
including the council; a missing provider bill is a failure, not zero spend.
The provider attempt log also checks retry costs and every requested model.
Every completed response must report the exact requested model ID. Changing
`CODEAF_E2E_MODEL` does not change this lane's model.

1. `every_role` makes a real text connection check for every registered seat
   except the three exercised by the scenarios below. These calls prove model
   routing, not each role's feature behavior or image/speech/video support.
2. `memory_decider` runs the production reflex memory-decision prompt and parser
   against a new preference with no neighbours; it must choose `add`.
3. `learn_propose_decide` asks the real deciding judge for a formatting proposal,
   raises permission questions through `Agent.AskQuestion`, and answers through
   `ResolveQuestion`. It stays in learning until 18 agreements in 20 answers,
   then asks once more because the smoothed confidence is still 89%. At 19/20
   it automatically answers a reversible permission and leaves a receipt and
   real decision ledger row. Public askers are observed through
   `WatchQuestions`; `OpenQuestions` enumerates lane-owned waiters.
4. `two_place_council` runs the production council runner with Marketing and
   Software. Both must speak, reach a decision within six turns and below
   $0.25, and file the same outcome with chat provenance in both places.

Each model call prints `LIVE role=… requested=… actual=… cost=… total=…`.
The place prints its graduation and receipt; the council prints its outcome,
turn count and actual spend; `ATTEMPTS` prints the complete attempt bill.
Acceptance requires all four subtests to pass and a nonempty attempt bill.
Without a product-resolved provider key, the lane reports **SKIP**, which is
not live acceptance.

This is an engine integration test through public engine APIs, not a desktop
browser test. Judge and council callers supply a real provider-backed role
door, as those packages require. The judge-derived score is attached through
`SetDecideGate`; graduation confidence is calculated by the production scorer
from recorded proposal outcomes. This does not prove that the default desktop
session gate invokes the optional judge, or that renderer controls deliver
answers. Those integration seams need their own acceptance.

## Recorded live proof

On 2026-10-10, all four subtests passed in 46.47 seconds. Every response and
attempt requested `deepseek/deepseek-v4.1-flash`. The attempt log recorded
29 completed calls costing $0.015582 in total. The two-place council decided
in two turns for $0.000988 and filed its Friday release announcement in both
places. The place graduated at 18/20 and automatically allowed its next
permission after one further agreement, leaving the 19/20 receipt.

TypeScript, design policy, Go build/vet, `make build`, and the provider-key
resolution law passed. `make test-laws` failed in the existing host-guard
test and two manual-retrieval tests; all three failures reproduced from an
unchanged archive of the starting commit `36ba96795`. No desktop UI changed,
so Chromium/WebKit specs were not run. This run introduces no product feature
requiring a manual page.
