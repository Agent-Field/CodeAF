#!/usr/bin/env python3
"""Compare two folder manifests and print one markdown row per owner requirement.

Usage: fidelity-compare.py a.json b.json [--platform-b mac] [--expect-gone p1,p2] [--reported p1,p2]
Exit code 1 when any row is FAIL.
"""
from __future__ import annotations

import argparse
import base64
import json
import sys
import unicodedata
from types import SimpleNamespace

PASS, FAIL, DEFINED = "PASS", "FAIL", "DEFINED"
BIG = 50 << 20
DEEP = 200
SHOWN = 5


# ---------------------------------------------------------------- small helpers

def raw(key):
    return base64.b64decode(key)


def text(key):
    return raw(key).decode("utf-8", "surrogateescape")


def show(entries, key):
    """Display name; a name with non-ASCII bytes also shows its byte escape so NFC and NFD can be told apart."""
    name = raw(key).decode("utf-8", "backslashreplace")
    return name if raw(key).isascii() else "%s [%s]" % (name, entries[key]["esc"])


def listing(items):
    """First few items, with a count of the rest."""
    items = list(items)
    more = " (+%d more)" % (len(items) - SHOWN) if len(items) > SHOWN else ""
    return "; ".join(items[:SHOWN]) + more


def scoped(entries):
    """Keys outside any node_modules folder, which is withheld by design and reported by R10 only."""
    return [k for k in entries if b"node_modules" not in raw(k).split(b"/")]


def nfc(key):
    return unicodedata.normalize("NFC", text(key))


def is_reported(ctx, entries, key):
    return entries[key]["esc"] in ctx.reported or text(key) in ctx.reported


def vacuous(what):
    return FAIL, "A has no %s, so this check would prove nothing" % what


def entry_diff(ra, rb):
    """Reason two entries differ in kind, mode, size, content or link target, else None."""
    if ra["kind"] != rb["kind"]:
        return "kind %s->%s" % (ra["kind"], rb["kind"])
    # The index and the logs are rewritten by the receiving machine's own git, with its umask, so their mode is not the move's.
    if ra["kind"] != "symlink" and ra["mode"] != rb["mode"] and not (ra.get("sha_exempt") or rb.get("sha_exempt")):
        return "mode %s->%s" % (ra["mode"], rb["mode"])
    if ra["kind"] == "symlink":
        return None if ra["target_b64"] == rb["target_b64"] else "link target changed"
    return content_diff(ra, rb) if ra["kind"] == "file" else None


def content_diff(ra, rb):
    if ra.get("sha_exempt") or rb.get("sha_exempt"):
        return None
    if ra.get("error") or rb.get("error"):
        return "content could not be read, so it is unverified"
    if ra["size"] != rb["size"]:
        return "size %d->%d" % (ra["size"], rb["size"])
    return None if ra["sha256"] == rb["sha256"] else "sha256 differs"


def is_git_index(path):
    return path.startswith(b".git/") and path.endswith(b"/index") or path == b".git/index"


def compare_keys(a, b, keys):
    """(missing, differing) display strings for the given keys of A against B."""
    missing, differing = [], []
    for key in keys:
        if key not in b["entries"]:
            missing.append(show(a["entries"], key))
            continue
        why = entry_diff(a["entries"][key], b["entries"][key])
        if why and why.startswith("mode") and is_git_index(raw(key)):
            why = None   # a repository's index (a submodule's too) is rewritten by the receiving git with its own umask
        if why:
            differing.append("%s (%s)" % (show(a["entries"], key), why))
    return missing, differing


# ---------------------------------------------------------------- R4: unicode names

def non_ascii_keys(entries):
    return [k for k in scoped(entries) if not raw(k).isascii()]


def fold_targets(a, b):
    """Map each non-ASCII A key to the B key that holds it (same bytes first, else same NFC form), or None."""
    by_nfc = {}
    for key in scoped(b["entries"]):
        by_nfc.setdefault(nfc(key), []).append(key)
    targets = {}
    for key in non_ascii_keys(a["entries"]):
        if key in b["entries"]:
            targets[key] = key
        else:
            found = by_nfc.get(nfc(key), [])
            targets[key] = found[0] if found else None
    return targets


