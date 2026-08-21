#!/usr/bin/env bash
# Build the calcengine package with exactly the three planted bugs the task
# names, and a test suite that catches each in its own file. Deterministic:
# the same package every time, so a verdict delta measures the harness, not
# the fixture.
set -u
d="$1"
mkdir -p "$d/calcengine" "$d/tests"

cat > "$d/calcengine/__init__.py" <<'PY'
PY
cat > "$d/calcengine/parser.py" <<'PY'
def evaluate(tokens):
    # BUG 1: treats '-' and '/' at one precedence level, left to right, so
    # "10-6/2" parses as (10-6)/2 = 2 instead of 10-(6/2) = 7.
    nums = [float(t) for t in tokens[0::2]]
    ops = tokens[1::2]
    acc = nums[0]
    for op, n in zip(ops, nums[1:]):
        if op == "+":
            acc += n
        elif op == "-":
            acc -= n
        elif op == "*":
            acc *= n
        elif op == "/":
            acc /= n
    return acc
PY
cat > "$d/calcengine/cache.py" <<'PY'
_store = {}

def memoized(func, *args, **kwargs):
    # BUG 2: the key is kwargs' repr in definition order, so
    # f(a=1,b=2) and f(b=2,a=1) miss each other and recompute/stale.
    key = (func.__name__, args, tuple(kwargs.items()))
    if key not in _store:
        _store[key] = func(*args, **kwargs)
    return _store[key]
PY
cat > "$d/calcengine/units.py" <<'PY'
def meters_to_feet(m):
    # BUG 3: inverted factor.
    return m / 3.28084

def feet_to_meters(ft):
    return ft / 3.28084
PY

cat > "$d/tests/test_parser.py" <<'PY'
from calcengine.parser import evaluate
def test_subtraction_beats_division():
    assert evaluate(["10", "-", "6", "/", "2"]) == 7
PY
cat > "$d/tests/test_cache.py" <<'PY'
from calcengine.cache import memoized
def test_keyword_order_stability():
    calls = []
    def f(a=0, b=0):
        calls.append(1)
        return a * b
    memoized(f, a=2, b=3)
    memoized(f, b=3, a=2)  # same call, kwargs reordered
    assert len(calls) == 1
PY
cat > "$d/tests/test_units.py" <<'PY'
from calcengine.units import meters_to_feet
def test_length_factor():
    assert abs(meters_to_feet(1.0) - 3.28084) < 1e-4
PY
echo "fixture bugfix-repo"
