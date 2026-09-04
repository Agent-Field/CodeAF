#!/usr/bin/env bash
# slowwork.sh — the interactive scenarios' fixture: work that takes long enough
# to be interrupted, and that says out loud when it started and when it ended.
#
# The phase markers are the point. "The screen looked busy" only proves a model
# request was in flight, which can be true before any work has begun; and a
# transcript that ends with the right answer proves nothing about WHEN it was
# given. So the slow job writes two timestamps into a directory of its own, by
# absolute path, and the driver reads them:
#
#   <phases>/build-started    written the moment the job begins
#   <phases>/build-finished   written when it is done
#
# The path is baked into the script when the fixture is generated, so the
# markers land in the same place even if the agent copies the script elsewhere
# or runs it from another directory. Timestamps are epoch seconds with
# fractions, from one clock on one machine, so the driver can order them
# against its own samples.

fixture_slowwork() {
  local work="$1" seconds="${CONV_SLOW_SECONDS:-45}"
  local phases="$work/.phases"
  mkdir -p "$work" "$phases"
  rm -f "$phases"/build-*

  cat > "$work/slow-build.sh" <<SH
#!/bin/sh
# Stands in for a build: it takes a while and then writes its result. The two
# marker files are what the benchmark reads to know when the work actually ran.
python3 -c 'import time; print(time.time())' > "$phases/build-started"
sleep $seconds
echo "BUILD-OK marker=QUARTZLINE" > "$work/build.log"
python3 -c 'import time; print(time.time())' > "$phases/build-finished"
echo "build finished"
SH
  chmod +x "$work/slow-build.sh"

  # The followup's answer. The question asks for this word REVERSED, so an
  # answer containing the reversal cannot have come from a tool echoing the
  # file: it had to be read and then transformed by whatever was answering.
  cat > "$work/NOTES.txt" <<'TXT'
Release checklist for the pricing service.

The checksum word for this release is CINNABAR.
The rollback window is 30 minutes.
TXT

  cat > "$work/services.txt" <<'TXT'
kestrel 8431
gasket 9002
flange 7710
TXT

  cat > "$work/inventory.txt" <<'TXT'
widget 12
gasket 40
flange 3
widget 22
gasket 15
TXT
}

# fixture_slowwork_phase prints the epoch time in one marker, or nothing.
fixture_slowwork_phase() {
  local work="$1" name="$2"
  [ -s "$work/.phases/$name" ] || return 1
  tr -d '[:space:]' < "$work/.phases/$name"
}
