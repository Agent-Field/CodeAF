#!/usr/bin/env python3
"""The rubric engine: one file, two places.

`phase-a` runs INSIDE the verifier container (no network): apply the graded
patch, then run every criterion that a container can answer — command,
classical, reverse-classical and scope — in rubric order, and write
phaseA.json plus the evidence the LLM judge (which runs on the host, the only
networked part of grading) will read.

`combine` runs on the HOST: merge phase A's verdicts with the judge's results
and answer the run's grade — score, blockers, and the rule that a criterion
whose machinery failed is `rig`, never 0 and never silently skipped.

The rubric file is the task's own rubric.toml; see it for the schema. The six
criterion kinds follow FrontierCode's published table (classical, command,
reverse-classical, adaptive-classical, scope, prompt), with our own choices
where FrontierCode publishes nothing — every choice is stated in the rig's
README.
"""

import argparse
import json
import os
import pathlib
import re
import subprocess
import sys
import tomllib

# Status vocabulary. pass/fail are verdicts about the work; rig is a verdict
# about the rig: a criterion the machinery could not answer is never averaged
# in as a 0 (bench/deepswe's law, carried over whole).
PASS, FAIL, RIG = "pass", "fail", "rig"


def log(msg):
    print(f"[grade] {msg}", file=sys.stderr, flush=True)


def sh(command, cwd=None, timeout=None, env=None):
    """Run one shell command; answer (exit_code, tail-of-output). A timeout or
    a spawn failure is a rig-conditioned error for the criterion that ran it,
    not a pass or a fail."""
    try:
        out = subprocess.run(
            ["bash", "-o", "pipefail", "-c", command],
            cwd=cwd, timeout=timeout, env=env,
            capture_output=True, text=True, errors="replace",
        )
        tail = (out.stdout + ("\n[stderr]\n" + out.stderr if out.stderr.strip() else ""))[-8000:]
        return out.returncode, tail
    except subprocess.TimeoutExpired as e:
        return 124, (e.stdout or "")[-4000:] + "\n[timed out]"
    except Exception as e:  # spawn failure is the rig's, not the work's
        return 127, f"[could not run: {e}]"


def load_rubric(path):
    raw = tomllib.loads(pathlib.Path(path).read_text())
    rubric = {
        "schema_version": raw.get("schema_version"),
        "judge_model": raw.get("judge_model", ""),
        "judge_prompt_version": raw.get("judge_prompt_version", ""),
        "criteria": raw.get("criterion", []),
    }
    ids = [c["id"] for c in rubric["criteria"]]
    if len(ids) != len(set(ids)):
        sys.exit("rubric: duplicate criterion ids")
    return rubric


def apply_patch(repo, patch_path, log_name):
    """Apply the graded patch to the pristine base tree. Answers (ok, note)."""
    code, out = sh(f"git apply --binary --whitespace=nowarn '{patch_path}'", cwd=repo)
    if code == 0:
        return True, ""
    # A patch that cannot apply to a pristine base is the agent's fault — a
    # legitimate 0 — but the note must say what git saw, so the row is
    # auditable.
    return False, f"git apply exit {code}: {out[-2000:]}"


def apply_overlay_idempotent(repo, overlay):
    """Apply the reference test overlay over the agent's tree. An agent that
    made the same test change already carries it; `git apply --reverse --check`
    answers whether the overlay's content is already in the tree, which is how
    a gold patch (which includes its own test change) grades without conflict."""
    code, out = sh(f"git apply --whitespace=nowarn '{overlay}'", cwd=repo)
    if code == 0:
        return True, "applied"
    code, _ = sh(f"git apply --reverse --check --whitespace=nowarn '{overlay}'", cwd=repo)
    if code == 0:
        return True, "already in the tree (reverse-applies cleanly)"
    return False, f"overlay conflict: {out[-2000:]}"


# The fixture's rebuild recipe (touch the C++/shell sources so make cannot
# keep the patched objects, then rebuild). A task that is not the fixture
# names its own rebuild under [verifier] rebuild_command in task.toml.
FIXTURE_REBUILD = ("find src test -type f \\( -name '*.cc' -o -name '*.h' -o -name '*.sh' \\) "
                   "-exec touch {} + && make configure compile")


