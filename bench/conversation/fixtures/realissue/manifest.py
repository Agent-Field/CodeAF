#!/usr/bin/env python3
"""Validate a real-issue manifest, and preflight it against the repository it names.

A real-issue cell is one closed bug, its frozen base commit, and the test the fix
brought with it — held out, so the workspace the harness sees never contains it.
Everything that runs a cell already exists in `bench/conversation` and, for
container corpora, in `bench/deepswe`; this file is only the part that says
whether a candidate is honest enough to run.

    manifest.py validate candidates/*.json
    manifest.py preflight candidates/528-reopened-jobs.json --repo /path/to/checkout

`validate` is offline, needs no git and calls nothing: it is what the unit tests
run. `preflight` adds the checks that need the object store.

**The lexical checks here are lint, not proof of isolation.** They catch the
disclosures we have actually seen — a fenced block of the implementation
language, a test function name, a fix sha, a pull-request pointer, a diff, a
`## The fix` heading. They cannot prove a prose paragraph does not describe the
repair. Isolation comes from curating the prompt and recording what was dropped;
the lint is what stops the obvious regression.

Scope, v1: `task_kind: bug-existing-api` — a behavioural defect whose held-out
test drives an API that already exists at the base commit. A feature task may
legitimately need a public API that does not exist at base and its test cannot
build there; that is a different kind and this file does not accept it yet.
"""
import argparse
import hashlib
import json
from pathlib import PurePosixPath
import re
import subprocess
import sys

SCHEMA = 2
# The doors bench/conversation actually has, and which arms it can drive through
# each. opencode has no calibrated interactive markers, so naming it there is the
# one substitution that would make a table a lie (README: "the two doors").
DOORS = {"print": {"aforge", "omp", "pi", "opencode"},
         "interactive": {"aforge", "omp", "pi"}}
ARMS = DOORS["print"]
# Open models only. Exact catalog ids, matching lib/allowlist.sh — no wildcards
# and no family names, because a substring match accepts dated siblings too.
ALLOWED_MODELS = {"deepseek/deepseek-v4-flash-0731"}
TASK_KINDS = {"bug-existing-api"}
READINESS = ["rejected", "candidate", "preflight-clean", "grader-calibrated"]
SHA1 = re.compile(r"^[0-9a-f]{40}$")
SHA256 = re.compile(r"^[0-9a-f]{64}$")
HEX7 = re.compile(r"\b[0-9a-f]{7,40}\b")
DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
REQUIRED = ["schema", "id", "task_kind", "readiness", "provenance", "workspace",
            "prompt", "acceptance", "arms", "doors", "model_pin"]


class Refusal(Exception):
    """A manifest that would misrepresent what is ready."""


def check(condition, message):
    if not condition:
        raise Refusal(message)


def a_list_of_paths(node, field, name):
    """A path list that is a list, of strings, that stay inside the workspace."""
    value = node.get(field)
    check(isinstance(value, list), "%s: %s must be a list" % (name, field))
    for path in value:
        check(isinstance(path, str) and path.strip(), "%s: %s holds a non-path %r" % (name, field, path))
        pure = PurePosixPath(path)
        check(not pure.is_absolute() and "\\" not in path and "\0" not in path,
              "%s: %s %r must be workspace-relative" % (name, field, path))
        check(".." not in pure.parts, "%s: %s %r escapes the workspace" % (name, field, path))
        check(str(pure) == path.rstrip("/") and path == path.strip(),
              "%s: %s %r is not in normal form" % (name, field, path))
    check(len(set(value)) == len(value), "%s: %s repeats a path" % (name, field))
    return value


def load(path):
    manifest = json.loads(open(path).read())
    check(isinstance(manifest, dict), "%s: manifest is not an object" % path)
    return manifest


