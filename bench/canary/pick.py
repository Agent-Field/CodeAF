#!/usr/bin/env python3
"""Pick real GitHub issues whose fix is graded by tests the fix itself shipped.

The canary needs tasks a person would actually paste into a coding assistant,
each small enough to finish in a few minutes, and each with an outcome a script
can check without a model's opinion. Closed issues that were closed by a merged
pull request carrying tests give exactly that: the pull request's tests fail on
the commit before the fix and pass after it, so a candidate solution is graded
by running them.

Every pick is validated offline before it is admitted: the repository is
cloned, the base commit checked out, the tests installed, and the three facts a
grade depends on are measured rather than assumed — the fix's tests fail at
base, the pre-existing tests pass at base, and the fix itself turns the tests
green. A candidate that fails any of these is dropped with the reason printed;
nothing is admitted on the strength of its metadata.

Usage:
  pick.py --anchors 3                                 write frozen anchors into pool.json
  pick.py --fresh 2 --exclude pool.json --out f.json  fresh picks for one run
  pick.py --dry-run                                   list candidates and reasons, no clones
  pick.py --seed 7 ...                                reproducible candidate order
  pick.py --remeasure owner/repo [owner/repo ...]     re-measure those entries' base in pool.json

Only the standard library is used; `gh`, `git` and `python3 -m venv` are
shelled out to. Nothing is pushed, opened or commented anywhere: repositories
are cloned, measured and deleted.
"""

import argparse
import datetime as dt
import json
import os
import random
import re
import shutil
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
DEFAULT_POOL = os.path.join(HERE, "pool.json")

# The closing sentence every harness receives, spelled exactly as bench/run.sh
# spells it, so a canary prompt and a benchmark prompt are the same instruction.
TAIL = ("Work in this repository. Implement the change and make the existing "
        "test suite pass. Do not weaken or delete tests to make them pass.")

# Two size bands, because a canary wants both the five-minute fix and the
# half-hour one, and the difference must be a NAMED band rather than a quietly
# raised cap: a "small" pick and a "medium" pick are different measurements and
# the scoreboard has to be able to tell them apart. `--tier` selects which band
# screens a run; every admitted entry records the band it actually fits.
SIZE_BANDS = {
    "small": {"max_source_files": 2, "max_source_lines": 150},
    "medium": {"max_source_files": 4, "max_source_lines": 400},
}

# The criteria are data, written into pool.json beside the picks, so a reader
# of the pool can see what a pick had to satisfy without opening this file.
CRITERIA = {
    "language": "python",
    "labels": ["bug", "good first issue", "help wanted", "regression"],
    # `linked:pr` restricts the search to issues GitHub knows were closed by a
    # pull request, which is most of the filtering done for free.
    "search_qualifiers": "linked:pr",
    "issue_state": "closed",
    "pr_state": "merged",
    "size_bands": SIZE_BANDS,
    "max_source_files": SIZE_BANDS["small"]["max_source_files"],
    "max_source_lines": SIZE_BANDS["small"]["max_source_lines"],
    "min_test_files": 1,
    "min_stars": 30,
    "min_body_chars": 150,
    "max_body_chars": 4000,
    "interface_filter": True,
    "install_ladder": [".[dev]", ".[test]", ".[tests]", "--group dev", "--group test", ".",
                       "requirements*.txt", "pytest"],
    "install_cap_seconds": 240,
    "suite_cap_seconds": 600,
}

# THE BASE IS MEASURED EXACTLY THE WAY A CELL IS GRADED. The whole-suite run
# continues past a module that cannot import, so an unmet optional dependency
# counts as one error and the rest of the suite still runs; without the flag
# pytest stops at collection in half a second and the base says nothing at all.
# The fix's own tests are NOT run this way — a collection error there is the
# finding. lib/judge.py spells the same list in its own `SUITE_FLAGS`, because
# the two files do not import each other; change one and change the other in
# the same edit.
SUITE_FLAGS = ["--continue-on-collection-errors"]

# Files whose change would mean the fix needed packaging or CI work, which a
# harness given only the source tree cannot be expected to reproduce.
INFRA_PATTERNS = re.compile(
    r"(^|/)(\.github/|\.gitlab|\.circleci|\.travis|azure-pipelines|"
    r"setup\.py$|setup\.cfg$|pyproject\.toml$|requirements[^/]*\.txt$|"
    r"tox\.ini$|noxfile\.py$|MANIFEST\.in$|Makefile$|Dockerfile|"
    r"\.pre-commit-config|conftest\.py$)")

