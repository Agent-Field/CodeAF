---
kind: added
title: a running turn now says what went up and what came back, in tokens, at the right edge of its work
pr: 666
surface: [chat]
invalidates:
  - "The running turn's compact block carried only step titles, a clock and a shimmer — the surface deliberately drew no figures on it. It now carries two: ↑ what the turn has sent and ↓ what has come back, on the row that stands for the turn, and ↓ alone on a running step's own caption row inside an opened block."
  - "A live token figure was said to be impossible between steps because the books only move when a provider answers. It is drawn: the bytes already on the page are the floor under ↓ until the exact figure lands, through session.EstimateTokens — the package's own divisor, now exported so the surface and the engine cannot disagree about what a token weighs."
  - "captionRows and captionRow take the deck they are drawing (internal/tui3/caption.go). A caller passing only a width no longer compiles."
  - "liveWorkDoor takes the frame width and the deck (internal/tui3/livesteps.go), because the door now carries the turn's pair."
  - "The column was gated to the conversation's page by its lens. It is not: its state lives on the shared feed (feed.col, a tokenCol) and a deck carries a pointer to it (deck.col), so a task room draws the same column off its own lane's EventTurnDone usage. A page with nothing to carry — a run's read-only transcript — leaves deck.col nil."
  - "The figures were painted in the payload ink (pal.data). They are dim, with the arrow at the fade ramp's faintest stop: a moving figure is salient by moving, and the payload hue made a number at the right edge outrank the sentence beside it."
  - "A turn split into several runs by a kept row drew the pair on every run's door, which read as the same turn running twice. Only the frontier run (liveWork.last) carries it."
---

The shimmer answered "is this alive" and nothing answered "is anything moving,
and how fast" — the question a person watching a slow endpoint is actually
asking, and the one a number answers better than a sentence. The pair is
turn-scoped and leaves with the turn: it is a sign of motion, not a total, and
the session's totals stay on the status line where they never go away.
