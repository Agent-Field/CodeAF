#!/usr/bin/env python3
"""Run touched suites once, then attribute bounded, named failures."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

# FIVE FAILURES IS THE DIAGNOSTIC BUDGET. Ten focused runs plus at most one
# base checkout are useful evidence; a broken tree must not spawn hundreds.
FAILURE_CAP = 5
ISSUE_TITLE = "touched packages: flaky or already-failing tests"


def annotation(level, message):
    escaped = message.replace("%", "%25").replace("\r", "%0D").replace("\n", "%0A")
    print(f"::{level}::{escaped}", flush=True)


def capture(command, cwd=None):
    print("+ " + " ".join(command), flush=True)
    process = subprocess.Popen(command, cwd=cwd, stdout=subprocess.PIPE,
                               stderr=subprocess.STDOUT, text=True, errors="replace")
    lines = []
    for line in process.stdout:
        # Keep Go's human output in the job log and its events for attribution.
        # Event metadata without Output is bookkeeping, not a readable message.
        try:
            event = json.loads(line)
        except ValueError:
            event = None
        if isinstance(event, dict) and "Action" in event:
            sys.stdout.write(event.get("Output", ""))
        else:
            sys.stdout.write(line)
        sys.stdout.flush()
        lines.append(line)
    return process.wait(), "".join(lines)


def events(output):
    result = []
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if isinstance(event, dict) and "Action" in event:
            result.append(event)
    return result


def infrastructure(output, stream, status):
    if status < 0 or status in (137, 143):
        return "killed process"
    prose = output + "\n" + "".join(event.get("Output", "") for event in stream)
    raw = []
    for line in output.splitlines():
        try:
            json.loads(line)
        except ValueError:
            raw.append(line)
    diagnostics = "\n".join(raw) + "\n" + "".join(
        event.get("Output", "") for event in stream if not event.get("Test"))
    for pattern, reason in [
        (r"\[setup failed\]", "package setup failure ([setup failed])"),
        (r"\[build failed\]|build-fail|^# \S+", "build failure ([build failed])"),
        (r"shard-test:", "shard runner error"),
        (r"signal: killed|\bKilled\b|(?:exit status|Error) (137|143)", "killed process"),
    ]:
        if re.search(pattern, diagnostics, re.MULTILINE):
            return reason
    if re.search(r"^panic: test timed out", prose, re.MULTILINE):
        return "package-level timeout (panic: test timed out)"
    if any(event.get("Action") == "build-fail" for event in stream):
        return "build failure"
    # A panic under a named test may be attributed. A panic outside one cannot,
    # even when other tests in the same invocation also failed assertions.
    panic_events = [event for event in stream if "panic:" in event.get("Output", "")]
    if any(not event.get("Test") for event in panic_events):
        return "panic outside a test"
    if "panic:" in prose and not panic_events:
        return "panic outside a test"
    if status and not failed_tests(stream):
        return "runner failed without an attributable test name"
    failed_packages = {event.get("Package") for event in stream
                       if event.get("Action") == "fail" and not event.get("Test")}
    attributed = {package for package, _ in failed_tests(stream)}
    if failed_packages - attributed:
        return "package failed without an attributable test name"
    return None


def failed_tests(stream):
    failed = sorted({(event["Package"], event["Test"]) for event in stream
                     if event.get("Action") == "fail" and event.get("Test") and event.get("Package")})
    # Go reports each failed child and its parents. Retry the leaves so a
    # propagated parent failure does not rerun passing siblings as well.
    return [(package, name) for package, name in failed
            if not any(other_package == package and other.startswith(name + "/")
                       for other_package, other in failed)]


def selector(name):
    # Go splits -run on slashes before matching each component. Escape only
    # RE2 metacharacters; Python's escaped hyphens are not valid RE2 escapes.
    return "/".join("^(" + re.sub(r"([\\.\[\]{}()*+?^$|])", r"\\\1", part) + ")$"
                    for part in name.split("/"))


def focused(package, name, timeout, cwd=None):
    status, output = capture(["go", "test", "-json", "-count=1", "-p", "2",
                              "-timeout", timeout, "-run", selector(name), package], cwd)
    stream = events(output)
    reason = infrastructure(output, stream, status)
    plain_output = output + "\n" + "".join(event.get("Output", "") for event in stream)
    if reason and any(re.search(pattern, plain_output) for pattern in (
        r"no required module provides package\s+" + re.escape(package) + r"(?:;|\s)",
        r"package " + re.escape(package) + r" is not in std",
        r"main module .* does not contain package\s+" + re.escape(package) + r"(?:\s|$)",
    )):
        return "absent", "package absent (cannot resolve target)"
    target = [event for event in stream if event.get("Package") == package
              and event.get("Test") == name and event.get("Action") in ("pass", "fail", "skip")]
    if reason:
        return "error", reason
    if not target:
        return "absent", "test or package absent (no matching test result)"
    if target[-1]["Action"] == "skip":
        return "absent", "test did not run (skipped)"
    if target[-1]["Action"] == "fail":
        return "fail", None
    if status or failed_tests(stream):
        return "error", "focused run failed outside the selected test"
    return "pass", None


def write_report(path, rows, reasons):
    if path:
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        Path(path).write_text(json.dumps({"tests": rows, "errors": reasons}, indent=2) + "\n")
    if not rows and not reasons:
        print("Every selected test passed on its first run.", flush=True)
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a") as out:
            out.write("\n### Touched package results\n\n")
            for reason in reasons:
                out.write(f"- {reason}\n")
            for row in rows:
                out.write(f"- `{row['package']}` / `{row['test']}`: {row['verdict']}\n")
            if not rows and not reasons:
                out.write("Every selected test passed on its first run.\n")


def run(args):
    rows, reasons = [], []
    base_tree = None
    result = 0
    try:
        status, output = capture(["make", "--no-print-directory", "-s", "test",
                                  "PKGS=" + " ".join(args.packages),
                                  "KNOWN_RED=", "TEST_SKIP=",
                                  "TEST_FLAGS=-json -count=1 -p 2", "SHARDS=" + args.shards,
                                  "TEST_TIMEOUT=" + args.timeout])
        stream = events(output)
        reason = infrastructure(output, stream, status)
        failures = failed_tests(stream)
        if not any(event.get("Action") in ("pass", "fail", "skip")
                   and not event.get("Test") for event in stream):
            reason = reason or "runner produced no terminal package result"
        if reason:
            reasons.append(reason + "; no retry")
            annotation("error", reasons[-1])
            return 1
        if not status and not failures:
            return 0
        if len(failures) > FAILURE_CAP:
            reasons.append(f"{len(failures)} failing tests exceed the cap of {FAILURE_CAP}; "
                           "no retries or base probes: mass breakage needs investigation at head and base")
            annotation("error", reasons[-1])
            for package, name in failures:
                rows.append({"package": package, "test": name, "verdict": "failure cap exceeded"})
                annotation("error", f"{package}: {name} (failure cap exceeded)")
            return 1
        for package, name in failures:
            retry, detail = focused(package, name, args.timeout)
            if retry == "pass":
                verdict = "flaky"
            elif retry != "fail":
                verdict = "head retry could not execute: " + detail
                result = 1
            else:
                if base_tree is None:
                    base_tree = tempfile.TemporaryDirectory(prefix="codeaf-touched-base-")
                    status, _ = capture(["git", "worktree", "add", "--detach",
                                         str(Path(base_tree.name) / "tree"), args.base])
                    if status:
                        reasons.append("cannot create base worktree; stopping without a verdict")
                        annotation("error", reasons[-1])
                        return 1
                base, detail = focused(package, name, args.timeout, str(Path(base_tree.name) / "tree"))
                if base == "fail":
                    verdict = "already failing on the base"
                elif base == "pass":
                    verdict = "introduced by this change"
                    result = 1
                elif base == "absent" and "absent" in detail:
                    verdict = "introduced by this change; base could not run the test: " + detail
                    result = 1
                else:
                    verdict = "attribution inconclusive; base could not run the test: " + detail
                    result = 1
            rows.append({"package": package, "test": name, "verdict": verdict})
            level = "warning" if verdict in ("flaky", "already failing on the base") else "error"
            annotation(level, f"{package}: {name}: {verdict}")
            if retry == "error":
                reasons.append("head retry infrastructure failure; stopping further probes")
                return 1
        return result
    except OSError as error:
        reasons.append("runner error: " + str(error) + "; no retry")
        annotation("error", reasons[-1])
        return 1
    finally:
        if base_tree is not None:
            tree = str(Path(base_tree.name) / "tree")
            if Path(tree).exists():
                status, _ = capture(["git", "worktree", "remove", "--force", tree])
                if status:
                    # Do not erase a checkout whose git registration could not
                    # be removed. Leave the exact path for a person to repair.
                    base_tree._finalizer.detach()
                    reasons.append("cannot remove base worktree: " + tree)
                    annotation("error", reasons[-1])
                    write_report(args.report, rows, reasons)
                    raise RuntimeError(reasons[-1])
            base_tree.cleanup()
        write_report(args.report, rows, reasons)


def gh(arguments):
    return subprocess.check_output(["gh", *arguments], text=True, stderr=subprocess.PIPE, timeout=30)


def report_issues(args):
    # This runs once in the aggregate job, so four concurrent legs make one
    # comment and cannot race each other to create the standing issue.
    if os.environ.get("TOUCHED_REPORT_ISSUE") != "true":
        print("Issue reporting disabled for this run (fork or local run).")
        return 0
    try:
        rows = []
        for path in sorted(Path(args.directory).glob("*.json")):
            rows.extend(row for row in json.loads(path.read_text())["tests"]
                        if row["verdict"] in ("flaky", "already failing on the base"))
        if not rows:
            return 0
        repository = os.environ["GITHUB_REPOSITORY"]
        matches = json.loads(gh(["issue", "list", "--repo", repository, "--state", "open",
                                 "--search", '"' + ISSUE_TITLE + '" in:title', "--limit", "100",
                                 "--json", "number,title"]))
        # Concurrent runs may create duplicates. Converge on the oldest open
        # exact-title issue instead of relying on the API's result ordering.
        issue = min((row["number"] for row in matches if row["title"] == ISSUE_TITLE), default=None)
        if issue is None:
            url = gh(["issue", "create", "--repo", repository, "--title", ISSUE_TITLE,
                      "--body", "These tests remain bugs with an owner. Each comment records "
                      "a flaky or inherited failure; no test is skipped and this issue never closes automatically."]).strip()
            issue = url.rsplit("/", 1)[-1]
        run_url = (os.environ.get("GITHUB_SERVER_URL", "https://github.com") + "/" + repository
                   + "/actions/runs/" + os.environ["GITHUB_RUN_ID"])
        body = f"Run: {run_url}\nSHA: `{os.environ['GITHUB_SHA']}`\n\n"
        body += "\n".join(f"- `{row['package']}` / `{row['test']}`: {row['verdict']}" for row in rows)
        with tempfile.NamedTemporaryFile(mode="w", suffix=".md") as out:
            out.write(body + "\n")
            out.flush()
            gh(["issue", "comment", str(issue), "--repo", repository, "--body-file", out.name])
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        annotation("warning", "Could not record touched-test ownership; test result is unchanged: " + str(error))
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subcommands = parser.add_subparsers(dest="command", required=True)
    runner = subcommands.add_parser("run")
    runner.add_argument("--base", required=True)
    runner.add_argument("--report")
    # The Makefile owns the timeout for initial suites and focused diagnosis.
    # Reading its literal avoids a second policy number drifting in this file.
    makefile = (Path(__file__).resolve().parent.parent / "Makefile").read_text()
    timeout = re.search(r"^TEST_TIMEOUT\s*:?=\s*(\S+)", makefile, re.MULTILINE).group(1)
    runner.add_argument("--timeout", default=timeout)
    runner.add_argument("--shards", default=os.environ.get("SHARDS", "4"))
    runner.add_argument("packages", nargs="+")
    reporter = subcommands.add_parser("report-issues")
    reporter.add_argument("directory")
    args = parser.parse_args()
    try:
        return run(args) if args.command == "run" else report_issues(args)
    except (OSError, RuntimeError) as error:
        annotation("error", str(error))
        return 1


if __name__ == "__main__":
    sys.exit(main())
