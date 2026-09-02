---
kind: fixed
title: under --one-model the status line names the flag, and no crew receipt is posted
pr: 503
surface: [chat, docs]
invalidates:
  - "The v3 surface did not know `--one-model` existed. It read the profile's four crew rows through `config.CrewAt` and drew `crew custom` (or `crew balanced`, or `crew max`) for the whole of a run in which those rows seated nothing, because the door had already emptied the roles source and the task model. `internal/tui3.Options` carries `OneModel`, set from the session's own `session.Config.OneModel`, and the surface reports the flag."
  - "`app.crewReading` read the profile and nothing else. It answers the flag FIRST and returns a fixed reading under it — segment `one model`, word `one model · every call rides the model you are talking to` — taken once and never invalidated by the settings generation, because nothing a session can do moves it. The five surfaces that read through it therefore agree by construction: the status line's segment, `/status`'s crew line, the model picker's hint slot, the status note and the welcome box's clause."
  - "The welcome box built its clause at the draw as `reading.preset + \" crew\"`. `crewReading` carries a `clause` field now, because under the flag the preset word is not what stands behind the model and the reading is the one place that knows which."
  - "A profile older than the worker row was told `your crew was set before the work seat existed · it is running on your small work model until you pick a crew again` when its first task started, under `--one-model` as well as without it — and \"until you pick a crew again\" promises a change picking one would not make. `app.workSeat` returns the zero `config.Seat` under the flag the way it already does over a connection, so the thread's receipt and the `/crew` sheet's inherited row are both silent. Without the flag the same profile still draws its crew word and still says the line once; the #311/#314 behaviour is unchanged."
  - "`internal/manual/chat/models-and-cost.md` described what `--one-model` settles and said nothing about what the screen does under it. It has a section of its own for the screen: the crew segment reads `one model`, the rows are overridden rather than gone, and no crew receipt is posted."
---

The two sentences the flag produced were true about the file on disk and false
about the run, which is the shape of defect a surface reading state instead of
posture always makes. The repair is not five careful edits to five places that
print a crew word — it is one reading that knows about the flag, since the
reason `crewReading` exists at all is that the crew is printed in five places
and a second source of the answer is a sixth thing to keep in step.

The receipt is the same lesson in the other direction. It reports a
SUBSTITUTION — a crew written before the worker row existed, so the build's own
model spends the person's money without a word — and under the flag there is no
substitution, because every call is already on the model the person is talking
to. One empty seat takes both surfaces that report it quiet at once.
