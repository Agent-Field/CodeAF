# A shared sign of work

The ten chevron-and-dot motions occupy a fixed nine-column field on ONE line.
Native font glyphs keep the chevron recognizable at reading size. The dot bounces,
passes between paddles, compresses at contact or travels around the chevron. No
pixel raster, background rectangles, graphics protocol or patched font is needed.

`tokens.WorkActivity` owns an operation's selection and origin, with no timer,
I/O, application state or palette:

```go
var activity tokens.WorkActivity
activity.Start(now, tokens.WorkLogoRandom) // Or tokens.WorkLogoRally, etc.
frame := activity.Frame(now)              // Read-only, fixed-width glyphs.
```

The surface shares `activityMark` and `activityRows`. Chat owns one instance per
turn; each task/run page owns another. The indicator is pinned in chrome directly
above the input, outside transcript layout and all changing tool captions. Its
label is always `Working`, with no rates, timers or phase words attached.

The chrome reserves one row even when idle. The animation reserves nine terminal
columns whether its pose is wide or narrow, followed by two columns of whitespace.
Including the two-column left inset, following content always starts at column
14 (zero-based `activityLabelColumn` is 13). Future callers must use that fixed
slot rather than measuring visible glyphs. Completion clears the row without
moving the input. Questions replace the active indicator, never imply work is
continuing while user input is required.

Motion is sampled on the existing clock. Twenty-eight deliberately held poses form a
2.8-second loop; holding contact gives the ball weight, while all text to its right
stays still. All frames are nine columns wide. The ball's brand gold is distinct
from the question hue. Completed text never moves. ASCII, monochrome, linear,
copy and small-window views retain the existing compact text treatment.
