---
kind: fixed
title: the background timer is a (home, program) pair, and a launch speaks only for its own
pr: 366
surface: [chat]
invalidates:
  - "every chat launch put the background timer back on the program that was running whenever it named any other program; now a timer naming another program that can still run is left alone and nothing is logged, and a launch repairs only a timer for its own home whose program is gone or whose bytes this build would not have written."
  - "the unit and plist named only the program, so `aforge tick` always ran against the login's default home; now both carry `AFORGE_HOME` and a tick runs against the home that installed it."
  - "a launch under an isolated `AFORGE_HOME` read and rewrote the machine's one timer; now it reads that timer as another home's, and neither claims it nor touches it."
  - "the settings row and `/status` said background checks were off when the timer ran a different build; now they say on, because something is checking this home."
  - "the repair's log line said `which is not this program`; it now says `which is no longer there` for a deleted program, and `was not as this program writes it` otherwise."
---

One timer per login was following whichever aforge launched last. Two builds on one
machine took it from each other on every launch, a build that was then deleted left it
failing every five minutes in silence (a quarter of the ticks over three days on the
machine that reported it), and a throwaway home used for one `--once` run changed the
real machine's timer on its way out. The definition now carries both halves of the pair
and one reading judges it well-formed for that pair and alive; `Drift` and `Status` are
two views of it. A definition written before this change carries no home and reads as the
default home's: the first launch under that home puts it back once, in the new shape.
Turning the background-checks row off and on remains the one deliberate hand that moves
the timer to the build you are running.
