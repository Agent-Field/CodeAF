---
kind: added
title: bench/frontiercode — a FrontierCode-style evaluation rig
pr: 1670
surface: [build, docs]
invalidates:
  - "There was no \"would a maintainer merge this\" grading rig. bench/frontiercode/ now runs a harness on a containerized real-repository task and grades the result against a twelve-criterion rubric (prompt blockers judged by an LLM judge from the host, command and classical criteria run offline in a --network none verifier), with gold, negative and sealed-fixture controls calibrating it."
  - "The agent's egress in a rollout was either blocked or unobserved. run.sh now routes the agent container through an inverted egress proxy that is OPEN and LOGS every host it touches, and grade/scanner.py cross-references that log and the session transcript against a host list, flagging patch-shape and upstream-clone evidence."
  - "No baseline arm existed. pier-arm.sh runs mini-swe-agent through Pier on the same fixture, the same model and the same grader (Pier's stricter allowlist egress posture is recorded in the row and in PIER-BASELINE.md)."
  - "The first two rollouts of the fixture terminated fail on a pristine-passing change because the environment image lacked xxd, which three base-tree tests need. The image installs xxd now and the full upstream suite exits 0 on the base tree."
  - "The rig was local-only: ordinary Docker on the developer's Mac, one container budget at a time, with no gcloud, no Compute Engine, no image distribution, no shard scheduling, no result retrieval and no teardown. bench/frontiercode/ now also carries a GCP-native campaign path — a per-campaign manifest as the single source of truth, gcp-create.sh / gcp-stage.sh / gcp-key.sh / launch.sh / fetch-results.sh with --plan and --check paths that run the gates and touch nothing, credential-free staging with the binary re-hashed on the host, waves of at most wave_capacity with the reference's gates, and a fetch-before-teardown completeness proof — while the grader, proxy, scanner, controls, tasks and README are unchanged. No GCP instance was created and no model money was spent landing it; the design notes and the adapted-vs-copied ledger are in bench/frontiercode/GCP.md."
---
---