def force_rebuild(repo, command=None):
    """Invalidate any build products of the patched tree and rebuild, so the
    reverse check tests the REVERTED tree and not a stale patched binary."""
    code, out = sh(command or FIXTURE_REBUILD, cwd=repo, timeout=None)
    return code, out


def diff_hunks(repo, paths, base):
    """The applied diff, restricted to the paths a prompt criterion names."""
    hunks = {}
    for path in paths:
        code, out = sh(f"git diff '{base}' -- '{path}'", cwd=repo)
        hunks[path] = out if code == 0 else f"[diff failed: {out[-500:]}]"
    return hunks


def mechanical_evidence(repo, paths):
    """The counts a judge is shown beside the diff: how many bare stderr
    writes remain in each file, and how often the helper appears. This is what
    grounds a prompt criterion that could otherwise be judged on vibes."""
    evidence = {}
    for path in paths:
        full = pathlib.Path(repo) / path
        if not full.exists():
            evidence[path] = {"exists": False}
            continue
        lines = full.read_text(errors="replace").split("\n")
        cerr = [f"{i+1}: {line.strip()}" for i, line in enumerate(lines) if "std::cerr" in line]
        helper = sum(1 for line in lines if "LOG_WARNING()" in line)
        evidence[path] = {"exists": True, "std_cerr_statements": len(cerr),
                          "std_cerr_lines": cerr[:20], "log_warning_calls": helper}
    return evidence


def scope_check(patch_path, spec, repo):
    """Scope: files, size and meaning limits, computed off the patch itself.
    The patch's paths are read against allowed/forbidden prefixes; the changed
    line count is the patch's own + and - lines (source-only judgement is the
    rig's, not the patch's)."""
    text = pathlib.Path(patch_path).read_text(errors="replace")
    files, added, removed, current = set(), 0, 0, None
    binary = False
    for line in text.split("\n"):
        if line.startswith("diff --git "):
            current = re.split(r" b/", line, maxsplit=1)[-1]
            files.add(current)
        elif line.startswith("GIT binary patch") or line.startswith("Binary files"):
            binary = True
        elif line.startswith("+") and not line.startswith("+++"):
            added += 1
        elif line.startswith("-") and not line.startswith("---"):
            removed += 1
    reasons = []
    if binary:
        reasons.append("the patch carries a binary change")
    if len(files) > spec.get("max_files", 10**9):
        reasons.append(f"{len(files)} files changed, limit {spec['max_files']}")
    if added + removed > spec.get("max_changed_lines", 10**9):
        reasons.append(f"{added + removed} changed lines, limit {spec['max_changed_lines']}")
    allowed = spec.get("allowed_paths") or []
    forbidden = spec.get("forbidden_paths") or []
    for path in sorted(files):
        if any(path.startswith(f) for f in forbidden):
            reasons.append(f"touches forbidden path {path}")
        elif allowed and not any(path.startswith(f) for f in allowed):
            reasons.append(f"touches out-of-scope path {path}")
    return (not reasons), reasons, {"files": sorted(files), "added": added, "removed": removed,
                                    "binary": binary}