def split_survivors(targets):
    """Split A keys into survivors (own entry on B) and lost (no entry, or folded into another key's entry).

    Why: a name that kept its exact bytes owns its B entry; a renamed one only survives when it is alone on its target.
    """
    owner, lost = {}, []
    for key in sorted(targets):
        if targets[key] == key:
            owner[key] = key
    for key in sorted(targets):
        target = targets[key]
        if target == key:
            continue
        if target is None or target in owner:
            lost.append(key)
        else:
            owner[target] = key
    return [(k, t) for t, k in owner.items()], lost


def check_fold_survivors(a, b, targets, lost, problems):
    """Why: when two names fold into one entry, the survivor must still hold the bytes of one of them, else data was invented or lost."""
    survivors = {targets[k] for k in lost if targets[k] is not None}
    for target in survivors:
        originals = {a["entries"][k]["sha256"] for k, t in targets.items() if t == target}
        if b["entries"][target]["sha256"] not in originals:
            problems.append("survivor content equals no original: %s" % show(b["entries"], target))
    return survivors


def unicode_result(a, b, ctx):
    """Returns (verdict, evidence, explained_keys) for R4."""
    targets = fold_targets(a, b)
    if not targets:
        return (*vacuous("non-ASCII names"), set())
    survivors, lost = split_survivors(targets)
    renamed = [(k, t) for k, t in survivors if k != t]
    problems, notes, explained = [], [], set()
    for key, target in renamed:
        notes.append("%s became %s" % (show(a["entries"], key), show(b["entries"], target)))
        explained.update([key, target])
        if content_diff(a["entries"][key], b["entries"][target]):
            problems.append("renamed copy differs in content: %s" % show(a["entries"], key))
        if not (ctx.platform_b == "mac" or is_reported(ctx, a["entries"], key)):
            problems.append("bytes changed without a report: %s" % show(a["entries"], key))
    for key in lost:
        explained.add(key)
        if is_reported(ctx, a["entries"], key):
            notes.append("lost but reported: %s" % show(a["entries"], key))
        else:
            problems.append("entry lost, not reported: %s" % show(a["entries"], key))
    folded_into = check_fold_survivors(a, b, targets, lost, problems)
    explained |= folded_into
    if problems:
        return FAIL, listing(problems), set()
    if notes:
        return DEFINED, listing(notes), explained
    return PASS, "%d non-ASCII names byte-identical" % len(targets), set()


def r4(a, b, ctx):
    return unicode_result(a, b, ctx)[:2]


# ---------------------------------------------------------------- R5: case collisions

def case_groups(entries):
    groups = {}
    for key in scoped(entries):
        groups.setdefault(text(key).lower(), []).append(key)
    return {name: keys for name, keys in groups.items() if len(keys) > 1}


def judge_case_group(a, b, ctx, keys):
    """Returns (verdict, evidence, explained_keys) for one set of names that differ only in case."""
    lower = text(keys[0]).lower()
    present = [k for k in scoped(b["entries"]) if text(k).lower() == lower]
    gone = [k for k in keys if k not in b["entries"]]
    if not gone:
        _, differing = compare_keys(a, b, keys)
        return (FAIL, "content differs: " + listing(differing), set()) if differing else (PASS, "", set())
    if not present:
        return FAIL, "no survivor for %s" % listing(show(a["entries"], k) for k in keys), set()
    originals = {a["entries"][k]["sha256"] for k in keys}
    bad = [show(b["entries"], k) for k in present if b["entries"][k]["sha256"] not in originals]
    if bad:
        return FAIL, "survivor content equals neither original: " + listing(bad), set()
    named = "survivor %s" % listing(show(b["entries"], k) for k in present)
    reported = all(is_reported(ctx, a["entries"], k) for k in gone)
    if ctx.platform_b == "mac" or reported:
        return DEFINED, "%s; folded away: %s" % (named, listing(show(a["entries"], k) for k in gone)), set(keys) | set(present)
    return FAIL, "case-sensitive B lost %s, not reported; %s" % (listing(show(a["entries"], k) for k in gone), named), set()


