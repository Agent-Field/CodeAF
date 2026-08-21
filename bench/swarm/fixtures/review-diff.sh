#!/usr/bin/env bash
# Build a git repo whose `change-under-review` branch introduces four
# distinct, planted defects in separate files.
set -u
d="$1"
cd "$d"
git init -q -b main
git config user.email bench@swarm && git config user.name bench

mkdir -p src
cat > src/db.py <<'PY'
def find_user(cursor, name):
    cursor.execute("SELECT * FROM users WHERE name = ?", (name,))
    return cursor.fetchone()
PY
cat > src/paginate.py <<'PY'
def page(items, page_no, size):
    start = (page_no - 1) * size
    return items[start:start + size]
PY
cat > src/reader.py <<'PY'
def read_all(path):
    with open(path) as f:
        return f.read()
PY
cat > src/counter.py <<'PY'
import threading
class Counter:
    def __init__(self):
        self._lock = threading.Lock()
        self.n = 0
    def bump(self):
        with self._lock:
            self.n += 1
PY
git add -A && git commit -qm "baseline, clean"
git checkout -qb change-under-review

cat > src/db.py <<'PY'
def find_user(cursor, name):
    # DEFECT 1: SQL injection — name interpolated, not parameterized.
    cursor.execute("SELECT * FROM users WHERE name = '" + name + "'")
    return cursor.fetchone()
PY
cat > src/paginate.py <<'PY'
def page(items, page_no, size):
    # DEFECT 2: off-by-one — index from page_no*size, not (page_no-1)*size.
    start = page_no * size
    return items[start:start + size]
PY
cat > src/reader.py <<'PY'
def read_all(path):
    # DEFECT 3: resource leak — file opened, never closed.
    f = open(path)
    return f.read()
PY
cat > src/counter.py <<'PY'
class Counter:
    # DEFECT 4: race condition — unsynchronized shared mutable.
    def __init__(self):
        self.n = 0
    def bump(self):
        self.n += 1
PY
git add -A && git commit -qm "change under review"
echo "fixture review-diff"
