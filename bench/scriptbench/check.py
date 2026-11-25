#!/usr/bin/env python3
"""check.py — the true answer to each scriptbench question, and a reply's score.

Usage:
  check.py truth <task> <repo>            print the true answer as JSON
  check.py score <task> <repo> <reply>    score a reply file; prints JSON

Every answer is computed from the source with Python's own parser (ast), never
from a model, so a score is a fact about the reply. The reply's answer is its
LAST fenced ```json block; a reply without one scores 0 with a reason.

The tasks (tasks/<name>.md is the prompt each one is asked with):

  public-defs  For every module in src/click except __init__.py: how many
               top-level functions and classes whose names do not start with
               "_". Score: the share of modules whose two counts are both right.
  raisers      Every function or method in src/click whose own body raises an
               exception class defined in src/click/exceptions.py, as
               "file.py:Qualified.name" (nested defs joined with "."; a raise
               belongs to the innermost def around it). Score: F1 of the sets.
  longest      The ten longest functions or methods in src/click by lines,
               end_lineno - lineno + 1 (the def line to the last line, not its
               decorators). Score: the share of the true ten named with the
               right line count; a tie at the tenth place accepts any of the
               tied defs.
  manual-last  For each page in manual_pages.json's list: its title and its
               last section heading, read from codeaf's manual. Score: the
               share of pages with both exactly right (whitespace collapsed).
  manual-find  Which of manual_pages.json's pages have a section heading
               containing a word that starts with "delet", in any case.
               Score: F1 of the sets.

The manual tasks are checked against MANUAL_TRUTH, a file manualtruth/ writes
from the chat corpus the binary under test ships (run.sh makes it); their
<repo> argument is not read.
"""
import ast, json, os, re, sys

SRC = os.path.join("src", "click")


def modules(repo):
    base = os.path.join(repo, SRC)
    return sorted(n for n in os.listdir(base) if n.endswith(".py"))


def parse(repo, name):
    with open(os.path.join(repo, SRC, name), encoding="utf-8") as fh:
        return ast.parse(fh.read())


def defs(tree):
    """Every function or method with its dotted qualified name."""
    out = []

    def walk(node, prefix):
        for child in ast.iter_child_nodes(node):
            if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
                name = prefix + child.name
                if not isinstance(child, ast.ClassDef):
                    out.append((name, child))
                walk(child, name + ".")
            else:
                walk(child, prefix)

    walk(tree, "")
    return out


def own_nodes(fn):
    """The nodes of fn's body that do not belong to a def nested in it."""
    stack = list(fn.body)
    while stack:
        node = stack.pop()
        yield node
        for child in ast.iter_child_nodes(node):
            if not isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef, ast.Lambda)):
                stack.append(child)


def exception_names(repo):
    tree = parse(repo, "exceptions.py")
    return {n.name for n in tree.body if isinstance(n, ast.ClassDef)}


def raised_name(node):
    exc = node.exc
    if isinstance(exc, ast.Call):
        exc = exc.func
    if isinstance(exc, ast.Name):
        return exc.id
    if isinstance(exc, ast.Attribute):
        return exc.attr
    return None


HERE = os.path.dirname(os.path.abspath(__file__))

# A WORD THAT STARTS WITH "delet", as manual-find asks: "deleting" and
# "Deletion" match, "undelete" does not.
DELET = re.compile(r"\bdelet", re.IGNORECASE)


def manual_truth():
    path = os.environ.get("MANUAL_TRUTH") or os.path.join(HERE, "results", "manual-truth.json")
    with open(path, encoding="utf-8") as fh:
        corpus = json.load(fh)
    with open(os.path.join(HERE, "manual_pages.json"), encoding="utf-8") as fh:
        return corpus, json.load(fh)


def flat(text):
    return " ".join(str(text).split())