def case_result(a, b, ctx):
    groups = case_groups(a["entries"])
    if not groups:
        return (*vacuous("case-colliding names"), set())
    verdicts, notes, explained = [], [], set()
    for keys in groups.values():
        verdict, note, keys_explained = judge_case_group(a, b, ctx, sorted(keys))
        verdicts.append(verdict)
        notes.append(note)
        explained |= keys_explained
    if FAIL in verdicts:
        return FAIL, listing(n for v, n in zip(verdicts, notes) if v == FAIL), set()
    if DEFINED in verdicts:
        return DEFINED, listing(n for v, n in zip(verdicts, notes) if v == DEFINED), explained
    return PASS, "%d case-colliding group(s), every name present with its own content" % len(groups), set()


def r5(a, b, ctx):
    return case_result(a, b, ctx)[:2]


# ---------------------------------------------------------------- rows

def explained_keys(a, b, ctx):
    """Keys whose absence or change R4 or R5 already judged DEFINED; R1 and R8 must not count them twice."""
    return unicode_result(a, b, ctx)[2] | case_result(a, b, ctx)[2]


def r1(a, b, ctx):
    explained = explained_keys(a, b, ctx)
    keys = [k for k in scoped(a["entries"]) if k not in explained]
    missing, differing = compare_keys(a, b, keys)
    ghosts = [show(b["entries"], k) for k in scoped(b["entries"])
              if k not in a["entries"] and k not in explained]
    if missing or differing or ghosts:
        parts = ["missing: " + listing(missing)] if missing else []
        parts += ["differing: " + listing(differing)] if differing else []
        parts += ["ghosts on B: " + listing(ghosts)] if ghosts else []
        return FAIL, " | ".join(parts)
    if explained:
        return DEFINED, "all other entries equal; %d entries are the defined R4/R5 differences" % len(explained)
    return PASS, "%d entries equal in kind, mode, size, sha256" % len(keys)


def r2(a, b, ctx):
    wanted = [k for k in scoped(a["entries"])
              if a["entries"][k]["kind"] == "dir" and a["entries"][k].get("empty")]
    if not wanted:
        return vacuous("empty directories")
    bad = [show(a["entries"], k) for k in wanted
           if not (k in b["entries"] and b["entries"][k]["kind"] == "dir" and b["entries"][k].get("empty"))]
    return (FAIL, "lost or no longer empty: " + listing(bad)) if bad else (PASS, "%d empty directories kept" % len(wanted))


def r3(a, b, ctx):
    wanted = [k for k in scoped(a["entries"]) if len(raw(k)) > DEEP]
    if not wanted:
        return vacuous("path longer than %d characters" % DEEP)
    missing, differing = compare_keys(a, b, wanted)
    if missing or differing:
        return FAIL, listing(["missing " + m for m in missing] + differing)
    return PASS, "%d deep entries kept, longest %d characters" % (len(wanted), max(len(raw(k)) for k in wanted))


def r6(a, b, ctx):
    wanted = [k for k in scoped(a["entries"]) if a["entries"][k]["kind"] == "file" and a["entries"][k]["size"] >= BIG]
    if not wanted:
        return vacuous("file of 50 MiB or more")
    missing, differing = compare_keys(a, b, wanted)
    if missing or differing:
        return FAIL, listing(["missing " + m for m in missing] + differing)
    sizes = ", ".join("%s %d MiB" % (show(a["entries"], k), a["entries"][k]["size"] >> 20) for k in wanted)
    return PASS, "sha256 equal: " + sizes


def broken_symlinks(a, b):
    keys = [k for k in scoped(a["entries"]) if a["entries"][k]["kind"] == "symlink"]
    bad = [show(a["entries"], k) for k in keys
           if k not in b["entries"] or entry_diff(a["entries"][k], b["entries"][k])]
    return keys, ["symlink " + n for n in bad]


def hard_link_groups(entries):
    groups = {}
    for key in scoped(entries):
        if entries[key].get("group_n", 1) > 1:
            groups.setdefault(entries[key]["group"], []).append(key)
    return groups


def broken_hard_links(a, b):
    groups = hard_link_groups(a["entries"])
    bad = []
    for members in groups.values():
        found = [b["entries"].get(k) for k in members]
        ids = {rec.get("group") if rec else None for rec in found}
        if len(ids) != 1 or None in ids or found[0].get("group_n", 1) != len(members):
            names = ", ".join(show(a["entries"], k) for k in members)
            bad.append("hard-linked pair no longer one inode on B (B cannot keep it): " + names)
    return groups, bad