# Documentation and changelog fragments ride along with most fixes and do not
# affect what the tests grade, so they are allowed but not counted as source.
DOC_PATTERNS = re.compile(
    # A release-note fragment is a changelog entry, which the rule already
    # lets ride without counting it as source.
    r"(\.(md|rst|txt)$|(^|/)releasenotes/notes/[^/]+\.ya?ml$"
    r"|(^|/)(docs?|doc|changelog\.d|changes|news|newsfragments)/"
    # Half the projects a search reaches keep their changelog as an
    # extensionless file at the root — `ChangeLog`, `CHANGES`, `NEWS`. Without
    # this arm such a file reads as a non-source file and rejects the whole
    # candidate, which is the opposite of the stated rule that changelog
    # fragments ride along and are not counted as source.
    r"|(^|/)(changelog|changes|news|history)[^/.]*$)",
    re.IGNORECASE)


def log(msg):
    """Progress goes to stderr so stdout stays a clean ledger of decisions."""
    print(msg, file=sys.stderr, flush=True)


def reject(cand, reason, tally):
    """Every rejection is one line, in one shape, so a run's output can be
    tallied with grep. The tally keys on the reason's leading phrase."""
    print("reject %s#%d: %s" % (cand["repo"], cand["number"], reason), flush=True)
    key = re.split(r": | \(", reason, maxsplit=1)[0]
    tally[key] = tally.get(key, 0) + 1


# ── shelling out ─────────────────────────────────────────────────────────────

def run(cmd, cwd=None, timeout=None, env=None):
    """Run a command and return the completed process; a timeout is reported as
    returncode 124 rather than raised, so callers treat it like any failure."""
    try:
        return subprocess.run(cmd, cwd=cwd, timeout=timeout, env=env,
                              capture_output=True, text=True, errors="replace")
    except subprocess.TimeoutExpired as exc:
        # A run cut at its cap hands its output back as bytes even in text
        # mode, and a caller concatenating it with a string would die on the
        # one suite that is slow: decode it here, once.
        return subprocess.CompletedProcess(cmd, 124, _text(exc.stdout), _text(exc.stderr))


def _text(chunk):
    """Whatever a cut run left in a pipe, as text: bytes decode, None is nothing."""
    if chunk is None:
        return ""
    return chunk.decode("utf-8", "replace") if isinstance(chunk, bytes) else chunk


RATE_LIMITED = re.compile(r"rate limit|secondary|abuse|HTTP 403|HTTP 422|HTTP 429", re.I)


def gh(args, retries=3):
    """Run gh and parse its JSON output. GitHub's search API allows thirty
    requests a minute and answers a secondary limit with 403 or 422; a
    minute's sleep is the only correct response, so that is what happens."""
    for attempt in range(retries):
        proc = run(["gh"] + args, timeout=120)
        if proc.returncode == 0:
            return json.loads(proc.stdout) if proc.stdout.strip() else None
        if RATE_LIMITED.search(proc.stderr) and attempt + 1 < retries:
            log("  gh rate-limited; sleeping 65s")
            time.sleep(65)
            continue
        raise RuntimeError("gh %s failed: %s" % (" ".join(args[:3]), proc.stderr.strip()[:300]))
    raise RuntimeError("gh gave up")


# ── search and resolution ────────────────────────────────────────────────────

def search(labels, per_label, seed, sort="updated", repos=()):
    """One search per label, sorted as asked — by recent activity, or by
    interactions when the recently-updated stream is drowning in one-author
    repositories — deduplicated across labels, then shuffled with the seed so
    two runs on the same day walk the same order and two runs with different
    seeds walk different ones."""
    seen, found = set(), []
    for label in labels:
        # A shortlist of repositories narrows the stream to projects known to
        # keep a suite; the criteria still decide, the list only says where to look.
        scope = [flag for repo in repos for flag in ("--repo", repo)]
        rows = gh(["search", "issues", CRITERIA["search_qualifiers"], *scope,
                   "--state", CRITERIA["issue_state"], "--language", CRITERIA["language"],
                   "--label", label, "--sort", sort, "--limit", str(per_label),
                   "--json", "repository,number,title"]) or []
        for row in rows:
            key = (row["repository"]["nameWithOwner"], row["number"])
            if key in seen:
                continue
            seen.add(key)
            found.append({"repo": key[0], "number": key[1], "title": row["title"]})
        log("search label=%r gave %d rows (%d unique so far)" % (label, len(rows), len(found)))
    random.Random(seed).shuffle(found)
    return found


def resolve_batch(cands):
    """Resolve up to twenty issues in one GraphQL call: repository stars and
    archive state, the issue body, and the pull requests GitHub records as
    having closed it. Aliasing them into one query keeps the per-issue cost
    off the rate-limited search budget entirely."""
    parts = []
    for i, c in enumerate(cands):
        owner, name = c["repo"].split("/", 1)
        parts.append(
            'a%d: repository(owner:%s, name:%s) { isArchived stargazerCount '
            'defaultBranchRef { name } issue(number:%d) { title body '
            'closedByPullRequestsReferences(first:5, includeClosedPrs:true) '
            '{ nodes { number merged mergeCommit { oid } repository { nameWithOwner } } } } }'
            % (i, json.dumps(owner), json.dumps(name), c["number"]))
    data = gh(["api", "graphql", "-f", "query=query { %s }" % " ".join(parts)])
    out = []
    for i, c in enumerate(cands):
        out.append((data or {}).get("data", {}).get("a%d" % i))
    return out