def validate(manifest, name="manifest"):
    """Everything that can be decided from the file alone. Raises on the first refusal."""
    for field in REQUIRED:
        check(field in manifest, "%s: missing %s" % (name, field))
    check(manifest["schema"] == SCHEMA, "%s: unknown schema %r" % (name, manifest["schema"]))
    check(manifest["task_kind"] in TASK_KINDS,
          "%s: task_kind %r is outside this version's scope (%s)"
          % (name, manifest["task_kind"], ", ".join(sorted(TASK_KINDS))))
    check(manifest["readiness"] in READINESS,
          "%s: readiness must be one of %s" % (name, ", ".join(READINESS)))
    for field in ("provenance", "workspace", "prompt", "acceptance", "doors"):
        check(isinstance(manifest[field], dict), "%s: %s must be an object" % (name, field))

    _provenance(manifest, name)
    _workspace(manifest, name)
    held_out = _acceptance(manifest, name)
    _arms_and_doors(manifest, name)
    check(manifest.get("model_pin") in ALLOWED_MODELS,
          "%s: model_pin %r is not an allowlisted open model" % (name, manifest.get("model_pin")))
    # A rejected candidate is a record of why not, and nothing runs it — so it is
    # allowed to hold the very leak that rejected it. Everything else still holds:
    # a wrong sha or an unpaired door would mislead the next reader just as much.
    _prompt(manifest, name, held_out, lint=manifest["readiness"] != "rejected")
    _readiness_is_earned(manifest, name)
    return True


def _provenance(manifest, name):
    provenance = manifest["provenance"]
    for field in ("repo", "issue", "fix_pr", "base_commit", "fix_commit", "language"):
        check(provenance.get(field) not in (None, ""), "%s: provenance.%s is missing" % (name, field))
    check(isinstance(provenance["issue"], int), "%s: provenance.issue must be a number" % name)
    # A short sha, a branch name or a tag is not a frozen base: all three move.
    for field in ("base_commit", "fix_commit"):
        check(isinstance(provenance[field], str) and SHA1.match(provenance[field]),
              "%s: provenance.%s must be a full 40-character sha, got %r"
              % (name, field, provenance[field]))
    check(provenance["base_commit"] != provenance["fix_commit"],
          "%s: base and fix are the same commit, so the bug is not in the workspace" % name)


def _workspace(manifest, name):
    workspace = manifest["workspace"]
    # The workspace is what the harness sees. Future history in it is the whole
    # answer sitting one `git log` away.
    check(workspace.get("export") in ("git-archive", "single-commit"),
          "%s: workspace.export must be git-archive or single-commit" % name)
    check(workspace.get("carries_git_history") is False,
          "%s: workspace.carries_git_history must be false" % name)


def _acceptance(manifest, name):
    acceptance = manifest["acceptance"]
    held_out = a_list_of_paths(acceptance, "held_out_paths", name)
    guarded = a_list_of_paths(acceptance, "guarded_paths", name)
    fix_paths = a_list_of_paths(acceptance, "fix_paths", name)
    check(held_out, "%s: acceptance.held_out_paths is empty — there is nothing to grade with" % name)
    check(fix_paths, "%s: acceptance.fix_paths is empty — the paths the repair is known to "
                     "need have to be named, or nothing can check what is guarded" % name)
    check(isinstance(acceptance.get("command"), str) and acceptance["command"].strip(),
          "%s: acceptance.command is missing" % name)
    check(not (set(held_out) & set(guarded)),
          "%s: a held-out path cannot also be guarded in the workspace" % name)
    check(not (set(held_out) & set(fix_paths)),
          "%s: %s is both held out and a path the repair must write"
          % (name, sorted(set(held_out) & set(fix_paths))[0] if set(held_out) & set(fix_paths) else ""))
    # Guarding the source the agent has to edit rejects every real repair. Guards
    # are for the instruments — the tests, the fixtures, the graded config.
    overlap = sorted(set(guarded) & set(fix_paths))
    check(not overlap, "%s: guarded_paths holds %s, which the repair must edit — guard the "
                       "evaluation instruments, never a known implementation path"
          % (name, overlap[0] if overlap else ""))
    return held_out


def _arms_and_doors(manifest, name):
    arms = manifest["arms"]
    check(isinstance(arms, list) and arms, "%s: arms must be a non-empty list" % name)
    declared = set(arms)
    check(len(arms) == len(declared), "%s: arms repeats an arm" % name)
    check(declared <= ARMS, "%s: arms must be a subset of %s" % (name, ", ".join(sorted(ARMS))))
    doors = manifest["doors"]
    check(doors, "%s: doors must name at least one door" % name)
    for door, named in doors.items():
        check(door in DOORS, "%s: unknown door %r" % (name, door))
        check(isinstance(named, list) and named, "%s: door %s names no arm" % (name, door))
        check(len(set(named)) == len(named), "%s: door %s repeats an arm" % (name, door))
        for arm in named:
            check(arm in DOORS.get(door, ()),
                  "%s: this suite drives no %s door for %s" % (name, door, arm))
        # Every compared door carries the WHOLE set. A print row for one arm and an
        # interactive row for another is two experiments printed as one comparison.
        missing = sorted(declared - set(named))
        check(not missing, "%s: door %s is missing %s — a compared door carries every arm"
              % (name, door, ", ".join(missing)))
        extra = sorted(set(named) - declared)
        check(not extra, "%s: door %s names %s, which is not in arms" % (name, door, ", ".join(extra)))


