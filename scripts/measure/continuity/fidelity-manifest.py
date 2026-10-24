#!/usr/bin/env python3
"""Record a byte-level manifest of a folder, without following symlinks.

Usage: fidelity-manifest.py <dir> [--out file.json]
Runs on python 3.9 and later with the standard library only.
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import platform
import stat
import subprocess
import sys

CHUNK = 1 << 20

# Why: each of these git views must match after a move; the name is the key in the manifest.
GIT_VIEWS = {
    "stash": ["stash", "list"],
    "status": ["status", "--porcelain=v1", "--untracked-files=all"],
    "branches": ["branch", "-a"],
    "tags": ["tag"],
    "submodules": ["submodule", "status"],
}


def b64(raw):
    return base64.b64encode(raw).decode("ascii")


def escape(raw):
    """Printable form of raw bytes: ASCII stays, every other byte becomes \\xNN, so NFC and NFD differ."""
    return "".join(chr(c) if 32 <= c < 127 and c != 0x5C else "\\x%02x" % c for c in raw)


def kind_of(mode):
    if stat.S_ISREG(mode):
        return "file"
    if stat.S_ISDIR(mode):
        return "dir"
    if stat.S_ISLNK(mode):
        return "symlink"
    return "other"


def is_rewritten_by_move(rel):
    """Why: git rewrites its index and reflogs when a folder is opened elsewhere, so their bytes are not comparable.

    Submodule gitdirs under .git/modules keep their own index and logs, so they get the same treatment.
    """
    parts = rel.split(b"/")
    if parts[0] != b".git":
        return False
    if len(parts) == 2 and parts[1] == b"index":
        return True
    if len(parts) > 1 and parts[1] == b"logs":
        return True
    return len(parts) > 3 and parts[1] == b"modules" and parts[3] in (b"index", b"logs")


def sha256_stream(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(CHUNK), b""):
            digest.update(chunk)
    return digest.hexdigest()


def sha256_of(path, perm):
    """Return (sha, note). A 0000-mode file gets its read bit for the instant of hashing, then its mode back."""
    try:
        return sha256_stream(path), None
    except PermissionError:
        pass
    except OSError as exc:
        return None, "unreadable: %s" % exc
    try:
        os.chmod(path, perm | 0o400)
    except OSError as exc:
        return None, "unreadable: %s" % exc
    try:
        return sha256_stream(path), "hashed with a temporary read bit"
    except OSError as exc:
        return None, "unreadable: %s" % exc
    finally:
        os.chmod(path, perm)


def describe(path, rel, st):
    kind = kind_of(st.st_mode)
    perm = stat.S_IMODE(st.st_mode)
    rec = {
        "esc": escape(rel),
        "kind": kind,
        "mode": "%04o" % perm,
        "size": st.st_size,
        "nlink": st.st_nlink,
        "sha256": None,
    }
    if kind == "file" and is_rewritten_by_move(rel):
        rec["sha_exempt"] = True
    elif kind == "file":
        rec["sha256"], note = sha256_of(path, perm)
        if note:
            rec["note"] = note
            if rec["sha256"] is None:
                rec["error"] = note
    if kind == "symlink":
        target = os.readlink(path)
        rec["target_b64"], rec["target_esc"] = b64(target), escape(target)
    return rec


def scan_children(path):
    """Child names of a directory, or None when it cannot be listed."""
    try:
        with os.scandir(path) as it:
            return sorted(entry.name for entry in it)
    except OSError:
        return None


def walk(root):
    """Visit every entry under root (bytes path) once; returns {b64(rel): record} and an inode map."""
    entries, inodes, stack = {}, {}, [b""]
    while stack:
        rel = stack.pop()
        here = os.path.join(root, rel) if rel else root
        children = scan_children(here)
        if rel:
            entries[b64(rel)]["empty"] = None if children is None else not children
        for name in children or []:
            child = rel + b"/" + name if rel else name
            path = os.path.join(root, child)
            st = os.lstat(path)
            entries[b64(child)] = describe(path, child, st)
            if not stat.S_ISDIR(st.st_mode):
                inodes.setdefault((st.st_dev, st.st_ino), []).append(child)
            else:
                stack.append(child)
    return entries, inodes


def add_inode_groups(entries, inodes):
    """Why: a hard-linked pair must still be one inode after a move, so each entry names its group by the first path."""
    for members in inodes.values():
        members.sort()
        for member in members:
            entries[b64(member)]["group"] = b64(members[0])
            entries[b64(member)]["group_n"] = len(members)


def run_git(root, args):
    env = dict(os.environ, GIT_OPTIONAL_LOCKS="0")
    cmd = ["git", "-c", "core.quotepath=off", "-C", root] + args
    try:
        done = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env)
    except OSError as exc:
        return 127, str(exc)
    text = (done.stdout + done.stderr).decode("utf-8", "surrogateescape")
    return done.returncode, text


def git_section(root):
    """Why: GIT_OPTIONAL_LOCKS=0 stops `git status` from refreshing the index, so asking does not change the folder."""
    code, text = run_git(root, ["fsck", "--full"])
    section = {"fsck": {"code": code, "tail": text.splitlines()[-5:]}}
    for name, args in GIT_VIEWS.items():
        code, text = run_git(root, args)
        section[name] = {"code": code, "out": text}
    return section


def build_manifest(directory):
    root = os.fsencode(os.path.abspath(directory))
    entries, inodes = walk(root)
    add_inode_groups(entries, inodes)
    manifest = {
        "root": os.fsdecode(root),
        "platform": sys.platform,
        "python": platform.python_version(),
        "entries": entries,
    }
    if os.path.exists(os.path.join(root, b".git")):
        manifest["git"] = git_section(os.fsdecode(root))
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("dir")
    parser.add_argument("--out")
    args = parser.parse_args()
    manifest = build_manifest(args.dir)
    text = json.dumps(manifest, indent=1, sort_keys=True)
    if args.out:
        with open(args.out, "w") as handle:
            handle.write(text)
    else:
        print(text)
    print("manifest: %d entries" % len(manifest["entries"]), file=sys.stderr)


if __name__ == "__main__":
    main()