def phase_a(task_dir, repo, base, out_dir):
    rubric = load_rubric(os.path.join(task_dir, "rubric.toml"))
    rebuild_command = None
    tpath = os.path.join(task_dir, "task.toml")
    if os.path.exists(tpath):
        with open(tpath, "rb") as fh:
            rebuild_command = tomllib.load(fh).get("verifier", {}).get("rebuild_command")
    patch_path = os.environ.get("FC_PATCH", "/logs/artifacts/model.patch")
    out = pathlib.Path(out_dir)
    out.mkdir(parents=True, exist_ok=True)
    results, evidence_dir = {}, out / "evidence"
    evidence_dir.mkdir(exist_ok=True)

    ok, note = apply_patch(repo, patch_path, "apply")
    if not ok:
        # A non-applying patch is a legitimate 0; every criterion records the
        # same note so the row is one fact, not twelve.
        for c in rubric["criteria"]:
            results[c["id"]] = {"status": FAIL, "note": f"patch did not apply: {note}"}
        (out / "apply.txt").write_text(note)
    else:
        # The agent's own diff, captured BEFORE any reference overlay is
        # applied, so the record carries exactly what the agent changed.
        sh(f"git diff '{base}' --binary > /logs/grade/agent.diff", cwd=repo)

        # The judge's input is captured NOW, while the tree is exactly the
        # agent's: the reverse-classical criterion reverts src/ later, and
        # evidence gathered after that would show the base tree and judge
        # nothing. (This really happened: the first gold control graded its
        # prompt criteria against an unmodified tree.)
        judge_input = {"base": base, "criteria": {}, "test_files": {}, "overlay": ""}
        for c in rubric["criteria"]:
            if c["kind"] == "prompt":
                paths = (c.get("prompt") or {}).get("paths", [])
                judge_input["criteria"][c["id"]] = {
                    "question": c["prompt"]["question"], "paths": paths,
                    "hunks": diff_hunks(repo, paths, base),
                    "mechanical": mechanical_evidence(repo, paths),
                }
            elif c["kind"] == "classical":
                overlay = c["classical"]["overlay"]
                judge_input["overlay"] = pathlib.Path(overlay).read_text(errors="replace")
                for path in re.findall(r"^diff --git a/(\S+) b/", judge_input["overlay"], re.M):
                    code, content = sh(f"git show '{base}:{path}'", cwd=repo)
                    judge_input["test_files"][path] = content if code == 0 else f"[missing at base: {path}]"

        for c in rubric["criteria"]:
            kind, cid = c["kind"], c["id"]
            spec = c.get(kind) or c.get(kind.replace("-", "_")) or {}
            entry = {"status": RIG, "note": ""}
            try:
                if kind == "command":
                    code, outtext = sh(spec["command"], cwd=repo,
                                       timeout=spec.get("timeout_sec"))
                    entry["status"] = PASS if code == 0 else FAIL
                    entry["exit_code"] = code
                    entry["note"] = f"exit {code}"
                    (evidence_dir / f"{cid}.log").write_text(outtext)
                elif kind == "classical":
                    applied, onote = apply_overlay_idempotent(repo, spec["overlay"])
                    if not applied:
                        # The overlay conflict is the adaptive path's trigger,
                        # not a verdict: the host decides.
                        entry = {"status": RIG, "note": f"test overlay conflict — adaptive path: {onote}"}
                    else:
                        code, outtext = sh(spec["run"], cwd=repo, timeout=spec.get("timeout_sec"))
                        entry["status"] = PASS if code == 0 else FAIL
                        entry["exit_code"] = code
                        entry["note"] = f"overlay {onote}; tests exit {code}"
                        (evidence_dir / f"{cid}.log").write_text(outtext)
                elif kind == "reverse-classical":
                    reverted = True
                    for path in spec.get("revert_paths", []):
                        code, outtext = sh(f"git checkout HEAD -- '{path}' && git clean -fdq '{path}'", cwd=repo)
                        if code != 0:
                            entry = {"status": RIG, "note": f"revert of {path} failed: {outtext[-1000:]}"}
                            reverted = False
                            break
                    if reverted:
                        code, outtext = force_rebuild(repo, rebuild_command)
                        if code != 0:
                            entry = {"status": RIG, "note": f"rebuild after revert failed: {outtext[-2000:]}"}
                        else:
                            code, outtext = sh(spec["run"], cwd=repo, timeout=spec.get("timeout_sec"))
                            # The criterion passes when the tests FAIL: that is
                            # what proves they test the change.
                            entry["status"] = PASS if code != 0 else FAIL
                            entry["exit_code"] = code
                            entry["note"] = f"tests exit {code} after revert (fail expected)"
                            (evidence_dir / f"{cid}.log").write_text(outtext)
                elif kind == "scope":
                    passed, reasons, stats = scope_check(patch_path, spec, repo)
                    entry["status"] = PASS if passed else FAIL
                    entry["note"] = "; ".join(reasons) if reasons else f"{stats['added']}+/{stats['removed']}- in {len(stats['files'])} files"
                    entry["stats"] = stats
                elif kind in ("prompt", "adaptive-classical"):
                    # Answered on the host by the judge (prompt) or by the
                    # classical result plus the judge (adaptive). Phase A only
                    # prepares their evidence.
                    entry = {"status": "host", "note": "answered on the host"}
                else:
                    entry = {"status": RIG, "note": f"unknown kind {kind}"}
            except Exception as e:
                entry = {"status": RIG, "note": f"criterion machinery failed: {e!r}"}
            results[cid] = entry

