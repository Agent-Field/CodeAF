#!/usr/bin/env python3
"""One task copy, described the same way on either machine: every file's sha256, permission
bits and path (the .git and .furrow entries skipped), then the copy's git state.

  treehash.py <dir>
"""
import hashlib
import os
import subprocess
import sys

root = sys.argv[1]
for base, dirs, files in os.walk(root):
    dirs[:] = sorted(d for d in dirs if d not in (".git", ".furrow"))
    for f in sorted(files):
        if f in (".git", ".furrow"):
            continue
        p = os.path.join(base, f)
        if os.path.islink(p):
            continue
        print(hashlib.sha256(open(p, "rb").read()).hexdigest(), "%o" % (os.stat(p).st_mode & 0o7777), os.path.relpath(p, root))


def git(*a):
    r = subprocess.run(["git", "-C", root, *a], capture_output=True, text=True)
    return (r.stdout + r.stderr).rstrip()


print("--- status");  print(git("status", "--porcelain"))
print("--- log");     print(git("log", "--oneline", "-8"))
print("--- branch");  print(git("branch", "--show-current"))
print("--- diff sha"); print(hashlib.sha256(git("diff").encode()).hexdigest())
