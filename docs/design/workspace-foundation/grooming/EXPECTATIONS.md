# Synthetic expectation probes

Started 2026-09-09 at the user's request during topics 1/2. The purpose is to
generate plausible expectations, objections and counterexamples alongside the
person's own judgment. These are model-generated hypotheses, not a sample of
the public, an estimate of user preference, or a substitute for usability work
with people. Agreement across models can reflect shared training or prompting.

## Reusable experiment

A bounded delegated task owns
`/Users/santoshkumar/aforge-expectation-probe-20260909/`.
It contains a small runnable script, neutral scenario configuration, retained
raw responses and analysis. It uses the existing OpenRouter credential without
printing or storing it in results. It must verify current model IDs/prices,
record substitutions, preserve settings and failures, and enforce bounded
calls, output, concurrency and spending. No persistent service or product
primitive is being added.

The initial planned design is 5 models × 5 situations × 4 neutral question
wordings, shuffled reproducibly, with up to 100 calls, at most 4 concurrent,
500 output tokens per call (later raised to 1000 for Qwen's required reasoning)
and a $2 admission ceiling. Pilot calls count
toward that ceiling. The delegate reports live catalog availability for GLM 5.3,
GLM 5.3 Flash, DeepSeek V4 Flash, Kimi K3 and Qwen 3.8. Exact IDs, pricing,
request settings, successful counts and actual costs belong to the final run
manifest. GLM variants are the same model family, not two independent user
populations. Completed execution is recorded below.

Vary the work situation and what the person is returning to retrieve: a concrete
artifact, the original discussion or work status. Include starts with no folder,
an initially mistaken folder, and a conversation that changes subject. Avoid
demographic role-play and implying that simulated personas represent actual
demographic groups. Ask open questions before offering product mechanisms.

Preserve results by model and situation rather than treating the most frequent
answer as a product decision. Report dissent, missing information and sensitivity
to wording. Label any rule-based coding as such; preserve the raw text so the
coding can be questioned. Do not use another model's confidence as ground truth.

The first prompts intentionally leave the app's filing behavior unspecified.
They can surface assumptions imported from familiar applications, but cannot
measure expectations after someone learns aforge's actual behavior. A useful
future comparison would hold the situation constant and change a short stated
behavioral contract, then test whether expectations become coherent. Do not
interpret manual-filing assumptions as evidence against an explicitly explained
automatic organization design. The current five situations also vary starting
location and retrieval goal together, so differences are descriptive, not
isolated causal effects of either factor.

## Product hypotheses under discussion

The user's return expectation was to find the travel plan under Travel and
launch-related work under Launch, while remaining uncertain whether another
combined place would be necessary. This does not confirm creating that place,
automatic moving, splitting conversations or changing scope.

Our hypothesis is that part of the apparent complexity comes from asking where
the whole conversation belongs before identifying what the person wants to
find. Existing artifacts, chats and work are already distinct:

- Someone retrieving an itinerary may want the artifact and its current state.
- Someone resuming an argument may want the source conversation.
- Someone checking a launch may want its work and relevant dependencies.

Those can have different useful entry points while retaining common provenance.
This suggests testing a stable conversation with related artifacts/references
before inventing a combined folder or automatic fragmentation. It is a design
hypothesis, not an instruction supplied to the probe's models or an accepted rule.

After the run, use the returned concerns to sharpen a small number of alternative
behaviors or falsifying examples. Measures of actual usability require observing
people finding and steering work; this probe alone cannot establish that aforge
is more intuitive, efficient or capable than another product.

## Completed first run

The run stopped at 100 attempts, including all pilot failures and retries. It
returned 78 schema-compliant answers, 20 API rejections (9 parameter errors and
11 rate limits), one truncated answer and one schema failure. It attempted 90
distinct planned items; 10 were not attempted before the ceiling. Usable counts
were DeepSeek 16, GLM 5.3 17, GLM Flash 9, Kimi 17 and Qwen 19. Coverage is uneven.
Schema compliance does not establish a sound expectation: some responses invent
details, and the report identifies examples.

API receipts report $0.147289462 across 80 responses. The 20 rejected calls
returned no cost receipt, so their cost is unknown. Conservative reservations
for all attempts total $0.577059588, below the $2 admission ceiling. Four offline
runner tests and an audit of limits, receipts and credential exclusion passed.

The reusable runner and exact prompts, model settings, responses and analysis
are retained together:

- [Usage and reuse](/Users/santoshkumar/aforge-expectation-probe-20260909/README.md)
- [Findings, coverage and limitations](/Users/santoshkumar/aforge-expectation-probe-20260909/RESULTS.md)
- [Runner](/Users/santoshkumar/aforge-expectation-probe-20260909/probe.py)
- [Scenario configuration](/Users/santoshkumar/aforge-expectation-probe-20260909/scenarios.json)

Assistant review surfaced three useful tensions to test, not product decisions:

1. A person may remember the original conversation's location while looking for
   an output by subject. Both routes can matter for the same underlying work.
2. Multiple routes need a consistent current output; copies and silent moves or
   renaming can make people lose track of the current plan or their way back.
3. A conversation started from Home may be resumed through recent history. The
   person need not decide on a folder merely to continue talking.

The missing filing contract is itself consequential: responses disagree about
whether the app files anything automatically and whether generated plans are
durable artifacts. Clarifying and demonstrating those behaviors is more useful
than treating the response counts as votes. The proposed stable-conversation,
multiple-entry-point interpretation remains unconfirmed. Discoverability in a
folder must also remain distinct from applicability or authority there.

## Complementary human research

[Stuff I've Seen (Dumais et al., SIGIR 2003)](https://www.microsoft.com/en-us/research/publication/stuff-ive-seen-a-system-for-personal-information-retrieval-and-re-use/)
studied a unified index across previously seen personal information, with more
than 230 internal users. Its reported initial findings identify time and people
as useful retrieval cues. This is actual, historical usage research in a
specific environment, not proof of our AI-workspace design or a representative
modern user population. It gives us another reason to investigate multiple
ways of returning to the same information rather than assuming everyone will
remember one filing location. That last implication is our interpretation.
