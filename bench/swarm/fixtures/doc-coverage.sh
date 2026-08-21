#!/usr/bin/env bash
# Build a toolkit package with undocumented public names for the task to document.
set -u
d="$1"
mkdir -p "$d/toolkit"
cat > "$d/toolkit/__init__.py" <<'PY'
PY
cat > "$d/toolkit/strings.py" <<'PY'
def slugify(text):
    return "-".join(text.lower().split())

def truncate(text, n):
    return text if len(text) <= n else text[:n - 1] + "…"

class WordCounter:
    def __init__(self):
        self.counts = {}
    def add(self, word):
        self.counts[word] = self.counts.get(word, 0) + 1
PY
cat > "$d/toolkit/nums.py" <<'PY'
def mean(xs):
    return sum(xs) / len(xs)

def median(xs):
    s = sorted(xs); m = len(s) // 2
    return s[m] if len(s) % 2 else (s[m - 1] + s[m]) / 2
PY
cat > "$d/toolkit/files.py" <<'PY'
import os

def size_of(path):
    return os.path.getsize(path)

def list_ext(directory, ext):
    return [f for f in os.listdir(directory) if f.endswith(ext)]
PY
echo "fixture doc-coverage"
