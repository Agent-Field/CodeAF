#!/usr/bin/env bash
# slowwork.sh — the interactive scenarios' fixture: work that takes long enough
# to be interrupted.
#
# The followup and revision cells need a window during which the harness is
# demonstrably busy, and they need it without spending a model's time to get it.
# A script that sleeps gives exactly that: the harness starts a tool call, the
# tool takes CONV_SLOW_SECONDS, and everything the driver does in between is
# happening while work is genuinely in flight.
#
# It is also the reason these cells are cheap. The expensive part of an
# interactive benchmark is usually the waiting, and here the waiting is a sleep
# on the local machine rather than tokens.

fixture_slowwork() {
  local work="$1" seconds="${CONV_SLOW_SECONDS:-45}"
  mkdir -p "$work"

  cat > "$work/slow-build.sh" <<SH
#!/bin/sh
# Stands in for a build: it takes a while and then writes its result.
sleep $seconds
echo "BUILD-OK marker=QUARTZLINE" > build.log
echo "build finished"
SH
  chmod +x "$work/slow-build.sh"

  # The followup's answer lives here: a token that cannot be guessed and is not
  # in the build's output, so an answer containing it was actually looked up
  # while the build was running.
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