# The judge's input was captured right after the patch applied, while the
    # tree was exactly the agent's — the reverse criterion below reverts src/,
    # and evidence gathered after that would judge the base tree.
    (out / "phaseA.json").write_text(json.dumps(
        {"criteria": results, "judge_input": judge_input,
         "apply_ok": ok, "apply_note": note}, indent=2))
    log(f"phase A: {sum(1 for r in results.values() if r['status'] == PASS)} pass, "
        f"{sum(1 for r in results.values() if r['status'] == FAIL)} fail, "
        f"{sum(1 for r in results.values() if r['status'] == RIG)} rig")
    return 0


def combine(task_dir, grade_dir):
    """Host side: phase A + judge results (+ adaptive phase B, when it ran)
    become the run's grade. A `rig` criterion holds the run's grade at `rig` —
    the score is not computed and never silently dropped."""
    rubric = load_rubric(os.path.join(task_dir, "rubric.toml"))
    grade_dir = pathlib.Path(grade_dir)
    phase_a = json.loads((grade_dir / "phaseA.json").read_text())
    judge = {}
    judge_path = grade_dir / "judge.json"
    if judge_path.exists():
        judge = json.loads(judge_path.read_text())
    # The verdicts live under "criteria" (the file also carries the model, the
    # prompt version and the usage); read them from there, and the adaptive
    # result from its own key.
    judge_criteria = judge.get("criteria", {})
    phase_b = {}
    pb_path = grade_dir / "phaseB.json"
    if pb_path.exists():
        phase_b = json.loads(pb_path.read_text())

    criteria = {}
    for c in rubric["criteria"]:
        cid, kind = c["id"], c["kind"]
        entry = dict(phase_a["criteria"].get(cid, {"status": RIG, "note": "no phase A result"}))
        if kind == "prompt":
            verdict = judge_criteria.get(cid, {})
            entry["status"] = PASS if verdict.get("pass") else (
                FAIL if verdict else RIG)
            entry["judge"] = verdict
            entry["note"] = verdict.get("reasoning", entry["note"])[:300]
        elif kind == "adaptive-classical":
            # Three honest paths. The verbatim overlay applied and passed: the
            # tests fit the solution as written, and the criterion is derived.
            # The overlay conflicted or the tests failed: the judge either
            # adapts them to the solution (phase B re-runs the adapted tests)
            # or refuses to — a behavioural failure is not an interface
            # divergence, and a test rewritten to match wrong behaviour tests
            # nothing. What the judge could not decide stays rig.
            classical_id = next((x["id"] for x in rubric["criteria"]
                                 if x["kind"] == "classical"), "")
            classical = phase_a["criteria"].get(classical_id, {})
            adapt = judge.get("adaptive", {})
            if classical.get("status") == PASS:
                entry["status"] = PASS
                entry["note"] = "derived: the verbatim reference tests apply and pass"
            elif adapt.get("adapted"):
                if cid in phase_b:
                    entry.update(phase_b[cid])
                else:
                    entry = {"status": RIG, "note": "judge adapted the tests but phase B did not run"}
            elif adapt:
                entry["status"] = FAIL
                entry["note"] = f"no legitimate adaptation: {adapt.get('reasoning', '')[:280]}"
            elif classical.get("note", "").startswith("test overlay conflict"):
                entry = {"status": RIG, "note": "overlay conflict and no judge adaptation result"}
            else:
                entry = {"status": RIG, "note": "no judge adaptation result"}
        criteria[cid] = entry

    blockers = [c["id"] for c in rubric["criteria"] if c.get("blocker")]
    failed_blockers = [cid for cid in blockers if criteria[cid]["status"] == FAIL]
    rig_criteria = [cid for cid, e in criteria.items() if e["status"] == RIG]

    score = None
    if not rig_criteria:
        weights = [(c["id"], c.get("weight", 1)) for c in rubric["criteria"] if not c.get("blocker")]
        total = sum(w for _, w in weights)
        earned = sum(w for cid, w in weights if criteria[cid]["status"] == PASS)
        # Blocker gating first: any failed blocker scores the whole run 0,
        # whatever the weighted sum says.
        score = 0.0 if failed_blockers else round(earned / total, 4) if total else 0.0

    grade = {
        "task": os.path.basename(task_dir.rstrip("/")),
        "rubric_schema": rubric["schema_version"],
        "judge_model": rubric["judge_model"],
        "judge_prompt_version": rubric["judge_prompt_version"],
        "criteria": criteria,
        "failed_blockers": failed_blockers,
        "rig_criteria": rig_criteria,
        "score": score,
        "pass": bool(score) and not failed_blockers and not rig_criteria,
        "status": "rig" if rig_criteria else ("done" if score is not None else "incomplete"),
    }
    (grade_dir / "grade.json").write_text(json.dumps(grade, indent=2))
    log(f"combine: score={score} failed_blockers={failed_blockers} rig={rig_criteria}")
    return grade


