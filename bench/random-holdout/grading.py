"""The same withheld-test grader for every holdout harness."""
import hashlib
from pathlib import Path
import subprocess
import time
import xml.etree.ElementTree as ET


def call(args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs).stdout


def test_outcomes(path):
    """Preserve duplicate parametrized names instead of collapsing test cases."""
    outcomes = []
    for case in ET.parse(path).getroot().iter("testcase"):
        status = next((kind for kind in ("failure", "error", "skipped")
                       if case.find(kind) is not None), "passed")
        outcomes.append((case.get("classname", ""), case.get("name", ""), status))
    return sorted(outcomes)


def restore_tests(repo, base, test_roots):
    # Restore the original regression tests even when a repository nests them
    # inside a package. The paths come from the frozen fixture, never a model.
    for root in test_roots:
        path = Path(root)
        if path.is_absolute() or ".." in path.parts or not path.parts:
            raise ValueError("Unsafe test root")
        tracked = call(["git", "ls-tree", "--name-only", base, "--", root], cwd=repo)
        if tracked.strip():
            call(["git", "restore", "--source=" + base, "--staged", "--worktree", "--", root], cwd=repo)
        call(["git", "clean", "-fd", "--", root], cwd=repo)


def grade(root, task, prepared, out, export_tree):
    """Apply the candidate to a fresh base and run the calibrated offline checks."""
    root, out = Path(root), Path(out)
    repo = out / "grading-workspace"
    fixture = root / "prepared" / task["id"]
    base = export_tree(fixture / "base-source.tar", repo)
    patch = out / "model.patch"
    try:
        if patch.stat().st_size:
            call(["git", "apply", str(patch)], cwd=repo)
    except subprocess.CalledProcessError as error:
        (out / "grade-apply-error.txt").write_text(error.stderr)
        return {"status": "patch_apply_failed"}
    restore_tests(repo, base, prepared["test_roots"])
    try:
        call(["git", "apply", str(root / "private" / task["id"] / "heldout.patch")], cwd=repo)
    except subprocess.CalledProcessError as error:
        (out / "grade-apply-error.txt").write_text(error.stderr)
        return {"status": "heldout_apply_failed"}
    logs = out / "grading"
    logs.mkdir(exist_ok=False)
    # Include the arm's full output path so concurrent arms cannot stop each
    # other's grader when their issue and seed are identical.
    container = "afholdout-grade-" + hashlib.sha256(str(out).encode()).hexdigest()[:16]
    command = ["docker", "run", "--rm", "--name", container, "--platform", "linux/amd64",
               "--network", "none", "--cpus", "2", "--memory", "8g",
               "--mount", f"type=bind,src={repo},dst={prepared['repo_dir']}",
               "--mount", f"type=bind,src={logs},dst=/grading",
               "-e", "PYTHONPYCACHEPREFIX=/tmp/afholdout-pycache",
               "-w", prepared["repo_dir"], "--entrypoint", "/bin/bash",
               prepared["runtime_image_id"], "-c", 'exec "$@"', "_"]
    command += prepared["test_command"] + ["--junitxml=/grading/results.xml"]
    started = time.monotonic()
    try:
        with (logs / "tests.log").open("w") as log:
            process = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, timeout=900)
    except subprocess.TimeoutExpired:
        subprocess.run(["docker", "rm", "-f", container], capture_output=True)
        return {"status": "grader_timeout", "seconds": round(time.monotonic() - started, 2)}
    result = {"exit": process.returncode, "seconds": round(time.monotonic() - started, 2)}
    report = logs / "results.xml"
    if not report.exists():
        return {**result, "status": "missing_report"}
    try:
        observed = test_outcomes(report)
        expected = test_outcomes(fixture / "gold.xml")
    except ET.ParseError:
        return {**result, "status": "invalid_report"}
    result.update(tests=len(observed), failed=sum(x[2] == "failure" for x in observed),
                  errors=sum(x[2] == "error" for x in observed),
                  skipped=sum(x[2] == "skipped" for x in observed))
    # A green exit with missing tests or newly skipped acceptance is not a
    # passing solution. Keep the reference's original skipped tests as skipped.
    result["reference_test_outcomes_match"] = observed == expected
    result["status"] = "passed" if process.returncode == 0 and observed == expected else "failed"
    return result
