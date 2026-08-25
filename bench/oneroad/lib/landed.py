#!/usr/bin/env python3
"""landed.py — what the attempt actually changed, on EITHER road.

WHY THIS EXISTS. `git status --porcelain` was the whole measure of a cell's work,
and it is correct for the words road: chat edits the checkout and leaves the
changes uncommitted. IT IS BLIND TO THE TASK ROAD. A task works in a git
worktree, COMMITS, and merges back — so the checkout comes out clean and the
cell records zero changed files over work that really happened. Wave 1d turned
nine cells onto that road at once and every one of them was being under-read.

So the work is the union of two things:

  - UNCOMMITTED — `git status --porcelain`, the words road's whole answer.
  - LANDED — `git diff <base>..HEAD`, the commits a task merged into the
    checkout.

AND ONE THING IS SUBTRACTED, LOUDLY. A cell whose HEAD carries the UPSTREAM's own
commits is contaminated: the repository's later history already contains fixes
for these issues, so a suite that passes there is passing on somebody else's
work. That is not a smaller number, it is an invalid row, and it is flagged
rather than quietly counted.
"""
import glob, os, re, subprocess, sys

# The repository's own later merges. A commit reachable from the pin that says
# this is upstream history the attempt pulled in, not work it did.
UPSTREAM = re.compile(r"Merge pull request|MALIBA-AI")

# The BENCHMARK's own leavings, and nothing else. The filter is anchored on
# purpose: a substring test for ".venv" also swallows `.venv_test/`, which one
# attempt CREATED AND COMMITTED — 3159 files of virtualenv in the diff. That is
# the attempt's own scope failure and precisely what JUDGE.md's scope_discipline
# exists to catch, so it must reach the judge rather than be tidied away by a
# filter meant for the harness's own venv.
def ours(path):
    return path.startswith(".venv/") or path == ".venv" or "__pycache__" in path


def git(repo, *args):
    out = subprocess.run(["git"] + list(args), cwd=repo,
                         capture_output=True, text=True)
    return out.stdout


def measure(repo, base):
    """Returns (uncommitted, landed, upstream_commits, landed_files)."""
    status = [l for l in git(repo, "status", "--porcelain").splitlines()
              if l and not ours(l[3:])]
    log = git(repo, "log", "--oneline", f"{base}..HEAD")
    upstream = sum(1 for l in log.splitlines() if UPSTREAM.search(l))
    landed = [f for f in git(repo, "diff", "--name-only", f"{base}..HEAD").splitlines()
              if f and not ours(f)]
    return len(status), len(landed), upstream, landed


def worktrees(cell):
    """The task worktrees, and the work still sitting in them.

    A task does not edit the clone: it branches into its own worktree under the
    session folder (`<session>/trees/<n>`), works there, commits, and merges home
    only when it LANDS. So a cell whose task was still running — or was killed
    while running, which is what the settle bug did to six wave-1f cells — has
    all of its work here and none of it in the clone. Measuring only the clone
    reported those cells as "0 files changed" over eighteen minutes of real work.

    Both halves are counted: what the worker has committed on its branch since
    the base, and what it had not committed yet. The second is the larger half
    for a killed cell, because the kill arrives between edits and the commit.
    """
    found = []
    for tree in sorted(glob.glob(os.path.join(cell, "profile", "v3", "projects",
                                              "*", "*", "trees", "*"))):
        if not os.path.isdir(os.path.join(tree, ".git")) and not os.path.exists(
                os.path.join(tree, ".git")):
            continue
        branch = git(tree, "rev-parse", "--abbrev-ref", "HEAD").strip()
        uncommitted = [l for l in git(tree, "status", "--porcelain").splitlines()
                       if l and not ours(l[3:])]
        found.append({"tree": tree, "branch": branch,
                      "uncommitted": len(uncommitted)})
    return found


def worktree_patch(cell):
    """The diff a judge should read for work that never left its worktree."""
    parts = []
    for tree in sorted(glob.glob(os.path.join(cell, "profile", "v3", "projects",
                                              "*", "*", "trees", "*"))):
        body = git(tree, "diff")
        if body.strip():
            parts.append(f"# ---- uncommitted in task worktree {os.path.basename(tree)} ----\n" + body)
        for line in git(tree, "status", "--porcelain").splitlines():
            if not line.startswith("??"):
                continue
            name = line[3:]
            if ours(name):
                continue
            path = os.path.join(tree, name)
            try:
                text = open(path, errors="replace").read()
            except (OSError, IsADirectoryError):
                continue
            parts.append(f"--- /dev/null\n+++ b/{name}\n" +
                         "".join("+" + l + "\n" for l in text.splitlines()))
    return "\n".join(parts)


def patch(repo, base):
    """The diff a judge should read: landed commits first, then the working tree."""
    parts = []
    landed = git(repo, "diff", f"{base}..HEAD")
    if landed.strip():
        parts.append("# ---- committed and merged by the task ----\n" + landed)
    tree = git(repo, "diff")
    if tree.strip():
        parts.append("# ---- uncommitted in the working tree ----\n" + tree)
    for line in git(repo, "status", "--porcelain").splitlines():
        if not line.startswith("??"):
            continue
        name = line[3:]
        if ours(name):
            continue
        path = os.path.join(repo, name)
        try:
            body = open(path, errors="replace").read()
        except (OSError, IsADirectoryError):
            continue
        parts.append(f"--- /dev/null\n+++ b/{name}\n" +
                     "".join("+" + l + "\n" for l in body.splitlines()))
    return "\n".join(parts)


if __name__ == "__main__":
    repo, base = sys.argv[1], sys.argv[2]
    u, l, up, files = measure(repo, base)
    print(f"uncommitted={u} landed={l} upstream_commits={up}")