def merged_pr(meta, repo):
    """The first pull request GitHub lists as closing the issue that was
    actually merged, has a merge commit, and lives in the issue's own
    repository. A fix that landed in a sibling repository (an issue tracker
    separate from the code, say) cannot be graded in the tracker's clone."""
    for node in meta["issue"]["closedByPullRequestsReferences"]["nodes"]:
        if node["merged"] and node.get("mergeCommit") and node["repository"]["nameWithOwner"] == repo:
            return node
    return None


def empty_sections(body):
    """Count template headings with nothing under them. A body that is mostly
    an unfilled issue form is not something a person wrote for a reader."""
    text = re.sub(r"<!--.*?-->", "", body, flags=re.S)
    sections = re.split(r"^(?:#{1,6}\s+\S.*|\*\*[^*\n]+\*\*\s*)$", text, flags=re.M)
    blanks = 0
    for sec in sections[1:]:
        content = sec.strip()
        if not content or content.lower() in ("_no response_", "n/a", "none", "-"):
            blanks += 1
    return blanks, len(text.strip())


def screen_metadata(cand, meta):
    """The cheap filters, applied before anything touches a network beyond the
    one GraphQL call. Returns a rejection reason or None."""
    if meta is None or meta.get("issue") is None:
        return "unresolvable via GraphQL"
    if meta["isArchived"]:
        return "repository archived"
    if meta["stargazerCount"] < CRITERIA["min_stars"]:
        return "stars below floor (%d)" % meta["stargazerCount"]
    body = meta["issue"]["body"] or ""
    blanks, prose = empty_sections(body)
    if not CRITERIA["min_body_chars"] <= len(body) <= CRITERIA["max_body_chars"]:
        return "body length out of range (%d chars)" % len(body)
    if blanks >= 2 or prose < CRITERIA["min_body_chars"]:
        return "body is an unfilled template (%d empty sections)" % blanks
    if merged_pr(meta, cand["repo"]) is None:
        return "no merged closing pull request in the same repository"
    return None


# ── the pull request's shape ─────────────────────────────────────────────────

def is_test_path(path):
    """Whether a path is a test file rather than library code. A bare search
    for "test" anywhere in the path is not enough: werkzeug ships its test
    CLIENT as `src/werkzeug/test.py` and sphinx ships fixtures as
    `sphinx/testing/`, both of which are library code a fix may legitimately
    change. Mistaking one for a test file is not a cosmetic error — the graders
    are checked out from the merge commit, so a source file counted as a test
    would hand the harness the fix it is supposed to write. A test is therefore
    a file that lives in a `test`/`tests` directory, or whose own name is
    `test_*.py` or `*_test.py`."""
    parts = path.lower().split("/")
    if any(seg in ("test", "tests") for seg in parts[:-1]):
        return True
    name = parts[-1]
    return name.startswith("test_") or name.endswith("_test.py")


def classify_files(files):
    """Split the pull request's files into source, tests and documentation, or
    return a reason the shape disqualifies the candidate."""
    src, tests, docs = [], [], []
    for f in files:
        path = f["filename"]
        if f["status"] in ("renamed", "removed"):
            return None, "pull request renames or deletes a file (%s)" % path
        if is_test_path(path):
            tests.append(f)
        elif INFRA_PATTERNS.search(path):
            return None, "pull request touches packaging or CI (%s)" % path
        elif path.endswith((".py", ".pyi")):
            src.append(f)
        elif DOC_PATTERNS.search(path):
            docs.append(f)
        else:
            return None, "pull request touches a non-source file (%s)" % path
    return (src, tests, docs), None


def band_of(files, lines):
    """The narrowest size band a fix fits in, or None if it fits none. The
    band is recorded on the entry, so a pick admitted by a medium run that
    happens to be small is still labelled small."""
    for name in ("small", "medium"):
        band = SIZE_BANDS[name]
        if files <= band["max_source_files"] and lines <= band["max_source_lines"]:
            return name
    return None


def shape_reason(src, tests, tier="small"):
    """The size limits for the band this run screens by, as one reason string
    or None."""
    band = SIZE_BANDS[tier]
    if not src:
        return "no source file changed"
    if len(src) > band["max_source_files"]:
        return "too many source files (%d)" % len(src)
    if len(tests) < CRITERIA["min_test_files"]:
        return "no test file changed"
    lines = sum(f["additions"] + f["deletions"] for f in src)
    if lines > band["max_source_lines"]:
        return "source diff too large (%d lines)" % lines
    if any(f.get("patch") is None for f in src + tests):
        return "a patch is too large for the API to return"
    return None


# ── the interface filter ─────────────────────────────────────────────────────

