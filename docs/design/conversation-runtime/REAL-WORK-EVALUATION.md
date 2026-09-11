# Real work, with an independent judge

Inventory and decisions checked on 2026-09-05. This supplements the execution plan;
it is not a new result table.

## Use the two existing rigs

`bench/conversation` already compares Aforge, Pi and OMP through print and real
tmux conversation doors, with frozen campaigns and attributable model billing.
Its small fixtures are good diagnostics but cannot establish performance on large
repository changes.

`bench/deepswe` already supplies containerized repository tasks and a separate
network-disabled grader. Spark has all 113 task directories at
`/home/santosh/src/swe-pro/tools/deepswe-bench/tasks`, in repository revision
`ecf7638b3705c2e08d2b10e38fcca8c70ee73f70`. Absence on the Mac is not absence of
the corpus. Open PR #634 adds a chat door to this rig; inspect and reuse that work
before implementing a second container/chat adapter.

The integration gap is comparison adapters and comparable conditions. Add Pi and
OMP behind the existing container execution boundary, retaining the conversation
rig's inference guard and receipt accounting. Do not copy the corpus's hidden tests
or reference patches into any agent container. Freeze the task files and actual
image digest, not just a mutable image tag.

## Two complementary sources

Actual GitHub defects supply clear provenance and realistic reports. The initial
five local-repository candidates cover jobs on reopen, spending display, usage
frames, named-file scope and JSON error reporting. They remain diagnostics from
one Go repository. A twelve-line fix is a real issue but not evidence of complex
parallel execution. Aforge working on its own code may have product-specific
advantages; disclose them and include unrelated repositories.

DeepSWE supplies broader repository feature work. These are benchmark tasks with
repository provenance, not automatically original GitHub issues: require the
source issue URL before labeling a task an actual GitHub issue. Initial candidates
include:

| Repository task | Frozen source base | Why evaluate it |
| --- | --- | --- |
| `unjs/ofetch`: per-origin circuit breaker | `dfbe3ca4ef8a22fc023fca5a5ef530e525f5e523` | Stateful behavior, failure handling and regression control |
| `vadimdemedes/ink`: grid box layout | `0cea59169ef0f3f83e4aa7fbedbff9d165646472` | Layout semantics and integration across components |
| `dry-python/returns`: error-accumulating Validated | `41607fae1289de2787523c452d75212206b9c7c0` | Interface compatibility and interacting laws |
| `vitest-dev/vitest`: duration sharding | `647e6ade3b99523e3a0387a65fccfe918c331236` | Scheduling behavior and existing test-runner expectations |

These choices cover different forms of coupling, not a guarantee that splitting
them is profitable. Predeclare selection before comparing harness scores. Include
an easy control, moderate multi-file work and substantial coupled work rather than
selecting only the previous rig's five easiest cases.

## Controls before inference

1. Inspect the exact model-visible issue or instruction. Some GitHub reports
   contain a complete suggested fix; copying the body verbatim can turn an
   investigation benchmark into transcription. Store provenance separately and
   record any derived prompt and its checksum. A lexical leak check is lint, not
   proof that the task contains no answer.
2. Check that the starting environment works. Run the independent tests on the
   base and reference. Classify behavioral failures separately from unavailable
   dependencies, emulator crashes and missing reports. A missing public API that
   the feature explicitly asks for differs from a test demanding the reference
   fix's private helper name.
3. Protect test and grader material. Do not protect the implementation files that
   the issue requires changing. Run grading outside the candidate process and
   account for modified harness hooks, tests and dependencies.
4. Validate alternate implementations where feasible. A test can pass the gold
   patch and still overfit its internal structure. Existing tests supply evidence,
   not automatic authority over the required behavior.
5. Freeze machine architecture, toolchain, resource limits, images and network
   policy for every arm. Spark is arm64; existing corpus images use amd64
   emulation. The rig has an emulation workaround, which must be recorded and
   revalidated rather than silently treated as native performance.
6. Establish all-arm startup and cost capture before a scored run. A model guard
   and a harness that cannot use the same capabilities is an infrastructure
   problem, not a quality zero. Judge runtime and harness runtime are distinct.

The corpus's current verifier builder uses tags and its positive-control script
does not itself prove the negative control. Neither is a sufficient freeze for a
new comparison campaign without the checks above. Cached images for ofetch, ink
and returns were found on Spark; vitest's image was not cached at inspection.
Availability is not a fresh base/reference validation.

## Promotion to a comparative result

Run a small all-arm preflight, then paired randomized repetitions under a fixed
cap appropriate to each complexity tier. Keep the original issue outcome primary;
test counts, patch size and number of lanes are diagnostic. Include a chat variant
with a clear mid-work requirement change and an unrelated question while work
continues. Follow-up text must add the same information through each harness's
supported interaction door.

Report quality, billed cost, time to a usable accepted result, correct follow-up
latency and revision adoption separately. Preserve timeouts even when some files
look correct. Unknown billing stays unknown. The next optimization should address
the measured critical path or quality failure, followed by a held-out check;
increased parallel activity is not by itself an improvement.