def mode_cases(entries):
    """Entries whose mode is the point: executables, 0000 files and read-only directories."""
    picks = []
    for key in scoped(entries):
        rec, perm = entries[key], int(entries[key]["mode"], 8)
        if rec["kind"] == "file" and (perm & 0o111 or perm & 0o777 == 0):
            picks.append(key)
        elif rec["kind"] == "dir" and not perm & 0o222:
            picks.append(key)
    return picks


def broken_modes(a, b):
    picks = mode_cases(a["entries"])
    bad = ["mode %s: %s" % (show(a["entries"], k), "missing" if k not in b["entries"] else
                            "%s->%s" % (a["entries"][k]["mode"], b["entries"][k]["mode"]))
           for k in picks if k not in b["entries"] or b["entries"][k]["mode"] != a["entries"][k]["mode"]]
    return picks, bad


def r7(a, b, ctx):
    links, bad_links = broken_symlinks(a, b)
    groups, bad_groups = broken_hard_links(a, b)
    picks, bad_modes = broken_modes(a, b)
    if not (links or groups or picks):
        return vacuous("symlinks, hard links or special modes")
    bad = bad_links + bad_groups + bad_modes
    if bad:
        return FAIL, listing(bad)
    return PASS, "%d symlinks, %d hard-link group(s), %d special-mode entries equal" % (len(links), len(groups), len(picks))


def git_file_problems(a, b):
    keys = [k for k in a["entries"] if raw(k).startswith(b".git/") and a["entries"][k]["kind"] != "dir"]
    missing, differing = compare_keys(a, b, keys)
    problems = ["git file missing: " + listing(missing)] if missing else []
    return problems + (["git file differs: " + listing(differing)] if differing else [])


def status_defined_only(a_out, b_out, a, b, ctx):
    """(unexplained differing lines, count of explained ones); R4/R5 differences show up in `git status` too."""
    names = {text(k) for k in explained_keys(a, b, ctx)}
    changed = set(a_out.splitlines()) ^ set(b_out.splitlines())
    rest = [line for line in changed if not any(n in line for n in names)]
    return rest, len(changed) - len(rest)


def normal_branches(out):
    return "\n".join(sorted(line.lstrip("* ").strip() for line in out.splitlines() if line.strip()))


def git_view_problems(a, b, ctx):
    problems, explained = [], 0
    for view in ("stash", "status", "branches", "tags", "submodules"):
        left, right = a["git"][view]["out"], b["git"][view]["out"]
        if view == "branches":   # git sorts names by the locale of the machine; the same set of branches is the same answer
            left, right = [normal_branches(x) for x in (left, right)]
        if left == right:
            continue
        if view == "status":
            rest, explained = status_defined_only(left, right, a, b, ctx)
            if rest:
                problems.append("status differs: " + listing(rest))
        else:
            problems.append("%s differs: A=%r B=%r" % (view, left.strip()[:60], right.strip()[:60]))
    return problems, explained


def r8(a, b, ctx):
    if "git" not in a:
        return (PASS, "no .git in A") if "git" not in b else (FAIL, "B has a .git that A lacks")
    if "git" not in b:
        return FAIL, ".git missing on B"
    problems = git_file_problems(a, b)
    if b["git"]["fsck"]["code"] != 0:
        problems.append("git fsck --full exit %d: %s" % (b["git"]["fsck"]["code"], " / ".join(b["git"]["fsck"]["tail"])))
    more, explained = git_view_problems(a, b, ctx)
    if problems or more:
        return FAIL, listing(problems + more)
    if explained:
        return DEFINED, "git state equal; %d status lines differ only by the defined R4/R5 names" % explained
    skip = " (submodule status empty on both: SKIP)" if not a["git"]["submodules"]["out"].strip() else ""
    return PASS, "fsck exit 0; .git files, stash, status, branches, tags, submodules equal" + skip


