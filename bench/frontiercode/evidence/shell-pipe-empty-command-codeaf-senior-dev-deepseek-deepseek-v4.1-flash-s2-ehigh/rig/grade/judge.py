#!/usr/bin/env python3
"""The LLM judge — the only networked part of grading, and it runs from the
host, never from the no-network verifier container.

Two pinned jobs, both recorded per grade with the model and prompt version:

1. prompt criteria: one call per criterion, given the criterion's own pinned
   question, the diff hunks of the files it names, and the mechanical excerpts
   phase A gathered. The answer is pass/fail with reasoning, parsed out of a
   JSON object and never trusted further than the criterion asks.

2. adaptive-classical adaptation: when the verbatim reference tests do not fit
   the agent's solution (a conflict, or a failure), the judge rewrites the
   test-side patch to the solution's interface — or refuses, when the failure
   is behavioural rather than a matter of naming. A refusal is a criterion
   fail; adapting wrong behaviour would be grading nothing.

The judge's key comes from the same keychain service the rigs use and is
never written to a result file. Its usage is recorded in judge.json so a
grade's cost carries the judge's own tokens.
"""

import argparse
import json
import re
import os
import pathlib
import subprocess
import sys
import urllib.error
import urllib.request

ENDPOINT = "https://openrouter.ai/api/v1/chat/completions"

# The pinned judge prompt, versioned in the rubric (judge_prompt_version) and
# spelled here. Changing a word is a new version and must be recorded as one.
JUDGE_PROMPTS = {
    "fc-judge-1": {
        "review": (
            "You are the review judge of a benchmark that grades whether a "
            "maintainer would merge a change. You are given one criterion's "
            "question, the diff of the change for the files the criterion "
            "names, and mechanical excerpts. Decide pass or fail STRICTLY on "
            "the criterion's question. Grade nothing the question does not "
            "ask: not style, not scope, not whether you would write it "
            "differently. If the evidence does not let you decide, answer "
            "fail and say what evidence is missing. Reply with a single JSON "
            "object {\"pass\": true|false, \"reasoning\": \"...\"} and "
            "nothing else."
        ),
        "adapt": (
            "You adapt a benchmark task's reference tests to an agent's "
            "solution. You are given the reference test-side patch, the "
            "agent's diff, and the failure the verbatim tests met. Rewrite "
            "the test patch so it tests the SAME behaviour against the "
            "solution's own interface — names, files, spelling. If the "
            "failure is behavioural (the solution genuinely does not do what "
            "the task asked), do NOT adapt: adapting wrong behaviour would "
            "test nothing. Reply with a single JSON object: "
            "{\"adapted\": true|false, \"reasoning\": \"...\", \"patch\": "
            "\"<unified diff against the base commit's test files, or empty "
            "when adapted is false>\"} and nothing else."
        ),
    }
}


def log(msg):
    print(f"[judge] {msg}", file=sys.stderr, flush=True)


def keychain_key():
    """The key, from the environment or the keychain service the rigs use.
    Read only; never logged."""
    key = os.environ.get("API_KEY") or os.environ.get("OPENROUTER_API_KEY") or ""
    if not key:
        try:
            key = subprocess.run(
                ["security", "find-generic-password", "-a", os.environ.get("USER", ""),
                 "-s", "delta-openrouter", "-w"],
                capture_output=True, text=True).stdout.strip()
        except Exception:
            key = ""
    return key


def call(model, system, user, key, temperature=0.0, max_tokens=900):
    """One completion. Answers (content, usage) or raises with the upstream's
    own words — a judge failure must be visible, never a silent default."""
    body = {
        "model": model.removeprefix("openrouter/"),
        "messages": [{"role": "system", "content": system},
                     {"role": "user", "content": user}],
        "temperature": temperature,
        "max_tokens": max_tokens,
    }
    request = urllib.request.Request(
        ENDPOINT, data=json.dumps(body).encode(),
        headers={"Authorization": f"Bearer {key}", "Content-Type": "application/json"},
        method="POST")
    with urllib.request.urlopen(request, timeout=180) as response:
        payload = json.loads(response.read())
    content = payload["choices"][0]["message"]["content"]
    usage = payload.get("usage", {})
    return content, usage