DEF_RE = re.compile(r"^[-+]\s*(?:async\s+)?def\s+(\w+)\s*\((.*)")
CLASS_RE = re.compile(r"^[-+]\s*class\s+(\w+)")
CONST_RE = re.compile(r"^[-+]([A-Za-z_]\w*)\s*(?::[^=]+)?=[^=]")


def params_of(signature_tail):
    """Parameter names from the text after `def name(`, best effort on a
    single line; multi-line signatures yield whatever fits on the first."""
    body = signature_tail.rsplit(")", 1)[0] if ")" in signature_tail else signature_tail
    names = set()
    for part in body.split(","):
        m = re.match(r"\s*\**(\w+)", part)
        if m and m.group(1) not in ("self", "cls"):
            names.add(m.group(1))
    return names


def introduced_names(patch):
    """Names the source diff brings into existence: new functions and methods,
    new classes, new module-level constants, and new parameters on signatures
    that already existed. A test that mentions any of them is grading the fix
    author's naming, which a different correct fix would not share."""
    added, removed = {"def": {}, "class": set(), "const": set()}, {"def": {}, "class": set(), "const": set()}
    for line in patch.splitlines():
        side = added if line.startswith("+") else removed if line.startswith("-") else None
        if side is None:
            continue
        m = DEF_RE.match(line)
        if m:
            side["def"].setdefault(m.group(1), set()).update(params_of(m.group(2)))
        m = CLASS_RE.match(line)
        if m:
            side["class"].add(m.group(1))
        m = CONST_RE.match(line)
        if m:
            side["const"].add(m.group(1))
    names = set(added["class"] - removed["class"]) | (added["const"] - removed["const"])
    for name, params in added["def"].items():
        if name not in removed["def"]:
            names.add(name)
        else:
            names |= params - removed["def"][name]
    return names


def interface_conflicts(src, tests):
    """The introduced names that the tests' added lines mention."""
    names = set()
    for f in src:
        names |= introduced_names(f["patch"])
    added_test = "\n".join(l for f in tests for l in f["patch"].splitlines() if l.startswith("+"))
    return sorted(n for n in names if re.search(r"\b%s\b" % re.escape(n), added_test))


# ── candidate assembly ───────────────────────────────────────────────────────

def base_of(repo, merge):
    """The merge commit's first parent, which is the pre-fix tree for both a
    squash and a true merge. A commit with no parents cannot anchor anything."""
    info = gh(["api", "repos/%s/commits/%s" % (repo, merge)])
    parents = info.get("parents") or []
    return parents[0]["sha"] if parents else None


def build_candidate(cand, meta, tally, tier="small"):
    """Turn a search hit into a fully described candidate, or reject it. This is
    everything that can be decided from the API alone."""
    pr = merged_pr(meta, cand["repo"])
    files = gh(["api", "repos/%s/pulls/%d/files?per_page=100" % (cand["repo"], pr["number"])])
    if len(files) >= 100:
        return reject(cand, "pull request touches too many files", tally)
    groups, reason = classify_files(files)
    if reason:
        return reject(cand, reason, tally)
    src, tests, _docs = groups
    reason = shape_reason(src, tests, tier)
    if reason:
        return reject(cand, reason, tally)
    conflicts = interface_conflicts(src, tests)
    if conflicts:
        return reject(cand, "tests depend on names the fix invented: %s" % ", ".join(conflicts), tally)
    base = base_of(cand["repo"], pr["mergeCommit"]["oid"])
    if base is None:
        return reject(cand, "merge commit has no parent", tally)
    issue = meta["issue"]
    return {
        "id": "%s-%d" % (cand["repo"].replace("/", "-"), cand["number"]),
        "repo": cand["repo"], "issue": cand["number"], "pr": pr["number"],
        "title": issue["title"], "base": base, "merge": pr["mergeCommit"]["oid"],
        "test_files": [f["filename"] for f in tests],
        "src_files": [f["filename"] for f in src],
        "all_files": [f["filename"] for f in files],
        "src_lines": sum(f["additions"] + f["deletions"] for f in src),
        "stars": meta["stargazerCount"],
        "prompt": "Implement issue #%d: %s\n\n%s\n\n%s" % (cand["number"], issue["title"], issue["body"], TAIL),
    }


# ── offline validation ───────────────────────────────────────────────────────

COUNTS_RE = re.compile(r"(\d+) (passed|failed|error|errors|skipped)")

# The stats line pytest writes last is decorated with '=' at default
# verbosity but printed BARE under -q, which is how every run here invokes
# pytest: `1 failed, 11 passed in 0.25s`. What both spellings share is the
# trailing duration, so that is what identifies the line. Requiring the '='
# alone read every quiet run as zero of everything, and a zero count is
# exactly what `check_fail_to_pass` treats as "the fix's tests already pass at
# base" — so a correctly failing candidate was rejected as a passing one, and
# `check_gold` refused a green gold run for having passed nothing.
DURATION_RE = re.compile(r"\bin \d+(?:\.\d+)?s\b")


