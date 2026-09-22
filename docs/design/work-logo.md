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
turn; each task/run page owns another. The indicator anchors immediately after
the latest submitted question, including its wrapped text and attachments. Work
details and the answer grow below it. On adaptive graph pages, it sits below the
goal header. It scrolls with that question rather than occupying input chrome.
Its caption is generated once per operation from compatible action/object
families: two or three words such as `Rebasing reality`. The last eight phrases
are excluded from the next choice. These are playful metaphors, not tool claims.

The animation reserves nine terminal columns whether its pose is wide or narrow,
followed by two columns of whitespace. Including its two-column inset, following
content starts at column 14 relative to the content area (`activityLabelColumn`
is 13). Future callers must use that slot rather than measuring visible ink. The caption itself reserves 28 columns
and another two-column gap before any future trailing content.
Completion removes the transient row; it is never stored in the transcript.
Questions requiring input stop the animation. No extra row is reserved above
the input, and the composer geometry is unchanged.

Motion is sampled on the existing clock. Twenty-eight deliberately held poses form a
2.8-second loop; holding contact gives the ball weight, while all text to its right
stays still. All frames are nine columns wide. The ball's brand gold is distinct
from the question hue. Completed text never moves. ASCII, monochrome, linear,
copy and small-window views retain the existing compact text treatment.

The caption uses the existing cosine-feather shimmer at half speed and one quarter
of its ordinary contrast: a four-second light sweep, with no font-weight changes,
text replacement or movement. Truecolor only; lower-color and accessible modes
stay still. Other live captions do not shimmer while the logo is present.
