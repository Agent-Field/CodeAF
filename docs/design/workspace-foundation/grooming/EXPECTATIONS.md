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

C14 establishes how these probes enter ongoing grooming. Bring back the concrete
situation, expectations and dissent, meaningful alternatives, and our proposed
behavior. The person can confirm what is clear and leave the rest open. Do not
ask them to supply an architecture alone or infer acceptance from model output.
Every checkpoint states what is settled, what remains open, and the next step.

Keep the whole goal visible: one personal AI environment for direct creation,
delegated work and ongoing assistance across domains, with less repeated
explanation, inconsistent assumptions and effort to find or steer work. The
aspiration to encompass competing systems' useful task space is a goal, not a
claim established by a folder experiment. Compare proposals by those consequences
and existing conceptual boundaries, not the number of mechanisms they add.

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

## Second run: concrete behavior and alternatives

Following C14, round 2 sampled five models × three situations × two question
modes: open consequences and explicit alternatives. It stopped at 30 attempts,
returning 24 schema-compliant answers, five rate-limit rejections and one schema
failure. Receipts report $0.05071750499936 across 25 responses; five rejection
costs are unavailable. Conservative reservation was $0.197674776 against the
$0.75 ceiling. Four offline tests and the complete receipt audit passed. No
additional paid judge calls were made.

[Round 2 results](/Users/santoshkumar/aforge-expectation-probe-20260909/round2/RESULTS.md)
and [reusable configuration/usage](/Users/santoshkumar/aforge-expectation-probe-20260909/round2/README.md)
retain exact prompts, outputs, model/mode coverage and limitations.

The concrete situations and proposed interpretation are:

- Editing the hotel through Travel updates the one current itinerary reached
  through either folder. Prior conversation messages retain what was said;
  reaching the current output must not require treating old prose as current.
- “This holiday has nothing to do with Launch” corrects the mistaken association
  without deleting the plan or erasing the mixed conversation. How to represent
  the correction so it is not immediately inferred again remains to groom.
- “Keep this trip under $2000” remains about that trip after a discovery link is
  removed; it does not become a budget for other trips or launch work.

Meaningful dissent: Kimi call 26 favored confirmation before unlinking; GLM call
12 favored immediate reversible correction with an acknowledgment. The proposed
default is the latter when the person's correction has a clear referent and the
change concerns organization alone. Ambiguous targets or changes to underlying
work require resolving that ambiguity, not a blanket confirmation ceremony.

Do not mistake the experiment for independent support for automatic discovery.
Case 1 explicitly supplies that candidate contract, even in open mode. GLM call
29 rejects manual filing partly because it conflicts with the supplied contract;
that is not an unbiased preference comparison. Some outputs invent features or
overstate uncertainty in the explicit phrase “this trip.” Such simulation defects
are not product requirements. These results refine proposals, not settled D02 or
D10 behavior, and do not establish any new implementation capability.

Next: present these concrete consequences, the automatic/suggested/manual
discovery alternatives and a reasoned recommendation to the person. Once behavior
is clear, tick only that contract and examine explicit folder/subtree rules versus
trip-specific rules, including corrections that must persist across later work.

## Third run: explicit scope, exceptions and remembered corrections

After C15 was accepted, the user requested the next simulation and a return to the
whole diagram. Round 3 used five models, three cases and two open wordings, with
30 total attempts and a $0.75 ceiling. It returned 28 schema-compliant answers,
one API rejection and one schema failure. Reported cost is $0.054263774 across
29 receipts; one rejected call has no cost receipt. Conservative reservation was
$0.203497992. Four offline tests and the complete receipt audit passed.

[Round 3 results](/Users/santoshkumar/aforge-expectation-probe-20260909/round3/RESULTS.md)
retain the exact configuration, responses, dissent and model/mode coverage. Both
prompts were open, but the scenarios explicitly supplied an unresolved move policy;
that uncertainty was not independently discovered. Some answers manufacture doubt
about clear statements or mistake compatible instructions for conflicts.

Proposals to discuss, not yet new confirmed behavior:

- “Travel and its subfolders: $2000” plus “this conference trip: $2600” means the
  local exception changes this trip only. A later correction follows its actual
  wording; neither newest-wins nor most-specific-wins is a complete universal
  policy. Keep compatible guidance: formal can still be concise.
- Reprocessing unchanged history must not recreate a corrected inferred relation.
  A later explicit request to use that itinerary as a reference for different work
  provides new, purpose-specific relevance without changing the holiday's purpose.
- Actual moves and references differ. For rules expressly tied to current folder
  membership, our proposed behavior is to recalculate future applicable direction
  after a move, preserve existing drafts/history and item-specific instructions,
  and make changed scope understandable. An inherited-at-creation rule that follows
  the work is an alternative; this contract remains to confirm.

The [diagram checkpoint](DISCUSSION.md#return-to-diagram-checkpoint-after-c15)
separates agreed product behavior, partial implementation evidence and full-journey
gaps. Topics 1/2 are not wholly done. After confirming these scope transitions, the
next demonstration must carry a correction through applicable context, affected
work, the resulting artifact and the person's view, without asserting automatic
activation or peer coordination until their actual paths are tested.

## Complementary human research

[Stuff I've Seen (Dumais et al., SIGIR 2003)](https://www.microsoft.com/en-us/research/publication/stuff-ive-seen-a-system-for-personal-information-retrieval-and-re-use/)
studied a unified index across previously seen personal information, with more
than 230 internal users. Its reported initial findings identify time and people
as useful retrieval cues. This is actual, historical usage research in a
specific environment, not proof of our AI-workspace design or a representative
modern user population. It gives us another reason to investigate multiple
ways of returning to the same information rather than assuming everyone will
remember one filing location. That last implication is our interpretation.