def pytest_counts(output):
    """Read pytest's summary line into counts. Errors are kept apart from
    failures because a file that fails to import is a different fact from a
    test that ran and failed."""
    counts = {"passed": 0, "failed": 0, "errors": 0, "skipped": 0}
    for line in reversed(output.splitlines()):
        found = COUNTS_RE.findall(line)
        if found and (line.startswith("=") or DURATION_RE.search(line)):
            for n, kind in found:
                counts["errors" if kind.startswith("error") else kind] = int(n)
            break
    return counts


def last_line_of(output):
    lines = [l.strip() for l in output.splitlines() if l.strip()]
    return lines[-1][:160] if lines else ""


def not_green(what, counts, code, wb):
    """A rejection reason that says how the tests were not green: the counts
    when pytest summarised, and its exit code and last line when it did not."""
    if counts["failed"] + counts["errors"]:
        return "%s (%d failed, %d errors)" % (what, counts["failed"], counts["errors"])
    return "%s (pytest exit %d: %s)" % (what, code, wb.last_line)


def ladder_from(rung):
    """The install ladder from `rung` downwards, or the whole of it when no rung
    is named. A rung that is not on the ladder at all — an entry written when the
    ladder was spelled differently — walks the whole ladder rather than nothing."""
    ladder = CRITERIA["install_ladder"]
    return ladder[ladder.index(rung):] if rung in ladder else ladder


class Workbench:
    """One scratch clone with its own venv, at the candidate's base commit."""

    def __init__(self, cand, scratch):
        self.cand = cand
        self.dir = os.path.join(scratch, cand["id"])
        self.venv = os.path.join(scratch, cand["id"] + "-venv")
        self.install = None
        self.python = None
        self.last_line = ""

    def py(self):
        return os.path.join(self.venv, "bin", "python")

    def git(self, *args, timeout=300):
        return run(["git", "-c", "advice.detachedHead=false"] + list(args), cwd=self.dir, timeout=timeout)

    def clone(self):
        shutil.rmtree(self.dir, ignore_errors=True)
        proc = run(["git", "clone", "--quiet", "https://github.com/%s.git" % self.cand["repo"], self.dir], timeout=300)
        if proc.returncode != 0:
            return "clone failed"
        if self.git("checkout", "--quiet", self.cand["base"]).returncode != 0:
            return "base commit not reachable"
        return None

    def pip(self, args, deadline):
        remaining = max(1, int(deadline - time.time()))
        env = dict(os.environ, PIP_DISABLE_PIP_VERSION_CHECK="1")
        return run([self.py(), "-m", "pip", "install", "--quiet"] + args, cwd=self.dir, timeout=remaining, env=env)

    def make_venv(self, start_rung=None):
        """Install the tests' dependencies by the same ladder bench/run.sh
        climbs: dev extras first, then the bare package, then requirement
        files, then nothing but pytest. The first rung that installs is
        recorded, because a run must reproduce the same environment.

        A remeasure passes `start_rung` so a frozen entry is rebuilt from the
        rung it was validated on, and — when that rung has stopped resolving —
        from the next one DOWN. Never from a richer rung above it: the ladder is
        ordered by how much of the suite's dependencies a rung installs, so
        climbing back up would quietly measure a fuller environment than the
        cells that read this base will ever build."""
        shutil.rmtree(self.venv, ignore_errors=True)
        if run([sys.executable, "-m", "venv", self.venv], timeout=120).returncode != 0:
            return "venv creation failed"
        deadline = time.time() + CRITERIA["install_cap_seconds"]
        # The system pip predates PEP 735 dependency groups; bench/run.sh
        # upgrades before installing and this must build the same environment.
        self.pip(["--upgrade", "pip"], deadline)
        for rung in ladder_from(start_rung):
            if time.time() > deadline:
                return "install exceeded %ds" % CRITERIA["install_cap_seconds"]
            if self.try_rung(rung, deadline):
                self.install = rung
                break
        if self.install is None:
            return "no install rung succeeded"
        if run([self.py(), "-m", "pytest", "--version"], cwd=self.dir, timeout=60).returncode != 0:
            if self.pip(["pytest"], deadline).returncode != 0:
                return "pytest could not be installed"
        self.python = run([self.py(), "-c", "import sys; print('%d.%d' % sys.version_info[:2])"], timeout=30).stdout.strip()
        return None

    def try_rung(self, rung, deadline):
        """One rung. An extra the project does not declare is a failed rung,
        not a success: pip exits 0 and merely warns, and the warning is the
        only sign that nothing the tests need was installed."""
        if rung == "pytest":
            return self.pip(["pytest"], deadline).returncode == 0
        if rung == "requirements*.txt":
            reqs = sorted(f for f in os.listdir(self.dir) if re.match(r"requirements.*\.txt$", f))
            return bool(reqs) and all(self.pip(["-r", r], deadline).returncode == 0 for r in reqs)
        if rung.startswith("--group "):
            proc = self.pip(["-e", ".", "--group", rung.split()[1]], deadline)
        else:
            proc = self.pip(["-e", rung], deadline)
        return proc.returncode == 0 and "does not provide the extra" not in proc.stderr

    def pytest(self, paths, timeout, flags=()):
        """Run pytest with the cache disabled so the tree stays clean between
        checks; returns (counts, exit code). The last line pytest printed is
        kept on the counts, because a non-zero exit with no summary line (a
        usage error, a broken conftest) is only explicable from that line.
        Only the whole-suite measurement passes `flags`; see SUITE_FLAGS."""
        env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
        proc = run([self.py(), "-m", "pytest", "-q", "-p", "no:cacheprovider"] + list(flags) + paths,
                   cwd=self.dir, timeout=timeout, env=env)
        counts = pytest_counts(proc.stdout + proc.stderr)
        self.last_line = last_line_of(proc.stdout + proc.stderr)
        return counts, proc.returncode

    def existing_tests(self):
        return [t for t in self.cand["test_files"] if os.path.exists(os.path.join(self.dir, t))]

    def reset(self):
        self.git("reset", "-q", "--hard", self.cand["base"])
        self.git("clean", "-fdq")

    def cleanup(self):
        shutil.rmtree(self.dir, ignore_errors=True)
        shutil.rmtree(self.venv, ignore_errors=True)