def _model_visible(manifest):
    """Every string the harness is given: the prompt, and anything written beside it."""
    prompt = manifest["prompt"]
    text = [prompt.get("text", "")]
    attachments = prompt.get("attachments") or {}
    check(isinstance(attachments, dict), "prompt.attachments must be an object")
    text.extend(str(value) for value in attachments.values())
    text.append(manifest["workspace"].get("notes") or "")
    return "\n".join(text)


def _prompt(manifest, name, held_out, lint):
    prompt = manifest["prompt"]
    text = prompt.get("text")
    check(isinstance(text, str) and text.strip(), "%s: prompt.text is empty" % name)
    # An issue in a repository like this one is an engineering report: it carries
    # the diagnosis, the patch and the names of the tests. Handing it over whole
    # is not a task, so a prompt here is DERIVED and says so.
    check(prompt.get("verbatim") is False,
          "%s: prompt.verbatim must be false — the issue body carries its own fix" % name)
    check(SHA256.match(str(prompt.get("sha256") or "")),
          "%s: prompt.sha256 must be the sha256 of prompt.text" % name)
    check(hashlib.sha256(text.encode()).hexdigest() == prompt["sha256"],
          "%s: prompt.sha256 does not match prompt.text — the prompt moved after curation" % name)
    derivation = prompt.get("derivation")
    check(isinstance(derivation, dict), "%s: prompt.derivation is missing" % name)
    for field in ("source_url", "source_sha256", "rule", "curated_at"):
        check(derivation.get(field), "%s: prompt.derivation.%s is missing" % (name, field))
    check(SHA256.match(str(derivation["source_sha256"])),
          "%s: prompt.derivation.source_sha256 must be a sha256 of the issue body as fetched" % name)
    for field in ("kept_sections", "dropped_sections"):
        check(isinstance(derivation.get(field), list),
              "%s: prompt.derivation.%s must list the headings" % (name, field))
    check(isinstance(derivation.get("dropped_paragraphs", {}), dict),
          "%s: prompt.derivation.dropped_paragraphs must record each pattern and its count" % name)
    check(derivation["dropped_sections"],
          "%s: nothing was dropped, so this is a verbatim body under another name" % name)
    if lint:
        _leak_lint(manifest, name, held_out, text)


def _leak_lint(manifest, name, held_out, _text):
    """Lint, not proof: the disclosures we have actually caught."""
    text = _model_visible(manifest)
    provenance = manifest["provenance"]
    hexes = {h.lower() for h in HEX7.findall(text.lower())}
    for field in ("fix_commit", "base_commit"):
        for seen in hexes:
            check(not provenance[field].startswith(seen),
                  "%s: model-visible text names the %s (%s)" % (name, field, seen))
    pr = str(provenance["fix_pr"]).lstrip("#")
    for pattern in (r"#%s\b" % re.escape(pr), r"pull/%s\b" % re.escape(pr),
                    r"\bPR\s*%s\b" % re.escape(pr)):
        check(not re.search(pattern, text, re.I),
              "%s: model-visible text points at the fixing pull request" % name)
    for path in held_out:
        for token in (path, PurePosixPath(path).name):
            check(token not in text, "%s: model-visible text names the held-out test %s" % (name, token))
    # The report's own shape. `## The fix` carried a literal Go patch in issue #528
    # and the names of both held-out tests in `## Acceptance`; the first version of
    # this file shipped that as a "verbatim" prompt and neither caught it.
    heading = re.search(r"^#{1,6}\s*(the fix\b.*|fix shape|acceptance\b.*|where)\s*$",
                        text, re.I | re.M)
    check(not heading, "%s: model-visible text keeps the report's %r section"
          % (name, (heading.group(1).strip() if heading else "")))
    fenced = re.search(r"^```[ \t]*%s\b" % re.escape(provenance["language"]), text, re.I | re.M)
    check(not fenced, "%s: model-visible text carries a fenced %s block — candidate fix code"
          % (name, provenance["language"]))
    # Naming the file the repair must edit is the diagnosis, however it is phrased.
    for path in manifest["acceptance"]["fix_paths"]:
        for token in (path, PurePosixPath(path).name):
            check(token not in text,
                  "%s: model-visible text names %s, which the repair must edit" % (name, token))
    test_name = re.search(r"\bTest[A-Z][A-Za-z0-9_]{3,}", text)
    check(not test_name, "%s: model-visible text names the test function %s"
          % (name, test_name.group(0) if test_name else ""))
    check(not re.search(r"^(diff --git|@@ -\d)", text, re.M),
          "%s: model-visible text contains a diff of the fix" % name)


