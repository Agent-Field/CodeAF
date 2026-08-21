#!/usr/bin/env bash
# Build a project whose legacy_api.fetch_records(query, db) is called from
# eight files, plus a test suite that only holds when every caller migrates
# to fetch_records(db, query, *, limit=...)
set -u
d="$1"
mkdir -p "$d/app" "$d/tests"

cat > "$d/legacy_api.py" <<'PY'
_DB = {"rows": [1, 2, 3, 4, 5]}

def fetch_records(query, db):
    return [r for r in db.get("rows", []) if r in query]
PY

for i in 1 2 3 4 5 6 7 8; do
  cat > "$d/app/use$i.py" <<PY
from legacy_api import fetch_records, _DB

def load$i():
    return fetch_records({1, 2}, _DB)
PY
done

cat > "$d/tests/test_api.py" <<'PY'
import importlib, legacy_api
def test_signature():
    spec = legacy_api.fetch_records.__code__.co_varnames
    assert spec[:2] == ("db", "query"), "db must be first"
    assert "limit" in spec
def test_all_callers_migrated():
    for i in range(1, 9):
        mod = importlib.import_module(f"app.use{i}")
        assert mod.load() == [1, 2], f"app/use{i} not migrated"
PY
echo "fixture refactor-api"