def check_original_green(wb, record):
    """The tests that existed before the fix must pass at base; otherwise a
    red result later could not be blamed on the fix's absence."""
    existing = wb.existing_tests()
    if not existing:
        record["original_tests"] = {"note": "every test file is new in the pull request; check skipped"}
        return None
    counts, code = wb.pytest(existing, 600)
    record["original_tests"] = counts
    if code != 0:
        return not_green("pre-existing tests are not green at base", counts, code, wb)
    return None


def check_fail_to_pass(wb, record):
    """With the fix's tests but not the fix, something must fail."""
    if wb.git("checkout", "--quiet", wb.cand["merge"], "--", *wb.cand["test_files"]).returncode != 0:
        return "could not check out the fix's tests"
    counts, _code = wb.pytest(wb.cand["test_files"], 600)
    record["f2p_at_base"] = counts
    if counts["failed"] + counts["errors"] == 0:
        return "the fix's tests already pass at base"
    return None


def check_gold(wb, record):
    """With the whole fix applied, the same tests must be fully green."""
    if wb.git("checkout", "--quiet", wb.cand["merge"], "--", *wb.cand["all_files"]).returncode != 0:
        return "could not check out the fix"
    counts, code = wb.pytest(wb.cand["test_files"], 600)
    record["gold"] = counts
    if code != 0 or counts["passed"] == 0:
        return not_green("gold is not green", counts, code, wb)
    return None


def measure_base_suite(wb, record):
    """The whole suite at base, recorded not asserted: a suite with failures
    unrelated to the issue is still a usable task, but a run must know the
    baseline to read its own result. The cap is honoured by writing a note
    instead of a number."""
    wb.reset()
    cap = CRITERIA["suite_cap_seconds"]
    counts, code = wb.pytest([], cap, SUITE_FLAGS)
    if code == 124:
        record["base_suite"] = None
        record["base_suite_note"] = "full suite exceeded the %ds cap; not measured" % cap
    else:
        record["base_suite"] = {"passed": counts["passed"], "failed": counts["failed"], "errors": counts["errors"]}


def validate(cand, scratch, keep, source="fresh"):
    """Clone, install, and run the checks in order of cheapness. Returns the
    admitted entry or a rejection reason."""
    wb = Workbench(cand, scratch)
    record = {}
    try:
        reason = wb.clone() or wb.make_venv()
        for check in (check_original_green, check_fail_to_pass, check_gold):
            if reason:
                break
            reason = check(wb, record)
        if reason:
            return None, reason
        measure_base_suite(wb, record)
        return entry_for(cand, wb, record, source), None
    finally:
        if not keep:
            wb.cleanup()


def entry_for(cand, wb, record, source="fresh"):
    entry = {k: cand[k] for k in ("id", "repo", "issue", "pr", "title", "base", "merge",
                                  "test_files", "src_files", "src_lines", "stars", "prompt")}
    # Where the candidate came from is part of the measurement: an issue fed
    # from a published benchmark list may sit in a model's training data, and a
    # scoreboard that cannot say so is comparing two different things.
    entry["source"] = source
    entry["tier"] = band_of(len(cand["src_files"]), cand["src_lines"])
    entry["install"] = wb.install
    entry["python"] = wb.python
    entry.update(record)
    entry["picked_at"] = dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    return entry


# ── the pool ─────────────────────────────────────────────────────────────────

def load_json(path):
    with open(path) as fh:
        return json.load(fh)


