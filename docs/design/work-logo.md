# A shared sign of work

The owner's ten approved chevron-and-dot studies replace the plain working foot
on chat and task/run pages. This is an explicit exception to keeping aliveness
inside a single text row: the 10-column, four-row mark is one transient object
beside truthful state words, aligned with the transcript's existing two-cell
machinery lead. It is not a second transcript entry or a progress percentage.
The gold is on the logo's dot, not the label or background.

`tokens.WorkActivity` owns an operation's selection and origin. It has no timer,
I/O, application state or palette. Every caller can choose randomly or by name:

```go
var activity tokens.WorkActivity
activity.Start(now, tokens.WorkLogoRandom) // Or tokens.WorkLogoRally, etc.
frame := activity.Frame(now)              // Read-only, fixed-size coverage.
```

The surface's `activityRows` is the shared layout/painting door. Chat owns one
instance per turn; a task/run page owns another per opened page. Their state
predicates decide visibility independently. All named studies live in the token
vocabulary, including their raster material, so surfaces do not copy glyphs or
motion code. An additional surface supplies an instance, truthful label and
available width rather than a new spinner implementation.

The running transcript's captions and inline waiting dots stop shimmering when
the logo is visible. Tool-state icons and the status bar retain their existing
compact vocabulary. Details, retries, provider phases and task state words are
preserved. Completed text never bounces or changes character.

The existing frame clock samples the 2.8-second loops at its local or remote
cadence. Both halves of a character cell are sampled nine times. On measured
truecolour grounds partial coverage softens the contours. Unmeasured grounds
and 256-colour terminals use crisp half-block silhouettes, leaving blank cells
transparent. ASCII, monochrome, linear and small-window surfaces keep their
existing compact text treatment. No terminal graphics protocol or patched font
is required.
