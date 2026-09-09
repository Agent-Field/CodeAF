---
kind: changed
title: the first run asks one screen about models and spending instead of five questions
pr: 680
surface: [chat]
invalidates:
  - >-
    The first-run setup after the key was a crew chooser and then a rails screen with three
    money rows on it — five answers. It is now ONE screen, `Models and spending`, with three
    controls: the daily limit, the chat model and the work crew. `setupCrew` and the rails step
    no longer exist; the flow is `setupKey` then `setupControls`, and the header counts `setup
    · 2 of 2` rather than `1 of 3`.
  - >-
    The setup used to write a crew preset unconditionally on the way out, so a profile with
    hand-pinned tier rows and no daily limit had all five replaced by Frugal. It now writes a
    crew only where the person chose one on that screen or the profile had none at all, and a
    crew that is nobody's preset reads `Custom` on the row.
  - >-
    The setup's model row offered the first five catalog entries and nothing else, and the
    cursor fell to zero when the model in use was not among them — so enter changed the model
    rather than confirming it. It is now a scrolling, type-to-narrow viewport over the whole
    catalog with the model in use always on it and always the row the cursor opens on.
  - >-
    The setup wrote the chat model by calling `switchModel` directly. It now goes through the
    same settings row `/model` writes, so `AFORGE_MODEL` precedence and the profile's own
    choice cannot disagree between the setup and `/settings`.
  - >-
    v3 drew no borders anywhere. There is now exactly one: the example panel beside the setup
    form, at 112 columns and wider, labelled `◌ Example · what you can do`. No control is boxed
    and nothing else on the surface has an edge.
  - >-
    The onboarding crew chooser used `config.CrewLine`, whose Frugal row ends `· pennies a
    day`. The setup screen no longer makes any claim about what anything costs; its three
    preset lines describe the choice, and the actual models are behind `?`.
  - >-
    The first conversation on a machine used to open on the three-row wordmark and a line
    reading the full model id and the crew. It now leads with `What would you like to work
    on?`, a one-word signature above it, and three starting points that fill the box and send
    nothing. Every LATER greeting is unchanged and still draws both.
---

The screen is one screen because a person who has never run aforge cannot have an opinion
about four of the five things the old flow asked. Every row on it opens on the value that
is already in force, so `Start a conversation` agrees to exactly what is on the frame, and
the screen writes only what it was given: leaving it never overwrites a crew somebody
arranged by hand, and esc out of a chooser chooses nothing.

`docs/design/onboarding/DESIGN.md` carries the selection rule, the geometry at each width,
the motion contract of the example panel, and the discoverability ledger — where every
control on the screen is reachable from afterwards.