def r9(a, b, ctx):
    if not ctx.expect_gone:
        return PASS, "no --expect-gone paths given"
    ghosts = [p for p in ctx.expect_gone if still_exists(b["entries"], p.encode("utf-8", "surrogateescape"))]
    if ghosts:
        return FAIL, "still on B after deletion: " + listing(ghosts)
    return PASS, "%d deleted or renamed paths are gone from B" % len(ctx.expect_gone)


def still_exists(entries, path):
    prefix = path + b"/"
    return any(raw(k) == path or raw(k).startswith(prefix) for k in entries)


IGNORED_SHOULD_TRAVEL = [".env.local", "dist/out.js"]
SECRETS = [".env", "svc/api/.env"]


def ignored_problems(a, b, rels):
    """Missing or changed files among those that must travel."""
    bad = []
    for rel in rels:
        key = base64.b64encode(rel.encode()).decode()
        if key in a["entries"]:
            missing, differing = compare_keys(a, b, [key])
            bad += ["missing " + m for m in missing] + differing
    return bad


def secrets_report(a, b):
    """(problems, notes): a secret that arrives must keep its mode and content; one that does not is reported."""
    problems, notes = [], []
    for rel in SECRETS:
        key = base64.b64encode(rel.encode()).decode()
        if key not in a["entries"]:
            continue
        if key not in b["entries"]:
            notes.append("%s absent on B (withheld)" % rel)
            continue
        missing, differing = compare_keys(a, b, [key])
        problems += differing
        notes.append("%s present, mode %s" % (rel, b["entries"][key]["mode"]))
    return problems, notes


def r10(a, b, ctx):
    bad = ignored_problems(a, b, IGNORED_SHOULD_TRAVEL)
    problems, notes = secrets_report(a, b)
    nodes = [k for k in b["entries"] if b"node_modules" in raw(k).split(b"/")]
    notes.append("node_modules: %s on B (informational)" % ("%d entries" % len(nodes) if nodes else "absent"))
    if bad or problems:
        return FAIL, listing(bad + problems)
    present = [r for r in IGNORED_SHOULD_TRAVEL if base64.b64encode(r.encode()).decode() in b["entries"]]
    note = "present on B: %s; %s" % (", ".join(present) or "none", "; ".join(notes))
    withheld = any("absent on B" in n for n in notes if n.startswith(".env") or n.startswith("svc"))
    return (DEFINED if withheld else PASS), note


ROWS = [
    ("R1", "every path equal in kind, mode, size, sha256; no ghosts", r1),
    ("R2", "empty directories preserved", r2),
    ("R3", "path over 200 characters preserved", r3),
    ("R4", "unicode names byte-identical", r4),
    ("R5", "case-colliding names", r5),
    ("R6", "big files (50 and 300 MiB) sha256 equal", r6),
    ("R7", "symlinks, hard links, executable, 0000, read-only dir", r7),
    ("R8", ".git intact (fsck, files, stash, status, refs, submodules)", r8),
    ("R9", "no ghosts after a mutation (--expect-gone)", r9),
    ("R10", "ignored files, secret modes, node_modules (informational)", r10),
]


# ---------------------------------------------------------------- output

def evaluate(a, b, ctx):
    return [(rid, title) + tuple(fn(a, b, ctx)) for rid, title, fn in ROWS]


def render(results):
    lines = ["| Row | Requirement | Verdict | Evidence |", "|---|---|---|---|"]
    for rid, title, verdict, evidence in results:
        lines.append("| %s | %s | %s | %s |" % (rid, title, verdict, evidence.replace("|", "\\|")))
    return "\n".join(lines)


def split_list(value):
    return [v for v in value.split(",") if v] if value else []


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("a")
    parser.add_argument("b")
    parser.add_argument("--platform-b", default="linux")
    parser.add_argument("--expect-gone", default="")
    parser.add_argument("--reported", default="")
    args = parser.parse_args()
    ctx = SimpleNamespace(platform_b=args.platform_b, expect_gone=split_list(args.expect_gone),
                          reported=split_list(args.reported))
    with open(args.a) as fa, open(args.b) as fb:
        results = evaluate(json.load(fa), json.load(fb), ctx)
    print(render(results))
    sys.exit(1 if any(r[2] == FAIL for r in results) else 0)


if __name__ == "__main__":
    main()