def _balanced(text, start):
    """The balanced {...} span starting at `start`, or None. Respects JSON
    strings so a brace inside one does not close the span."""
    depth, in_str, esc = 0, False, False
    for i in range(start, len(text)):
        c = text[i]
        if in_str:
            if esc:
                esc = False
            elif c == "\\":
                esc = True
            elif c == '"':
                in_str = False
            continue
        if c == '"':
            in_str = True
        elif c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
            if depth == 0:
                return text[start : i + 1]
    return None


def parse_json_reply(content):
    """The judge replies with one JSON object; strip the fences a model adds
    around it and parse. A reply with no object is a rig-conditioned failure
    for that criterion, never a default pass."""
    text = content.strip()
    if text.startswith("```"):
        text = text.strip("`")
        text = text[text.find("{"):] if "{" in text else text
    start, end = text.find("{"), text.rfind("}")
    if start < 0 or end <= start:
        raise ValueError(f"no JSON object in judge reply: {content[:200]!r}")
    # Preferred shape: a fenced ```json block. The reply may also carry
    # prose and code fences whose braces are not JSON (observed with the
    # pinned judge model), so scan every balanced top-level {...} span and
    # take the first span that parses as JSON.
    fence = re.search(r"```(?:json)?\s*(\{.*?\})\s*```", text, re.S)
    if fence:
        try:
            return json.loads(fence.group(1))
        except json.JSONDecodeError:
            pass
    for m in re.finditer(r"\{", text):
        obj = _balanced(text, m.start())
        if obj:
            try:
                return json.loads(obj)
            except json.JSONDecodeError:
                continue
    obj = text[start : end + 1]
    try:
        return json.loads(obj)
    except json.JSONDecodeError:
        pass
    # Common model lapses, repaired in order and re-parsed; if none of them
    # was the problem the reply is a rig-conditioned failure like before.
    repaired = re.sub(r"([{,]\s*)([A-Za-z_][\w-]*)(\s*):", r'\1"\2"\3:', obj)
    repaired = re.sub(r",\s*([}\]])", r"\1", repaired)
    repaired = re.sub(r"^\s*//.*$", "", repaired, flags=re.M)
    try:
        return json.loads(repaired)
    except json.JSONDecodeError as e:
        raise ValueError(f"unparseable judge reply ({e}): {content[:400]!r}")


def review_criteria(rubric, judge_input, key, model, prompt_version):
    results, usage_total = {}, {"prompt_tokens": 0, "completion_tokens": 0, "calls": 0}
    system = JUDGE_PROMPTS[prompt_version]["review"]
    for cid, item in judge_input["criteria"].items():
        user = (
            f"Criterion question:\n{item['question']}\n\n"
            f"Diff of the change for {', '.join(item['paths'])}:\n"
            f"{item['hunks']}\n\nMechanical excerpts of the changed tree:\n"
            f"{json.dumps(item['mechanical'], indent=2)}\n\n"
            "Answer the criterion question. Your reply must be ONLY the JSON object — no prose before it, no code fences, no commentary."
        )
        usage = {}
        try:
            content, usage = call(model, system, user, key)
            verdict = parse_json_reply(content)
            results[cid] = {"pass": bool(verdict.get("pass")),
                            "reasoning": str(verdict.get("reasoning", ""))[:600],
                            "usage": usage}
            log(f"{cid}: {'pass' if verdict.get('pass') else 'fail'}")
        except Exception as e:
            results[cid] = {"error": repr(e)[:400]}
            log(f"{cid}: judge call failed: {e}")
        usage_total["calls"] += 1
        usage_total["prompt_tokens"] += (usage or {}).get("prompt_tokens", 0)
        usage_total["completion_tokens"] += (usage or {}).get("completion_tokens", 0)
    return results, usage_total


