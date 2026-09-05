#!/usr/bin/env python3
"""Validate a real-issue manifest, and preflight it against the repository it names.

A real-issue cell is one closed bug, its frozen base commit, and the test the fix
brought with it — held out, so the workspace the harness sees never contains it.
Everything that runs a cell already exists in `bench/conversation`; this file is
only the part that says whether a candidate is honest enough to run.

    manifest.py validate candidates/*.json
    manifest.py preflight candidates/528-reopened-jobs.json --repo /path/to/checkout

`validate` is offline, needs no git and calls nothing: it is what the unit tests
run. `preflight` adds the checks that need the object store — the base commit
exists, the fix commit sits directly on top of it, and the held-out test is
absent at base and present at the fix.

Neither command can promote a candidate. `calibrated` means somebody observed the
held-out test fail at base for a BEHAVIOURAL reason and pass on the reference fix,
and both observations have to be written into the manifest with the command that
produced them. A test that fails to build at base is not acceptance: it is grading
the reference implementation's shape, and a correct independent repair scores zero
against it (issue #370 in this repository does exactly that).
"""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parent
SCHEMA = 1
# The doors bench/conversation actually has, and which arms it can drive through
# each. opencode has no calibrated interactive markers, so naming it there is the
# one substitution that would make a table a lie (README: "the two doors").
DOORS = {"print": {"aforge", "omp", "pi", "opencode"},
         "interactive": {"aforge", "omp", "pi"}}
ARMS = DOORS["print"]
# Open models only. Exact catalog ids, matching lib/allowlist.sh — no wildcards
# and no family names, because a substring match accepts dated siblings too.
ALLOWED_MODELS = {"deepseek/deepseek-v4-flash-0731"}
READINESS = ["rejected", "candidate", "preflight-clean", "calibrated"]
SHA = re.compile(r"^[0-9a-f]{40}$")
HEX7 = re.compile(r"\b[0-9a-f]{7,40}\b")
REQUIRED = ["schema", "id", "readiness", "provenance", "workspace", "prompt",
            "acceptance", "arms", "doors"]


class Refusal(Exception):
    """A manifest that would misrepresent what is ready."""


def check(condition, message):
    if not condition:
        raise Refusal(message)


def load(path):
    manifest = json.loads(Path(path).read_text())
    check(isinstance(manifest, dict), "%s: manifest is not an object" % path)
    return manifest


def validate(manifest, name="manifest"):
    """Everything that can be decided from the file alone. Raises on the first refusal."""
    for field in REQUIRED:
        check(field in manifest, "%s: missing %s" % (name, field))
    check(manifest["schema"] == SCHEMA, "%s: unknown schema %r" % (name, manifest["schema"]))
    check(manifest["readiness"] in READINESS,
          "%s: readiness must be one of %s" % (name, ", ".join(READINESS)))

    provenance = manifest["provenance"]
    for field in ("repo", "issue", "fix_pr", "base_commit", "fix_commit"):
        check(provenance.get(field) not in (None, ""), "%s: provenance.%s is missing" % (name, field))
    # A short sha, a branch name or a tag is not a frozen base: all three move.
    for field in ("base_commit", "fix_commit"):
        check(SHA.match(str(provenance[field])),
              "%s: provenance.%s must be a full 40-character sha, got %r"
              % (name, field, provenance[field]))
    check(provenance["base_commit"] != provenance["fix_commit"],
          "%s: base and fix are the same commit, so the bug is not in the workspace" % name)

    workspace = manifest["workspace"]
    # The workspace is what the harness sees. Future history in it is the whole
    # answer sitting one `git log` away.
    check(workspace.get("export") in ("git-archive", "single-commit"),
          "%s: workspace.export must be git-archive or single-commit" % name)
    check(workspace.get("carries_git_history") is False,
          "%s: workspace.carries_git_history must be false" % name)

    acceptance = manifest["acceptance"]
    held_out = acceptance.get("held_out_paths") or []
    check(held_out, "%s: acceptance.held_out_paths is empty — there is nothing to grade with" % name)
    check(acceptance.get("command"), "%s: acceptance.command is missing" % name)
    guarded = acceptance.get("guarded_paths") or []
    check(not (set(held_out) & set(guarded)),
          "%s: a held-out path cannot also be guarded in the workspace" % name)

    # Doors and arms are paired in both directions: a door with no arm runs
    # nothing, and an arm named nowhere is a row that will never exist.
    doors = manifest["doors"]
    check(isinstance(doors, dict) and doors, "%s: doors must name at least one door" % name)
    declared = set(manifest["arms"])
    check(declared and declared <= ARMS,
          "%s: arms must be a non-empty subset of %s" % (name, ", ".join(sorted(ARMS))))
    check(len(manifest["arms"]) == len(declared), "%s: arms repeats an arm" % name)
    seen = set()
    for door, arms in doors.items():
        check(door in DOORS, "%s: unknown door %r" % (name, door))
        check(arms, "%s: door %s names no arm" % (name, door))
        for arm in arms:
            check(arm in declared, "%s: door %s names %s, which is not in arms" % (name, door, arm))
            check(arm in DOORS[door],
                  "%s: this suite drives no %s door for %s" % (name, door, arm))
            seen.add(arm)
    check(seen == declared,
          "%s: arms named by no door: %s" % (name, ", ".join(sorted(declared - seen))))

    model = manifest.get("model_pin")
    check(model in ALLOWED_MODELS,
          "%s: model_pin %r is not an allowlisted open model" % (name, model))

    # A rejected candidate is a record of why not, and nothing runs it — so it is
    # allowed to hold the very leak that rejected it. Everything above still holds:
    # a wrong sha or an unpaired door in a rejected file would mislead the next
    # person reading it just as much.
    if manifest["readiness"] != "rejected":
        _no_leak(manifest, name, held_out)
    _readiness_is_earned(manifest, name)
    return True