def truth(task, repo):
    if task == "manual-last":
        corpus, pages = manual_truth()
        return {p: {"title": corpus[p]["title"], "last": corpus[p]["headings"][-1]}
                for p in pages[task] if p in corpus}
    if task == "manual-find":
        corpus, pages = manual_truth()
        return sorted(p for p in pages[task] if p in corpus
                      and any(DELET.search(h) for h in corpus[p]["headings"]))
    if task == "public-defs":
        answer = {}
        for name in modules(repo):
            if name == "__init__.py":
                continue
            body = parse(repo, name).body
            fns = sum(isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef)) and not n.name.startswith("_") for n in body)
            cls = sum(isinstance(n, ast.ClassDef) and not n.name.startswith("_") for n in body)
            answer[name] = {"functions": fns, "classes": cls}
        return answer
    if task == "raisers":
        names = exception_names(repo)
        found = set()
        for name in modules(repo):
            for qual, fn in defs(parse(repo, name)):
                if any(isinstance(n, ast.Raise) and raised_name(n) in names for n in own_nodes(fn)):
                    found.add(f"{name}:{qual}")
        return sorted(found)
    if task == "longest":
        rows = []
        for name in modules(repo):
            for qual, fn in defs(parse(repo, name)):
                rows.append({"name": f"{name}:{qual}", "lines": fn.end_lineno - fn.lineno + 1})
        rows.sort(key=lambda r: (-r["lines"], r["name"]))
        return rows
    raise SystemExit(f"unknown task {task}")


def answer_of(reply):
    blocks = re.findall(r"```json\s*\n(.*?)```", reply, re.S)
    if not blocks:
        return None, "no json block"
    try:
        return json.loads(blocks[-1]), ""
    except json.JSONDecodeError as err:
        return None, f"json does not parse: {err}"


def score(task, repo, reply):
    got, why = answer_of(reply)
    if got is None:
        return {"score": 0.0, "why": why}
    want = truth(task, repo)
    if task == "public-defs":
        if not isinstance(got, dict):
            return {"score": 0.0, "why": "answer is not an object"}
        right = [m for m, counts in want.items() if isinstance(got.get(m), dict)
                 and got[m].get("functions") == counts["functions"] and got[m].get("classes") == counts["classes"]]
        return {"score": round(len(right) / len(want), 4), "right": len(right), "of": len(want),
                "wrong": sorted(set(want) - set(right))}
    if task == "manual-last":
        if not isinstance(got, dict):
            return {"score": 0.0, "why": "answer is not an object"}
        right = [p for p, w in want.items() if isinstance(got.get(p), dict)
                 and flat(got[p].get("title", "")) == flat(w["title"]) and flat(got[p].get("last", "")) == flat(w["last"])]
        return {"score": round(len(right) / len(want), 4), "right": len(right), "of": len(want),
                "wrong": sorted(set(want) - set(right))}
    if task in ("raisers", "manual-find"):
        if not isinstance(got, list):
            return {"score": 0.0, "why": "answer is not a list"}
        g, w = {str(x).strip() for x in got}, set(want)
        hit = len(g & w)
        p = hit / len(g) if g else 0.0
        r = hit / len(w) if w else 0.0
        f1 = 2 * p * r / (p + r) if p + r else 0.0
        return {"score": round(f1, 4), "precision": round(p, 4), "recall": round(r, 4),
                "missed": sorted(w - g), "extra": sorted(g - w)}
    if task == "longest":
        if not isinstance(got, list):
            return {"score": 0.0, "why": "answer is not a list"}
        tenth = want[9]["lines"]
        allowed = {r["name"]: r["lines"] for r in want if r["lines"] >= tenth}
        named = {}
        for row in got[:10]:
            if isinstance(row, dict):
                named[str(row.get("name", "")).strip()] = row.get("lines")
        hit = sum(1 for n, lines in named.items() if allowed.get(n) == lines)
        return {"score": round(min(hit, 10) / 10, 4), "right": hit,
                "true_top": want[:10]}
    raise SystemExit(f"unknown task {task}")


if __name__ == "__main__":
    mode, task, repo = sys.argv[1:4]
    if mode == "truth":
        print(json.dumps(truth(task, repo), indent=1))
    else:
        with open(sys.argv[4], encoding="utf-8") as fh:
            print(json.dumps(score(task, repo, fh.read())))
