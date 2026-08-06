#!/usr/bin/env python3
"""Validate every task and grader before a cent is spent.

Both Phase A labs ran a check like this and between them it caught 26 defects
in ground truth -- wrong expected answers, infeasible tasks, graders that
accepted a near-miss. Every one of those would have surfaced later as "the
models are weak" and been unfalsifiable after the fact.

What is checked here, per task:

  * a **reference solution** exists and scores full marks. If the reference
    cannot pass, a model failure says nothing about the model.
  * an **empty or unattempted workspace** scores zero, and does so through the
    gate rather than by accident.
  * **near-miss decoys** are rejected. This is the check that catches a grader
    which is really just testing whether a file exists: for each field, a
    submission that is correct everywhere except that field must lose exactly
    that field.
  * for T2, the answer key is **re-derived from the corpus text** rather than
    trusted. truth.py holds transcribed numbers; if a transcription and the
    document disagree, the document wins and this fails.

Exit status is 0 only if everything passes.

Usage: python3 selfcheck.py [-v]
"""
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
TASKS = os.path.join(HERE, "tasks")
sys.path.insert(0, os.path.join(TASKS, "t2-synthesis"))

import truth  # noqa: E402

FAILURES = []
CHECKS = 0
VERBOSE = "-v" in sys.argv


def check(label, condition, detail=""):
    global CHECKS
    CHECKS += 1
    if condition:
        if VERBOSE:
            print(f"  ok    {label}")
        return True
    FAILURES.append(f"{label}{(': ' + detail) if detail else ''}")
    print(f"  FAIL  {label}" + (f"  -- {detail}" if detail else ""))
    return False


def run_grader(task, workspace):
    p = subprocess.run(
        [sys.executable, os.path.join(TASKS, task, "grade.py"), workspace, "--json"],
        capture_output=True, text=True, timeout=600)
    line = [l for l in p.stdout.splitlines() if l.startswith("{")]
    if not line:
        return {"_error": (p.stdout + p.stderr)[-800:]}
    return json.loads(line[-1])


# ---------------------------------------------------------------------------
# T2: re-derive the answer key from the corpus text

CORPUS = os.path.join(TASKS, "t2-synthesis", "seed", "corpus")


def read(name):
    with open(os.path.join(CORPUS, name), encoding="utf-8") as f:
        return f.read()


