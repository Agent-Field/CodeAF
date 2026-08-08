#!/usr/bin/env bash
# Q1 · Does it actually solve a coding task?
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'Q1 Coding quality — a real programming task, graded by running the code the suite is given, not by reading the claim'

# The whole point: aforge saying "done, tests pass" is a claim. This journey
# takes the files it produced, runs them here, and grades the claim against
# what the interpreter says.

since="$(mark)"
started="$(date +%s)"

say "write a python file called stats.py with a function median(numbers) that returns the median of a list of numbers and raises ValueError on an empty list. write pytest tests for it in test_stats.py. run the tests and tell me the result."

assert_journal "select count(*) from nodes where created_seq > $since and origin='user' and status='done'" \
  'the coding job finished' 420
elapsed=$(( $(date +%s) - started ))
sleep 12
snap delivered

record 'wall time' "${elapsed}s"
record 'cost' "\$$(journal "select round(coalesce(sum(cost),0),5) from usage where seq > $since")"
record 'nodes formed' "$(journal "select count(*) from nodes where created_seq > $since")"
record 'node shape' "$(journal "select group_concat(id || '(' || status || ')' || case when parent_id='root' then '' else ' under ' || parent_id end, ' · ') from nodes where created_seq > $since")"

# ------------------------------------------------------------ find the files

work="$(find "$UX_STATE/workspace" -name 'stats.py' -newermt "@$((started - 60))" 2>/dev/null | head -1)"
if [ -z "$work" ]; then
  work="$(find "$UX_STATE/workspace" -name 'stats.py' 2>/dev/null | head -1)"
fi
if [ -n "$work" ]; then
  dir="$(dirname "$work")"
  _check yes 'stats.py exists in the workspace' 'a file the user can pick up' "$work"
else
  dir=""
  _check no 'stats.py exists in the workspace' 'a file the user can pick up' 'not found anywhere under the workspace'
fi
record 'workspace' "${dir:-<none>}"
[ -n "$dir" ] && record 'files produced' "$(ls "$dir" | tr '\n' ' ')"
[ -n "$dir" ] && cp -R "$dir" "$UX_DIR/workspace" 2>/dev/null

if [ -n "$dir" ] && [ -f "$dir/test_stats.py" ]; then
  _check yes 'it wrote the tests it was asked for' 'test_stats.py' 'present'
else
  _check no 'it wrote the tests it was asked for' 'test_stats.py' 'missing'
fi

# ------------------------------------------------- grade by running the code

if [ -n "$dir" ]; then
  grade="$(cd "$dir" && python3 - <<'PY' 2>&1
import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("stats", "stats.py")
verdict = {"import": False, "odd": None, "even": None, "empty": None}
try:
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    verdict["import"] = True
    median = getattr(module, "median")
    verdict["odd"] = (median([3, 1, 2]) == 2)
    verdict["even"] = (abs(median([1, 2, 3, 4]) - 2.5) < 1e-9)
    try:
        median([])
        verdict["empty"] = False
    except ValueError:
        verdict["empty"] = True
    except Exception as exc:
        verdict["empty"] = "wrong exception: %s" % type(exc).__name__
except Exception as exc:
    verdict["error"] = "%s: %s" % (type(exc).__name__, exc)
print(json.dumps(verdict))
PY
)"
  record 'independent grade' "$grade"

  for facet in odd even empty; do
    if printf '%s' "$grade" | python3 -c "import json,sys; d=json.loads(sys.stdin.read()); sys.exit(0 if d.get('$facet') is True else 1)" 2>/dev/null; then
      _check yes "median() is correct: $facet case" "the suite's own call returns the right answer" 'correct'
    else
      _check no "median() is correct: $facet case" "the suite's own call returns the right answer" "$grade"
    fi
  done

  # And the tests it wrote — do they pass when someone else runs them?
  pytest_out="$(cd "$dir" && python3 -m pytest -q 2>&1 | tail -5)"
  record 'pytest, run by the suite' "$(printf '%s' "$pytest_out" | tr '\n' ' ')"
  if printf '%s' "$pytest_out" | grep -Eq '[0-9]+ passed'; then
    _check yes 'the tests it wrote pass when the suite runs them' 'pytest green' "$(printf '%s' "$pytest_out" | grep -Eo '[0-9]+ passed[^ ]*' | head -1)"
  elif printf '%s' "$pytest_out" | grep -Eq 'No module named pytest'; then
    record 'note' 'pytest is not installed here; the direct grade above is the authority'
  else
    _check no 'the tests it wrote pass when the suite runs them' 'pytest green' "$(printf '%s' "$pytest_out" | tr '\n' ' ')"
  fi
fi

# ------------------------------------------------- did it run what it built?

claim="$(journal "select group_concat(lower(body),' ') from messages where seq > $since and role in ('agent','system')")"
record 'what it claimed' "$(printf '%s' "$claim" | head -c 700)"

if printf '%s' "$claim" | grep -Eq '[0-9]+ passed|tests? pass|all pass|passed in|=+ .*passed'; then
  _check yes 'it ran the tests and reported real output, not a promise' \
    'test output in the deliverable' "$(printf '%s' "$claim" | grep -Eo '[0-9]+ passed[a-z ,]*' | head -1)"
else
  _check no 'it ran the tests and reported real output, not a promise' \
    'test output in the deliverable' "$(printf '%s' "$claim" | head -c 200)"
fi

dump_turn "$since"
finish