def excluded_repos(paths):
    """Repositories already used by any listed pool or pick file. A fresh pick
    from an anchor's repository would share its dependency quirks and its
    contributors' habits with the anchor, and measure less."""
    repos = set()
    for path in paths:
        data = load_json(path)
        for entry in data.get("anchors", []) + data.get("picks", []):
            repos.add(entry["repo"])
    return repos


def write_json(path, doc):
    """Write a pool document in the one spelling this file uses, so a pool
    rewritten in place differs from the one it replaced only where a
    measurement actually moved."""
    with open(path, "w") as fh:
        json.dump(doc, fh, indent=2, ensure_ascii=False)
        fh.write("\n")


def write_pool(path, key, entries, seed):
    write_json(path, {"criteria": CRITERIA, "seed": seed, key: entries})


# ── remeasuring a pool that is already frozen ────────────────────────────────
#
# A frozen anchor is still a measurement of a moving world. A dependency rung
# that resolved in June stops resolving in September, and a base measured before
# SUITE_FLAGS existed was measured differently from how every cell is now
# graded. Neither is a reason to repick the issue, so a remeasure rewrites ONLY
# what was measured — the rung that installed, the counts at base, and the day
# it was measured. The issue, its base, its merge, its tests and its prompt are
# left exactly as they were, which is what keeps the anchor an anchor.

def counts_text(suite):
    """One phrase for a base_suite, for the line a remeasure prints."""
    if not suite:
        return "not measured"
    return "%d passed, %d failed, %d errors" % (suite["passed"], suite["failed"], suite["errors"])


def remeasure_entry(entry, scratch, keep):
    """Rebuild one entry's environment and re-measure its base. A clone or an
    install that fails leaves the entry untouched and says so: a base nobody
    could measure today must not overwrite the one somebody measured before."""
    was_rung, was_suite = entry.get("install"), entry.get("base_suite")
    wb = Workbench(entry, scratch)
    record = {}
    try:
        reason = wb.clone() or wb.make_venv(entry.get("install"))
        if reason:
            print("remeasure %s: %s — entry left unchanged" % (entry["repo"], reason), flush=True)
            return
        measure_base_suite(wb, record)
    finally:
        if not keep:
            wb.cleanup()
    entry["install"] = wb.install
    entry["base_suite"] = record["base_suite"]
    # The cap's note is rewritten with the number beside it: a measurement that
    # no longer hits the cap must not leave the old note standing over a count.
    if "base_suite_note" in record:
        entry["base_suite_note"] = record["base_suite_note"]
    else:
        entry.pop("base_suite_note", None)
    entry["measured"] = dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%d")
    print("remeasure %s: %s -> %s · %s -> %s" % (
        entry["repo"], was_rung, wb.install, counts_text(was_suite), counts_text(entry["base_suite"])), flush=True)


def remeasure(pool_path, repos, scratch, keep):
    """Re-measure every entry of the named repositories, in place. A repository
    that is not in the pool is a typo rather than a no-op, so it stops the run
    before anything is cloned."""
    doc = load_json(pool_path)
    by_repo = {}
    for entry in doc.get("anchors", []) + doc.get("picks", []):
        by_repo.setdefault(entry["repo"], []).append(entry)
    missing = [repo for repo in repos if repo not in by_repo]
    if missing:
        sys.exit("not in %s: %s" % (pool_path, ", ".join(missing)))
    for repo in repos:
        for entry in by_repo[repo]:
            remeasure_entry(entry, scratch, keep)
    write_json(pool_path, doc)
    log("rewrote %s" % pool_path)


# ── main ─────────────────────────────────────────────────────────────────────

def parse_args():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--anchors", type=int, default=0, help="write this many frozen anchors into --pool")
    p.add_argument("--fresh", type=int, default=0, help="produce this many fresh validated picks into --out")
    p.add_argument("--remeasure", nargs="+", default=[], metavar="REPO",
                   help="re-measure these repositories' entries in --pool in place: install rung, base counts, date")
    p.add_argument("--pool", default=DEFAULT_POOL, help="the anchors file (default: pool.json beside this script)")
    p.add_argument("--out", default=None, help="where fresh picks are written")
    p.add_argument("--exclude", action="append", default=[], help="pool or pick files whose repositories are skipped")
    p.add_argument("--seed", type=int, default=0, help="candidate order; the same seed on the same day walks the same order")
    p.add_argument("--per-label", type=int, default=100, help="search rows per label")
    p.add_argument("--dry-run", action="store_true", help="list candidates and reasons; clone nothing")
    p.add_argument("--sort", default="updated", choices=["updated", "interactions", "reactions", "created"],
                   help="search order; interactions reaches the repositories people actually use")
    p.add_argument("--repo", action="append", default=[], help="search only these repositories (repeatable)")
    p.add_argument("--only", default="", help="skip the search and consider these, as owner/repo#n,owner/repo#n")
    p.add_argument("--scratch", default=os.environ.get("CANARY_SCRATCH", "/tmp/canary-picks"))
    p.add_argument("--keep", action="store_true", help="keep clones and venvs after validation")
    p.add_argument("--tier", default="small", choices=sorted(SIZE_BANDS),
                   help="which size band screens this run (default: small)")
    p.add_argument("--source", default="fresh",
                   help="recorded on every entry as where the candidate came from, e.g. swe-bench-verified")
    p.add_argument("--budget-minutes", type=float, default=40, help="stop admitting new candidates after this long")
    return p.parse_args()