def adapt_tests(rubric, grade_dir, key, model, prompt_version):
    """The adaptive path's judge call. Reads phase A's classical evidence, the
    reference overlay and the base test files phase A carried out, and answers
    an adapted test patch or a refusal."""
    grade_dir = pathlib.Path(grade_dir)
    phase_a = json.loads((grade_dir / "phaseA.json").read_text())
    raw_criteria = rubric.get("criteria") or rubric.get("criterion", [])
    classical_id = next((c["id"] for c in raw_criteria if c["kind"] == "classical"), "")
    classical = phase_a["criteria"].get(classical_id, {})
    agent_diff = grade_dir / "agent.diff"
    evidence = ""
    ev = grade_dir / "evidence"
    if ev.exists() and classical_id and (ev / f"{classical_id}.log").exists():
        evidence = (ev / f"{classical_id}.log").read_text(errors="replace")[-4000:]
    system = JUDGE_PROMPTS[prompt_version]["adapt"]
    user = (
        f"The reference test-side patch (a diff against the base commit's "
        f"test files):\n{phase_a['judge_input'].get('overlay', '(none)')}\n\n"
        f"The base commit's own text of those test files:\n"
        f"{json.dumps(phase_a['judge_input'].get('test_files', {}), indent=2)[:20000]}\n\n"
        f"The reference tests' failure under this solution:\n{evidence}\n\n"
        f"The agent's diff:\n{(agent_diff.read_text(errors='replace') if agent_diff.exists() else '(missing)')[:24000]}\n\n"
        "Decide whether the reference tests can be legitimately adapted to "
        "this solution, and if so produce the adapted test patch — a unified "
        "diff against the base commit's test files, which the grader applies "
        "over the agent's tree."
    )
    content, usage = call(model, system, user, key, max_tokens=2400)
    verdict = parse_json_reply(content)
    result = {"adapted": bool(verdict.get("adapted")),
              "reasoning": str(verdict.get("reasoning", ""))[:600],
              "usage": usage}
    patch = verdict.get("patch") or ""
    if result["adapted"] and patch.strip():
        (grade_dir / "adapted-tests.patch").write_text(patch)
    return result


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="cmd", required=True)
    rv = sub.add_parser("review")
    rv.add_argument("--task", required=True)
    rv.add_argument("--grade-dir", required=True)
    ad = sub.add_parser("adapt")
    ad.add_argument("--task", required=True)
    ad.add_argument("--grade-dir", required=True)
    args = parser.parse_args()

    import tomllib
    rubric = tomllib.loads(pathlib.Path(args.task, "rubric.toml").read_text())
    model = os.environ.get("FC_JUDGE_MODEL") or rubric.get("judge_model", "")
    version = rubric.get("judge_prompt_version", "")
    if version not in JUDGE_PROMPTS:
        sys.exit(f"judge: unknown pinned prompt version {version!r}")
    key = keychain_key()
    if not key:
        sys.exit("judge: no key (API_KEY / OPENROUTER_API_KEY / keychain)")
    grade_dir = pathlib.Path(args.grade_dir)
    judge_input = json.loads((grade_dir / "phaseA.json").read_text())["judge_input"]

    if args.cmd == "review":
        results, usage = review_criteria(rubric, judge_input, key, model, version)
    else:
        results = adapt_tests(rubric, args.grade_dir, key, model, version)
        usage = results.get("usage", {})

    judge_path = grade_dir / "judge.json"
    existing = json.loads(judge_path.read_text()) if judge_path.exists() else {}
    if args.cmd == "review":
        existing["criteria"] = results
    else:
        existing["adaptive"] = results
    existing["model"] = model
    existing["prompt_version"] = version
    existing["usage"] = {
        "prompt_tokens": existing.get("usage", {}).get("prompt_tokens", 0) + usage.get("prompt_tokens", 0),
        "completion_tokens": existing.get("usage", {}).get("completion_tokens", 0) + usage.get("completion_tokens", 0),
        "calls": existing.get("usage", {}).get("calls", 0) + 1,
    }
    judge_path.write_text(json.dumps(existing, indent=2))
    log(f"judge.json written ({model}, {version})")
    return 0


if __name__ == "__main__":
    sys.exit(main())