def phase_b(task_dir, repo, base, out_dir):
    """The adaptive path's second no-network container: the agent's tree, the
    judge's adapted test patch in place of the verbatim overlay, build, run.
    Written to phaseB.json in the same shape phase A uses, keyed by the
    adaptive criterion ids."""
    rubric = load_rubric(os.path.join(task_dir, "rubric.toml"))
    rebuild_command = None
    tpath = os.path.join(task_dir, "task.toml")
    if os.path.exists(tpath):
        with open(tpath, "rb") as fh:
            rebuild_command = tomllib.load(fh).get("verifier", {}).get("rebuild_command")
    patch_path = os.environ.get("FC_PATCH", "/logs/artifacts/model.patch")
    grade_dir = pathlib.Path(out_dir)
    adapted = grade_dir / "adapted-tests.patch"
    results = {}
    ok, note = apply_patch(repo, patch_path, "phase-b apply")
    if not ok:
        for c in rubric["criteria"]:
            if c["kind"] == "adaptive-classical":
                results[c["id"]] = {"status": RIG, "note": f"phase B could not apply the patch: {note}"}
    else:
        overlay_ok, onote = apply_overlay_idempotent(repo, str(adapted))
        for c in rubric["criteria"]:
            if c["kind"] != "adaptive-classical":
                continue
            spec = c.get("adaptive_classical") or c.get("adaptive-classical") or {}
            if not overlay_ok:
                results[c["id"]] = {"status": RIG, "note": f"the adapted test patch did not apply: {onote}"}
                continue
            code, outtext = sh(f"make configure compile", cwd=repo, timeout=spec.get("timeout_sec"))
            if code != 0:
                results[c["id"]] = {"status": RIG, "note": f"phase B build failed: {outtext[-2000:]}"}
                continue
            code, outtext = sh(spec["run"], cwd=repo, timeout=spec.get("timeout_sec"))
            results[c["id"]] = {"status": PASS if code == 0 else FAIL, "exit_code": code,
                                "note": f"adapted tests exit {code}"}
            (grade_dir / "evidence").mkdir(exist_ok=True)
            (grade_dir / "evidence" / f"{c['id']}-phaseb.log").write_text(outtext)
    (grade_dir / "phaseB.json").write_text(json.dumps({"criteria": results}, indent=2))
    log(f"phase B: {json.dumps({k: v['status'] for k, v in results.items()})}")
    return 0


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="cmd", required=True)
    pa = sub.add_parser("phase-a")
    pa.add_argument("--task", required=True)
    pa.add_argument("--repo", default="/root/repos/jsonschema")
    pa.add_argument("--base", required=True)
    pa.add_argument("--out", default="/logs/grade")
    pb = sub.add_parser("phase-b")
    pb.add_argument("--task", required=True)
    pb.add_argument("--repo", default="/root/repos/jsonschema")
    pb.add_argument("--base", required=True)
    pb.add_argument("--out", default="/logs/grade")
    cb = sub.add_parser("combine")
    cb.add_argument("--task", required=True)
    cb.add_argument("--grade-dir", required=True)
    args = parser.parse_args()
    if args.cmd == "phase-a":
        return phase_a(args.task, args.repo, args.base, args.out)
    if args.cmd == "phase-b":
        return phase_b(args.task, args.repo, args.base, args.out)
    if args.cmd == "combine":
        combine(args.task, args.grade_dir)
        return 0
    return 2


if __name__ == "__main__":
    sys.exit(main())