def _evidence(record, key, name, want_zero_exit, kind):
    check(isinstance(record, dict), "%s: calibration.%s must be an object" % (name, key))
    check(isinstance(record.get("command"), str) and record["command"].strip(),
          "%s: calibration.%s has no command, so nobody can repeat it" % (name, key))
    check(DATE.match(str(record.get("observed_at") or "")),
          "%s: calibration.%s needs observed_at as YYYY-MM-DD" % (name, key))
    exit_code = record.get("exit_code")
    check(isinstance(exit_code, int) and not isinstance(exit_code, bool),
          "%s: calibration.%s needs the exit code it was read from" % (name, key))
    check(SHA256.match(str(record.get("output_sha256") or "")),
          "%s: calibration.%s needs output_sha256 of the captured output" % (name, key))
    # Recorded by a person from a run they did; this file cannot re-run it.
    check(record.get("verification") == "manual-recorded",
          "%s: calibration.%s must say verification: manual-recorded — nothing here "
          "machine-verified it" % (name, key))
    check(isinstance(record.get("build_ok"), bool),
          "%s: calibration.%s needs build_ok" % (name, key))
    # Polarity, offline: the pair only means something if it points both ways.
    if want_zero_exit:
        check(exit_code == 0, "%s: calibration.%s records exit %d — that is not a pass"
              % (name, key, exit_code))
    else:
        check(exit_code != 0, "%s: calibration.%s records exit 0 — that is not a failure at base"
              % (name, key))
    if kind == "bug-existing-api":
        check(record["build_ok"] is True,
              "%s: %s did not build, so the held-out test needs a symbol that is not in the "
              "base API — that is not a bug-existing-api task" % (name, key))


def _readiness_is_earned(manifest, name):
    """A readiness word is a claim about evidence that has to be in the file."""
    readiness = manifest["readiness"]
    blockers = manifest.get("blockers") or []
    check(isinstance(blockers, list), "%s: blockers must be a list" % name)
    # Grading a repair is not the same as being ready to run a campaign: the arms,
    # the cell cap and the doors are unevidenced here, whatever the grader did.
    check(manifest.get("campaign_ready", False) is False,
          "%s: campaign readiness is not decided by a grader pair — no arm, cap or door "
          "evidence lives in this file" % name)
    if readiness == "rejected":
        check(manifest.get("rejection"), "%s: a rejected candidate must say why" % name)
        return
    check(not blockers or readiness == "candidate",
          "%s: %d blocker(s) recorded, so readiness cannot be %r"
          % (name, len(blockers), readiness))
    calibration = manifest.get("calibration") or {}
    check(isinstance(calibration, dict), "%s: calibration must be an object" % name)
    if readiness == "grader-calibrated":
        for key, want_zero_exit in (("fails_at_base", False), ("passes_on_fix", True)):
            check(key in calibration, "%s: grader-calibrated claims %s with no record" % (name, key))
            _evidence(calibration[key], key, name, want_zero_exit, manifest["task_kind"])
    else:
        for key, record in calibration.items():
            _evidence(record, key, name, key == "passes_on_fix", manifest["task_kind"])


def git(repo, *args):
    result = subprocess.run(["git", "-C", str(repo), *args], capture_output=True, text=True)
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
    for field in ("guarded_paths", "fix_paths"):
        for path in manifest["acceptance"][field]:
            if git(repo, "cat-file", "-e", "%s:%s" % (base, path))[0] != 0:
                findings.append("%s %s does not exist at the base commit" % (field[:-6].strip("_"), path))
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
        name = path.rsplit("/", 1)[-1]
        try:
            manifest = load(path)
            validate(manifest, name)
        except Refusal as refusal:
            print("REFUSED  %s" % refusal, file=sys.stderr)
            failed += 1
            continue
        except (ValueError, OSError, TypeError, KeyError, AttributeError, IndexError) as error:
            # A malformed manifest is a refusal, not a traceback.
            print("REFUSED  %s: malformed manifest (%s: %s)"
                  % (name, type(error).__name__, error), file=sys.stderr)
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
