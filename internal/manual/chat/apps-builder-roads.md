# How the Apps Builder builds your app

## The Builder takes a second pass when the first write fails

When the Builder writes your app and its first attempt does not pass its own check, it does not show you an error. It says "Taking a second pass…" and tries again, working from the plan you already approved. The second pass is short on purpose: at most 60 steps, 6 minutes and $0.30. Whichever limit it reaches first stops it.

If the second pass also cannot produce a working preview, you see the failed card with the line "The second pass could not finish", and two choices: Fix it or Cancel. You never see the raw error. An app that passes the first time is built exactly as before, with no second pass and no extra cost.

## Why the plan card fills in line by line

On the loop road (see the next section) the Builder works by adding one small piece at a time to a draft: the name, then each data, screen, run and agent line. Each piece appears on the plan card as soon as it is added, on the app's draft stream, `app/<id>/draft`. You watch the plan grow instead of waiting for a finished card.

What you see is a short sentence per piece, such as "Deal · name, company, stage". You do not see the commands behind it, the raw files, or any error text. On the other roads the card appears whole, once, and the draft stream stays quiet.

## Choosing the Builder's road (CODEAF_APPS_BUILDER_ROAD)

The environment variable `CODEAF_APPS_BUILDER_ROAD` picks how the Builder builds:

- `rescue`, the default: build with one write; if that fails, take the second pass described above.
- `calls`: one write and its repair rounds only, with no second pass.
- `loop`: build piece by piece through the draft, so the plan card fills in line by line.

The `rescue` and `loop` roads need the bash task belt. Without it, or with a name that is not one of the three, the Builder logs one line and builds on `calls`.

## App tint

Every app has a tint, a colour for its frame, chosen in the plan. The six names are iris, sage, sand, tide, rose and graphite. The Builder picks one that fits the app (money apps lean sage, people apps rose) and, by default, a different one from the apps you already have, so neighbours are easy to tell apart. Crews use the same six names.