def _model_visible(manifest):
    """Every string the harness is given: the prompt, and anything written beside it."""
    text = [manifest["prompt"].get("text", "")]
    text.extend(manifest["prompt"].get("attachments", {}).values())
    text.append(manifest["workspace"].get("notes", "") or "")
    return "\n".join(text)


def _no_leak(manifest, name, held_out):
    """The model-visible text must not carry the fix, or the road to it."""
    text = _model_visible(manifest)
    check(text.strip(), "%s: prompt.text is empty" % name)
    provenance = manifest["provenance"]
    hexes = {h.lower() for h in HEX7.findall(text.lower())}
    for field in ("fix_commit", "base_commit"):
        sha = provenance[field]
        for seen in hexes:
            check(not sha.startswith(seen),
                  "%s: model-visible text names the %s (%s)" % (name, field, seen))
    pr = str(provenance["fix_pr"]).lstrip("#")
    for pattern in (r"#%s\b" % re.escape(pr), r"pull/%s\b" % re.escape(pr),
                    r"\bPR\s*%s\b" % re.escape(pr)):
        check(not re.search(pattern, text, re.I),
              "%s: model-visible text points at the fixing pull request" % name)
    for path in held_out:
        for token in (path, Path(path).name):
            check(token not in text,
                  "%s: model-visible text names the held-out test %s" % (name, token))
    # A patch pasted into the brief is the answer, whatever it is called.
    check(not re.search(r"^(diff --git|@@ -\d)", text, re.M),
          "%s: model-visible text contains a diff of the fix" % name)


def _readiness_is_earned(manifest, name):
    """A readiness word is a claim about evidence that has to be in the file."""
    readiness = manifest["readiness"]
    blockers = manifest.get("blockers") or []
    if readiness == "rejected":
        check(manifest.get("rejection"), "%s: a rejected candidate must say why" % name)
        return
    check(not blockers or readiness == "candidate",
          "%s: %d blocker(s) recorded, so readiness cannot be %r"
          % (name, len(blockers), readiness))
    calibration = manifest.get("calibration") or {}
    if readiness == "calibrated":
        for key in ("fails_at_base", "passes_on_fix"):
            record = calibration.get(key)
            check(isinstance(record, dict) and record.get("command") and record.get("observed_at"),
                  "%s: calibrated claims %s with no command and date" % (name, key))
            check(record.get("build_ok") is True,
                  "%s: %s must record build_ok — a test that does not build at base "
                  "grades the reference implementation's shape, not the behaviour" % (name, key))
    for key, record in calibration.items():
        check(isinstance(record, dict) and record.get("command"),
              "%s: calibration.%s has no command, so nobody can repeat it" % (name, key))
    return


def git(repo, *args):
    result = subprocess.run(["git", "-C", str(repo), *args],
                            capture_output=True, text=True)
    return result.returncode, result.stdout.strip(), result.stderr.strip()


def preflight(manifest, repo, name="manifest"):
    """The checks that need the object store. Reports; never promotes."""
    findings = []
    provenance = manifest["provenance"]
    base, fix = provenance["base_commit"], provenance["fix_commit"]
    for sha in (base, fix):
        code, kind, _ = git(repo, "cat-file", "-t", sha)
        if code != 0 or kind != "commit":
            findings.append("%s is not a commit in %s" % (sha[:12], repo))
    if findings:
        return findings
    code, parent, _ = git(repo, "rev-parse", fix + "^")
    if code != 0 or parent != base:
        findings.append("the fix commit's parent is %s, not the declared base %s"
                        % ((parent or "unknown")[:12], base[:12]))
    for path in manifest["acceptance"]["held_out_paths"]:
        if git(repo, "cat-file", "-e", "%s:%s" % (fix, path))[0] != 0:
            findings.append("held-out %s does not exist at the fix commit" % path)
        if git(repo, "cat-file", "-e", "%s:%s" % (base, path))[0] == 0:
            findings.append("held-out %s ALREADY exists at the base commit — the added "
                            "cases have to be extracted before this is held out" % path)
    for path in manifest["acceptance"].get("guarded_paths") or []:
        if git(repo, "cat-file", "-e", "%s:%s" % (base, path))[0] != 0:
            findings.append("guarded %s does not exist at the base commit" % path)
    return findings


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    p = sub.add_parser("validate")
    p.add_argument("manifest", nargs="+")
    p = sub.add_parser("preflight")
    p.add_argument("manifest", nargs="+")
    p.add_argument("--repo", required=True)
    args = parser.parse_args()

    failed = 0
    for path in args.manifest:
        name = Path(path).name
        try:
            manifest = load(path)
            validate(manifest, name)
        except (Refusal, ValueError, OSError) as error:
            print("REFUSED  %s" % error, file=sys.stderr)
            failed += 1
            continue
        if args.command == "validate":
            print("ok       %-34s %s" % (name, manifest["readiness"]))
            continue
        findings = preflight(manifest, args.repo, name)
        print("%-8s %-34s %s" % ("ok" if not findings else "FINDING", name, manifest["readiness"]))
        for finding in findings:
            print("         %s" % finding)
        failed += bool(findings and manifest["readiness"] != "rejected")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