def check_t2_corpus():
    print("t2-synthesis: corpus vs. transcribed answer key")
    catalog = read("01-service-catalog.md")

    # Every service block, parsed out of the prose the agent will read.
    blocks = re.split(r"^## ", catalog, flags=re.M)[1:]
    parsed = {}
    for block in blocks:
        sid = block.split(" ", 1)[0].strip()
        owner = re.search(r"- Owner:\s*(.+)", block)
        queue = re.search(r"- Queue client:\s*(\S+)", block)
        msgs = re.search(r"- Messages per day:\s*([\d,]+)", block)
        if not (owner and queue and msgs):
            continue
        owner_text = owner.group(1).strip()
        parsed[sid] = {
            "owner": None if "vacant" in owner_text.lower() else owner_text,
            "queue": queue.group(1).strip(),
            "msgs_per_day": int(msgs.group(1).replace(",", "")),
        }

    check("t2 catalog: every service in truth.py is in the document",
          set(parsed) == set(truth.CATALOG),
          f"document has {sorted(parsed)}, truth has {sorted(truth.CATALOG)}")
    for sid, meta in truth.CATALOG.items():
        got = parsed.get(sid, {})
        check(f"t2 catalog {sid}: queue",
              got.get("queue") == meta["queue"],
              f"document says {got.get('queue')!r}, truth says {meta['queue']!r}")
        check(f"t2 catalog {sid}: messages per day",
              got.get("msgs_per_day") == meta["msgs_per_day"],
              f"document says {got.get('msgs_per_day')}, truth says {meta['msgs_per_day']}")
        check(f"t2 catalog {sid}: owner vacancy",
              (got.get("owner") is None) == (meta["owner"] is None),
              f"document owner {got.get('owner')!r}, truth {meta['owner']!r}")

    # Pricing: the schedule in force must be the one truth.py priced from.
    current = read("08-vendor-pricing-2026-03.md")
    superseded = read("07-vendor-pricing-2025-11.md")
    check("t2 pricing: 2026-03 is the later effective date",
          re.search(r"Effective date:\*\*\s*2026-03-01", current) is not None
          and re.search(r"Effective date:\*\*\s*2025-11-01", superseded) is not None)
    for tech, cents in truth.PRICE_CENTS_PER_10K.items():
        if tech in truth.TIERS:
            continue          # tiered technologies are checked below
        row = re.search(rf"\|\s*{re.escape(tech)}\s*\|\s*(\d+) cents per 10,000", current)
        check(f"t2 pricing {tech}", row is not None and int(row.group(1)) == cents,
              f"document says {row.group(1) if row else None}, truth says {cents}")
    # the round-2 tier, read out of the document rather than trusted
    for tech, tier in truth.TIERS.items():
        threshold = f"{tier['threshold_messages']:,}"
        check(f"t2 tier {tech}: threshold is in the document",
              threshold in current, f"looking for {threshold}")
        check(f"t2 tier {tech}: first-tier rate is in the document",
              re.search(rf"the first {re.escape(threshold)}\s*\|\s*"
                        rf"{truth.PRICE_CENTS_PER_10K[tech]} cents", current) is not None)
        check(f"t2 tier {tech}: above-tier rate is in the document",
              re.search(rf"above {re.escape(threshold)}\s*\|\s*"
                        rf"{tier['above_cents_per_10k']} cents", current) is not None)
        check(f"t2 tier {tech}: the estate actually crosses the threshold",
              truth.monthly_messages_by_queue()[tech] > tier["threshold_messages"],
              "a tier nobody reaches changes no answer and traps nothing")
    check("t2 the tier changes the answer",
          truth.monthly_cost_usd_cents() != sum(
              (m["msgs_per_day"] * truth.BILLING_DAYS // 10_000)
              * truth.PRICE_CENTS_PER_10K[m["queue"]] for m in truth.CATALOG.values()),
          "tiered and flat pricing give the same total, so the tier is invisible")
    check("t2 pricing: the superseded schedule really does differ",
          any(re.search(rf"\|\s*{re.escape(t)}\s*\|\s*(\d+) cents", superseded).group(1)
              != str(c) for t, c in truth.PRICE_CENTS_PER_10K.items()),
          "the distractor pricing table is identical to the live one, so it "
          "traps nothing")
    check("t2 pricing: billing month is 30 days in the document",
          "30 days" in current and truth.BILLING_DAYS == 30)

    # Incidents: each report's stated impact window must produce truth's minutes.
    auth_jan = read("02-incident-2026-01-14.md")
    check("t2 incident 02: stated duration",
          "3 hours and 40 minutes" in auth_jan and truth.INCIDENTS["02"]["minutes"] == 220)
    search_feb = read("03-incident-2026-02-03.md")
    check("t2 incident 03: impact window in the document",
          "14:07" in search_feb and "15:52" in search_feb
          and truth.INCIDENTS["03"]["minutes"] == 105)
    auth_feb = read("04-incident-2026-02-27.md")
    check("t2 incident 04: impact window crosses midnight",
          "22:40" in auth_feb and "01:25" in auth_feb
          and truth.INCIDENTS["04"]["minutes"] == 165)
    check("t2 incident services match the reports",
          all(f"**Affected service:** {truth.INCIDENTS[d]['service']}" in read(f)
              for d, f in [("02", "02-incident-2026-01-14.md"),
                           ("03", "03-incident-2026-02-03.md"),
                           ("04", "04-incident-2026-02-27.md")]))

    # Supersession and deprecation, straight from the ADRs.
    adr7 = read("05-adr-0007-queue-migration.md")
    adr11 = read("06-adr-0011-queue-migration-revised.md")
    check("t2 ADR-0007 declares itself superseded",
          "Superseded by ADR-0011" in adr7)
    check("t2 ADR-0011 deprecates both technologies",
          "rabbit-legacy" in adr11 and "kafka-shared" in adr11
          and "deprecated as of this ADR" in adr11
          and truth.DEPRECATED_QUEUES == {"rabbit-legacy", "kafka-shared"})
    check("t2 superseded_docs matches the three superseded documents",
          sorted(truth.SUPERSEDED_DOCS) == ["05", "07", "10"])

    # The planted contradictions must actually be present in the claiming docs.
    notes = read("09-oncall-notes.md")
    check("t2 contradiction 09/02: the wrong duration is in the notes",
          "90 minutes" in notes)
    check("t2 contradiction 09/01: the wrong owner is in the notes",
          "team-platform owns it" in notes)
    check("t2 contradiction 03/01: the wrong queue is in the incident report",
          "pulsar-edge" in search_feb and truth.CATALOG["svc-search"]["queue"] != "pulsar-edge")
    check("t2 contradiction 07/08: the superseded schedule really states "
          "different prices for the same technologies",
          any(int(re.search(rf"\|\s*{re.escape(t)}\s*\|\s*(\d+) cents", superseded).group(1)) != c
              for t, c in truth.PRICE_CENTS_PER_10K.items()))
    check("t2 every field in the vocabulary is used by at least one "
          "contradiction, or is documented as unused",
          {c["field"] for c in truth.CONTRADICTIONS} == set(truth.FIELD_VOCABULARY),
          f"unused: {sorted(set(truth.FIELD_VOCABULARY) - {c['field'] for c in truth.CONTRADICTIONS})}"
          " — a field offered with nothing pointing at it reads as a deliberate "
          "trap and was exactly how round 1's key went wrong")
    check("t2 contradiction fields are all in the closed vocabulary",
          all(c["field"] in truth.FIELD_VOCABULARY for c in truth.CONTRADICTIONS))
    check("t2 the answer is internally consistent",
          truth.most_impacted_service() == "svc-auth"
          and truth.total_incident_minutes() == 490
          and truth.monthly_cost_usd_cents() == 224820,
          f"minutes={truth.total_incident_minutes()} "
          f"cost={truth.monthly_cost_usd_cents()}")

    # The corpus must be readable at all: ten documents, no more, no fewer.
    files = sorted(os.listdir(CORPUS))
    check("t2 corpus has exactly eleven documents", len(files) == 11, str(files))

    # the round-2 duplicate filing
    refiled = read("10-incident-2026-02-27-refiled.md")
    original = read("04-incident-2026-02-27.md")
    check("t2 refiling 10: same service and night as doc 04",
          "**Affected service:** svc-auth" in refiled and "2026-02-27" in refiled)
    check("t2 refiling 10: states a different duration from doc 04",
          "3 hours and 5 minutes" in refiled
          and truth.REFILINGS["10"]["claimed_minutes"] != truth.INCIDENTS["04"]["minutes"])
    check("t2 refiling 10: doc 04 is demonstrably the earlier filing",
          "2026-02-28 03:10" in original and "2026-03-02 09:20" in refiled)
    check("t2 refiling 10: RULES states the one-incident rule",
          "filed more than once is still one incident" in read("RULES.md"))
    check("t2 refiling 10: it is not counted as a fourth incident",
          "10" not in truth.INCIDENTS and truth.total_incident_minutes() == 490)
    check("t2 refiling 10: it is superseded and it contradicts doc 04",
          "10" in truth.SUPERSEDED_DOCS
          and {"claim_doc": "10", "authoritative_doc": "04",
               "field": "incident_duration_minutes"} in truth.CONTRADICTIONS)


# ---------------------------------------------------------------------------
# T2: reference submission and decoys

def seed_corpus(directory):
    """Every t2 workspace carries the corpus, because the grader gates on it
    being unaltered. A decoy workspace without one would fail the gate rather
    than the field it was built to test, which would make every decoy check
    pass for the wrong reason."""
    shutil.copytree(CORPUS, os.path.join(directory, "corpus"))


def write_submission(directory, findings, report_text):
    seed_corpus(directory)
    with open(os.path.join(directory, "findings.json"), "w") as f:
        json.dump(findings, f, indent=2)
    with open(os.path.join(directory, "REPORT.md"), "w") as f:
        f.write(report_text)


def reference_report(key):
    services = ", ".join(key["services_on_deprecated_queue"])
    unowned = " and ".join(key["unowned_services"])
    body = f"""# Platform queue audit

## Deprecated queue technologies (doc 06)

doc 06 (ADR-0011) supersedes doc 05 (ADR-0007) in full and deprecates
kafka-shared alongside rabbit-legacy, leaving pulsar-edge as the only current
technology. Reading doc 05 alone gives the wrong answer. On that basis the
services on a deprecated queue are {services} — every service in doc 01 except
the one already on pulsar-edge.

## Incidents (doc 02, doc 03, doc 04)

Total customer impact across the three reports is
{key['total_incident_minutes']} minutes. doc 02 states its window in words,
doc 03 gives clock times, and doc 04 runs from 22:40 on one day to 01:25 on the
next, crossing midnight UTC. The most impacted service is
{key['most_impacted_service']}, which appears in two of the three reports.

## Cost (doc 08)

Under the schedule in doc 08, effective 2026-03-01, the monthly bill for the
whole catalog is {key['monthly_cost_usd_cents']} cents. doc 07 is the earlier
schedule and is superseded on its whole contents by rule 4 of RULES.md, so its
higher rabbit and kafka rates do not apply.

## Ownership (doc 01)

{unowned} have no owner. doc 01 records both as vacant with the owning team
dissolved or departed.

## Supersession and contradictions

Superseded documents: doc 05, superseded explicitly by doc 06; and doc 07,
superseded by doc 08 under rule 4.

Three contradictions, each decided by RULES.md:

- doc 03 states that svc-search publishes through pulsar-edge. doc 01 owns
  service metadata under rule 1, and records kafka-shared. doc 01 wins; this is
  a contradiction on queue_client. It matters, because believing doc 03 would
  drop svc-search from the deprecated list.
- doc 09 puts the 2026-01-14 auth impact at about 90 minutes. doc 02 owns
  incident facts under rule 2 and states 3 hours 40 minutes. doc 02 wins; a
  contradiction on incident_duration_minutes.
- doc 09 asserts that team-platform owns svc-notify. doc 01 owns service
  metadata under rule 1 and records the ownership as vacant. Operational
  coverage is not ownership. doc 01 wins; a contradiction on owner.

On-call notes are never authoritative under rule 5, which is what decides two
of the three. Nothing in this audit conflicts with anything outside the corpus,
and no finding here rests on a document that has been superseded.
"""
    return body + ("\nfiller " * max(0, 420 - len(re.findall(r"\S+", body))))


def check_t2_grader():
    print("t2-synthesis: grader")
    key = truth.answer()
    with tempfile.TemporaryDirectory() as d:
        write_submission(d, key, reference_report(key))
        r = run_grader("t2-synthesis", d)
        check("t2 reference submission scores full marks",
              r.get("score") == 1.0 and r.get("success") is True,
              json.dumps({k: v for k, v in r.get("checks", {}).items() if not v}))

    with tempfile.TemporaryDirectory() as d:
        seed_corpus(d)
        r = run_grader("t2-synthesis", d)
        check("t2 a workspace with the corpus and no deliverables scores zero",
              r.get("score") == 0.0 and r.get("success") is False)

    # the corpus gate: a submission that is right about everything but altered
    # the evidence it was reasoning from scores nothing
    with tempfile.TemporaryDirectory() as d:
        write_submission(d, key, reference_report(key))
        with open(os.path.join(d, "corpus", "01-service-catalog.md"), "a") as f:
            f.write("\n<!-- a harmless-looking edit -->\n")
        r = run_grader("t2-synthesis", d)
        check("t2 editing the corpus zeroes the score",
              r.get("score") == 0.0 and r.get("score_before_gates") == 1.0
              and r.get("gates", {}).get("corpus_intact") is False,
              json.dumps(r.get("gates", {})))

    with tempfile.TemporaryDirectory() as d:
        write_submission(d, key, reference_report(key))
        os.remove(os.path.join(d, "corpus", "09-oncall-notes.md"))
        r = run_grader("t2-synthesis", d)
        check("t2 deleting a corpus document zeroes the score",
              r.get("score") == 0.0)

    # One decoy per field: correct everywhere else, so the grader must lose
    # exactly the field that was spoiled and nothing else.
    decoys = {
        "deprecated_queue": ("services_on_deprecated_queue",
                             ["svc-auth", "svc-billing", "svc-notify", "svc-reports"]),
        "incident_minutes": ("total_incident_minutes", 400),
        "most_impacted": ("most_impacted_service", "svc-search"),
        "monthly_cost": ("monthly_cost_usd_cents", 348_720),
        "unowned": ("unowned_services", ["svc-notify"]),
    }
    for expected_loss, (field, bad) in decoys.items():
        with tempfile.TemporaryDirectory() as d:
            spoiled = dict(key)
            spoiled[field] = bad
            write_submission(d, spoiled, reference_report(key))
            r = run_grader("t2-synthesis", d)
            lost = {k for k, v in r.get("checks", {}).items() if not v}
            check(f"t2 decoy on {field} loses exactly {expected_loss}",
                  lost == {expected_loss},
                  f"lost {sorted(lost)}")

    # contradictions: one missing, and one invented, must both be rejected
    for label, bad in [
        ("a missing contradiction", key["contradictions"][:-1]),
        # The ADR pair. Round 1's arm-A run returned this alongside the
        # pricing pair; the pricing pair turned out to be correct and is now in
        # the key, this one is not, and the brief says why. Both readings are
        # pinned here so neither can drift back into ambiguity.
        ("the ADR pair, which is supersession rather than contradiction",
         key["contradictions"] + [{"claim_doc": "05", "authoritative_doc": "06",
                                   "field": "queue_client"}]),
        ("a right pair with the wrong field",
         [dict(c, field="owner") for c in key["contradictions"]]),
    ]:
        with tempfile.TemporaryDirectory() as d:
            spoiled = dict(key)
            spoiled["contradictions"] = bad
            write_submission(d, spoiled, reference_report(key))
            r = run_grader("t2-synthesis", d)
            check(f"t2 rejects {label}",
                  r.get("checks", {}).get("supersession_and_contradictions") is False)

    # a correct JSON with no report is not a pass
    with tempfile.TemporaryDirectory() as d:
        seed_corpus(d)
        with open(os.path.join(d, "findings.json"), "w") as f:
            json.dump(key, f)
        r = run_grader("t2-synthesis", d)
        check("t2 correct JSON with no REPORT.md is not a success",
              r.get("success") is False and r.get("score", 1.0) == 7 / 8)

    # a report that dodges the numbers is caught
    with tempfile.TemporaryDirectory() as d:
        write_submission(d, key, "# Audit\n\n" + ("words " * 500))
        r = run_grader("t2-synthesis", d)
        check("t2 a long report with no findings in it fails the report check",
              r.get("checks", {}).get("report") is False)

    # extra keys are a schema failure
    with tempfile.TemporaryDirectory() as d:
        write_submission(d, {**key, "confidence": "high"}, reference_report(key))
        r = run_grader("t2-synthesis", d)
        check("t2 an extra key fails the schema check",
              r.get("checks", {}).get("schema") is False)


# ---------------------------------------------------------------------------
# T1 and T3

def check_t1():
    print("t1-logstore: grader")
    reference = os.path.join(TASKS, "t1-logstore", "reference")
    r = run_grader("t1-logstore", reference)
    check("t1 reference passes every group",
          r.get("score") == 1.0 and r.get("success") is True,
          json.dumps({k: v.get("first_failure") for k, v in r.get("groups", {}).items()
                      if not v.get("ok")}))
    with tempfile.TemporaryDirectory() as d:
        r = run_grader("t1-logstore", d)
        check("t1 empty workspace scores zero through the package gate",
              r.get("score") == 0.0 and r.get("gates", {}).get("package_present") is False)

    # a package that imports but does nothing must score zero, not crash the
    # grader -- this is the shape of a real early-stopping run
    with tempfile.TemporaryDirectory() as d:
        os.mkdir(os.path.join(d, "tinylog"))
        with open(os.path.join(d, "tinylog", "__init__.py"), "w") as f:
            f.write("class LogStore:\n    def __init__(self, path):\n        pass\n")
        r = run_grader("t1-logstore", d)
        check("t1 a stub package scores zero without erroring",
              r.get("score") == 0.0 and r.get("gates", {}).get("import") is True,
              json.dumps(r)[:300])

    # nesting is found rather than punished as "produced nothing"
    with tempfile.TemporaryDirectory() as d:
        nested = os.path.join(d, "src")
        shutil.copytree(os.path.join(reference, "tinylog"),
                        os.path.join(nested, "tinylog"))
        r = run_grader("t1-logstore", d)
        check("t1 a nested package is found and scored",
              r.get("score") == 1.0 and r.get("package_root") == "src")


def check_t3():
    print("t3-shiftplan: grader")
    reference = os.path.join(TASKS, "t3-shiftplan", "reference")
    seed = os.path.join(TASKS, "t3-shiftplan", "seed")

    r = run_grader("t3-shiftplan", reference)
    check("t3 reference repairs every family",
          r.get("score") == 1.0 and r.get("success") is True,
          json.dumps({k: v.get("first_failure") for k, v in r.get("groups", {}).items()
                      if not v.get("ok")}))

    r = run_grader("t3-shiftplan", seed)
    check("t3 the shipped buggy package repairs no family",
          r.get("score") == 0.0 and r.get("groups_passed", 0) == 0,
          json.dumps({k: v.get("ok") for k, v in r.get("groups", {}).items()}))

    # every family must be individually detectable: reverting one module of the
    # reference back to the seed has to cost exactly the families that module
    # owns. Without this, a family could be passing for a reason unrelated to
    # the defect it is supposed to detect.
    module_families = {
        "interval.py": {"1"},
        "payroll.py": {"2"},
        "loader.py": {"6"},
        # assign.py carries four defects and the refactor, so reverting it must
        # cost exactly those five families and leave the other two standing.
        "assign.py": {"3", "4", "5", "7"},
    }
    for module, families in module_families.items():
        with tempfile.TemporaryDirectory() as d:
            shutil.copytree(reference, os.path.join(d, "w"),
                            ignore=shutil.ignore_patterns("__pycache__"))
            shutil.copy2(os.path.join(seed, "shiftplan", module),
                         os.path.join(d, "w", "shiftplan", module))
            r = run_grader("t3-shiftplan", os.path.join(d, "w"))
            lost = {k for k, v in r.get("groups", {}).items() if not v.get("ok")}
            check(f"t3 reverting {module} loses exactly families {sorted(families)}",
                  lost == families, f"lost {sorted(lost)}")

    # the tamper gate
    with tempfile.TemporaryDirectory() as d:
        shutil.copytree(reference, os.path.join(d, "w"),
                        ignore=shutil.ignore_patterns("__pycache__"))
        with open(os.path.join(d, "w", "tests", "test_visible.py"), "a") as f:
            f.write("\n# an innocuous-looking edit\n")
        r = run_grader("t3-shiftplan", os.path.join(d, "w"))
        check("t3 editing the visible suite zeroes the score",
              r.get("score") == 0.0 and r.get("score_before_gates") == 1.0,
              json.dumps(r.get("gates", {})))

    with tempfile.TemporaryDirectory() as d:
        shutil.copytree(reference, os.path.join(d, "w"),
                        ignore=shutil.ignore_patterns("__pycache__"))
        os.remove(os.path.join(d, "w", "tests", "test_visible.py"))
        r = run_grader("t3-shiftplan", os.path.join(d, "w"))
        check("t3 deleting the visible suite zeroes the score",
              r.get("score") == 0.0)

    # the seed the agent is actually handed must be the buggy one, and its
    # shipped tests must fail in exactly the two places the brief promises
    p = subprocess.run([sys.executable, "-m", "pytest", "-q", "-p", "no:cacheprovider",
                        "tests/test_visible.py"],
                       cwd=seed, capture_output=True, text=True, timeout=300)
    failed = int((re.search(r"(\d+) failed", p.stdout + p.stderr) or [0, 0])[1])
    passed = int((re.search(r"(\d+) passed", p.stdout + p.stderr) or [0, 0])[1])
    check("t3 seed's visible suite fails 4 and passes 8",
          (failed, passed) == (4, 8), f"got {failed} failed / {passed} passed")


def main():
    print("== selfcheck ==\n")
    check_t2_corpus()
    print()
    check_t2_grader()
    print()
    check_t1()
    print()
    check_t3()
    print(f"\n{CHECKS - len(FAILURES)}/{CHECKS} checks passed")
    if FAILURES:
        print(f"\n{len(FAILURES)} FAILURES — do not spend money until these are fixed:")
        for f in FAILURES:
            print(f"  - {f}")
        sys.exit(1)
    print("all clear")


if __name__ == "__main__":
    main()