def refuse_overwrite(path):
    """Anchors are frozen: a comparison across weeks is only a comparison if
    the weeks measured the same thing. Growing or replacing them is a new pool
    file, never an edit of this one."""
    if os.path.exists(path) and load_json(path).get("anchors"):
        sys.exit("refusing to overwrite anchors in %s; anchors are frozen" % path)


def walk(cands, want, args, excluded, tally):
    """Screen candidates in resolution batches and validate survivors until
    `want` are admitted or the budget is spent."""
    admitted, deadline = [], time.time() + args.budget_minutes * 60
    used = set(excluded)
    for start in range(0, len(cands), 20):
        batch = cands[start:start + 20]
        for cand, meta in zip(batch, resolve_batch(batch)):
            if len(admitted) >= want or time.time() > deadline:
                return admitted
            consider(cand, meta, args, used, tally, admitted)
    return admitted


def consider(cand, meta, args, used, tally, admitted):
    """One candidate, from metadata to admission or rejection. An API error on
    one candidate is that candidate's rejection, not the run's death."""
    if cand["repo"] in used:
        return reject(cand, "repository already used", tally)
    reason = screen_metadata(cand, meta)
    if reason:
        return reject(cand, reason, tally)
    try:
        full = build_candidate(cand, meta, tally, args.tier)
    except RuntimeError as exc:
        return reject(cand, "api error (%s)" % str(exc).splitlines()[-1][:120], tally)
    if full is None:
        return
    if args.dry_run:
        print("candidate %s#%d: %d src lines in %s; tests %s" % (
            cand["repo"], cand["number"], full["src_lines"], full["src_files"], full["test_files"]), flush=True)
        used.add(cand["repo"])
        return
    log("validating %s#%d (pr %d, %d src lines)" % (cand["repo"], cand["number"], full["pr"], full["src_lines"]))
    entry, reason = validate(full, args.scratch, args.keep, args.source)
    if reason:
        return reject(cand, reason, tally)
    used.add(cand["repo"])
    admitted.append(entry)
    print("admit %s#%d: f2p %d failing at base, gold %d passed, install %s" % (
        cand["repo"], cand["number"], entry["f2p_at_base"]["failed"] + entry["f2p_at_base"]["errors"],
        entry["gold"]["passed"], entry["install"]), flush=True)


def main():
    args = parse_args()
    if args.remeasure and (args.anchors or args.fresh):
        # A remeasure edits entries that already exist and a pick writes new
        # ones; asking for both in one invocation can only mean one of them was
        # a mistake, and guessing which would rewrite a frozen pool.
        sys.exit("--remeasure cannot be combined with --anchors or --fresh")
    if not args.dry_run and not (args.anchors or args.fresh or args.remeasure):
        sys.exit("say what you want: --anchors N, --fresh N, --remeasure REPO..., or --dry-run")
    if args.remeasure:
        os.makedirs(args.scratch, exist_ok=True)
        return remeasure(args.pool, args.remeasure, args.scratch, args.keep)
    if args.anchors:
        refuse_overwrite(args.pool)
    if args.fresh and not args.out:
        sys.exit("--fresh needs --out")
    os.makedirs(args.scratch, exist_ok=True)
    excluded = excluded_repos(args.exclude)
    want = args.anchors or args.fresh or 10 ** 6
    # A named candidate still walks every screen and every validation; --only
    # replaces the search, never the criteria.
    if args.only:
        cands = [{"repo": spec.split("#")[0], "number": int(spec.split("#")[1]), "title": ""}
                 for spec in args.only.split(",") if spec.strip()]
    else:
        cands = search(CRITERIA["labels"], args.per_label, args.seed, args.sort, args.repo)
    tally = {}
    admitted = walk(cands, want, args, excluded, tally)
    for reason, n in sorted(tally.items(), key=lambda kv: -kv[1]):
        log("tally %4d  %s" % (n, reason))
    if args.dry_run:
        return
    if len(admitted) < want:
        log("only %d of %d admitted before the budget or the candidates ran out" % (len(admitted), want))
    if args.anchors:
        write_pool(args.pool, "anchors", admitted, args.seed)
        log("wrote %d anchors to %s" % (len(admitted), args.pool))
    else:
        write_pool(args.out, "picks", admitted, args.seed)
        log("wrote %d picks to %s" % (len(admitted), args.out))


if __name__ == "__main__":
    main()
