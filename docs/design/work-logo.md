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
turn; a task/run page owns another per opened page. Their visibility predicates
read their own operation. The compact work block uses the mark IN its existing
activity line, preserving the disclosure action and status words. It does not add
a second Working row. A surface without a compact work block uses a single-line
transient foot. Harness step tables retain their full width.

Motion is sampled on the existing clock. Twenty-eight deliberately held poses form a
2.8-second loop; holding contact gives the ball weight, while all text to its right
stays still. All frames are nine columns wide. The ball's brand gold is distinct
from the question hue. Completed text never moves. ASCII, monochrome, linear,
copy and small-window views retain the existing compact text treatment.
