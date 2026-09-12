#!/usr/bin/env bash
# repo-fake — a stand-in for `aforge chat --once` that really does the job.
#
# The before/after scenarios judge the WORKSPACE: what changed in the clone,
# whether it still compiles, whether its own named test is green, and whether
# the thing asked for is in the source. None of that is exercised by a fake
# that only writes a receipt — so this one finds the clone the way a harness
# would (from a launch directory that is not the repository), makes the actual
# edit, and leaves the accounting behind.
#
# It exists to answer one question about the rig: does the judge tell a real
# repair from a plausible sentence about one? FAKE_REPO_MODE chooses which it
# stages:
#
#   fix        make the change, correctly, in the two files it lives in
#   narrate    change nothing and say convincingly that it was done — the
#              failure this whole battery is built to catch
#   halfway    change the source and NOT the test that quotes it, which is the
#              shape a build produces when it stops at the first green compile
set -uo pipefail

MODE="${FAKE_REPO_MODE:-fix}"

# THE MODEL IT WAS TOLD IS THE MODEL IT BILLS. A fake that wrote a hardcoded id
# into the usage file would fail its own cell on the open-model law and look
# exactly like a rig that cannot read a receipt — which is how an hour goes.
MODEL="unset"
while [ $# -gt 0 ]; do
  case "$1" in
    --version) echo "aforge 0.0.0-fake-repo built today"; exit 0 ;;
    --model)   MODEL="${2:-}"; shift ;;
  esac
  shift
done

[ -n "${FAKE_MARKER:-}" ] && date +%s >> "$FAKE_MARKER"

# The launch directory is not the repository, exactly as the scenario arranges.
# A harness has to find the folder; this one looks for the only git checkout
# under where it was started, which is the cheapest honest version of that.
clone="$(find . -maxdepth 2 -name .git -type d 2>/dev/null | head -1)"
clone="${clone%/.git}"

if [ "$MODE" != "narrate" ] && [ -n "$clone" ]; then
  source="$clone/internal/tui3/taskstable.go"
  test_file="$clone/internal/tui3/place_tasks_test.go"
  [ -f "$source" ] && perl -pi -e 's/\QtasksSortKeyChord + " sort"\E/tasksSortKeyChord + " sorts"/' "$source"
  if [ "$MODE" = "fix" ] && [ -f "$test_file" ]; then
    perl -pi -e 's/alt\+s sort · type to filter/alt+s sorts · type to filter/' "$test_file"
  fi
fi

echo "Done — the foot now reads \"alt+s sorts\", and the test that quotes it is updated."

home="${AFORGE_HOME:?repo-fake needs AFORGE_HOME, exactly as the real one does}"
mkdir -p "$home/v3/projects/fake/session"
FAKE_HOME="$home" FAKE_MODEL="$MODEL" python3 -c '
import json, os
home, model = os.environ["FAKE_HOME"], os.environ["FAKE_MODEL"]
with open(home + "/v3/usage.jsonl", "w") as handle:
    handle.write(json.dumps({"at": "2026-01-01T00:00:00Z", "model": model, "calls": 3,
                             "in": 40000, "out": 900, "usd": 0.11, "ttft_ms": 2400}) + "\n")
with open(home + "/v3/projects/fake/session/transcript.jsonl", "w") as handle:
    handle.write(json.dumps({"type": "usage", "usage": {"model": model, "input": 40000,
                                                        "output": 900, "costUsd": 0.11,
                                                        "calls": 3}}) + "\n")
    for _ in range(3):
        handle.write(json.dumps({"type": "call", "call": {"input": 13000, "output": 300}}) + "\n")
    handle.write(json.dumps({"type": "message", "role": "assistant",
                             "toolCalls": [{"id": "1", "function": {"name": "read"}},
                                           {"id": "2", "function": {"name": "write"}}]}) + "\n")
    handle.write(json.dumps({"type": "message", "role": "assistant",
                             "toolCalls": [{"id": "3", "function": {"name": "write"}}]}) + "\n")
'
exit 0
